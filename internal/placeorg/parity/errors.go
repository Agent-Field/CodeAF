package parity

import "errors"

var (
	errNotNpy   = errors.New("parity: not a little-endian float32 npy file")
	errRowCount = errors.New("parity: embedding rows do not match the id list")
)
