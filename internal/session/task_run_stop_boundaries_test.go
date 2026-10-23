package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestContinueWaitingBeforeStopCannotBorrowFreshSubmissionAdmission(t *testing.T) {
	agent, _, _, double := continueAgent(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseWaiter := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseWaiter)
	prior := beltStartWaits
	beltStartWaits = func() { close(entered); <-release }
	t.Cleanup(func() { beltStartWaits = prior })
	agent.beltStartMu.Lock()
	result := make(chan error, 1)
	go func() { _, err := agent.ContinueRun(context.Background(), 99); result <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		agent.beltStartMu.Unlock()
		t.Fatal("continue did not wait at start admission")
	}
	if err := agent.StopWork(); err != nil {
		agent.beltStartMu.Unlock()
		t.Fatal(err)
	}
	agent.beltStartMu.Unlock()
	agent.mu.Lock()
	stopped := agent.workStopDone
	agent.mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not join pending start admission")
	}
	// This is the same admission reopening the explicit Submit path performs.
	agent.mu.Lock()
	err := agent.resumeWorkLocked()
	agent.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	releaseWaiter()
	select {
	case err := <-result:
		if !errors.Is(err, errWorkStopping) {
			t.Fatalf("old continue request borrowed new admission: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("old continue request did not settle")
	}
	nothingStarted(t, agent, double)
}

func TestStopBeforeHomecomingKeepsItsCancellationAuthority(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ctx, cut := context.WithCancel(context.Background())
	run := &beltRun{row: 95, title: "fixture work", cut: cut}
	// The authoritative stop flag is set by stopBeltRow before its context cut.
	agent.beltMu.Lock()
	run.stopped = true
	agent.beltMu.Unlock()
	if agent.beginBeltHomecoming(run) {
		t.Fatal("a stopped isolated copy was admitted to the main checkout")
	}
	cut()
	if ctx.Err() == nil {
		t.Fatal("stop did not cut remaining calls")
	}
}

func TestStopAfterHomecomingBeganPreservesReceiptAndCutsFurtherCalls(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ctx, cut := context.WithCancel(context.Background())
	run := &beltRun{row: 96, title: "fixture finished work", cut: cut}
	agent.beltMu.Lock()
	agent.beltRun = run
	agent.beltMu.Unlock()
	// Clear the synthetic registration before Agent.Close, which expects a store.
	t.Cleanup(func() { agent.beltMu.Lock(); agent.beltRun = nil; agent.beltMu.Unlock() })
	if !agent.beginBeltHomecoming(run) {
		t.Fatal("finished work could not begin its homecoming")
	}
	line, owned, err := agent.stopBeltRow(run.row, "")
	if err != nil || !owned || !strings.Contains(line, "has already finished") {
		t.Fatalf("stop promised to undo a homecoming already begun: %q %v", line, err)
	}
	if ctx.Err() == nil || !agent.beltRunQuiet(run) {
		t.Fatal("post-homecoming stop left further narration able to spend")
	}
	agent.beltMu.Lock()
	stopped := run.stopped
	agent.beltMu.Unlock()
	if stopped {
		t.Fatal("post-homecoming stop rewrote the actual completion as an unlanded stopped branch")
	}
}

func TestQuietLandingKeepsOperationalTaskAndProgramProvenanceWithoutWake(t *testing.T) {
	for _, program := range []*delegate.Delegate{nil, {Name: "fixture-review", Lands: delegate.LandsText}} {
		name := "plain"
		if program != nil {
			name = program.Name
		}
		t.Run(name, func(t *testing.T) {
			completer := &scriptedCompleter{}
			agent, _ := newTestAgent(t, completer, nil)
			store := askStore(t, "repair parser")
			defer store.Close()
			question := "What did the repair find?"
			if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
				t.Fatal(err)
			}
			run := &beltRun{store: store, root: store.RootID(), row: 97, title: "Parser", brief: "repair parser", quiet: true, delegate: program}
			agent.deliverBeltRunLanding(run, RunSummary{Outcome: beltRunOutcomeDone, Result: "Root output preserved."}, RunLanding{})
			entries := agent.Transcript()
			if len(entries) != 1 || !entries[0].RoutineCompletion || len(entries[0].CompletionTasks) != 1 {
				t.Fatalf("quiet receipt lost operational attribution: %+v", entries)
			}
			tag := entries[0].CompletionTasks[0]
			if tag.ID != 97 || tag.Title != "Parser" || tag.Request != question || tag.State != TaskDone || !strings.Contains(entries[0].Text, "Root output preserved.") {
				t.Fatalf("quiet receipt lost actual result or task: %+v", entries[0])
			}
			agent.mu.Lock()
			woke := agent.running || len(agent.steering) != 0
			hold := agent.programHold
			agent.mu.Unlock()
			if woke || completer.requests() != 0 {
				t.Fatal("quiet receipt admitted another model turn")
			}
			if program != nil && (hold == nil || hold.row != run.row || hold.program != program.Name) {
				t.Fatalf("quiet program receipt lost its provenance: %+v", hold)
			}
		})
	}
}
