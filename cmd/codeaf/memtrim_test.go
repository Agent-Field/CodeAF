package main

import (
	"testing"
	"time"
)

// A BURST OF WORK LEAVES THE HEAP WITH MEMORY IT WILL NOT NEED AGAIN FOR HOURS:
// the collector is set to wait for five times the live heap, and the runtime
// hands pages back to the system only slowly. The trimmer gives them back once
// the held-but-unused share is large, and no more often than its cooldown.
func TestHeapTrimmerReturnsOnlyAnExcessAndOnlyOncePerCooldown(t *testing.T) {
	const mib = 1 << 20
	clock := time.Unix(1000, 0)
	var reading heapReading
	trims := 0
	tr := &heapTrimmer{
		read: func() heapReading { return reading },
		free: func() { trims++ },
		now:  func() time.Time { return clock },
	}
	step := func(live, held uint64) {
		reading = heapReading{Live: live * mib, Held: held * mib}
		tr.step()
	}

	step(60, 100) // 40 MiB over what is live: not worth a collection
	if trims != 0 {
		t.Fatalf("trimmed %d times for a small excess", trims)
	}
	step(60, 400) // 340 MiB over
	if trims != 1 {
		t.Fatalf("trimmed %d times for a large excess, want 1", trims)
	}
	step(60, 400)
	if trims != 1 {
		t.Fatalf("trimmed again inside the cooldown (%d)", trims)
	}
	clock = clock.Add(heapTrimCooldown)
	step(60, 400)
	if trims != 2 {
		t.Fatalf("did not trim after the cooldown (%d)", trims)
	}
	clock = clock.Add(heapTrimCooldown)
	step(400, 600) // a big live heap carries proportionally more
	if trims != 2 {
		t.Fatalf("trimmed a heap whose excess is under half its live size (%d)", trims)
	}
}

// THE COLLECTOR WAITS FOR A FIXED NUMBER OF BYTES OF GARBAGE, NOT FOR A MULTIPLE
// OF THE LIVE HEAP. A multiple is generous for a small heap and ruinous for a
// large one: five times 120 MiB is 600 MiB of garbage the process holds, and on
// a system that takes freed pages back lazily the resident set shows all of it.
// The percent follows the live heap so the headroom stays about the same.
func TestHeapGovernorKeepsTheHeadroomNearlyConstant(t *testing.T) {
	const mib = 1 << 20
	for _, tc := range []struct {
		live uint64
		want int
	}{
		{0, heapGCMax},
		{5, heapGCMax}, // a small heap keeps the surface's generous multiple
		{80, 40},
		{200, 16},
		{4000, heapGCMin},
	} {
		if got := heapGCPercent(tc.live * mib); got != tc.want {
			t.Errorf("heapGCPercent(%d MiB) = %d, want %d", tc.live, got, tc.want)
		}
	}

	var set []int
	tr := &heapTrimmer{
		read:    func() heapReading { return heapReading{Live: 80 * mib} },
		free:    func() {},
		now:     time.Now,
		percent: func(p int) { set = append(set, p) },
	}
	tr.step()
	tr.step() // the same live heap does not set it again
	if len(set) != 1 || set[0] != 40 {
		t.Fatalf("percent set %v, want [40] once", set)
	}
	tr.read = func() heapReading { return heapReading{Live: 84 * mib} } // 38: within a fifth of 40
	tr.step()
	if len(set) != 1 {
		t.Fatalf("a small move of the live heap set the percent again: %v", set)
	}
	tr.read = func() heapReading { return heapReading{Live: 200 * mib} }
	tr.step()
	if len(set) != 2 || set[1] != 16 {
		t.Fatalf("percent set %v, want a second setting of 16", set)
	}
}
