package session

import (
	"context"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The close-tab stop must not buy a new reply with the news of its own kill.
func TestStopWorkDoesNotWakeAndOnlyFreshSubmissionRestarts(t *testing.T) {
	a, _ := jobsAgent(t)
	a.mu.Lock()
	a.opened = true
	a.mu.Unlock()
	wakes, leave := a.WatchWakes()
	defer leave()
	id := startJob(t, a, "sleep 30")
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	a.enqueueJobNote(backgroundResult{text: "a late job completion"})
	select {
	case <-wakes:
		t.Fatal("Stop work started another model turn")
	case <-time.After(100 * time.Millisecond):
	}
	if a.jobs.find(id).running() {
		t.Fatal("stopped process remains running")
	}
	if _, err := a.jobs.start("sleep 30"); err == nil {
		t.Fatal("stopped conversation admitted another job")
	}
	waitFor(t, "work stop to settle", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	events, err := a.Submit(context.Background(), "fresh request")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if _, err := a.jobs.start("true"); err != nil {
		t.Fatalf("fresh request did not reopen work: %v", err)
	}
}

// A job can reserve its log before Stop and reach registration after restart.
func TestStopWorkRejectsPreStopJobReservationAfterFreshSubmit(t *testing.T) {
	a, _ := jobsAgent(t)
	old, err := a.jobs.newJob("late command", jobKindBash)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stop", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	events, err := a.Submit(context.Background(), "fresh request")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := a.jobs.join(old); err == nil {
		t.Fatal("old job joined the resumed conversation")
	}
}

func TestStopWorkDropsQueuedAndLateTasks(t *testing.T) {
	a, _ := jobsAgent(t)
	g := a.graph()
	id := g.reserve()
	g.mu.Lock()
	g.nodes[id] = &TaskNode{graph: g, id: id, spec: taskSpec{title: "queued"}, state: TaskQueued, done: make(chan struct{})}
	g.order = append(g.order, id)
	g.mu.Unlock()
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stop", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	late := g.reserve()
	if got := g.admit(late, taskSpec{title: "late"}); got != TaskFailed {
		t.Fatalf("late task: %v", got)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range []uint64{id, late} {
		if !g.nodes[id].stopped || g.nodes[id].state != TaskFailed {
			t.Fatalf("task %d was not stopped", id)
		}
	}
}

func TestStopWorkCutsAdaptiveWorkerAndRejectsNewRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &closeRunCompleter{started: make(chan struct{}), cut: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
	a, _ := newTestAgent(t, client, nil)
	defer close(client.release)
	id, err := a.RunOrchestrate(context.Background(), "read release notes", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitCloseRun(t, client.started, "worker start")
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	waitCloseRun(t, client.cut, "worker cancellation")
	live, known := a.orchestration(id)
	if !known || !live.run.Snapshot().Stopped {
		t.Fatal("stopped run lost its readable stopped trace")
	}

	if _, err := a.Submit(context.Background(), "too soon"); err != errWorkStopping {
		t.Fatalf("fresh input crossed unfinished cancellation: %v", err)
	}
	if _, err := a.RunOrchestrate(context.Background(), "more", "", 5); err == nil {
		t.Fatal("stopped session accepted adaptive work")
	}
}

func TestStopWorkCancelsPermissionWithoutRunningTheTool(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	a, runs := consentAgent(t, completer, promptAll(), true)
	events := drainAnswering(t, mustSubmit(t, a, "touch the file"), func(Event) {
		if err := a.StopWork(); err != nil {
			t.Fatal(err)
		}
	})
	if len(runs) != 0 {
		t.Fatal("tool ran after stop")
	}
	if _, ok := firstOfKind(events, EventConsentRequest); !ok {
		t.Fatal("no pending permission exercised")
	}
	if len(a.PendingConsent()) != 0 {
		t.Fatal("permission survived stop")
	}
	waitFor(t, "stop", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
}

func TestStopWorkCancelsTypedInputAndIgnoresLateAnswer(t *testing.T) {
	hub := &fakeHub{keyService: true}
	completer := &scriptedCompleter{steps: useServiceTurn("stripe")}
	a := connectAgent(t, completer, hub, true)
	events := mustSubmit(t, a, "look at billing")
	asked := false
	drainConnect(t, events, func(event Event) {
		if !event.NeedsKey {
			t.Fatal("not a typed input question")
		}
		asked = true
		if err := a.StopWork(); err != nil {
			t.Fatal(err)
		}
	})
	if !asked {
		t.Fatal("input was never requested")
	}
	waitFor(t, "stop", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	a.ResolveConnectKey("connect-1", "discarded-test-value")
	if a.NeedsPerson() {
		t.Fatal("stopped input still needs an answer")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.keyConnected || hub.key != "" {
		t.Fatal("late answer connected an account")
	}
}

// Adaptive setup owns work before its run has a registry entry. Stop must also
// close that interval, without letting a later registration escape cancellation.
func TestStopWorkOwnsAdaptiveSetupBeforeRegistration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	entered, release := make(chan struct{}), make(chan struct{})
	var armed atomic.Bool
	var once, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	a, _ := newTestAgent(t, replier(func([]ai.Message) string { return "{}" }), func(c *Config) {
		c.RolesSource = func(string) (string, bool) {
			if armed.Load() {
				once.Do(func() { close(entered); <-release })
			}
			return "", false
		}
	})
	t.Cleanup(unblock)
	armed.Store(true)
	result := make(chan error, 1)
	go func() { _, err := a.RunOrchestrate(context.Background(), "read release notes", "", 5); result <- err }()
	waitCloseRun(t, entered, "setup admission")
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Submit(context.Background(), "too soon"); err != errWorkStopping {
		t.Fatalf("setup escaped stop: %v", err)
	}
	unblock()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("run registered after Stop")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setup never returned")
	}
	waitFor(t, "setup stop", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	a.mu.Lock()
	graph := a.tasks
	a.mu.Unlock()
	if graph != nil {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		if !graph.quitting {
			t.Fatal("graph created during setup reopened admission")
		}
		for _, node := range graph.nodes {
			if node.state == TaskRunning || node.state == TaskQueued {
				t.Fatal("late setup left runnable work")
			}
		}
	}
}
