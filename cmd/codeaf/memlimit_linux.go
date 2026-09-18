//go:build linux

package main

// memlimit_linux.go answers "how much physical memory does this machine have"
// the way Linux answers it: the MemTotal row of /proc/meminfo.

import (
	"os"
	"strconv"
	"strings"
)

// readTotalMemory is the machine's physical memory in bytes, from /proc/meminfo,
// or zero where the file cannot be read or has no MemTotal row.
//
// THE FILE'S OWN UNIT IS kB and it is scanned line by line rather than parsed
// whole, the same way internal/session reads it: the one row wanted is near the
// top and the rest of the file is fifty rows with no bearing on the question.
func readTotalMemory() int64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || key != "MemTotal" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || kilobytes <= 0 {
			return 0
		}
		return kilobytes * 1024
	}
	return 0
}
