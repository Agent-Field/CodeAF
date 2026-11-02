//go:build wasip1 && wasm

// The reactor owns only byte buffers and consensus handles. The host imports
// this precompiled module and resumes calls after its own durability boundary.
package main

import (
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/raftabi"
)

var reactor = raftabi.New()
var input, output []byte

func main() {}

//go:wasmexport codeaf_abi_version
func abiVersion() uint32 { return raftabi.Version }

//go:wasmexport codeaf_alloc
func alloc(n uint32) uint32 {
	if n == 0 || n > raftabi.MaxBuffer {
		return 0
	}
	input = make([]byte, n)
	return uint32(uintptr(unsafe.Pointer(&input[0])))
}

//go:wasmexport codeaf_call
func call(n uint32) uint32 {
	if n == 0 || n > uint32(len(input)) {
		return 0
	}
	output = reactor.Call(input[:n])
	input = nil
	return uint32(len(output))
}

//go:wasmexport codeaf_result
func result() uint32 {
	if len(output) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&output[0])))
}

//go:wasmexport codeaf_free
func free() { input = nil; output = nil }
