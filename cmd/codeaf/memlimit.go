package main

// memlimit.go carries the soft memory limit a surface runs under, which is the
// other half of the heap bargain [tuneForTheSurface] strikes.
//
// THE TUNER's OWN COMMENT SAYS WHY THE LIMIT EXISTS AND WHY IT IS DRAWN FROM THE
// SMALLEST REAL BOUND RATHER THAN FROM A NUMBER OR FROM PHYSICAL MEMORY ALONE;
// this file holds the derivation and the two platform reads it leans on, so the
// whole thing can be tested without a machine.

// surfaceMemLimitFloor is the smallest soft memory limit a surface will run
// under, and surfaceMemLimitDivisor is the fraction of the tightest memory
// bound the limit is drawn from. The floor is a REFUSAL and never a value
// passed to the runtime: it exists so that a limit under the live heap is never
// set, because the collector working continuously against a limit it cannot meet
// is worse than the unbounded growth the limit was added to prevent.
//
// 512 MiB is chosen against the roughly 104 MB a surface's resident set was
// measured at — five times the working set, so the runtime has room to collect
// on its own schedule before the soft limit ever binds. The divisor of two makes
// the limit follow the bound on anything larger, and the floor takes over
// below it.
const (
	surfaceMemLimitFloor   = 512 << 20 // 512 MiB
	surfaceMemLimitDivisor = 2
)

// surfaceMemoryLimit is the soft memory limit for a surface on a machine whose
// readable memory bounds are the given bytes: HALF OF THE SMALLEST OF THEM, or
// ZERO when half would fall under [surfaceMemLimitFloor].
//
// THE SMALLEST BOUND IS THE ONE THAT BINDS. The caller passes every bound it
// could read — physical memory, and the cgroup the process runs in — and a bound
// that could not be read is a zero that does not participate. Inside a container
// the machine's PHYSICAL memory is the HOST's, so a limit drawn from it alone
// lands far above what the process may actually use: it never binds, and the
// kernel OOM-kills instead of the collector working, which is the usual reason
// to set GOMEMLIMIT at all. Taking the minimum makes the limit follow whichever
// bound is really this process's.
//
// ZERO IS THE REFUSAL — the caller sets nothing — and it is the answer when no
// bound could be read at all as well as for one too small to bound. An unread
// bound is not a licence to guess: the only safe guess would be a large one, and
// a large one is the fixed ceiling the tuner's own comment rejects.
func surfaceMemoryLimit(bounds ...int64) int64 {
	smallest := int64(0)
	for _, bound := range bounds {
		if bound <= 0 {
			continue
		}
		if smallest == 0 || bound < smallest {
			smallest = bound
		}
	}
	if smallest == 0 {
		return 0
	}
	limit := smallest / surfaceMemLimitDivisor
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

// hostCgroupMemoryLimit is the tightest finite memory bound the cgroup this
// process runs in places on it, in bytes, or zero when the hierarchy places no
// bound to give — no cgroup files, every file `max`, a value that will not read
// as a number, or the cgroup v1 "no limit" sentinel.
//
// IT IS A VARIABLE FOR THE SAME REASON [hostTotalMemory] IS: a test stands a
// bound in front of [tuneForTheSurface] without owning a cgroup to be in. The
// reader is the per-OS half — [readCgroupMemoryLimit] in memlimit_linux.go, and
// a zero on the platforms with no cgroup hierarchy to read.
var hostCgroupMemoryLimit = readCgroupMemoryLimit
