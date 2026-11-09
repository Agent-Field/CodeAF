package session

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Contract 3: a process whose stop never completes must remain unproven in
// both the model's kill result and the person's stop receipt and notes.
func TestContract3UnkillableJobIsNeverReportedStopped(t *testing.T) {
	for _, person := range []bool{false, true} {
		t.Run(map[bool]string{false: "model", true: "person"}[person], func(t *testing.T) {
			target := &job{id: 1, done: make(chan struct{}), stop: func() {}, sink: &jobSink{}}
			jobs := &jobRegistry{jobs: []*job{target}}
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // No grace can prove death; both signal rounds still run.
			if person {
				a := &Agent{}
				line, result := a.stopJob(ctx, jobs, target)
				if result != jobStopUnproven || strings.HasPrefix(line, "stopped ") {
					t.Fatalf("stop = %q, %v; want unproven", line, result)
				}
				if notes := sessionNotes(a); len(notes) != 0 {
					t.Fatalf("unverified stop sent a completion note: %v", notes)
				}
			} else {
				line, _ := jobs.killFor(ctx, target.id)
				if strings.Contains(line, "killed") || !strings.Contains(line, "had not ended") {
					t.Fatalf("kill = %q; want an unproven ending", line)
				}
			}
			if !target.running() {
				t.Fatal("unkillable job no longer reports running")
			}
		})
	}
}

// Contracts 1 and 3: a managed row must still accept stop after its leader has
// exited when the platform reports surviving descendants in that launch.
func TestContract1ManagedJobWaitsForItsTreeAfterLeaderExit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	original := waitJobTree
	waitJobTree = func(int, time.Duration) error { close(entered); <-release; return nil }
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		waitJobTree = original
	})
	target := &job{cmd: &exec.Cmd{Process: &os.Process{Pid: 4242}}, sink: &jobSink{}, done: make(chan struct{})}
	go target.settle(0)
	select {
	case <-entered:
	case <-target.done:
		t.Fatal("managed job ended while its launch tree was still alive")
	case <-time.After(5 * time.Second):
		t.Fatal("job did not ask the platform about its tree")
	}
	if !target.running() || !target.requestKill() {
		t.Fatal("managed row cannot stop its surviving descendants")
	}
	close(release)
	select {
	case <-target.done:
	case <-time.After(5 * time.Second):
		t.Fatal("empty launch tree did not finish the job")
	}
	if target.running() || !target.settledKilled() {
		t.Fatal("verified stop did not settle the managed launch")
	}
}
