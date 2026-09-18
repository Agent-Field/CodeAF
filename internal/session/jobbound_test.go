package session

// THE STORM THAT SPENT NO TOKENS. A run whose model spawns busy loops as bash
// jobs (`yes`, `awk 'BEGIN{for(;;){}}'`) burns no provider calls, so nothing in
// the run's dollar bound ever fires while the box is pegged. #1158 bounds a
// run's MEMORY and nothing bounds its CPU or its process count.
//
// The bound under test has to separate two shapes the numbers already told
// apart: an honest parallel verify (34 processes, 8.5 cores at once) must
// survive, and a spinner storm (16 loops, then 64) must be cut — even though 16
// busy loops are FEWER processes than the honest peak. CPU rate is what
// separates them, and it is a rate across a stretch rather than one reading.
//
// ── NOTHING HERE MAKES LOAD ──
//
// The storm is a sequence of numbers. The subtree's usage reader and the clock
// are the code's own seams ([jobSubtreeUsage], [jobBoundNow]), and the cores the
// ceiling is drawn from are a field on the bound, so the whole test runs injected
// readings with no `yes`, no `awk`, no cgroup and no subprocess at all — on a
// shared box, a bound proved by real load is the wrong proof and a danger to
// whoever else is working.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// subtreeReadings swaps the bound's usage reader and clock for scripted ones.
// Each read hands back the next reading and advances the clock by one step, so
// the rate the bound computes is exactly the scripted one and no wall time
// stands in for causality.
func subtreeReadings(t *testing.T, readings []processgroup.Usage, step time.Duration) {
	t.Helper()
	originalUsage, originalNow := jobSubtreeUsage, jobBoundNow
	t.Cleanup(func() { jobSubtreeUsage, jobBoundNow = originalUsage, originalNow })
	at := time.Unix(0, 0)
	index := 0
	jobBoundNow = func() time.Time { return at }
	jobSubtreeUsage = func(processgroup.Group) (processgroup.Usage, bool) {
		usage := readings[len(readings)-1]
		if index < len(readings) {
			usage = readings[index]
			index++
		}
		at = at.Add(step)
		return usage, true
	}
}

// storm is a subtree that holds `cores` cores and `processes` processes for
// `samples` readings. Cumulative CPU climbs by cores×step each step, which is
// what a busy loop looks like from the outside and what the bound measures.
func storm(cores float64, processes, samples int, step time.Duration) []processgroup.Usage {
	readings := make([]processgroup.Usage, samples)
	for i := range readings {
		readings[i] = processgroup.Usage{
			Processes:  processes,
			CPUSeconds: cores * step.Seconds() * float64(i),
		}
	}
	return readings
}

// driveBound takes reading after reading until the bound trips or the script is
// spent, and answers whether it tripped.
func driveBound(registry *jobRegistry, one *job, bound *subtreeBound, samples int) bool {
	for i := 0; i < samples; i++ {
		if registry.boundStep(one, bound) {
			return true
		}
	}
	return false
}

// boundWith is the bound as it would be drawn on a machine with the given
// effective cores — the seam that stands a core count in front of the ceiling
// without a cgroup to be under.
func boundWith(cores float64) *subtreeBound {
	bound := newSubtreeBound()
	bound.effectiveCores = cores
	return bound
}

// newTestBound is the bound as the box the numbers were measured on would draw
// it: 20 effective cores, so the CPU ceiling is 12 cores (three fifths of 20),
// which is above the honest 8.5 and below the storm's 16.
func newTestBound() *subtreeBound {
	return boundWith(20)
}

// TestAJobSubtreeOverItsBoundIsCutAndTheModelIsTold is the load-bearing half:
// the storm is cut, and the run hears about it in the bound's own voice rather
// than a silent kill.
func TestAJobSubtreeOverItsBoundIsCutAndTheModelIsTold(t *testing.T) {
	var notes []string
	registry := &jobRegistry{notify: func(note string) { notes = append(notes, note) }}
	one := &job{id: 7, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}

	subtreeReadings(t, storm(16, 16, 8, time.Second), time.Second)

	if !driveBound(registry, one, newTestBound(), 8) {
		t.Fatal("a 16-loop storm never tripped the bound: 16 cores is above the honest peak and must be cut")
	}
	if !one.killRequested {
		t.Fatal("the storm was not cut: no kill was requested for the subtree")
	}
	if len(notes) != 1 {
		t.Fatalf("the run was handed %d records, want exactly one", len(notes))
	}
	if !strings.Contains(notes[0], jobBoundReason) {
		t.Fatalf("the record is not in the bound's own family: %q", notes[0])
	}
	if !strings.Contains(notes[0], "job 7") {
		t.Fatalf("the record does not name the subtree it is about: %q", notes[0])
	}
}

// TestAJobSubtreeAtTheHonestPeakIsNotCut is the other half, and the half a
// number chosen for one storm alone fails: a max-tier verify measured 34
// processes and 8.5 cores at once, and that must survive untouched.
func TestAJobSubtreeAtTheHonestPeakIsNotCut(t *testing.T) {
	var notes []string
	registry := &jobRegistry{notify: func(note string) { notes = append(notes, note) }}
	one := &job{id: 3, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}

	subtreeReadings(t, storm(8.5, 34, 12, time.Second), time.Second)

	if driveBound(registry, one, newTestBound(), 12) {
		t.Fatal("an honest parallel verify of 34 processes and 8.5 cores was cut")
	}
	if one.killRequested {
		t.Fatal("a kill was requested for an honest parallel verify")
	}
	if len(notes) != 0 {
		t.Fatalf("an honest run was handed %d records, want none: %q", len(notes), notes)
	}
}

// TestTheSixtyFourLoopStormIsCut pins the larger storm the run was measured
// with: more than the machine's own cores, which no honest run reaches.
func TestTheSixtyFourLoopStormIsCut(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(20, 64, 8, time.Second), time.Second)
	if !driveBound(registry, one, newTestBound(), 8) {
		t.Fatal("a 64-loop storm never tripped the bound")
	}
}

// TestAStormIsCutUnderAFourCoreQuota is the case the machine's raw core count
// would miss, and the one that matters because it is where cells run: a cgroup
// quota holds the whole subtree to four cores, so a ceiling drawn from a 20-core
// machine (12) could never trip — a subtree capped at four cores cannot reach
// twelve. Drawn from the EFFECTIVE cores the quota allows (4), the ceiling is
// 2.4, and a storm sustained at 3.9 cores — near the quota's own ceiling, all a
// spinner storm can take there — is cut.
func TestAStormIsCutUnderAFourCoreQuota(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(3.9, 8, 8, time.Second), time.Second)
	if !driveBound(registry, one, boundWith(4), 8) {
		t.Fatal("a storm at 3.9 cores under a four-core quota never tripped: the ceiling must be drawn from the effective cores, not the machine's")
	}
	if !one.killRequested {
		t.Fatal("the storm under a four-core quota was not cut")
	}
}

// TestAnHonestBuildUnderAFourCoreQuotaIsNotCut guards the other side of the
// quota case: a build that uses about half its four cores is well under the 2.4
// ceiling and must survive.
func TestAnHonestBuildUnderAFourCoreQuotaIsNotCut(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 2, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(2.0, 20, 8, time.Second), time.Second)
	if driveBound(registry, one, boundWith(4), 8) {
		t.Fatal("an honest build at 2.0 cores under a four-core quota was cut")
	}
}

// TestAProcessStormIsCutEvenWithLittleCPU is the orthogonal storm the CPU rate
// alone would miss: a fork that has not yet accumulated CPU, caught by the
// process ceiling — which is set far above the honest peak so it is never what
// separates an honest run from the spinner storm.
func TestAProcessStormIsCutEvenWithLittleCPU(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(0.5, 5000, 8, time.Second), time.Second)
	if !driveBound(registry, one, newTestBound(), 8) {
		t.Fatal("a fork storm of 5000 processes never tripped the bound")
	}
}

// TestASubtreeThatCannotBeReadIsNeverCut is the silence rule every reader here
// keeps (task_pressure.go's own): a platform that cannot say what a subtree is
// using never holds, never cuts.
func TestASubtreeThatCannotBeReadIsNeverCut(t *testing.T) {
	originalUsage, originalNow := jobSubtreeUsage, jobBoundNow
	t.Cleanup(func() { jobSubtreeUsage, jobBoundNow = originalUsage, originalNow })
	at := time.Unix(0, 0)
	jobBoundNow = func() time.Time { at = at.Add(time.Second); return at }
	jobSubtreeUsage = func(processgroup.Group) (processgroup.Usage, bool) { return processgroup.Usage{}, false }

	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	if driveBound(registry, one, newTestBound(), 8) {
		t.Fatal("a subtree whose usage could not be read was cut: silence is not pressure")
	}
}
