package exec

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// testGovernor drives the gate off a settable reading and a settable clock, so
// every assertion is about the policy rather than about the host running it.
func testGovernor(load *float64, ok *bool, now *time.Time) *Governor {
	return newGovernor(
		func() (float64, bool) { return *load, *ok },
		func() time.Time { return *now },
	)
}

// calmGovernor and saturatedGovernor are the two host readings the scheduler
// tests need. Every scheduler test installs one of them: the process-wide gate
// reads the real machine, so a suite that left it in place would pass or hang
// depending on what else happened to be running while it ran.
func calmGovernor() *Governor {
	return NewGovernorFrom(func() (float64, bool) { return GovernorLoadResume / 2, true })
}

func saturatedGovernor() *Governor {
	return NewGovernorFrom(func() (float64, bool) { return GovernorLoadCeiling * 10, true })
}

func TestGovernorAdmitsUnderTheCeilingAndHoldsOverIt(t *testing.T) {
	load, ok := 0.4, true
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now)

	if !governor.Admit(2) {
		t.Fatal("an idle machine refused a claim")
	}
	load = GovernorLoadCeiling + 0.5
	now = now.Add(governorSampleTTL)
	if governor.Admit(2) {
		t.Fatal("a saturated machine admitted a claim")
	}
}

func TestGovernorHysteresisHoldsThroughTheBand(t *testing.T) {
	load, ok := GovernorLoadCeiling+0.5, true
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now)
	if governor.Admit(2) {
		t.Fatal("a saturated machine admitted a claim")
	}

	// Inside the band — below the ceiling, above the resume edge — the hold
	// stands. Releasing here is what makes the gate flap.
	load = (GovernorLoadCeiling + GovernorLoadResume) / 2
	now = now.Add(governorSampleTTL)
	if governor.Admit(2) {
		t.Fatal("the hold released inside the hysteresis band")
	}

	load = GovernorLoadResume - 0.01
	now = now.Add(governorSampleTTL)
	if !governor.Admit(2) {
		t.Fatal("the hold survived below the resume edge")
	}
}

func TestGovernorAdmitsWhenNothingIsRunning(t *testing.T) {
	load, ok := 99.0, true
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now)

	// Someone else's load must never leave aforge running nothing at all.
	if !governor.Admit(0) {
		t.Fatal("a fully saturated machine starved the runner of its first leaf")
	}
	if governor.Admit(1) {
		t.Fatal("the starvation guard admitted a second leaf")
	}
}

func TestGovernorNeverGatesWhenTheHostCannotAnswer(t *testing.T) {
	load, ok := 0.0, false
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now)
	if !governor.Admit(4) {
		t.Fatal("a platform that reports no pressure was treated as saturated")
	}
}

func TestGovernorSamplesAtMostOncePerTTL(t *testing.T) {
	load, ok := 0.2, true
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	samples := 0
	governor := newGovernor(
		func() (float64, bool) { samples++; return load, ok },
		func() time.Time { return now },
	)
	for range 20 {
		governor.Admit(2)
	}
	if samples != 1 {
		t.Fatalf("the host was read %d times inside one TTL, want 1", samples)
	}
	now = now.Add(governorSampleTTL)
	governor.Admit(2)
	if samples != 2 {
		t.Fatalf("the host was read %d times after the TTL, want 2", samples)
	}
}

func TestHostLoadPerCoreIsAPlausibleReading(t *testing.T) {
	load, ok := hostLoadPerCore()
	if !ok {
		t.Skip("this platform reports no load average")
	}
	if load < 0 || load > 1000 {
		t.Fatalf("host load per core is %f, which cannot be a real reading", load)
	}
}

func TestBackgroundJobYieldsTheInteractiveMachine(t *testing.T) {
	var pgid, priority atomic.Int64
	original := setProcessGroupPriority
	setProcessGroupPriority = func(group, value int) {
		pgid.Store(int64(group))
		priority.Store(int64(value))
		original(group, value)
	}
	t.Cleanup(func() { setProcessGroupPriority = original })

	tools, _ := backgroundToolbox(t)
	result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 30","bg":true}`)
	if result.IsError {
		t.Fatalf("background start failed: %s", result.Content)
	}
	// The real syscall is not asserted: a sandbox may forbid renicing, and the
	// job must start either way. What is asserted is that the whole detached
	// group is the thing asked to yield, and by how much.
	tools.jobs.mutex.Lock()
	job := tools.jobs.jobs[1]
	tools.jobs.mutex.Unlock()
	if job == nil {
		t.Fatal("job 1 not registered")
	}
	if got := int(pgid.Load()); got != job.cmd.Process.Pid {
		t.Fatalf("renice targeted %d, want the job's process group %d", got, job.cmd.Process.Pid)
	}
	if got := int(priority.Load()); got != backgroundJobNice {
		t.Fatalf("background job priority = %d, want %d", got, backgroundJobNice)
	}
}
