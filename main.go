package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"validator-predictor/internal/client"
	"validator-predictor/internal/scheduler"

	"github.com/vechain/thor/v2/thor"
)

// jsonSlot is a single proposer slot in -json output.
type jsonSlot struct {
	Number uint32 `json:"number"`
	Time   uint64 `json:"time"`
	In     int64  `json:"in"`
}

// jsonSlotsOutput is the -json output shape for address mode.
type jsonSlotsOutput struct {
	Validator string    `json:"validator"`
	First     *jsonSlot `json:"first"`
	Second    *jsonSlot `json:"second"`
}

func toJSONSlot(s *scheduler.BlockSlot) *jsonSlot {
	if s == nil {
		return nil
	}
	return &jsonSlot{
		Number: s.Number,
		Time:   s.Time,
		In:     int64(time.Until(time.Unix(int64(s.Time), 0)).Round(time.Second).Seconds()),
	}
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

func main() {
	nodeURL := flag.String("node", "https://mainnet.vechain.org", "VeChain node REST API URL")
	predict := flag.Bool("predict", false, "Predict next N blocks (use -n to specify count, default 20)")
	numBlocks := flag.Int("n", 20, "Number of blocks to predict (used with -predict)")
	jsonOut := flag.Bool("json", false, "Output JSON (address mode only)")
	lookahead := flag.Int("lookahead", 2160, "Number of blocks to search for slots in address mode (default 2160, about 6 hours)")
	flag.Parse()

	if *lookahead <= 0 {
		log.Fatal("-lookahead must be greater than 0")
	}

	if *jsonOut && (*predict || flag.NArg() < 1) {
		log.Fatal("-json requires a validator address")
	}

	c := client.NewVeChainClient(*nodeURL)
	ctx := context.Background()

	// 1. Get best block
	bestBlockID, bestBlockNum, err := c.GetBestBlock(ctx)
	if err != nil {
		log.Fatalf("Failed to get best block: %v", err)
	}
	revision := bestBlockID.String()

	// 2. Get all validators for scheduler
	validators, err := c.GetActiveValidators(ctx, revision)
	if err != nil {
		log.Fatalf("Failed to get validators: %v", err)
	}

	if len(validators) == 0 {
		log.Fatal("No active validators found")
	}

	// Convert to Proposer list
	proposers := make([]scheduler.Proposer, len(validators))
	for i, v := range validators {
		proposers[i] = scheduler.Proposer{
			Address: v.Address,
			Active:  v.Active,
			Weight:  v.Weight,
		}
	}

	// 3. Get best block details
	bestBlock, err := c.ThorClient().Block(revision)
	if err != nil {
		log.Fatalf("Failed to get best block details: %v", err)
	}

	// 4. Create seed provider function
	getSeed := func(seedBlockNum uint32) ([]byte, error) {
		return c.GetBlockBeta(ctx, fmt.Sprintf("%d", seedBlockNum))
	}

	// Mode 1: Prediction mode - show next N blocks
	if *predict {
		fmt.Printf("%s%sPredicting next %d blocks:%s\n\n", colorBold, colorCyan, *numBlocks, colorReset)

		var seed []byte
		for i := 0; i < *numBlocks; i++ {
			blockNum := bestBlockNum + uint32(i) + 1

			// Get seed if needed
			if seed == nil || blockNum%thor.SeederInterval() == 0 {
				seedBlockNum := scheduler.GetSeedBlockNumber(blockNum - 1)
				var err error
				seed, err = getSeed(seedBlockNum)
				if err != nil {
					log.Fatalf("Failed to get seed: %v", err)
				}
			}

			// Compute sequence for this block
			sequence := scheduler.ComputeSequence(proposers, blockNum-1, seed)

			// Find first active proposer
			var firstProposer *scheduler.Entry
			for j := range sequence {
				if sequence[j].Active {
					firstProposer = &sequence[j]
					break
				}
			}

			if firstProposer != nil {
				blockTime := time.Unix(int64(bestBlock.Timestamp+uint64(i+1)*10), 0)
				fmt.Printf("Block #%s%d%s: %s%s%s at %s (in %s%v%s)\n",
					colorCyan, blockNum, colorReset,
					colorGreen, firstProposer.Address, colorReset,
					blockTime.Format(time.RFC3339),
					colorYellow, time.Until(blockTime).Round(time.Second), colorReset)
			}
		}
		return
	}

	// Mode 2: Default mode - show next block proposers
	if flag.NArg() < 1 {
		// No address provided - show next first and second active proposers
		// Get seed for next block
		seedBlockNum := scheduler.GetSeedBlockNumber(bestBlockNum)
		seed, err := getSeed(seedBlockNum)
		if err != nil {
			log.Fatalf("Failed to get seed: %v", err)
		}

		// Compute sequence for next block
		nextBlockNum := bestBlockNum + 1
		sequence := scheduler.ComputeSequence(proposers, nextBlockNum-1, seed)

		// Find first and second active proposers
		var firstProposer, secondProposer *scheduler.Entry
		for i := range sequence {
			if sequence[i].Active {
				if firstProposer == nil {
					firstProposer = &sequence[i]
				} else if secondProposer == nil {
					secondProposer = &sequence[i]
					break
				}
			}
		}

		// Display results
		fmt.Printf("%s%sNext Block: #%d%s\n", colorBold, colorCyan, nextBlockNum, colorReset)
		if firstProposer != nil {
			firstTime := time.Unix(int64(bestBlock.Timestamp+10), 0)
			fmt.Printf("%sFirst Proposer:%s  %s%s%s\n", colorBold, colorReset, colorGreen, firstProposer.Address, colorReset)
			fmt.Printf("  Expected time: %s (in %s%v%s)\n",
				firstTime.Format(time.RFC3339),
				colorYellow,
				time.Until(firstTime).Round(time.Second),
				colorReset)
		}
		if secondProposer != nil {
			secondTime := time.Unix(int64(bestBlock.Timestamp+20), 0)
			fmt.Printf("\n%sSecond Proposer:%s %s%s%s\n", colorBold, colorReset, colorBlue, secondProposer.Address, colorReset)
			fmt.Printf("  Backup time:   %s (in %s%v%s)\n",
				secondTime.Format(time.RFC3339),
				colorYellow,
				time.Until(secondTime).Round(time.Second),
				colorReset)
		}
		return
	}

	// Address provided - find next slots for target validator
	addrStr := flag.Arg(0)
	targetAddr, err := thor.ParseAddress(addrStr)
	if err != nil {
		log.Fatalf("Invalid address: %v", err)
	}

	// Check if validator exists in proposers list
	found := false
	for _, p := range proposers {
		if p.Address == targetAddr {
			found = true
			if !p.Active {
				log.Fatalf("Error: %s is not active", targetAddr)
			}
			break
		}
	}
	if !found {
		log.Fatalf("Error: %s is not a validator", targetAddr)
	}

	// Find next two slots for target validator
	first, second, err := scheduler.FindNextSlots(
		proposers,
		bestBlockNum,
		bestBlock.Timestamp,
		targetAddr,
		*lookahead,
		getSeed,
	)
	if err != nil {
		log.Fatalf("Failed to find next slots: %v", err)
	}

	if *jsonOut {
		out := jsonSlotsOutput{
			Validator: targetAddr.String(),
			First:     toJSONSlot(first),
			Second:    toJSONSlot(second),
		}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			log.Fatalf("Failed to encode JSON: %v", err)
		}
		return
	}

	// Display results
	fmt.Printf("%sTarget Validator:%s %s%s%s\n", colorBold, colorReset, colorCyan, targetAddr, colorReset)
	if first != nil {
		firstTime := time.Unix(int64(first.Time), 0)
		fmt.Printf("%sAs First Proposer:%s  Block #%s%d%s at %s (in %s%v%s)\n",
			colorBold,
			colorReset,
			colorGreen,
			first.Number,
			colorReset,
			firstTime.Format(time.RFC3339),
			colorYellow,
			time.Until(firstTime).Round(time.Second),
			colorReset)
	} else {
		fmt.Printf("As First Proposer:  Not found in next %d blocks\n", *lookahead)
	}

	if second != nil {
		secondTime := time.Unix(int64(second.Time), 0)
		fmt.Printf("%sAs Second Proposer:%s Block #%s%d%s at %s (in %s%v%s)\n",
			colorBold,
			colorReset,
			colorBlue,
			second.Number,
			colorReset,
			secondTime.Format(time.RFC3339),
			colorYellow,
			time.Until(secondTime).Round(time.Second),
			colorReset)
	} else if first != nil {
		fmt.Printf("As Second Proposer: Not found in next %d blocks\n", *lookahead)
	}
}
