//go:build windows

package main

import "os"

// appLeafOpenFlags is a plain read on Windows, which has neither O_NOFOLLOW nor
// pipes in a working tree. A leaf swapped for a link after its stat is refused
// by the same-file check that follows the open (appcheckoutopen.go).
const appLeafOpenFlags = os.O_RDONLY
