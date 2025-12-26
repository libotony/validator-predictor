# VeChain Validator Next Slot Predictor

A Go tool to predict the next block production time for VeChain validators using the PoA 2.0 (SURFACE) consensus algorithm.

## Features

- ✅ **Multi-Clause Batch Optimization** - 80x faster validator fetching (69s → 0.87s for 101 validators)
- ✅ **Direct Staker Contract Queries** - Queries Staker contract via linked list iteration
- ✅ **Accurate Sequence Computation** - Verified against actual blockchain data
- ✅ **Comprehensive Tests** - Sequence verification and weight calculation tests
- ✅ **Network Resilience** - Exponential backoff retry with 30s timeout

## Usage

```bash
# Build
go build

# Run with validator address
./validator-next-slot 0x<validator_address>

# Custom node
./validator-next-slot 0x<validator_address> --node https://mainnet.vechain.org

# Run tests
go test -v ./test
```

## Architecture

```
validator-next-slot/
├── main.go                    # Entry point
├── internal/
│   ├── client/
│   │   └── vechain_client.go  # VeChain node client with retry logic
│   ├── scheduler/
│   │   └── pos.go             # PoS scheduler implementation with sequence computation
│   ├── consensus/
│   │   └── pos.go             # Next slot finder logic
│   ├── model/
│   │   └── validator.go       # Simplified validator data model
│   └── utils/
│       └── retry.go           # Retry with exponential backoff
└── test/
    ├── verify_sequence_test.go  # Sequence computation verification test
    └── total_weight_test.go     # Validator weight calculation test
```

## How It Works

1. **Get Best Block** - Fetches current best block as revision for consistent state queries
2. **Fetch Validators** - Queries Staker contract's linked list for all validators with weights and online status
3. **Calculate Seed Block** - Uses `GetSeedBlockNumber(parentBlockNum)` to determine seed block: `((parentBlockNum + 1) / 8640 - 1) * 8640`
4. **Retrieve Seed** - Gets Beta (VRF proof) from seed block header
5. **Compute Sequence** - Uses ChaCha8 PRNG with weighted random selection to generate deterministic block production sequence
6. **Find Next Slot** - Searches sequence for target validator's next scheduled block time

## Algorithm

VeChain PoA 2.0 uses weighted random selection for fair block production:
- **Priority Score**: `-ln(random) / weight` (A-Res reservoir sampling)
- **Deterministic**: Same seed + validators = same sequence
- **Skip Offline**: Inactive validators are skipped during block production
- **Weight**: Converted from wei to VET (÷ 1e18)

## Testing

### Sequence Verification Test
Validates the sequence computation by comparing with actual blockchain data:
```bash
go test -v ./test -run TestVerifySequence
```

### Weight Calculation Test
Verifies validator fetching and weight calculations:
```bash
go test -v ./test -run TestTotalWeight
```

## Performance

- **Validator Fetching**: 101 validators in ~1s (multi-clause batch optimization)
- **Before Optimization**: 202 API calls, 69 seconds
- **After Optimization**: 101 batch calls (getValidation + next), 0.87 seconds
- **Improvement**: 80x faster

## License

MIT
