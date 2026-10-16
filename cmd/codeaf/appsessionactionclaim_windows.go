//go:build windows

package main

import "os"

// appClaimReadFlag is a plain read on Windows, where a name in a folder cannot
// be swapped for a pipe that blocks an open; the descriptor is still checked
// to be the plain file that was looked at ([appReadClaimFile]).
const appClaimReadFlag = os.O_RDONLY
