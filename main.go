package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"validator-next-slot/internal/client"
	"validator-next-slot/internal/scheduler"

	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func main() {
	nodeURL := flag.String("node", "https://mainnet.vechain.org", "VeChain node REST API URL")
	flag.Parse()

	// Address is a positional argument
	if flag.NArg() < 1 {
		fmt.Println("Usage: validator-next-slot <validator_address> [--node <url>]")
		fmt.Println("\nExample:")
		fmt.Println("  validator-next-slot 0x3970329d7964b9eaa7244c936d18b0846522cd96")
		fmt.Println("  validator-next-slot 0x3970329d7964b9eaa7244c936d18b0846522cd96 --node https://mainnet.vechain.org")
		os.Exit(1)
	}

	addrStr := flag.Arg(0)
	targetAddr, err := thor.ParseAddress(addrStr)
	if err != nil {
		log.Fatalf("Invalid address: %v", err)
	}

	c := client.NewVeChainClient(*nodeURL)
	ctx := context.Background()

	// 1. Get best block
	bestBlockID, bestBlockNum, err := c.GetBestBlock(ctx)
	if err != nil {
		log.Fatalf("Failed to get best block: %v", err)
	}
	revision := bestBlockID.String()

	fmt.Printf("Best Block: %d (%s)\n", bestBlockNum, revision)

	// 2. Validate target validator
	fmt.Printf("Checking validator: %s\n", targetAddr)
	validation, err := c.GetValidation(ctx, targetAddr.String(), revision)
	if err != nil {
		log.Fatalf("Failed to get validation for %s: %v", targetAddr, err)
	}

	// Check if validator is registered
	if validation.Status != builtin.StakerStatusActive {
		log.Fatalf("Error: %s is not active", targetAddr)
	}

	fmt.Printf("✓ Target validator is registered\n")
	fmt.Printf("  Weight: %s\n", validation.Weight.String())
	fmt.Printf("  Stake: %s VET\n", validation.Stake.String())

	// 3. Get all validators for scheduler
	fmt.Println("\nFetching all active validators...")
	validators, err := c.GetActiveValidators(ctx, revision)
	if err != nil {
		log.Fatalf("Failed to get validators: %v", err)
	}

	if len(validators) == 0 {
		log.Fatal("No active validators found")
	}

	fmt.Printf("Found %d active validators\n\n", len(validators))

	// Convert to Proposer list
	proposers := make([]scheduler.Proposer, len(validators))
	for i, v := range validators {
		proposers[i] = scheduler.Proposer{
			Address: v.Address,
			Active:  v.Active,
			Weight:  v.Weight,
		}
	}

	// 4. Get best block details
	bestBlock, err := c.ThorClient().Block(revision)
	if err != nil {
		log.Fatalf("Failed to get best block details: %v", err)
	}

	// 5. Create seed provider function
	getSeed := func(seedBlockNum uint32) ([]byte, error) {
		return c.GetBlockBeta(ctx, fmt.Sprintf("%d", seedBlockNum))
	}

	// 6. Find next two slots for target validator
	fmt.Printf("Searching for next slots for %s...\n\n", targetAddr)
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

	// 7. Display results
	fmt.Printf("Target Validator: %s\n", targetAddr)
	if first != nil {
		firstTime := time.Unix(int64(first.Time), 0)
		fmt.Printf("First Slot:  Block #%d at %s (in %v)\n",
			first.Number,
			firstTime.Format(time.RFC3339),
			time.Until(firstTime).Round(time.Second))
	} else {
		fmt.Println("First Slot:  Not found in next 400 blocks")
	}

	if second != nil {
		secondTime := time.Unix(int64(second.Time), 0)
		fmt.Printf("Second Slot: Block #%d at %s (in %v)\n",
			second.Number,
			secondTime.Format(time.RFC3339),
			time.Until(secondTime).Round(time.Second))
	} else if first != nil {
		fmt.Println("Second Slot: Not found in next 400 blocks")
	}
}
