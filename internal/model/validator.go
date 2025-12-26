package model

import (
	"math/big"

	"github.com/vechain/thor/v2/thor"
)

// ValidatorInfo represents the simplified validator data we need
type ValidatorInfo struct {
	Address thor.Address
	Weight  *big.Int
	Active  bool // Active means online (offlineBlock == maxUint32)
}
