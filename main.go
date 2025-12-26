package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"validator-next-slot/internal/client"
	"validator-next-slot/internal/scheduler"

	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

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
	flag.Parse()

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

	// Check if address argument is provided
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

	// Validate target validator
	validation, err := c.GetValidation(ctx, targetAddr.String(), revision)
	if err != nil {
		log.Fatalf("Failed to get validation for %s: %v", targetAddr, err)
	}

	// Check if validator is registered
	if validation.Status != builtin.StakerStatusActive {
		log.Fatalf("Error: %s is not active", targetAddr)
	}

	// Find next two slots for target validator
	first, second, err := scheduler.FindNextSlots(
		proposers,
		bestBlockNum,
		bestBlock.Timestamp,
		targetAddr,
		getSeed,
	)
	if err != nil {
		log.Fatalf("Failed to find next slots: %v", err)
	}

	// Display results
	fmt.Printf("%sTarget Validator:%s %s%s%s\n", colorBold, colorReset, colorCyan, targetAddr, colorReset)
	if first != nil {
		firstTime := time.Unix(int64(first.Time), 0)
		fmt.Printf("%sFirst Slot:%s  Block #%s%d%s at %s (in %s%v%s)\n",
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
		fmt.Println("First Slot:  Not found in next 400 blocks")
	}

	if second != nil {
		secondTime := time.Unix(int64(second.Time), 0)
		fmt.Printf("%sSecond Slot:%s Block #%s%d%s at %s (in %s%v%s)\n",
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
		fmt.Println("Second Slot: Not found in next 400 blocks")
	}
}
