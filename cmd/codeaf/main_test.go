package main

import (
	"os"
	"runtime"
	"strconv"
	"testing"
)

// The decision, on its own and without a machine: the cap for anything bigger
// than it, and the machine's own count for anything that is not — which is what
// keeps a box with fewer cores than the cap completely unaffected.
func TestSurfaceProcsCapsBigMachinesAndLeavesSmallOnesAlone(t *testing.T) {
	cases := []struct {
		ncpu, want int
	}{
		{1, 1},
		{4, 4},
		{8, 8},
		{9, surfaceMaxProcs},
		{20, surfaceMaxProcs},
		{128, surfaceMaxProcs},
	}
	for _, c := range cases {
		if got := surfaceProcs(c.ncpu); got != c.want {
			t.Errorf("surfaceProcs(%d) = %d, want %d", c.ncpu, got, c.want)
		}
	}
}

// The whole change at the seam a person can see: with the environment silent,
// a big machine's surface is capped AND the cap is put where the surface's
// separate engine host will read it at its own startup.
func TestTuneForTheSurfaceCapsTheSchedulerWhenTheEnvironmentIsSilent(t *testing.T) {
	if runtime.NumCPU() <= surfaceMaxProcs {
		t.Skipf("this machine has %d cores, at or under the cap", runtime.NumCPU())
	}
	restore := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(restore)
	runtime.GOMAXPROCS(runtime.NumCPU()) // as an uncapped launch would

	t.Setenv("GOMAXPROCS", "")
	t.Setenv("GOGC", "off") // leave the heap target to whoever tests it
	tuneForTheSurface()

	if got := runtime.GOMAXPROCS(0); got != surfaceMaxProcs {
		t.Fatalf("GOMAXPROCS = %d, want the cap %d", got, surfaceMaxProcs)
	}
	if got := os.Getenv("GOMAXPROCS"); got != strconv.Itoa(surfaceMaxProcs) {
		t.Fatalf("GOMAXPROCS in the environment = %q, want %q so the engine host inherits the cap",
			got, strconv.Itoa(surfaceMaxProcs))
	}
}

// An explicit GOMAXPROCS is the person deciding, and it wins the way an
// explicit GOGC wins over the raised heap target in the same function.
func TestTuneForTheSurfaceLeavesAnExplicitGOMAXPROCSAlone(t *testing.T) {
	restore := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(restore)
	runtime.GOMAXPROCS(3)

	t.Setenv("GOMAXPROCS", "3")
	t.Setenv("GOGC", "off")
	tuneForTheSurface()

	if got := runtime.GOMAXPROCS(0); got != 3 {
		t.Fatalf("GOMAXPROCS = %d, want the explicit 3 left alone", got)
	}
	if got := os.Getenv("GOMAXPROCS"); got != "3" {
		t.Fatalf("GOMAXPROCS in the environment = %q, want the explicit 3 left alone", got)
	}
}
