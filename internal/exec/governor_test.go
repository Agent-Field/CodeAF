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

// The regression this file exists for. An API-bound leaf is a goroutine parked
// on a socket: it puts no load on this machine, so a machine ten times over its
// load ceiling is no evidence about it whatsoever. Measured before the fix,
// gating this class on load average pinned live concurrency at three leaves in
// seven of eight runs — the starvation floor was the only admission that ever
// happened, on a sixteen-core host that was idle.
//
// The assertion is not only that the leaves are admitted; it is that the host
// is never even asked. A reading taken here is a reading that can gate here.
func TestAPIBoundLeavesAdmitOnAnyHostReadingAndNeverAskTheHost(t *testing.T) {
	samples := 0
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	governor := newGovernor(
		func() (float64, bool) { samples++; return 99.0, true },
		func() time.Time { return now },
	)
	for inFlight := range GovernorInFlightCeiling {
		if !governor.Admit(inFlight) {
			t.Fatalf("a socket-parked leaf was refused at %d in flight on an idle machine's behalf", inFlight)
		}
	}
	if samples != 0 {
		t.Fatalf("host load was read %d times deciding an API-bound leaf, want never", samples)
	}
}

// The backstop is the only bound on that class, and it is a backstop: it names
// a resource this process really can run out of — handles, goroutines — at a
// width no honest plan reaches.
func TestTheBackstopIsTheOnlyCeilingOnAPIBoundLeaves(t *testing.T) {
	governor := calmGovernor()
	if !governor.Admit(GovernorInFlightCeiling - 1) {
		t.Fatal("the leaf under the backstop was refused")
	}
	if governor.Admit(GovernorInFlightCeiling) {
		t.Fatal("the backstop let a leaf through above itself")
	}
	if GovernorInFlightCeiling <= GovernorLocalFloor*4 {
		t.Fatalf("the backstop is %d, which is a scheduler rather than a backstop",
			GovernorInFlightCeiling)
	}
	// A nil gate is not a closed one: the runner that never installed a
	// governor still runs work.
	var absent *Governor
	if !absent.Admit(1) {
		t.Fatal("a nil governor refused a claim")
	}
}

// Local work is the other class, and for it nothing about the old doctrine
// changed: a swe leaf spawns real compilers and real test binaries, which is
// what the load average was always actually measuring.
func TestLocalWorkStillHoldsOverTheLoadCeiling(t *testing.T) {
	load, ok := 0.4, true
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	// A generous cap, so what is asserted below is the load doctrine rather
	// than the core count of whatever machine runs the suite.
	governor := testGovernor(&load, &ok, &now).WithLocalCap(64)

	if !governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor) {
		t.Fatal("an idle machine refused a compile")
	}
	load = GovernorLoadCeiling + 0.5
	now = now.Add(governorSampleTTL)
	if governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor) {
		t.Fatal("a saturated machine admitted a compile")
	}
	// And the same saturated machine still admits the socket-parked leaf
	// behind it, which is the whole point of splitting the classes.
	if !governor.Admit(GovernorLocalFloor) {
		t.Fatal("a busy compile throttled an API-bound leaf")
	}
}

func TestLocalWorkHysteresisHoldsThroughTheBand(t *testing.T) {
	load, ok := GovernorLoadCeiling+0.5, true
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now).WithLocalCap(64)
	if governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor) {
		t.Fatal("a saturated machine admitted a compile")
	}

	// Inside the band — below the ceiling, above the resume edge — the hold
	// stands. Releasing here is what makes the gate flap.
	load = (GovernorLoadCeiling + GovernorLoadResume) / 2
	now = now.Add(governorSampleTTL)
	if governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor) {
		t.Fatal("the hold released inside the hysteresis band")
	}

	load = GovernorLoadResume - 0.01
	now = now.Add(governorSampleTTL)
	if !governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor) {
		t.Fatal("the hold survived below the resume edge")
	}
}

// The floor is a floor: someone else's compile must never leave aforge running
// nothing at all. It is emphatically not a ceiling, which is what it silently
// became when every class was gated through it.
func TestLocalWorkAdmitsUpToTheFloorHoweverSaturatedTheHostIs(t *testing.T) {
	load, ok := 99.0, true
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now)

	for local := range GovernorLocalFloor {
		if !governor.AdmitLocal(local, local) {
			t.Fatalf("a fully saturated machine starved local work at %d in flight", local)
		}
	}
	if governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor) {
		t.Fatal("the floor admitted a compile above itself on a saturated machine")
	}
}

// Genuine host work is counted against the host's own cores even when the host
// says it is calm — a load average is a minute old, and eight compiles started
// in one pass are eight compiles before it moves at all.
func TestLocalWorkClampsAtTheCPUDerivedCap(t *testing.T) {
	if LocalLeafCap() < GovernorLocalFloor {
		t.Fatalf("the CPU-derived cap is %d, below the starvation floor %d",
			LocalLeafCap(), GovernorLocalFloor)
	}
	const cores = GovernorLocalFloor + 2
	governor := calmGovernor().WithLocalCap(cores)
	if !governor.AdmitLocal(cores-1, cores-1) {
		t.Fatal("a calm machine refused a compile under its core count")
	}
	if governor.AdmitLocal(cores, cores) {
		t.Fatalf("a calm machine admitted a %dth compile on %d cores", cores+1, cores)
	}
	// The cap belongs to its own class. The same governor, asked about the
	// leaves that only hold sockets, is unmoved by how many compiles are out.
	if !governor.Admit(cores) {
		t.Fatal("the CPU-derived cap leaked onto API-bound leaves")
	}
}

// The backstop binds every class: a compile needs a handle and a goroutine too.
func TestTheBackstopBindsLocalWorkAsWell(t *testing.T) {
	governor := calmGovernor().WithLocalCap(GovernorInFlightCeiling * 4)
	if governor.AdmitLocal(GovernorInFlightCeiling, 1) {
		t.Fatal("the backstop let a compile through above itself")
	}
}

func TestGovernorNeverGatesWhenTheHostCannotAnswer(t *testing.T) {
	load, ok := 0.0, false
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	governor := testGovernor(&load, &ok, &now).WithLocalCap(64)
	if !governor.AdmitLocal(4, 4) {
		t.Fatal("a platform that reports no pressure was treated as saturated")
	}
}

func TestGovernorSamplesAtMostOncePerTTL(t *testing.T) {
	load, ok := 0.2, true
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	samples := 0
	governor := newGovernor(
		func() (float64, bool) { samples++; return load, ok },
		func() time.Time { return now },
	)
	for range 20 {
		governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor)
	}
	if samples != 1 {
		t.Fatalf("the host was read %d times inside one TTL, want 1", samples)
	}
	now = now.Add(governorSampleTTL)
	governor.AdmitLocal(GovernorLocalFloor, GovernorLocalFloor)
	if samples != 2 {
		t.Fatalf("the host was read %d times after the TTL, want 2", samples)
	}
}

// Which workers count as local work is the whole of the classification, so it
// is asserted rather than assumed. The generalist is a socket; swe is a shell.
func TestLocalWorkNamesTheWorkerThatSpawnsProcesses(t *testing.T) {
	for _, name := range []string{SWESubharness, " SWE ", "Swe"} {
		if !LocalWorkSubharness(name) {
			t.Errorf("%q is not classified as local work, but it spawns compilers", name)
		}
	}
	for _, name := range []string{LinearSubharness, "", "  ", "research"} {
		if LocalWorkSubharness(name) {
			t.Errorf("%q was classified as local work; it is a leaf parked on a socket", name)
		}
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
