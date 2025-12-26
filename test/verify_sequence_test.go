package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"validator-next-slot/internal/client"
	"validator-next-slot/internal/scheduler"

	"github.com/vechain/thor/v2/thor"
)

func TestVerifySequence(t *testing.T) {
	t.Logf("Testing sequence computation by verifying best block signer")
	t.Logf("Node: %s", nodeURL)

	c := client.NewVeChainClient(nodeURL)
	ctx := context.Background()

	// 1. Get best block
	bestBlockID, bestBlockNum, err := c.GetBestBlock(ctx)
	if err != nil {
		t.Fatalf("Failed to get best block: %v", err)
	}
	t.Logf("Best Block: %d (%s)", bestBlockNum, bestBlockID)

	// 2. Get best block details to find signer
	bestBlock, err := c.ThorClient().Block(bestBlockID.String())
	if err != nil {
		t.Fatalf("Failed to get best block details: %v", err)
	}
	bestBlockSigner := bestBlock.Signer
	t.Logf("Best Block Signer: %s", bestBlockSigner)

	// 3. Get parent block number and details
	parentBlockNum := bestBlockNum - 1
	parentBlockID := bestBlock.ParentID
	t.Logf("Parent Block: %d (%s)", parentBlockNum, parentBlockID)

	parentBlock, err := c.ThorClient().Block(parentBlockID.String())
	if err != nil {
		t.Fatalf("Failed to get parent block: %v", err)
	}
	parentTime := parentBlock.Timestamp

	// 4. Get seed (Beta) using seeder interval
	seedNum := scheduler.GetSeedBlockNumber(parentBlockNum)
	seed, err := c.GetBlockBeta(ctx, fmt.Sprintf("%d", seedNum))
	if err != nil {
		t.Fatalf("Failed to get seed from block %d: %v", seedNum, err)
	}
	t.Logf("Using seed from block %d", seedNum)

	// 5. Get validator list at parent block
	t.Logf("Fetching validators at parent block...")
	start := time.Now()
	validators, err := c.GetActiveValidators(ctx, parentBlockID.String())
	if err != nil {
		t.Fatalf("Failed to get validators: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("✓ Fetched %d validators in %v", len(validators), elapsed.Round(time.Millisecond))

	// 6. Build proposer list
	proposers := make([]scheduler.Proposer, len(validators))
	for i, v := range validators {
		proposers[i] = scheduler.Proposer{
			Address: v.Address,
			Active:  v.Active,
			Weight:  v.Weight,
		}
	}

	// 7. Compute sequence using parent block info
	sequence := scheduler.ComputeSequence(proposers, parentBlockNum, parentTime, seed)

	t.Logf("\n=== Verification ===")
	t.Logf("Expected Signer: %s", bestBlockSigner)
	t.Logf("Sequence Length: %d", len(sequence))

	// 8. Calculate which position in sequence the best block should be
	T := thor.BlockInterval()
	bestBlockTime := bestBlock.Timestamp
	offset := (bestBlockTime - parentTime) / T
	t.Logf("Time offset from parent: %d slots", offset)

	// 9. Check sequence to find the signer
	found := false
	expectedIndex := (offset - 1) % uint64(len(sequence))

	t.Logf("\n=== Checking Sequence ===")
	for i := uint64(0); i < uint64(len(sequence)); i++ {
		index := (i + offset - 1) % uint64(len(sequence))
		entry := sequence[index]

		if entry.Address == bestBlockSigner {
			t.Logf("✓ Position %d (sequence[%d]): %s - MATCH! (Active: %v)",
				i+1, index, entry.Address, entry.Active)
			found = true

			// Verify this is at expected position
			if index == expectedIndex {
				t.Logf("✓ Signer found at expected position!")
			} else {
				t.Errorf("✗ Signer found but at wrong position! Expected index %d, got %d", expectedIndex, index)
			}
			break
		} else if entry.Active {
			// Found an active validator that is NOT the signer
			t.Errorf("✗ Position %d (sequence[%d]): %s - ACTIVE but NOT signer! Sequence computation ERROR!",
				i+1, index, entry.Address)
			t.Errorf("  Expected: %s", bestBlockSigner)
			t.Errorf("  Got:      %s", entry.Address)
			break
		} else {
			// Inactive validator, continue checking
			if i < 10 {
				t.Logf("  Position %d (sequence[%d]): %s - OFFLINE, continue...", i+1, index, entry.Address)
			}
		}
	}

	if !found {
		t.Errorf("✗ Signer %s not found in sequence!", bestBlockSigner)

		// Debug: search for signer in entire sequence
		t.Logf("\nSearching for signer in entire sequence:")
		for i, entry := range sequence {
			if entry.Address == bestBlockSigner {
				status := "OFFLINE"
				if entry.Active {
					status = "ONLINE"
				}
				t.Logf("  Found at index %d - %s - %s", i, entry.Address, status)
			}
		}

		// Debug: print first 20 in sequence
		t.Logf("\nFirst 20 addresses in sequence:")
		for i := 0; i < 20 && i < len(sequence); i++ {
			status := "OFFLINE"
			if sequence[i].Active {
				status = "ONLINE"
			}
			t.Logf("  [%2d] %s - %s", i, sequence[i].Address, status)
		}
	}

	// Summary
	t.Logf("\n=== Summary ===")
	if found {
		t.Logf("✓ Sequence computation CORRECT!")
		t.Logf("✓ Best block signer matches computed sequence")
	} else {
		t.Errorf("✗ Sequence computation FAILED!")
		t.Errorf("✗ Best block signer does NOT match computed sequence")
	}
}
