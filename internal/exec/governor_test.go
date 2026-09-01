package exec

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// The regression this file exists for. A leaf is a goroutine parked on a
// socket: it puts no load on this machine, so what this machine's load average
// says is no evidence about it whatsoever. Measured before the fix, gating
// leaves on load average pinned live concurrency at three in seven of eight
// runs — a starvation floor was the only admission that ever happened, on a
// sixteen-core host that was idle.
//
// The law is stronger than "the leaves are admitted": the host is not asked at
// all, and there is nothing here that could ask it. A reading taken here is a
// reading that can gate here, so the absence is the assertion.
func TestALeafIsAdmittedWithoutTheHostBeingAskedAnything(t *testing.T) {
	governor := NewGovernor()
	for inFlight := range GovernorInFlightCeiling {
		if !governor.Admit(inFlight) {
			t.Fatalf("a socket-parked leaf was refused at %d in flight on an idle machine's behalf", inFlight)
		}
	}
	source, err := os.ReadFile("governor.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, reading := range []string{"loadAverage", "NumCPU", "getloadavg", "/proc/loadavg"} {
		if strings.Contains(string(source), reading) {
			t.Errorf("the admission gate reads %s — a reading taken here is a reading that can gate here, "+
				"and every leaf this process runs is a socket rather than a core", reading)
		}
	}
}

// The backstop is the only bound, and it is a backstop: it names a resource
// this process really can run out of — handles, goroutines — at a width no
// honest plan reaches.
func TestTheBackstopIsTheOnlyCeilingOnLeaves(t *testing.T) {
	governor := NewGovernor()
	if !governor.Admit(GovernorInFlightCeiling - 1) {
		t.Fatal("the leaf under the backstop was refused")
	}
	if governor.Admit(GovernorInFlightCeiling) {
		t.Fatal("the backstop let a leaf through above itself")
	}
	if GovernorInFlightCeiling <= 12 {
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
