package scheduler

import (
	"encoding/binary"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"

	"github.com/vechain/thor/v2/thor"
)

type Entry struct {
	Address thor.Address
	Weight  uint64
	Active  bool
	score   float64
}

type Sequence []Entry

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
) Sequence {
	var (
		shuffled = make(Sequence, 0, len(proposers))
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
		wt := new(big.Int).Div(p.Weight, big.NewInt(1e18)).Uint64()
		shuffled = append(shuffled, Entry{
			Address: p.Address,
			Weight:  wt,
			Active:  p.Active,
			score:   -math.Log(random) / float64(wt),
		})
		// https://en.wikipedia.org/wiki/Reservoir_sampling#Algorithm_A-Res
		// A-Res Reservoir sampling algorithm with -ln(random)/weight
	}

	// Step 3: Sort validators by priority score in ascending order
	slices.SortStableFunc(shuffled, func(a, b Entry) int {
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

func (s Sequence) findNextActive(index int, targetAddr thor.Address) (int, thor.Address) {
	for i := index; i < len(s); i++ {
		if s[i].Active || s[i].Address == targetAddr {
			return i, s[i].Address
		}
	}
	return -1, thor.Address{} // not found
}

func (s Sequence) IsFirstActive(targetAddr thor.Address) bool {
	_, addr := s.findNextActive(0, targetAddr)
	return addr == targetAddr
}

func (s Sequence) IsSecondActive(targetAddr thor.Address) bool {
	i, addr := s.findNextActive(0, targetAddr)
	if addr == targetAddr {
		return false
	}
	_, addr = s.findNextActive(i+1, targetAddr)
	return addr == targetAddr
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
// Proposers are reused but sequence is recomputed for each block number
func FindNextSlots(
	proposers []Proposer,
	startBlockNumber uint32,
	startBlockTime uint64,
	targetAddr thor.Address,
	getSeed SeedProvider,
) (first *BlockSlot, second *BlockSlot, err error) {
	const (
		maxTries      = 400
		blockInterval = 10 // seconds per block (thor.BlockInterval())
	)

	var seed []byte

	for i := 0; i < maxTries; i++ {
		blockNum := startBlockNumber + uint32(i)

		if seed == nil || blockNum%thor.SeederInterval() == 0 {
			seed, err = getSeed(GetSeedBlockNumber(blockNum))
			if err != nil {
				return nil, nil, err
			}
		}

		// Compute sequence for this specific block number
		sequence := ComputeSequence(proposers, blockNum, seed)

		// Check if target is first or second active in this block's sequence
		if sequence.IsFirstActive(targetAddr) {
			if first == nil {
				first = &BlockSlot{
					Number: blockNum,
					Time:   startBlockTime + uint64(i+1)*blockInterval,
				}
				if second != nil {
					return first, second, nil
				}
			}
		} else if sequence.IsSecondActive(targetAddr) {
			if second == nil {
				second = &BlockSlot{
					Number: blockNum,
					Time:   startBlockTime + uint64(i+2)*blockInterval,
				}
				if first != nil {
					return first, second, nil
				}
			}
		}
	}

	// Return what we found (might be only first, or none)
	return first, second, nil
}
