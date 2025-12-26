package test

import (
	"math/big"
	"testing"

	"validator-predictor/internal/scheduler"

	"github.com/vechain/thor/v2/thor"
)

func TestIsFirstActive(t *testing.T) {
	// Create a test sequence with mixed active/inactive validators
	addr1 := thor.MustParseAddress("0x0000000000000000000000000000000000000001")
	addr2 := thor.MustParseAddress("0x0000000000000000000000000000000000000002")
	addr3 := thor.MustParseAddress("0x0000000000000000000000000000000000000003")
	addr4 := thor.MustParseAddress("0x0000000000000000000000000000000000000004")

	tests := []struct {
		name       string
		sequence   scheduler.Sequence
		targetAddr thor.Address
		want       bool
	}{
		{
			name: "Target is first active validator",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
				{Address: addr3, Active: false},
			},
			targetAddr: addr1,
			want:       true,
		},
		{
			name: "Target is second active validator",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
				{Address: addr3, Active: false},
			},
			targetAddr: addr2,
			want:       false,
		},
		{
			name: "Target is first but inactive - should still match",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: false},
				{Address: addr2, Active: true},
				{Address: addr3, Active: true},
			},
			targetAddr: addr1,
			want:       true,
		},
		{
			name: "First is inactive, target is second but active",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: false},
				{Address: addr2, Active: true},
				{Address: addr3, Active: true},
			},
			targetAddr: addr2,
			want:       true,
		},
		{
			name: "Multiple inactive before target",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: false},
				{Address: addr2, Active: false},
				{Address: addr3, Active: true},
				{Address: addr4, Active: true},
			},
			targetAddr: addr3,
			want:       true,
		},
		{
			name: "Target not in sequence",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
			},
			targetAddr: addr3,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sequence.IsFirstActive(tt.targetAddr)
			if got != tt.want {
				t.Errorf("IsFirstActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSecondActive(t *testing.T) {
	addr1 := thor.MustParseAddress("0x0000000000000000000000000000000000000001")
	addr2 := thor.MustParseAddress("0x0000000000000000000000000000000000000002")
	addr3 := thor.MustParseAddress("0x0000000000000000000000000000000000000003")
	addr4 := thor.MustParseAddress("0x0000000000000000000000000000000000000004")

	tests := []struct {
		name       string
		sequence   scheduler.Sequence
		targetAddr thor.Address
		want       bool
	}{
		{
			name: "Target is second active validator",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
				{Address: addr3, Active: false},
			},
			targetAddr: addr2,
			want:       true,
		},
		{
			name: "Target is first active validator",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
				{Address: addr3, Active: false},
			},
			targetAddr: addr1,
			want:       false,
		},
		{
			name: "Target is third active validator",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
				{Address: addr3, Active: true},
			},
			targetAddr: addr3,
			want:       false,
		},
		{
			name: "First inactive, target is second active (position 2)",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: false},
				{Address: addr2, Active: true},
				{Address: addr3, Active: true},
			},
			targetAddr: addr3,
			want:       true,
		},
		{
			name: "First inactive, second inactive, target is third but first active",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: false},
				{Address: addr2, Active: false},
				{Address: addr3, Active: true},
				{Address: addr4, Active: true},
			},
			targetAddr: addr3,
			want:       false,
		},
		{
			name: "First inactive, second inactive, target is fourth and second active",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: false},
				{Address: addr2, Active: false},
				{Address: addr3, Active: true},
				{Address: addr4, Active: true},
			},
			targetAddr: addr4,
			want:       true,
		},
		{
			name: "Target is second but inactive - should still match",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: false},
				{Address: addr3, Active: true},
			},
			targetAddr: addr2,
			want:       true,
		},
		{
			name: "Target not in sequence",
			sequence: scheduler.Sequence{
				{Address: addr1, Active: true},
				{Address: addr2, Active: true},
			},
			targetAddr: addr3,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sequence.IsSecondActive(tt.targetAddr)
			if got != tt.want {
				t.Errorf("IsSecondActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSequenceGeneration(t *testing.T) {
	// Test that ComputeSequence generates a deterministic sequence
	weight1, _ := new(big.Int).SetString("100000000000000000000", 10) // 100e18
	weight2, _ := new(big.Int).SetString("200000000000000000000", 10) // 200e18
	weight3, _ := new(big.Int).SetString("150000000000000000000", 10) // 150e18

	proposers := []scheduler.Proposer{
		{
			Address: thor.MustParseAddress("0x0000000000000000000000000000000000000001"),
			Active:  true,
			Weight:  weight1,
		},
		{
			Address: thor.MustParseAddress("0x0000000000000000000000000000000000000002"),
			Active:  true,
			Weight:  weight2,
		},
		{
			Address: thor.MustParseAddress("0x0000000000000000000000000000000000000003"),
			Active:  false,
			Weight:  weight3,
		},
	}

	seed := []byte("test-seed")
	blockNum := uint32(1000)

	// Generate sequence twice with same inputs
	seq1 := scheduler.ComputeSequence(proposers, blockNum, seed)
	seq2 := scheduler.ComputeSequence(proposers, blockNum, seed)

	// Verify sequences are identical
	if len(seq1) != len(seq2) {
		t.Fatalf("Sequence lengths differ: %d vs %d", len(seq1), len(seq2))
	}

	for i := range seq1 {
		if seq1[i].Address != seq2[i].Address {
			t.Errorf("Address at index %d differs: %s vs %s", i, seq1[i].Address, seq2[i].Address)
		}
		if seq1[i].Active != seq2[i].Active {
			t.Errorf("Active status at index %d differs: %v vs %v", i, seq1[i].Active, seq2[i].Active)
		}
		if seq1[i].Weight != seq2[i].Weight {
			t.Errorf("Weight at index %d differs: %d vs %d", i, seq1[i].Weight, seq2[i].Weight)
		}
	}

	t.Logf("✓ Sequence is deterministic with %d validators", len(seq1))
}

func TestSequenceWithDifferentSeeds(t *testing.T) {
	// Test that different seeds produce different sequences
	weight1, _ := new(big.Int).SetString("100000000000000000000", 10) // 100e18
	weight2, _ := new(big.Int).SetString("200000000000000000000", 10) // 200e18
	weight3, _ := new(big.Int).SetString("150000000000000000000", 10) // 150e18

	proposers := []scheduler.Proposer{
		{
			Address: thor.MustParseAddress("0x0000000000000000000000000000000000000001"),
			Active:  true,
			Weight:  weight1,
		},
		{
			Address: thor.MustParseAddress("0x0000000000000000000000000000000000000002"),
			Active:  true,
			Weight:  weight2,
		},
		{
			Address: thor.MustParseAddress("0x0000000000000000000000000000000000000003"),
			Active:  true,
			Weight:  weight3,
		},
	}

	blockNum := uint32(1000)
	seed1 := []byte("seed-1")
	seed2 := []byte("seed-2")

	seq1 := scheduler.ComputeSequence(proposers, blockNum, seed1)
	seq2 := scheduler.ComputeSequence(proposers, blockNum, seed2)

	// Verify sequences are different (at least one position should differ)
	different := false
	for i := range seq1 {
		if seq1[i].Address != seq2[i].Address {
			different = true
			break
		}
	}

	if !different {
		t.Error("Expected different seeds to produce different sequences")
	}

	t.Logf("✓ Different seeds produce different sequences")
}
