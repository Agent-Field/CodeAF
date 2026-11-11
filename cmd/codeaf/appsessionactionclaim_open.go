//go:build !windows

package main

import (
	"os"
	"syscall"
)

// appClaimReadFlag opens a claim for reading WITHOUT WAITING: a pipe swapped in
// for the file opens at once instead of holding the request until a writer
// comes, and its descriptor is then refused ([appReadClaimFile]). On a plain
// file the flag changes nothing.
const appClaimReadFlag = os.O_RDONLY | syscall.O_NONBLOCK
