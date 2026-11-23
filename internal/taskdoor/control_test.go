package taskdoor_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// Contract 12 requires the original request and running admission to be on
// disk before any executor action, even if the origin never reads started.
func TestAdmissionIsSavedBeforeTheExecutorAndSurvivesALostReply(t *testing.T) {
	r := newRig(t)
	entered := make(chan struct{})
	problem := make(chan error, 1)
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		var saved taskdoor.Start
		raw, err := os.ReadFile(filepath.Join(a.Dir, "request.json"))
		if err == nil {
			err = json.Unmarshal(raw, &saved)
		}
		if err != nil {
			problem <- err
			return err
		}
		if saved.Instruction != request().Instruction || saved.Input.Head != request().Input.Head {
			t.Error("executor saw a different saved request")
		}
		var state taskdoor.State
		raw, err = os.ReadFile(filepath.Join(a.Dir, "state.json"))
		if err == nil {
			err = json.Unmarshal(raw, &state)
		}
		if err != nil {
			problem <- err
			return err
		}
		if state.Phase != "running" || state.StartedAt.IsZero() {
			t.Error("executor ran before admission")
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	r.serve(r.config())
	c := r.dial()
	id := attempt(30)
	send(t, c, id, request())
	_ = c.Close()
	select {
	case <-entered:
	case err := <-problem:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("lost start reply lost the admission")
	}
	c = r.dial()
	send(t, c, id, request())
	receive(t, c, "started")
	if r.executor.calls.Load() != 1 {
		t.Fatal("retry after a lost reply ran twice")
	}
}

// DESIGN section 4's stop road, supporting contract 9 recovery, returns the
// stopping receipt and then journals the
// executor's stopped result for every reconnecting watcher.
func TestAStopAsksTheExecutorAndItsResultCanBeWatched(t *testing.T) {
	r := newRig(t)
	r.executor.stops = make(chan string, 1)
	stopped := make(chan struct{})
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		select {
		case <-stopped:
			return j.Append(taskdoor.Finished{Outcome: "stopped", Report: "stopped by you"})
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.serve(r.config())
	c := r.dial()
	id := attempt(31)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Stop{})
	receive(t, c, "stopping")
	select {
	case got := <-r.executor.stops:
		if got != id {
			t.Fatalf("stopped %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not receive stop")
	}
	close(stopped)
	send(t, c, id, taskdoor.Watch{})
	var result taskdoor.Finished
	decode(t, receive(t, c, "finished"), &result)
	if result.Outcome != "stopped" {
		t.Fatalf("stop result = %+v", result)
	}
}

// Contracts 11 and 12: abandoning a running attempt erases it and prevents a
// late journal write from recreating its files.
func TestAnAbandonedRunningAttemptCannotRecreateItsJournal(t *testing.T) {
	r := newRig(t)
	r.executor.stops = make(chan string, 1)
	late := make(chan struct{})
	written := make(chan error, 1)
	entered := make(chan struct{})
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		close(entered)
		select {
		case <-late:
			err := j.Append(taskdoor.Finished{Outcome: "done"})
			written <- err
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.serve(r.config())
	c := r.dial()
	id := attempt(32)
	send(t, c, id, request())
	receive(t, c, "started")
	<-entered
	send(t, c, id, taskdoor.Ack{Disposition: "abandoned"})
	receive(t, c, "acked")
	close(late)
	if err := <-written; !os.IsNotExist(err) {
		t.Fatalf("late write = %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, id)); !os.IsNotExist(err) {
		t.Fatalf("late result recreated its folder: %v", err)
	}
}

// Contract 12 includes simultaneous retries arriving on different streams.
func TestConcurrentStartsForOneAttemptRunOnce(t *testing.T) {
	r := newRig(t)
	r.executor.run = func(ctx context.Context, _ *taskdoor.Attempt, _ taskdoor.Journal) error {
		<-ctx.Done()
		return ctx.Err()
	}
	r.serve(r.config())
	a, b := r.dial(), r.dial()
	id := attempt(33)
	var group sync.WaitGroup
	for _, c := range []*taskdoor.Conn{a, b} {
		group.Add(1)
		go func() { defer group.Done(); send(t, c, id, request()); receive(t, c, "started") }()
	}
	group.Wait()
	// The executor is asynchronous; a stop receipt is a connection-level fence,
	// while this channel-free wait only waits for the single admission to run.
	deadline := time.Now().Add(5 * time.Second)
	for r.executor.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.executor.calls.Load() != 1 {
		t.Fatalf("concurrent retries ran %d times", r.executor.calls.Load())
	}
}

// Contract 12's never-rerun rule survives a crash after a committed finished
// event but before the corresponding result and state writes.
func TestACommittedFinishIsRecoveredWithoutAdoptingOrRerunning(t *testing.T) {
	r := newRig(t)
	stop := r.serve(r.config())
	c := r.dial()
	id := attempt(34)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Watch{})
	receive(t, c, "finished")
	_ = c.Close()
	stop()
	path := filepath.Join(r.root, id, "state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state taskdoor.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state.Phase = "running"
	state.FinishedAt = time.Time{}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, id, "result.json")); err != nil {
		t.Fatal(err)
	}
	r.serve(r.config())
	c = r.dial()
	send(t, c, id, request())
	var started taskdoor.Started
	decode(t, receive(t, c, "started"), &started)
	if started.State.Phase != "done" {
		t.Fatalf("committed finish recovered as %s", started.State.Phase)
	}
	send(t, c, id, taskdoor.Watch{})
	if frame := receive(t, c, "finished"); frame.Seq != 1 {
		t.Fatalf("recovered finish seq = %d", frame.Seq)
	}
	if _, err := os.Stat(filepath.Join(r.root, id, "result.json")); err != nil {
		t.Fatal("result was not repaired:", err)
	}
	if r.executor.calls.Load() != 1 || r.executor.adopts.Load() != 0 {
		t.Fatalf("completed journal ran/adopted %d/%d", r.executor.calls.Load(), r.executor.adopts.Load())
	}
}
