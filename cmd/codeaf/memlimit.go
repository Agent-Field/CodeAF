package main

// memlimit.go carries the soft memory limit a surface runs under, which is the
// other half of the heap bargain [tuneForTheSurface] strikes.
//
// THE TUNER's OWN COMMENT SAYS WHY THE LIMIT EXISTS AND WHY IT IS DRAWN FROM
// PHYSICAL MEMORY RATHER THAN FROM A NUMBER OR A CGROUP; this file holds the
// derivation and the platform read it leans on, so the whole thing can be tested
// without a machine.

// surfaceMemLimitFloor is the smallest soft memory limit a surface will run
// under, and surfaceMemLimitDivisor is the fraction of the machine's physical
// memory the limit is drawn from. The floor is a REFUSAL and never a value
// passed to the runtime: it exists so that a limit under the live heap is never
// set, because the collector working continuously against a limit it cannot meet
// is worse than the unbounded growth the limit was added to prevent.
//
// 512 MiB is chosen against the roughly 104 MB a surface's resident set was
// measured at — five times the working set, so the runtime has room to collect
// on its own schedule before the soft limit ever binds. The divisor of two makes
// the limit follow the machine on anything larger, and the floor takes over
// below it.
const (
	surfaceMemLimitFloor   = 512 << 20 // 512 MiB
	surfaceMemLimitDivisor = 2
)

// surfaceMemoryLimit is the soft memory limit for a surface on a machine with
// totalBytes of physical memory: half of it, or ZERO when half would fall under
// [surfaceMemLimitFloor].
//
// ZERO IS THE REFUSAL — the caller sets nothing — and it is the answer for a
// machine whose memory could not be read (totalBytes <= 0) as well as for one
// too small to bound. An unread total is not a licence to guess: the only safe
// guess would be a large one, and a large one is the fixed ceiling the tuner's
// own comment rejects.
func surfaceMemoryLimit(totalBytes int64) int64 {
	if totalBytes <= 0 {
		return 0
	}
	limit := totalBytes / surfaceMemLimitDivisor
	if limit < surfaceMemLimitFloor {
		return 0
	}
	return limit
}

// hostTotalMemory is the machine's physical memory in bytes, or zero where this
// platform has no reader for it.
//
// IT IS A VARIABLE RATHER THAN THE READER DIRECTLY, and that is the one seam
// here: a test can stand a machine under [surfaceMemoryLimit]'s floor in front
// of [tuneForTheSurface] without owning one, which is the only way the refusal
// is held down on a box that is big enough to set a limit. The reader itself is
// the per-OS half — [readTotalMemory] in memlimit_linux.go, memlimit_darwin.go
// and memlimit_other.go — and nothing but a test ever reassigns this.
var hostTotalMemory = readTotalMemory
