package contracts

//go:generate docker run --rm -v ./:/sources ghcr.io/argotorg/solc:0.8.20 --evm-version paris --via-ir --overwrite --optimize --optimize-runs 200 -o /sources/compiled --abi --bin /sources/staker-util.sol
//go:generate docker run --rm -v ./compiled:/compiled alpine sh -c "rm ./compiled/IStaker.bin && rm ./compiled/IStaker.abi"
