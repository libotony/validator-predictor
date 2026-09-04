# VeChain Validator Predictor

A Go tool to predict block production times for VeChain validators using the **dPoS (Delegated Proof of Stake)** consensus algorithm.

## Features

- ✅ **Next Block Prediction** - Show first and second proposers for the next block
- ✅ **Multi-Block Prediction** - Predict proposers for next N blocks
- ✅ **Validator Schedule Lookup** - Find next slots for specific validator
- ✅ **Accurate dPoS Sequence** - Weighted random selection verified against blockchain data
- ✅ **Fast Performance** - Fetch all validators in ~1s using ghost contract deployment

## Usage

```bash
# Build
go build

# Show next block proposers (default mode)
./validator-predictor

# Predict next N blocks (default 20)
./validator-predictor --predict
./validator-predictor --predict -n 10

# Find slots for specific validator
./validator-predictor 0x<validator_address>
./validator-predictor -json 0x<validator_address>

# Custom node
./validator-predictor --node https://mainnet.vechain.org

# Run tests
go test -v ./test
```

## Algorithm

VeChain's dPoS uses weighted random selection for fair block production:
- **Deterministic** - Same seed + block number = same proposer sequence
- **Weighted** - Higher stake = higher probability of being selected
- **Priority Score** - `-ln(random) / weight` using A-Res reservoir sampling

## License

MIT
