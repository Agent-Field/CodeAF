package tui3

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// countedPlanAgent is the real remote client with its plan reads counted. Every
// call still crosses the real wire to the real engine; the count is how many did.
type countedPlanAgent struct {
	*remote.Agent
	rows atomic.Int64
}

func (c *countedPlanAgent) PlanTasks() []session.PlanTaskRow {
	c.rows.Add(1)
	return c.Agent.PlanTasks()
}

// hostedPlanApp is a surface over a REAL engine reached through a REAL client:
// a plan store on disk, a session agent that reads it, the remote server in
// front of that agent, and the client the surface is handed in a hosted
// conversation. `ended` completes the run's root before anything reads it.
func hostedPlanApp(t *testing.T, ended bool) (*app, *countedPlanAgent, string) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	workspace := t.TempDir()
	store, err := session.OpenRunPlan(workspace, "the run", "the hand-off's own words")
	if err != nil {
		t.Fatalf("seed the run's store: %v", err)
	}
	if ended {
		if err := store.CompleteRoot("the run is over"); err != nil {
			t.Fatalf("end the run: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	engine, err := session.New(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "fixture",
		BaseURL: "http://127.0.0.1:1/v1", System: "Test only.",
	})
	if err != nil {
		t.Fatalf("build the real session agent: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version, Workspace: workspace}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: engine, Workspace: workspace}, nil
		},
	})
	if err != nil {
		t.Fatalf("serve the engine: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	counted := &countedPlanAgent{Agent: loop.Client.Agent()}
	a := newTestApp(counted)
	a.width, a.height = 200, 40
	a.homeRoot = t.TempDir()
	return a, counted, session.PlanStorePath(workspace)
}

// A HOSTED CONVERSATION AT REST ASKS ITS ENGINE FOR NOTHING. The reader is always
// there in a hosted conversation, so a beat that ran whenever a reader existed
// would cross the wire every three seconds for ever, in a window nobody is
// touching. With no run that can still move, twenty beats cost no read beyond
// the first one that found the run ended.
func TestAHostedConversationAtRestReadsNoPlanOverTheWire(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, true)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	a.taskSheet.regroup(a)
	first := counted.rows.Load()
	if first != 1 {
		t.Fatalf("the first reading crossed the wire %d times, want once", first)
	}
	if len(a.taskSheet.mine.plan) == 0 {
		t.Fatal("the first reading did not hold the ended run's row, so the test proves nothing")
	}
	for beat := 0; beat < 20; beat++ {
		now = now.Add(elsewhereEvery)
		a.taskSheet.regroup(a)
	}
	if got := counted.rows.Load(); got != first {
		t.Fatalf("twenty beats at rest crossed the wire %d more times, want none", got-first)
	}
}

// AND WITH A RUN THAT CAN STILL MOVE, THE RAIL LEARNS OF A PART WITHIN ONE BEAT.
// The part is added to the real store by another handle, the way a run's worker
// adds one, and nothing is published to this window.
func TestAHostedRailLearnsOfAnAddedPartWithinOneBeat(t *testing.T) {
	a, counted, path := hostedPlanApp(t, false)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	a.taskSheet.regroup(a)
	held := len(a.taskSheet.mine.plan)
	if held == 0 {
		t.Fatal("the first reading did not hold the live run's row")
	}
	worker, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.AddMany([]plandb.TaskSpec{{ID: "part", Title: "a part the run added", Description: "its own words"}}); err != nil {
		t.Fatal(err)
	}
	_ = worker.Close()
	now = now.Add(elsewhereEvery - time.Millisecond)
	a.taskSheet.regroup(a)
	if got := len(a.taskSheet.mine.plan); got != held {
		t.Fatalf("the plan was read again inside its beat: %d rows, was %d", got, held)
	}
	now = now.Add(time.Millisecond)
	a.taskSheet.regroup(a)
	if got := len(a.taskSheet.mine.plan); got != held+1 {
		t.Fatalf("one beat later the rail holds %d rows, want the added part as well as the %d it had", got, held)
	}
	if got := counted.rows.Load(); got != 2 {
		t.Fatalf("learning of one part cost %d reads over the wire, want two", got)
	}
}

type heldHostedPlanAgent struct {
	*countedPlanAgent
	started chan struct{}
	release chan struct{}
}

func (h *heldHostedPlanAgent) PlanTaskPage(id string) (session.PlanTaskPage, bool) {
	close(h.started)
	<-h.release
	return h.Agent.PlanTaskPage(id)
}

// Once a rail gesture has chosen a task page, its delayed store read owns every
// following key. The conversation must never receive text intended for the page.
func TestHostedRailPageOwnsKeysWhileItsReadIsInFlight(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, false)
	held := &heldHostedPlanAgent{countedPlanAgent: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	a.input.value = []rune("conversation draft")
	cmd := a.openRailPlan("root", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	for _, r := range "keep the examples short" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := string(a.input.value); got != "conversation draft" {
		t.Fatalf("keys reached the conversation box: %q", got)
	}
	close(held.release)
	drive(t, a, <-answer)
	if !a.taskSheet.planOn || !a.railTaskPlanOn {
		t.Fatal("the delayed answer did not open the task page")
	}
}

func TestHostedRailPagePendingEscCancelsAndSecondPressSupersedes(t *testing.T) {
	a, _, _ := hostedPlanApp(t, false)
	a.railPlanPending = railPlanPending{id: "first", keys: []tea.KeyPressMsg{key("x")}}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.railPlanPending.id != "" || a.taskSheet.planOn {
		t.Fatal("esc did not cancel the pending page")
	}
	a.railPlanPending = railPlanPending{id: "first", keys: []tea.KeyPressMsg{key("x")}}
	a.beginRailPlan("second")
	if a.railPlanPending.id != "second" || len(a.railPlanPending.keys) != 0 {
		t.Fatalf("second press retained the first target or its keys: %+v", a.railPlanPending)
	}
}
