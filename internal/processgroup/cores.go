package processgroup

import "runtime"

// EffectiveCores is how many processor cores this process may ACTUALLY use: the
// smallest of the machine's logical CPUs, the scheduler affinity mask, and the
// cgroup CPU quota walked from the process's own cgroup upward. A job subtree's
// CPU ceiling is a share of THIS and not of the machine's raw core count,
// because where a job runs the subtree is usually held under a cgroup CPU quota
// or an affinity mask and can never reach more cores than these; a ceiling drawn
// from the machine's cores would sit above anything the quota lets the subtree
// produce and would never trip.
//
// It is a variable so a test — or a caller that already knows the figure — can
// stand a core count in front of the bound without a cgroup or an affinity mask
// to be under; nothing but a test reassigns it.
var EffectiveCores = effectiveCores

// effectiveCores folds the three readable bounds into the tightest, never below
// one. A bound that cannot be read is a zero that does not participate — the same
// silence rule the memory reader keeps (#1162): an unread bound is not a licence
// to guess a smaller machine.
func effectiveCores() float64 {
	cores := float64(runtime.NumCPU())
	if quota := cgroupCPUCores(); quota > 0 && quota < cores {
		cores = quota
	}
	if affinity := affinityCores(); affinity > 0 && float64(affinity) < cores {
		cores = float64(affinity)
	}
	if cores < 1 {
		cores = 1
	}
	return cores
}
