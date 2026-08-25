package resident

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

type fakeServiceRuntime struct {
	matches   map[int]bool
	healthErr error
	startErr  error
	nextPID   int
	starts    int
	stops     []int
}

func (runtime *fakeServiceRuntime) IdentityMatches(pid int, _ time.Time) (bool, error) {
	if runtime.matches == nil {
		return true, nil
	}
	return runtime.matches[pid], nil
}

func (runtime *fakeServiceRuntime) Healthy(context.Context, store.Service) error {
	return runtime.healthErr
}

func (runtime *fakeServiceRuntime) Start(service store.Service) (int, time.Time, error) {
	runtime.starts++
	if runtime.startErr != nil {
		return 0, time.Time{}, runtime.startErr
	}
	if runtime.nextPID == 0 {
		runtime.nextPID = service.PID + 100
	}
	return runtime.nextPID, service.StartedAt.Add(time.Minute), nil
}

func (runtime *fakeServiceRuntime) Stop(pid int) error {
	runtime.stops = append(runtime.stops, pid)
	return nil
}

func residentServiceFixture(t *testing.T, graph *store.Store, id, name, node string, pid int, auto bool) store.Service {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: node, Brief: "serve", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "service-session", Intent: "run the app", ServiceIntent: true}); err != nil {
		t.Fatal(err)
	}
	service, err := graph.PromoteService(store.Service{
		ID: id, Name: name, Command: "npm run dev", Dir: t.TempDir(), LogPath: t.TempDir() + "/service.log",
		Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: fmt.Sprint(5100 + pid)},
		PID:    pid, StartedAt: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC), AutoRestart: auto,
		Provenance: store.ServiceProvenance{OriginJobID: pid, LeafNodeID: node},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testSupervisor(graph *store.Store, runtime ServiceRuntime) *ServiceSupervisor {
	supervisor := NewServiceSupervisor(graph).WithRuntime(runtime)
	supervisor.now = func() time.Time { return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC) }
	supervisor.sleep = func(context.Context, time.Duration) error { return nil }
	return supervisor
}

func TestServiceSupervisorHealthFailureAndQuietAttention(t *testing.T) {
	graph := residentTestStore(t, "service-health.db")
	service := residentServiceFixture(t, graph, "svc", "dev-server", "leaf", 11, false)
	runtime := &fakeServiceRuntime{healthErr: errors.New("connection refused")}
	if err := testSupervisor(graph, runtime).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := graph.Service(service.ID)
	if got.Status != store.ServiceFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	messages, _ := graph.Messages("service-session", 0, 20)
	if len(messages) != 1 || messages[0].Body != "dev-server stopped answering on :5111" {
		t.Fatalf("attention messages = %+v", messages)
	}
	if err := testSupervisor(graph, runtime).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, _ = graph.Messages("service-session", 0, 20)
	if len(messages) != 1 {
		t.Fatalf("failed service repeated attention: %+v", messages)
	}
}

func TestServiceSupervisorHealthOKAndSuccessfulRestart(t *testing.T) {
	graph := residentTestStore(t, "service-restart.db")
	service := residentServiceFixture(t, graph, "svc", "preview", "leaf", 12, true)
	runtime := &fakeServiceRuntime{}
	supervisor := testSupervisor(graph, runtime)
	clock := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	supervisor.now = func() time.Time { return clock }
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := graph.Service(service.ID)
	if got.Status != store.ServiceRunning || runtime.starts != 0 {
		t.Fatalf("healthy service = %+v starts=%d", got, runtime.starts)
	}
	runtime.healthErr = errors.New("down")
	// The supervisor probes on its own interval now, so the second look has to
	// be a later moment rather than merely a later call.
	clock = clock.Add(serviceHealthInterval)
	if err := supervisor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ = graph.Service(service.ID)
	if got.Status != store.ServiceRunning || got.RestartCount != 1 || runtime.starts != 1 || len(runtime.stops) != 1 {
		t.Fatalf("restarted service = %+v runtime=%+v", got, runtime)
	}
}

func TestServiceSupervisorCapsAutoRestartAndRests(t *testing.T) {
	graph := residentTestStore(t, "service-rest.db")
	service := residentServiceFixture(t, graph, "svc", "dev-server", "leaf", 13, true)
	runtime := &fakeServiceRuntime{healthErr: errors.New("down"), startErr: errors.New("spawn failed")}
	supervisor := testSupervisor(graph, runtime)
	for attempt := 0; attempt < serviceRestartLimit; attempt++ {
		if err := supervisor.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got, _, _ := graph.Service(service.ID)
	if got.Status != store.ServiceResting || got.RestartCount != serviceRestartLimit || runtime.starts != serviceRestartLimit {
		t.Fatalf("rested service = %+v starts=%d", got, runtime.starts)
	}
	messages, _ := graph.Messages("service-session", 0, 20)
	if messages[len(messages)-1].Body != "dev-server rested after 3 restarts — say 'restart it' when ready" {
		t.Fatalf("rest receipt = %+v", messages)
	}
}

func TestServiceHygieneNudgeAsksOnceAndDefaultsToKeep(t *testing.T) {
	graph := residentTestStore(t, "service-hygiene.db")
	service := residentServiceFixture(t, graph, "svc", "dev-server", "leaf", 41, false)
	reconciler := New(graph, nil, nil)
	now := service.StartedAt.Add(6 * 24 * time.Hour)

	reconciler.proposeServiceHygiene(now)
	messages, _ := graph.Messages("service-session", 0, 20)
	if len(messages) != 1 {
		t.Fatalf("hygiene nudge messages = %+v", messages)
	}
	nudge := messages[0]
	if !strings.Contains(nudge.Body, "dev-server has run 6 days — still needed?") ||
		!strings.Contains(nudge.Body, `"default":"1"`) ||
		!strings.Contains(nudge.Body, "stop it") || len(nudge.Options) != 2 ||
		nudge.Options[0].Value != store.ServiceHygieneKeepValue(service.ID) {
		t.Fatalf("hygiene nudge = %+v", nudge)
	}

	// Once per service, whatever the answer: the durable question is the memory.
	reconciler.proposeServiceHygiene(now.Add(7 * 24 * time.Hour))
	messages, _ = graph.Messages("service-session", 0, 20)
	if len(messages) != 1 {
		t.Fatalf("hygiene nudge repeated: %+v", messages)
	}
}

func TestServiceHygieneStaysQuietForYoungOrLivelyServices(t *testing.T) {
	young := residentTestStore(t, "service-hygiene-young.db")
	service := residentServiceFixture(t, young, "svc", "dev-server", "leaf", 42, false)
	New(young, nil, nil).proposeServiceHygiene(service.StartedAt.Add(serviceHygieneAge - time.Hour))
	if messages, _ := young.Messages("service-session", 0, 20); len(messages) != 0 {
		t.Fatalf("young service nudged: %+v", messages)
	}

	// The quiet gate is measured against real message timestamps, so this
	// service is aged relative to the wall clock the journal actually writes.
	lively := residentTestStore(t, "service-hygiene-lively.db")
	if err := lively.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "leaf", Brief: "serve", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "service-session", Intent: "run the app", ServiceIntent: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := lively.PromoteService(store.Service{
		ID: "svc", Name: "dev-server", Command: "npm run dev", Dir: t.TempDir(), LogPath: t.TempDir() + "/service.log",
		Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "5143"},
		PID:    43, StartedAt: now.Add(-6 * 24 * time.Hour),
		Provenance: store.ServiceProvenance{OriginJobID: 43, LeafNodeID: "leaf"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := lively.PostMessage(store.Message{
		SessionID: "service-session", Role: store.RoleUser, Body: "still working on it"}); err != nil {
		t.Fatal(err)
	}
	New(lively, nil, nil).proposeServiceHygiene(now)
	messages, _ := lively.Messages("service-session", 0, 20)
	if len(messages) != 1 || messages[0].Role != store.RoleUser {
		t.Fatalf("lively session nudged: %+v", messages)
	}
}

func TestServiceStopAndStartupReadoption(t *testing.T) {
	graph := residentTestStore(t, "service-adopt.db")
	live := residentServiceFixture(t, graph, "live", "live-server", "leaf-live", 21, false)
	stale := residentServiceFixture(t, graph, "stale", "stale-server", "leaf-stale", 22, false)
	runtime := &fakeServiceRuntime{matches: map[int]bool{21: true, 22: false}}
	if err := ReAdoptServices(graph, "service-session", runtime); err != nil {
		t.Fatal(err)
	}
	liveAfter, _, _ := graph.Service(live.ID)
	staleAfter, _, _ := graph.Service(stale.ID)
	if liveAfter.Status != store.ServiceRunning || staleAfter.Status != store.ServiceStopped || runtime.starts != 0 {
		t.Fatalf("readoption live=%+v stale=%+v starts=%d", liveAfter, staleAfter, runtime.starts)
	}
	messages, _ := graph.Messages("service-session", 0, 20)
	if messages[len(messages)-1].Body != "stale-server was not running anymore — say 'start it again' to relaunch" {
		t.Fatalf("stale receipt = %+v", messages)
	}

	stopGraph := residentTestStore(t, "service-stop.db")
	service := residentServiceFixture(t, stopGraph, "svc", "stop-me", "leaf", 31, false)
	stopRuntime := &fakeServiceRuntime{}
	if err := testSupervisor(stopGraph, stopRuntime).Stop(service.ID, "test"); err != nil {
		t.Fatal(err)
	}
	stopped, _, _ := stopGraph.Service(service.ID)
	if stopped.Status != store.ServiceStopped || len(stopRuntime.stops) != 1 || stopRuntime.stops[0] != 31 {
		t.Fatalf("stopped service=%+v runtime=%+v", stopped, stopRuntime)
	}
	events, _ := stopGraph.Events(0, 100)
	if events[len(events)-1].Kind != store.EventServiceStopped {
		t.Fatalf("last event = %s, want service_stopped", events[len(events)-1].Kind)
	}
}

// TestChangeGateSleepsWhileAServiceIsSupervised is the regression for the
// finding that adopting a service disarmed the resident's change gate: the gate
// named "now" as the supervision deadline, so a machine with one dev server on
// it re-derived the whole graph twice a second for as long as that server lived.
func TestChangeGateSleepsWhileAServiceIsSupervised(t *testing.T) {
	graph := residentTestStore(t, "service-change-gate.db")
	residentServiceFixture(t, graph, "svc", "dev-server", "leaf", 21, false)
	clock := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	reconciler := New(graph, nil, nil)
	reconciler.now = func() time.Time { return clock }
	supervisor := testSupervisor(graph, &fakeServiceRuntime{})
	supervisor.now = reconciler.now
	reconciler.services = supervisor

	// Two passes prime the gate: the first records the watermark, the second
	// finds it unmoved and derives the deadlines.
	for pass := 0; pass < 4; pass++ {
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	quiet, err := reconciler.quietTickLocked()
	if err != nil {
		t.Fatal(err)
	}
	if !quiet {
		t.Fatalf("a supervised service pinned the change gate open: deadline=%s now=%s",
			reconciler.gateDeadline, clock)
	}

	// And the probe still happens on time — but WITHOUT waking the pass. A
	// health check is one question about one PID; naming it as a clock deadline
	// meant the whole reconciliation ran to ask it, six times a minute for as
	// long as the service lived. The supervisor runs on quiet passes instead, so
	// the gate stays shut and the operating system is asked exactly as often.
	clock = clock.Add(serviceHealthInterval)
	quiet, err = reconciler.quietTickLocked()
	if err != nil {
		t.Fatal(err)
	}
	if !quiet {
		t.Fatal("a due health check woke the whole pass")
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	supervisor.healthMu.Lock()
	probedAt := supervisor.checkedAt["svc"]
	supervisor.healthMu.Unlock()
	if !probedAt.Equal(clock) {
		t.Fatalf("the quiet pass skipped the health check: probed at %s, want %s", probedAt, clock)
	}
}
