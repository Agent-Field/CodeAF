//go:build !linux

package processgroup

// Off Linux there is no /proc/self/cgroup and no sched_getaffinity to read here,
// so neither bound participates and [effectiveCores] falls back to the machine's
// logical CPU count.
func cgroupCPUCores() float64 { return 0 }

func affinityCores() int { return 0 }
