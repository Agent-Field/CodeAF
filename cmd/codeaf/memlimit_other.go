//go:build !linux && !darwin

package main

// memlimit_other.go is the reader for a platform this build has no answer for
// (Windows is the one that ships). It says so with zero, and [surfaceMemoryLimit]
// turns that into the refusal: nothing is set, rather than a limit guessed at,
// and the surface keeps the runtime's own default the way it did before this
// existed.
func readTotalMemory() int64 { return 0 }

// readCgroupMemoryLimit is the zero for a platform with no cgroup hierarchy to
// read. A zero is not a bound, so [surfaceMemoryLimit] simply does not count it.
func readCgroupMemoryLimit() int64 { return 0 }
