//go:build linux

package main

// memlimit_linux.go answers "how much physical memory does this machine have"
// the way Linux answers it: the MemTotal row of /proc/meminfo.

import (
	"os"
	"path/filepath"
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

// cgroupV1Unlimited is the sentinel cgroup v1 writes into
// memory.limit_in_bytes to mean "no limit". It sits close to MaxInt64 and no
// real machine has this much memory, so any value at or above it is not a bound.
const cgroupV1Unlimited = int64(9223372036854771712)

// readCgroupMemoryLimit is [cgroupMemoryLimit] against the live filesystem: the
// process's own /proc/self/cgroup and a hierarchy rooted at /sys/fs/cgroup.
func readCgroupMemoryLimit() int64 {
	return cgroupMemoryLimit("/proc/self/cgroup", "/sys/fs/cgroup")
}

// cgroupMemoryLimit is the tightest finite cgroup memory bound on the process
// described by the procSelfCgroup file in the hierarchy rooted at root, or zero
// when the hierarchy places no bound to give.
//
// BOTH CGROUP GENERATIONS ARE READ, because a process may sit in either and the
// two keep their limit in different files: v2 has an empty controller list on
// the 0:: line and the bound in <path>/memory.max, while v1 names memory among
// its controllers and keeps the bound in memory/<path>/memory.limit_in_bytes.
// Either file may say `max`, which is not a bound.
//
// THE WHOLE PATH IS WALKED, NOT JUST THE LEAF. The process's own cgroup is the
// third field of its line, and a parent slice can be tighter than the leaf — a
// container often puts the real bound on a slice above the one the process is
// in, or leaves the leaf at `max` while the parent is bounded. Walking to the
// hierarchy root and taking the minimum can only tighten the answer, never
// loosen it, and it costs a few extra stats at startup; the root itself is read
// too, and on many hosts it has no memory.max, which simply does not
// participate.
func cgroupMemoryLimit(procSelfCgroup, root string) int64 {
	raw, err := os.ReadFile(procSelfCgroup)
	if err != nil {
		return 0
	}
	bound := int64(0)
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(fields) != 3 {
			continue
		}
		controllers, path := fields[1], fields[2]
		var v int64
		switch {
		case controllers == "": // cgroup v2, the unified hierarchy
			v = smallestCgroupBound(root, path, "memory.max")
		case hasController(controllers, "memory"): // cgroup v1
			v = smallestCgroupBound(filepath.Join(root, "memory"), path, "memory.limit_in_bytes")
		default:
			continue
		}
		if v > 0 && (bound == 0 || v < bound) {
			bound = v
		}
	}
	return bound
}

// smallestCgroupBound returns the smallest finite bound named by name at the
// cgroup directory dir or any of its ancestors up to the hierarchy root, or
// zero when none of them bound. dir is the cgroup path from /proc/self/cgroup,
// which is absolute (like "/user.slice/user-1001.slice/session.scope") and is
// joined onto root before the walk; the walk stops once root itself is read.
func smallestCgroupBound(root, dir, name string) int64 {
	root = filepath.Clean(root)
	at := filepath.Clean(filepath.Join(root, dir))
	if at != root && !strings.HasPrefix(at, root+string(filepath.Separator)) {
		return 0
	}
	bound := int64(0)
	for {
		if v := readCgroupBound(filepath.Join(at, name)); v > 0 {
			if bound == 0 || v < bound {
				bound = v
			}
		}
		if at == root {
			break
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	return bound
}

// readCgroupBound reads one cgroup limit file and returns the finite bound it
// holds, or zero when it holds none: the file is absent or unreadable, its value
// is the word `max`, it will not parse as a number, it is not positive, or it is
// the cgroup v1 "no limit" sentinel.
func readCgroupBound(path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "max" {
		return 0
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 || n >= cgroupV1Unlimited {
		return 0
	}
	return n
}

// hasController reports whether a cgroup v1 controller list (comma-separated)
// names want.
func hasController(list, want string) bool {
	for _, c := range strings.Split(list, ",") {
		if c == want {
			return true
		}
	}
	return false
}
