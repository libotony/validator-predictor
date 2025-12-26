package scheduler

import (
	"encoding/binary"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"

	"github.com/vechain/thor/v2/thor"
)

type SequenceEntry struct {
	Address thor.Address
	Weight  uint64
	Active  bool
	score   float64
}

// Proposer with status and weight
type Proposer struct {
	Address thor.Address
	Active  bool
	Weight  *big.Int
}

// GetSeedBlockNumber calculates the seed block number based on parent block number
// It adds 1 to parent block number (to get current block number) then calculates the seed block
func GetSeedBlockNumber(parentBlockNumber uint32) uint32 {
	interval := thor.SeederInterval()
	currentBlockNumber := parentBlockNumber + 1
	return (currentBlockNumber/interval - 1) * interval
}

func ComputeSequence(
	proposers []Proposer,
	parentBlockNumber uint32,
	seed []byte,
) []SequenceEntry {
	var (
		shuffled = make([]SequenceEntry, 0, len(proposers))
	)

	var num [4]byte
	binary.BigEndian.PutUint32(num[:], parentBlockNumber)

	// Step 1: Generate a seed for the deterministic pseudo-random generator
	hashedSeed := thor.Blake2b(seed, num[:])
	src := rand.NewChaCha8(hashedSeed)
	pseudoRND := rand.New(src) //#nosec G404

	// Step 2: Calculate priority scores for each validator based on their weight
	// using the exponential distribution method for weighted random sampling
	for _, p := range proposers {
		// IMPORTANT: Every validator in the leader group should be allocated
		// with the deterministic random number sequence from the same source.
		random := pseudoRND.Float64()
		if random == 0 {
			random = 1e-10 // prevent ln(0)
		}
		wt := p.Weight.Div(p.Weight, big.NewInt(1e18)).Uint64()
		shuffled = append(shuffled, SequenceEntry{
			Address: p.Address,
			Weight:  wt,
			Active:  p.Active,
			score:   -math.Log(random) / float64(wt),
		})
		// https://en.wikipedia.org/wiki/Reservoir_sampling#Algorithm_A-Res
		// A-Res Reservoir sampling algorithm with -ln(random)/weight
	}

	// Step 3: Sort validators by priority score in ascending order
	slices.SortStableFunc(shuffled, func(a, b SequenceEntry) int {
		switch {
		case a.score < b.score:
			return -1
		case a.score > b.score:
			return 1
		default:
			return 0
		}
	})

	return shuffled
}

// BlockSlot represents a scheduled block time slot
type BlockSlot struct {
	Number uint32
	Time   uint64
}

// SeedProvider is a function that returns the seed for a given seed block number
type SeedProvider func(seedBlockNum uint32) ([]byte, error)

// FindNextSlots finds the next two scheduled block times for the target validator
// It searches up to maxBlocks (400) future blocks
// The sequence is computed once at the start and reused for all blocks
func FindNextSlots(
	proposers []Proposer,
	startBlockNumber uint32,
	startBlockTime uint64,
	targetAddr thor.Address,
	getSeed SeedProvider,
) (first *BlockSlot, second *BlockSlot, err error) {
	const (
		maxBlocks     = 400
		blockInterval = 10 // seconds per block (thor.BlockInterval())
	)

	// Calculate seed block number and fetch seed once
	seedBlockNum := GetSeedBlockNumber(startBlockNumber)
	seed, err := getSeed(seedBlockNum)
	if err != nil {
		return nil, nil, err
	}

	// Compute sequence once using start block info
	sequence := ComputeSequence(proposers, startBlockNumber, seed)

	var foundCount int

	// Start from next block
	currentBlockNum := startBlockNumber + 1
	currentBlockTime := startBlockTime + blockInterval

	for i := 0; i < maxBlocks; i++ {
		// Calculate time offset from the ORIGINAL parent block (startBlockTime)
		// This is the key: offset grows as we search forward
		offset := (currentBlockTime - startBlockTime) / blockInterval

		// Search through sequence starting from offset, skipping offline validators
		// But if address == target, always match regardless of active status
		for j := uint64(0); j < uint64(len(sequence)); j++ {
			index := (j + offset - 1) % uint64(len(sequence))
			entry := sequence[index]

			// Check if this is the target validator
			if entry.Address == targetAddr {
				// Found target! Record this slot
				foundCount++
				slot := &BlockSlot{
					Number: currentBlockNum,
					Time:   currentBlockTime,
				}

				if foundCount == 1 {
					first = slot
				} else if foundCount == 2 {
					second = slot
					return first, second, nil
				}
				break
			}

			// Skip inactive validators (continue searching)
			if !entry.Active {
				continue
			}

			// Found an active validator that's not the target - this block belongs to them
			break
		}

		// Move to next block
		currentBlockNum++
		currentBlockTime += blockInterval
	}

	// Return what we found (might be only first, or none)
	return first, second, nil
}
