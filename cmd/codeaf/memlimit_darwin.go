//go:build darwin

package main

// memlimit_darwin.go answers "how much physical memory does this machine have"
// the way macOS answers it: sysctl's hw.memsize.

import "golang.org/x/sys/unix"

// readTotalMemory is the machine's physical memory in bytes, from
// `sysctl hw.memsize`, or zero where the kernel will not say.
func readTotalMemory() int64 {
	bytes, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return int64(bytes)
}
