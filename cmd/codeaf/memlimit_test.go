package main

import (
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
)

// The derivation, on its own and without a machine: half of a machine bigger
// than the floor, and NOTHING for a machine whose half would fall under it — the
// refusal that keeps a limit from ever being set below the live heap. Zero is
// also the answer for a machine whose memory could not be read at all, which is
// not a licence to guess a large number instead.
func TestSurfaceMemoryLimitHalvesAMachineAndRefusesOneUnderTheFloor(t *testing.T) {
	cases := []struct {
		name  string
		total int64
		want  int64
	}{
		{"unreadable", 0, 0},
		{"negative", -1, 0},
		{"256 MiB, half under the floor", 256 << 20, 0},
		{"one byte under twice the floor, refused", 2*surfaceMemLimitFloor - 1, 0},
		{"exactly twice the floor", 2 * surfaceMemLimitFloor, surfaceMemLimitFloor},
		{"16 GiB", 16 << 30, 8 << 30},
	}
	for _, c := range cases {
		if got := surfaceMemoryLimit(c.total); got != c.want {
			t.Errorf("surfaceMemoryLimit(%d) = %d, want %d (%s)", c.total, got, c.want, c.name)
		}
	}
}

// standInForMachine puts a machine of totalBytes in front of the tuner for the
// length of one test, and hands back the scheduler and heap readouts untouched:
// the memory tests are not the scheduler's and not the heap target's, so the
// tuner is given a silent GOMAXPROCS and GOGC=off and this restores the reader.
func standInForMachine(t *testing.T, totalBytes int64) {
	t.Helper()
	old := hostTotalMemory
	hostTotalMemory = func() int64 { return totalBytes }
	t.Cleanup(func() { hostTotalMemory = old })
	t.Setenv("GOMAXPROCS", strconv.Itoa(runtime.NumCPU()))
	t.Setenv("GOGC", "off")
}

// restoreMemoryLimit leaves the process's soft limit as the test found it, which
// is what "the test leaves the process as it found it" means for the one piece
// of global state these tests move.
func restoreMemoryLimit(t *testing.T) {
	t.Helper()
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(before) })
}

// An explicit GOMEMLIMIT is the person deciding, and it wins untouched — the
// variable is not rewritten and the runtime's limit is not moved, exactly the
// way an explicit GOGC and GOMAXPROCS win in the same function.
func TestTuneForTheSurfaceLeavesAnExplicitGOMEMLIMITAlone(t *testing.T) {
	restoreMemoryLimit(t)
	// A distinctive sentinel, so "the tuner touched nothing" is visible: an
	// untouched limit stays the number this test set, not the derived one.
	sentinel := int64(3 << 30)
	debug.SetMemoryLimit(sentinel)

	standInForMachine(t, 16<<30)
	t.Setenv("GOMEMLIMIT", "3GiB")
	tuneForTheSurface()

	if got := os.Getenv("GOMEMLIMIT"); got != "3GiB" {
		t.Fatalf("GOMEMLIMIT in the environment = %q, want the explicit 3GiB left alone", got)
	}
	if got := debug.SetMemoryLimit(-1); got != sentinel {
		t.Fatalf("the runtime memory limit = %d, want the explicit %d left alone", got, sentinel)
	}
}

// With the environment silent, a machine bigger than the floor gets a limit, and
// the limit is put where the surface's separate engine host will read it at its
// own startup.
func TestTuneForTheSurfaceSetsASoftMemoryLimitWhenTheEnvironmentIsSilent(t *testing.T) {
	restoreMemoryLimit(t)
	standInForMachine(t, 16<<30)
	t.Setenv("GOMEMLIMIT", "")

	want := surfaceMemoryLimit(16 << 30)
	tuneForTheSurface()

	if got := debug.SetMemoryLimit(-1); got != want {
		t.Fatalf("the runtime memory limit = %d, want the derived %d", got, want)
	}
	if got := os.Getenv("GOMEMLIMIT"); got != strconv.FormatInt(want, 10) {
		t.Fatalf("GOMEMLIMIT in the environment = %q, want %q so the engine host inherits the limit",
			got, strconv.FormatInt(want, 10))
	}
}

// The floor case: a machine whose half would fall under the floor gets NOTHING,
// because a limit under the live heap makes the collector thrash and is worse
// than the growth it was added to prevent. The variable is left unset as well,
// so nothing dangerous is carried to the engine host either.
func TestTuneForTheSurfaceSetsNothingOnAMachineUnderTheFloor(t *testing.T) {
	restoreMemoryLimit(t)
	// The runtime's own zero-ish limit, where an unset GOMEMLIMIT leaves it.
	debug.SetMemoryLimit(math.MaxInt64)

	standInForMachine(t, 256<<20)
	t.Setenv("GOMEMLIMIT", "")
	tuneForTheSurface()

	if got := os.Getenv("GOMEMLIMIT"); got != "" {
		t.Fatalf("GOMEMLIMIT in the environment = %q, want nothing set on a machine under the floor", got)
	}
	if got := debug.SetMemoryLimit(-1); got != math.MaxInt64 {
		t.Fatalf("the runtime memory limit = %d, want the runtime's own %d where nothing is set", got, int64(math.MaxInt64))
	}
}
