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
	parentBlockTime uint64,
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
