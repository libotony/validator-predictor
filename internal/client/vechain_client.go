package client

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/v2/abi"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/block"
	"github.com/vechain/thor/v2/builtin"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"

	"validator-predictor/internal/contracts"
	"validator-predictor/internal/model"
	"validator-predictor/internal/utils"
)

var (
	GHOST_CONTRACT_ADDRESS thor.Address
	activeValidators       *abi.Method
	stakerAbi              *abi.ABI
)

func init() {
	GHOST_CONTRACT_ADDRESS = thor.MustParseAddress("0x841a6556c524d47030762eb14dc4af897e605d9b")

	utilABI, err := abi.New([]byte(contracts.StakerUtilABI))
	if err != nil {
		panic(err)
	}

	var found bool
	activeValidators, found = utilABI.MethodByName("activeValidators")
	if !found {
		panic("activeValidators method not found")
	}

	stakerAbi, err = abi.New(builtin.Staker.RawABI())
	if err != nil {
		panic(err)
	}
}

type VeChainClient struct {
	client      *thorclient.Client
	httpClient  *http.Client
	baseURL     string
	retryConfig utils.RetryConfig
}

func NewVeChainClient(url string) *VeChainClient {
	baseURL := url
	if baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &VeChainClient{
		client: thorclient.New(url),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:     baseURL,
		retryConfig: utils.DefaultRetryConfig(),
	}
}

func (c *VeChainClient) ThorClient() *thorclient.Client {
	return c.client
}

func (c *VeChainClient) GetBestBlock(ctx context.Context) (thor.Bytes32, uint32, error) {
	var blockID thor.Bytes32
	var blockNum uint32

	err := utils.RetryWithBackoff(ctx, c.retryConfig, func() error {
		blk, err := c.client.Block("best")
		if err != nil {
			return err
		}
		blockID = blk.ID
		blockNum = blk.Number
		return nil
	})

	return blockID, blockNum, err
}

func (c *VeChainClient) GetBlockBeta(ctx context.Context, revision string) ([]byte, error) {
	var beta []byte

	err := utils.RetryWithBackoff(ctx, c.retryConfig, func() error {
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/blocks/%s?raw=true", c.baseURL, revision), nil)
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to fetch raw block: status %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		// Parse JSON response: {"raw": "0x..."}
		var rawResp struct {
			Raw string `json:"raw"`
		}
		if err := json.Unmarshal(body, &rawResp); err != nil {
			return fmt.Errorf("failed to parse JSON response: %w", err)
		}

		// Remove "0x" prefix and decode hex
		hexData := strings.TrimPrefix(rawResp.Raw, "0x")
		rlpData, err := hex.DecodeString(hexData)
		if err != nil {
			return fmt.Errorf("failed to decode hex: %w", err)
		}

		var h block.Header
		if err := rlp.DecodeBytes(rlpData, &h); err != nil {
			return fmt.Errorf("failed to RLP decode block header: %w", err)
		}

		beta, err = h.Beta()
		if err != nil {
			return fmt.Errorf("failed to get beta from header: %w", err)
		}

		return nil
	})

	return beta, err
}

// GetActiveValidators fetches active validators by iterating through the Staker contract's linked list
// Uses multi-clause batch calls to optimize performance (GetValidation + Next in one request)
func (c *VeChainClient) GetActiveValidators(ctx context.Context, revision string) ([]model.ValidatorInfo, error) {
	var validators []model.ValidatorInfo

	err := utils.RetryWithBackoff(ctx, c.retryConfig, func() error {
		input, err := activeValidators.EncodeInput()
		if err != nil {
			return fmt.Errorf("failed to encode activeValidators input: %w", err)
		}

		results, err := c.client.InspectClauses(&api.BatchCallData{
			Clauses: api.Clauses{
				&api.Clause{
					Value: nil,
					Data:  "0x" + contracts.StakerUtilBytecode,
				},
				&api.Clause{
					To:    &GHOST_CONTRACT_ADDRESS,
					Data:  "0x" + hex.EncodeToString(input),
					Value: nil,
				},
			},
		}, thorclient.Revision(revision))
		if err != nil {
			return fmt.Errorf("failed to inspect clauses: %w", err)
		}

		for index, result := range results {
			if result.Reverted {
				return fmt.Errorf("reverted in clause %d", index)
			}
		}

		var (
			addresses    []common.Address
			weights      []*big.Int
			onlineStatus []bool
		)
		dec, err := hex.DecodeString(strings.TrimPrefix(results[1].Data, "0x"))
		if err != nil {
			return fmt.Errorf("failed to decode hex: %w", err)
		}

		err = activeValidators.DecodeOutput(dec, &[]any{&addresses, &weights, &onlineStatus})
		if err != nil {
			return fmt.Errorf("failed to decode output: %w", err)
		}

		for i := range len(addresses) {
			validators = append(validators, model.ValidatorInfo{
				Address: thor.Address(addresses[i]),
				Weight:  weights[i],
				Active:  onlineStatus[i],
			})
		}

		return nil
	})

	return validators, err
}

// TotalStake represents the result of totalStake() call
type TotalStake struct {
	TotalVET    *big.Int
	TotalWeight *big.Int
}

// GetTotalStake fetches total VET and total weight from Staker contract
func (c *VeChainClient) GetTotalStake(ctx context.Context, revision string) (*TotalStake, error) {
	var result *TotalStake

	err := utils.RetryWithBackoff(ctx, c.retryConfig, func() error {
		method, found := stakerAbi.MethodByName("totalStake")
		if !found {
			return fmt.Errorf("totalStake method not found")
		}

		input, err := method.EncodeInput()
		if err != nil {
			return fmt.Errorf("failed to encode totalStake input: %w", err)
		}

		results, err := c.client.InspectClauses(&api.BatchCallData{
			Clauses: api.Clauses{
				&api.Clause{
					To:    &builtin.Staker.Address,
					Data:  "0x" + hex.EncodeToString(input),
					Value: nil,
				},
			},
		}, thorclient.Revision(revision))
		if err != nil {
			return fmt.Errorf("failed to inspect clauses: %w", err)
		}

		if len(results) != 1 || results[0].Reverted {
			return fmt.Errorf("failed to get total stake")
		}

		dec, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		if err != nil {
			return fmt.Errorf("failed to decode hex: %w", err)
		}

		var totalVET, totalWeight *big.Int
		err = method.DecodeOutput(dec, &[]any{&totalVET, &totalWeight})
		if err != nil {
			return fmt.Errorf("failed to decode output: %w", err)
		}

		result = &TotalStake{
			TotalVET:    totalVET,
			TotalWeight: totalWeight,
		}

		return nil
	})

	return result, err
}
