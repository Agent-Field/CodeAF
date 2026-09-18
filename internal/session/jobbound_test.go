package session

// THE STORM THAT SPENT NO TOKENS. A run whose model spawns busy loops as bash
// jobs (`yes`, `awk 'BEGIN{for(;;){}}'`) burns no provider calls, so nothing in
// the run's dollar bound ever fires while the box is pegged. #1158 bounds a
// run's MEMORY and nothing bounds its CPU or its process count.
//
// Two halves, applying in different places (see jobbound.go): the PROCESS
// backstop always, and the CPU rate rule only where NOTHING else caps the
// process — because under a cgroup quota the quota is already the ceiling and a
// CPU rule would cut an honest `go test` inside a cell.
//
// ── NOTHING HERE MAKES LOAD ──
//
// The storm is a sequence of numbers. The subtree's usage reader and the clock
// are the code's own seams ([jobSubtreeUsage], [jobBoundNow]), and the machine
// and effective cores are fields on the bound, so the whole test runs injected
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

// boundWith is the bound as it would be drawn on a machine with `machine` cores
// where this subtree may actually use `effective` of them — the seam that stands
// a machine and a quota in front of the ceiling without a cgroup to be under.
// effective below machine is a quota (or affinity) that BINDS.
func boundWith(effective, machine float64) *subtreeBound {
	bound := newSubtreeBound()
	bound.machineCores = machine
	bound.effectiveCores = effective
	return bound
}

// newTestBound is the bound on the unconstrained box the numbers were measured
// on: 20 cores, nothing capping it, so the CPU ceiling is 12 cores (three fifths
// of 20), above the honest 8.5 and below the storm's 16.
func newTestBound() *subtreeBound {
	return boundWith(20, 20)
}

// long is enough readings to cross the thirty-second window (jobBoundStrikes at
// jobBoundInterval) with room to spare.
const long = jobBoundStrikes + 5

// TestAJobSubtreeOverItsBoundIsCutAndTheModelIsTold is the load-bearing half on
// an unconstrained box: the storm is cut, and the run hears about it in the
// bound's own voice rather than a silent kill.
func TestAJobSubtreeOverItsBoundIsCutAndTheModelIsTold(t *testing.T) {
	var notes []string
	registry := &jobRegistry{notify: func(note string) { notes = append(notes, note) }}
	one := &job{id: 7, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}

	subtreeReadings(t, storm(16, 16, long, time.Second), time.Second)

	if !driveBound(registry, one, newTestBound(), long) {
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

// TestAnHonestVerifyOnAnUnconstrainedBoxIsNotCut is the other half of the
// unquota'd rule: a max-tier verify measured 34 processes and 8.5 cores at once,
// and that must survive untouched.
func TestAnHonestVerifyOnAnUnconstrainedBoxIsNotCut(t *testing.T) {
	var notes []string
	registry := &jobRegistry{notify: func(note string) { notes = append(notes, note) }}
	one := &job{id: 3, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}

	subtreeReadings(t, storm(8.5, 34, long, time.Second), time.Second)

	if driveBound(registry, one, newTestBound(), long) {
		t.Fatal("an honest parallel verify of 34 processes and 8.5 cores was cut")
	}
	if one.killRequested {
		t.Fatal("a kill was requested for an honest parallel verify")
	}
	if len(notes) != 0 {
		t.Fatalf("an honest run was handed %d records, want none: %q", len(notes), notes)
	}
}

// TestASustainedStormOnAnUnconstrainedBoxIsCut pins the thirty-second window: a
// subtree over the ceiling for the whole window is cut.
func TestASustainedStormOnAnUnconstrainedBoxIsCut(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(13, 8, long, time.Second), time.Second)
	if !driveBound(registry, one, newTestBound(), long) {
		t.Fatal("a 13-core storm sustained past the window never tripped on an unconstrained box")
	}
}

// TestAShortBurstOnAnUnconstrainedBoxIsNotCut is why the window is thirty
// seconds: an honest build can pass twelve cores for several seconds, and a
// burst shorter than the window must not be cut.
func TestAShortBurstOnAnUnconstrainedBoxIsNotCut(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	burst := jobBoundStrikes - 3 // over the ceiling, but for fewer than the window's readings
	subtreeReadings(t, storm(13, 8, burst, time.Second), time.Second)
	if driveBound(registry, one, newTestBound(), burst) {
		t.Fatal("a 13-core burst shorter than the thirty-second window was cut")
	}
}

// TestAFullQuotaStormIsNotCutByTheCPURule is the correction's heart: inside a
// four-core cell the quota is already the ceiling, so a subtree using all four
// cores — which is what an honest `go test` there does too — is NOT cut by the
// CPU rule. #1187's park bound is what returns a turn wedged on such a job.
func TestAFullQuotaStormIsNotCutByTheCPURule(t *testing.T) {
	var notes []string
	registry := &jobRegistry{notify: func(note string) { notes = append(notes, note) }}
	one := &job{id: 4, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}

	subtreeReadings(t, storm(4.0, 8, long, time.Second), time.Second)

	if driveBound(registry, one, boundWith(4, 20), long) {
		t.Fatal("a subtree at its four-core quota was cut by the CPU rule: under a binding quota the CPU rule must be off")
	}
	if one.killRequested {
		t.Fatal("a kill was requested for a subtree inside its quota")
	}
	if len(notes) != 0 {
		t.Fatalf("a quota'd subtree was handed %d records, want none: %q", len(notes), notes)
	}
}

// TestAProcessStormUnderAQuotaIsCutByTheBackstop is the half that still applies
// under a quota: a fork storm, which a CPU quota does nothing to, is caught by
// the process backstop.
func TestAProcessStormUnderAQuotaIsCutByTheBackstop(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(0.5, 200, long, time.Second), time.Second)
	if !driveBound(registry, one, boundWith(4, 20), long) {
		t.Fatal("a 200-process storm under a four-core quota was not cut by the process backstop")
	}
}

// TestAForkStormIsCutEvenWithLittleCPU is the process backstop on an
// unconstrained box: a fork that has not yet accumulated CPU, caught by the
// count alone.
func TestAForkStormIsCutEvenWithLittleCPU(t *testing.T) {
	registry := &jobRegistry{}
	one := &job{id: 1, kind: jobKindBash, state: jobRunning, done: make(chan struct{})}
	subtreeReadings(t, storm(0.5, 5000, long, time.Second), time.Second)
	if !driveBound(registry, one, newTestBound(), long) {
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
	if driveBound(registry, one, newTestBound(), long) {
		t.Fatal("a subtree whose usage could not be read was cut: silence is not pressure")
	}
}
