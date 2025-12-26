package client

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	thorAPI "github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/block"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"

	"validator-next-slot/internal/model"
	"validator-next-slot/internal/utils"
)

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
		// Create Staker instance
		staker, err := builtin.NewStaker(c.client)
		if err != nil {
			return fmt.Errorf("failed to create staker: %w", err)
		}

		// Use the specified revision
		stakerAtRevision := staker.Revision(revision)

		// Get first active validator
		_, firstAddr, err := stakerAtRevision.FirstActive()
		if err != nil {
			return fmt.Errorf("failed to get first active validator: %w", err)
		}

		validators = make([]model.ValidatorInfo, 0, 101)
		currentAddr := firstAddr

		// Staker contract address (20 bytes)
		stakerAddr := thor.MustParseAddress("0x00000000000000000000000000005374616b6572")

		// Parse Staker ABI for encoding
		stakerABI := `[
			{
				"inputs": [{"name": "addr","type": "address"}],
				"name": "getValidation",
				"outputs": [
					{"name": "endorser","type": "address"},
					{"name": "stake","type": "uint256"},
					{"name": "weight","type": "uint256"},
					{"name": "queuedStake","type": "uint256"},
					{"name": "status","type": "uint8"},
					{"name": "offlineBlock","type": "uint32"}
				],
				"type": "function"
			},
			{
				"inputs": [{"name": "addr","type": "address"}],
				"name": "next",
				"outputs": [
					{"name": "next","type": "address"}
				],
				"type": "function"
			}
		]`

		parsedABI, err := abi.JSON(strings.NewReader(stakerABI))
		if err != nil {
			return fmt.Errorf("failed to parse staker ABI: %w", err)
		}

		// Iterate through linked list using batch calls
		for currentAddr != (thor.Address{}) {
			// Encode GetValidation call
			getValidationData, err := parsedABI.Pack("getValidation", common.Address(currentAddr))
			if err != nil {
				return fmt.Errorf("failed to encode getValidation: %w", err)
			}

			// Encode Next call
			nextData, err := parsedABI.Pack("next", common.Address(currentAddr))
			if err != nil {
				return fmt.Errorf("failed to encode next: %w", err)
			}

			// Build batch call
			batchData := &thorAPI.BatchCallData{
				Clauses: thorAPI.Clauses{
					&thorAPI.Clause{
						To:    &stakerAddr,
						Value: nil,
						Data:  "0x" + hex.EncodeToString(getValidationData),
					},
					&thorAPI.Clause{
						To:    &stakerAddr,
						Value: nil,
						Data:  "0x" + hex.EncodeToString(nextData),
					},
				},
			}

			results, err := c.client.InspectClauses(batchData, thorclient.Revision(revision))
			if err != nil {
				return fmt.Errorf("failed batch call for %s: %w", currentAddr, err)
			}

			if len(results) != 2 {
				return fmt.Errorf("unexpected batch results count: %d", len(results))
			}

			// Check if calls reverted
			if results[0].Reverted {
				return fmt.Errorf("getValidation call reverted for %s", currentAddr)
			}
			if results[1].Reverted {
				return fmt.Errorf("next call reverted for %s", currentAddr)
			}

			// Decode GetValidation result
			validationData, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
			if err != nil {
				return fmt.Errorf("failed to decode validation hex: %w", err)
			}

			// Unpack using UnpackValues
			values, err := parsedABI.Methods["getValidation"].Outputs.UnpackValues(validationData)
			if err != nil {
				return fmt.Errorf("failed to unpack validation: %w", err)
			}

			if len(values) != 6 {
				return fmt.Errorf("unexpected unpack result length: %d", len(values))
			}

			// Extract values: endorser, stake, weight, queuedStake, offlineBlock, status
			weight := values[2].(*big.Int)
			offlineBlock := values[5].(uint32)

			// Only include validators with weight > 0
			if weight.Sign() > 0 {
				validators = append(validators, model.ValidatorInfo{
					Address: currentAddr,
					Weight:  weight,
					Active:  offlineBlock == math.MaxUint32,
				})
			}

			// Decode Next result
			nextResultData, err := hex.DecodeString(strings.TrimPrefix(results[1].Data, "0x"))
			if err != nil {
				return fmt.Errorf("failed to decode next hex: %w", err)
			}

			nextValues, err := parsedABI.Methods["next"].Outputs.UnpackValues(nextResultData)
			if err != nil {
				return fmt.Errorf("failed to unpack next: %w", err)
			}

			if len(nextValues) != 1 {
				return fmt.Errorf("unexpected next result length: %d", len(nextValues))
			}

			nextAddr := nextValues[0].(common.Address)
			currentAddr = thor.Address(nextAddr)
			if currentAddr == (thor.Address{}) {
				break
			}
		}

		return nil
	})

	return validators, err
}

// GetValidation fetches validation info for a specific validator
func (c *VeChainClient) GetValidation(ctx context.Context, validatorAddr string, revision string) (*builtin.Validation, error) {
	var validation *builtin.Validation

	err := utils.RetryWithBackoff(ctx, c.retryConfig, func() error {
		// Create Staker instance
		staker, err := builtin.NewStaker(c.client)
		if err != nil {
			return fmt.Errorf("failed to create staker: %w", err)
		}

		// Use the specified revision
		stakerAtRevision := staker.Revision(revision)

		// Parse validator address
		addr := thor.MustParseAddress(validatorAddr)

		// Get validation info
		validation, err = stakerAtRevision.GetValidation(addr)
		if err != nil {
			return fmt.Errorf("failed to get validation: %w", err)
		}

		return nil
	})

	return validation, err
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
		// Create Staker instance
		staker, err := builtin.NewStaker(c.client)
		if err != nil {
			return fmt.Errorf("failed to create staker: %w", err)
		}

		// Use the specified revision
		stakerAtRevision := staker.Revision(revision)

		// Call totalStake()
		totalVET, totalWeight, err := stakerAtRevision.TotalStake()
		if err != nil {
			return fmt.Errorf("failed to get total stake: %w", err)
		}

		result = &TotalStake{
			TotalVET:    totalVET,
			TotalWeight: totalWeight,
		}

		return nil
	})

	return result, err
}
