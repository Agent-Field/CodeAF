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

// readCgroupMemoryLimit is the zero for a platform with no cgroup hierarchy to
// read: macOS has none, and a container on a Mac runs the Linux process inside a
// VM, so the cgroup bound a Mac's own process could be under does not exist.
func readCgroupMemoryLimit() int64 { return 0 }
