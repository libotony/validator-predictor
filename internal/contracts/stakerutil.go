package contracts

import _ "embed"

//go:embed compiled/StakerUtil.abi
var StakerUtilABI string

//go:embed compiled/StakerUtil.bin
var StakerUtilBytecode string
