package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"validator-next-slot/internal/client"
	"validator-next-slot/internal/consensus"

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

	// 1. Get snapshot head
	bestBlockID, bestBlockNum, err := c.GetBestBlock(ctx)
	if err != nil {
		log.Fatalf("Failed to get best block: %v", err)
	}
	revision := bestBlockID.String()

	fmt.Printf("Best Block: %d (%s)\n", bestBlockNum, revision)

	// 2. Validate target validator first (fast path)
	fmt.Printf("Checking validator: %s\n", targetAddr)
	validation, err := c.GetValidation(ctx, targetAddr.String(), revision)
	if err != nil {
		log.Fatalf("Failed to get validation for %s: %v", targetAddr, err)
	}

	// Check if validator has weight (is registered)
	if validation.Status != builtin.StakerStatusActive {
		log.Fatalf("Error: %s is not active", targetAddr)
	}

	// Check if validator is ONLINE (Active)
	isOnline := validation.OfflineBlock == 0xFFFFFFFF // math.MaxUint32
	if !isOnline {
		log.Fatalf("Error: Target validator %s is OFFLINE (offlineBlock = %d)", targetAddr, validation.OfflineBlock)
	}

	fmt.Printf("✓ Target validator is ONLINE and active\n")
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

	proposers := make([]consensus.Proposer, len(validators))
	for i, v := range validators {
		proposers[i] = consensus.Proposer{
			Address: v.Address,
			Active:  v.Active,
			Weight:  v.Weight,
		}
	}

	// 4. Get Seed (Beta)
	interval := thor.SeederInterval()
	seedNum := (bestBlockNum / interval) * interval
	seed, err := c.GetBlockBeta(ctx, fmt.Sprintf("%d", seedNum))
	if err != nil {
		log.Fatalf("Failed to get seed from block %d: %v", seedNum, err)
	}

	// 5. Initialize Scheduler
	bestBlock, err := c.ThorClient().Block(revision)
	if err != nil {
		log.Fatalf("Failed to get best block details: %v", err)
	}
	parentTime := bestBlock.Timestamp

	sched := consensus.NewPoSScheduler(targetAddr, proposers, bestBlockNum, parentTime, seed)

	// 6. Predict Next Slot
	now := uint64(time.Now().Unix())
	nextSlot := sched.GetNextSlot(now)

	fmt.Printf("\nTarget Validator: %s\n", targetAddr)
	if nextSlot > 0 {
		nextTime := time.Unix(int64(nextSlot), 0)
		fmt.Printf("Next Scheduled Block: %s (in %v)\n", nextTime.Format(time.RFC3339), time.Until(nextTime).Round(time.Second))
	} else {
		fmt.Println("Next Scheduled Block: Not found in current sequence")
	}
}
