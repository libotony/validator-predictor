package test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"validator-next-slot/internal/client"
)

func TestTotalWeight(t *testing.T) {
	t.Logf("Testing GetActiveValidators and calculating total weight")
	t.Logf("Node: %s", nodeURL)

	c := client.NewVeChainClient(nodeURL)
	ctx := context.Background()

	// Get best block
	bestBlockID, bestBlockNum, err := c.GetBestBlock(ctx)
	if err != nil {
		t.Fatalf("Failed to get best block: %v", err)
	}
	revision := bestBlockID.String()

	t.Logf("Best Block: %d (%s)", bestBlockNum, revision)

	// Get all validators
	t.Log("Fetching all active validators...")
	start := time.Now()
	validators, err := c.GetActiveValidators(ctx, revision)
	if err != nil {
		t.Fatalf("Failed to get validators: %v", err)
	}
	elapsed := time.Since(start)

	t.Logf("✓ Fetched %d validators in %v", len(validators), elapsed.Round(time.Millisecond))

	// Calculate total weight
	totalWeight := new(big.Int)
	onlineCount := 0
	offlineCount := 0

	t.Log("Validator List (first 10 + every 10th):")
	for i, v := range validators {
		if i < 10 || (i+1)%10 == 0 {
			status := "ONLINE"
			if !v.Active {
				status = "OFFLINE"
			}
			t.Logf("Validator %3d: %s Weight: %s %s",
				i+1, v.Address, v.Weight.String(), status)
		}
		totalWeight.Add(totalWeight, v.Weight)
		if v.Active {
			onlineCount++
		} else {
			offlineCount++
		}
	}

	t.Logf("Total Validators:  %d", len(validators))
	t.Logf("  Online:          %d", onlineCount)
	t.Logf("  Offline:         %d", offlineCount)
	t.Logf("Calculated Total Weight: %s", totalWeight.String())

	// Get totalStake from contract to verify
	t.Log("Fetching totalStake from contract...")
	totalStake, err := c.GetTotalStake(ctx, revision)
	if err != nil {
		t.Fatalf("Failed to get total stake: %v", err)
	}

	t.Logf("Contract Total VET:      %s", totalStake.TotalVET.String())
	t.Logf("Contract Total Weight:   %s", totalStake.TotalWeight.String())

	// Compare
	if totalWeight.Cmp(totalStake.TotalWeight) == 0 {
		t.Log("✓ Calculated weight matches contract totalWeight!")
	} else {
		diff := new(big.Int).Sub(totalStake.TotalWeight, totalWeight)
		t.Errorf("Calculated weight does NOT match contract totalWeight. Difference: %s", diff.String())
	}

	// Assertions
	if len(validators) == 0 {
		t.Error("Expected at least one validator")
	}

	if totalWeight.Sign() <= 0 {
		t.Error("Expected total weight to be positive")
	}

	if totalWeight.Cmp(totalStake.TotalWeight) != 0 {
		t.Error("Total weight mismatch with contract")
	}
}
