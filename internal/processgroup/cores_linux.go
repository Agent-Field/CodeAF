//go:build linux

package processgroup

// cores_linux.go reads the two Linux bounds on how many cores this process may
// use — the cgroup CPU quota and the scheduler affinity mask — mirroring the way
// cmd/codeaf reads the cgroup MEMORY bound (#1162): the process's own
// /proc/self/cgroup names its cgroup, and the quota is walked from that cgroup to
// the hierarchy root with the tightest taken, because a parent slice can bound
// tighter than the leaf.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// cgroupCPUCores is the CPU quota the cgroup this process runs in places on it,
// in cores (quota / period), or zero when the hierarchy places no quota to give.
func cgroupCPUCores() float64 {
	return cgroupCPUCoresAt("/proc/self/cgroup", "/sys/fs/cgroup")
}

// cgroupCPUCoresAt is [cgroupCPUCores] against a named cgroup file and hierarchy
// root, so the walk can be tested without being in a cgroup. Both generations are
// read: v2 has an empty controller list on its 0:: line and the quota in
// <path>/cpu.max ("quota period", or "max period" for no quota); v1 names cpu
// among its controllers and keeps the quota in cpu/<path>/cpu.cfs_quota_us over
// cpu.cfs_period_us (a quota of -1 is no quota).
func cgroupCPUCoresAt(procSelfCgroup, root string) float64 {
	raw, err := os.ReadFile(procSelfCgroup)
	if err != nil {
		return 0
	}
	best := 0.0
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(fields) != 3 {
			continue
		}
		controllers, path := fields[1], fields[2]
		var cores float64
		switch {
		case controllers == "":
			cores = smallestCPUCores(root, path, cpuMaxV2)
		case hasController(controllers, "cpu"):
			cores = smallestCPUCores(filepath.Join(root, "cpu"), path, cpuMaxV1)
		default:
			continue
		}
		if cores > 0 && (best == 0 || cores < best) {
			best = cores
		}
	}
	return best
}

// smallestCPUCores returns the tightest finite core quota that read names at the
// cgroup directory dir or any ancestor up to root, or zero when none bound.
func smallestCPUCores(root, dir string, read func(string) float64) float64 {
	root = filepath.Clean(root)
	at := filepath.Clean(filepath.Join(root, dir))
	if at != root && !strings.HasPrefix(at, root+string(filepath.Separator)) {
		return 0
	}
	best := 0.0
	for {
		if cores := read(at); cores > 0 {
			if best == 0 || cores < best {
				best = cores
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
	return best
}

// cpuMaxV2 reads a cgroup v2 cpu.max at dir and returns quota / period in cores,
// or zero when the file is absent, unreadable, or says "max".
func cpuMaxV2(dir string) float64 {
	raw, err := os.ReadFile(filepath.Join(dir, "cpu.max"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 || fields[0] == "max" {
		return 0
	}
	quota, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || quota <= 0 {
		return 0
	}
	period, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || period <= 0 {
		return 0
	}
	return quota / period
}

// cpuMaxV1 reads a cgroup v1 cpu.cfs_quota_us over cpu.cfs_period_us at dir and
// returns their ratio in cores, or zero when there is no quota (the file is
// absent or holds -1).
func cpuMaxV1(dir string) float64 {
	quota := readInt(filepath.Join(dir, "cpu.cfs_quota_us"))
	if quota <= 0 {
		return 0
	}
	period := readInt(filepath.Join(dir, "cpu.cfs_period_us"))
	if period <= 0 {
		return 0
	}
	return float64(quota) / float64(period)
}

// readInt reads one integer from a cgroup file, or zero when it cannot.
func readInt(path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
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

// affinityCores is how many CPUs the process's scheduler affinity mask allows,
// or zero when it cannot be read.
func affinityCores() int {
	var set unix.CPUSet
	if err := unix.SchedGetaffinity(0, &set); err != nil {
		return 0
	}
	return set.Count()
}
