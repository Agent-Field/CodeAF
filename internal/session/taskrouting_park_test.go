package session

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// parkRouter is the router a conversation consults, scripted: it refuses the
// checker while the saved pin's provider is unreachable, and seats the crew
// once it is reachable again. Every ask it was given is kept.
type parkRouter struct {
	mu        sync.Mutex
	reachable bool
	asks      []config.CrewAsk
}

func (r *parkRouter) route(ask config.CrewAsk) (crewroute.Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asks = append(r.asks, ask)
	if !r.reachable {
		none := &crewroute.NoEligibleError{Seat: crewroute.Checker, Mode: crewroute.ModeAuto}
		// The saved pin is allowed; its provider is not reachable. THE PIN IS
		// HELD: the router names it and seats nothing in its place.
		none.Reject("claude-sonnet", "anthropic", crewroute.RejectProviderUnavailable)
		return crewroute.Decision{}, none
	}
	return liveDecision(), nil
}

func (r *parkRouter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.asks)
}

func (r *parkRouter) set(reachable bool) {
	r.mu.Lock()
	r.reachable = reachable
	r.mu.Unlock()
}

// A TASK WAITING ON A MODEL COMES BACK WAITING. Parked, the conversation
// closed and was opened again from its checkpoint: the row is still the
// waiting Ask (not interrupted), with the router's own role and reasons and
// the person's choice for the task; reading it routes nothing and calls no
// model. Retry while the pin's provider is still down keeps it waiting on the
// same exact pin, never on another model; once it is reachable, the same
// retry starts the work.
func TestAWaitingTaskSurvivesARestartAndRetriesOnlyWhenAsked(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	router := &parkRouter{}
	dir := t.TempDir()
	repo := newTestRepo(t)
	completer := &scriptedCompleter{}
	mutate := func(c *Config) {
		c.Workspace = repo
		c.Place = Place{Dir: dir}
		c.SessionFile = filepath.Join(dir, placeTranscript)
		c.AskConsent = false
		c.RouteCrew = router.route
	}
	first, _ := newTestAgent(t, completer, mutate)

	none := &crewroute.NoEligibleError{Seat: crewroute.Checker, Mode: crewroute.ModeAuto}
	none.Reject("claude-sonnet", "anthropic", crewroute.RejectProviderUnavailable)
	for i := range 25 {
		none.Reject("other-"+string(rune('a'+i)), "openrouter", crewroute.RejectNotAllowed)
	}
	// The id is the graph's own, as every hand-off's is.
	id := first.graph().reserve()
	sid := strconv.FormatUint(id, 10)
	// The person chose a planner for this task before it was parked.
	if err := first.actOnProposal(id, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Planner: "ds-flash@deepseek"}}); err != nil {
		t.Fatal(err)
	}
	p := &stagedProposal{id: id, stand: taskStand{dir: repo, mode: TaskModeWorktree}}
	if _, parked := first.parkUnroutedHandoff(context.Background(), p, taskSpec{title: "Update fixtures", brief: "update every fixture"}, "", nil, none); !parked {
		t.Fatal("no-eligible was not parked")
	}
	first.graph().checkpoint()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	again, _ := newTestAgent(t, completer, mutate)
	row, ok := again.appRunRow(id)
	if !ok || row.State != TaskQueued || row.Waiting != TaskWaitingNoModel || row.Report != none.Error() {
		t.Fatalf("restored row %+v", row)
	}
	view, err := again.TaskRouting(sid, 0)
	if err != nil || view.State != TaskRoutingBlocked || view.Blocked == nil {
		t.Fatalf("restored view %+v %v", view, err)
	}
	b := view.Blocked
	if b.Seat != crewroute.Checker || len(b.Rejected) != crewroute.MaxRejections || b.Omitted != 26-crewroute.MaxRejections ||
		b.Rejected[0] != (crewroute.Rejection{Model: "claude-sonnet", Provider: "anthropic", Reason: crewroute.RejectProviderUnavailable}) {
		t.Fatalf("restored refusal %+v", b)
	}
	if view.Requested[crewroute.Planner] != "ds-flash@deepseek" {
		t.Fatalf("the person's choice was lost: %v", view.Requested)
	}
	options, err := again.TaskRoutingOptions(sid)
	if err != nil || options.Scope != TaskRoutingWaiting || !options.Retry || !options.Pick {
		t.Fatalf("restored options %+v %v", options, err)
	}
	if router.count() != 0 || completer.requests() != 0 || double.didRun() {
		t.Fatal("reading a restored waiting task routed, called a model or ran")
	}

	// Retry with the provider still down: still waiting, same pin, nothing run.
	_, err = again.TaskRoutingAct(context.Background(), sid, TaskRoutingAction{Kind: TaskRoutingRetry})
	var refusal *TaskRoutingRefusal
	if !errors.As(err, &refusal) || refusal.Code != TaskRoutingNoEligible || refusal.Blocked.Rejected[0].Model != "claude-sonnet" {
		t.Fatalf("retry while unreachable: %v", err)
	}
	if router.count() != 1 {
		t.Fatalf("router asked %d times", router.count())
	}
	ask := router.asks[0]
	if len(ask.AutoSeats) != 0 || ask.Sends[crewroute.Planner] != "ds-flash@deepseek" || ask.Sends[crewroute.Checker] != "" {
		t.Fatalf("the retry changed what was asked: %+v", ask)
	}
	if row, _ := again.appRunRow(id); row.State != TaskQueued || row.Waiting != TaskWaitingNoModel || double.didRun() || completer.requests() != 0 {
		t.Fatalf("still-blocked retry left %+v", row)
	}

	// The person repaired the provider (a separate settings concern); the same
	// retry now starts the task.
	router.set(true)
	if _, err := again.TaskRoutingAct(context.Background(), sid, TaskRoutingAction{Kind: TaskRoutingRetry}); err != nil {
		t.Fatalf("retry once reachable: %v", err)
	}
	if again.taskRouting.isParked(id) {
		t.Fatal("a started task is still parked")
	}
	beltRunWaitFor(t, "the run to start", double.didRun)
	if row, _ := again.appRunRow(id); row.Waiting == TaskWaitingNoModel || row.park != nil {
		t.Fatalf("started row still waiting: %+v", row)
	}
	endBeltRun(t, again, double)
}

// Stop on a task that came back waiting ends it as not started, and it stays
// ended across the next restart.
func TestStoppingARestoredWaitingTaskEndsIt(t *testing.T) {
	router := &parkRouter{}
	dir := t.TempDir()
	mutate := func(c *Config) {
		c.Place = Place{Dir: dir}
		c.SessionFile = filepath.Join(dir, placeTranscript)
		c.RouteCrew = router.route
	}
	first, _ := newTestAgent(t, &recoveryModelRecorder{}, mutate)
	none := &crewroute.NoEligibleError{Seat: crewroute.Worker, Mode: crewroute.ModeOne}
	none.Reject("glm-5.3", "z-ai", crewroute.RejectRouteUnavailable)
	id := first.graph().reserve()
	first.parkUnroutedHandoff(context.Background(), &stagedProposal{id: id, stand: taskStand{dir: dir}}, taskSpec{title: "t"}, "", nil, none)
	first.graph().checkpoint()
	_ = first.Close()

	again, _ := newTestAgent(t, &recoveryModelRecorder{}, mutate)
	if !again.DropParkedTask(id) {
		t.Fatal("stop did not find the restored waiting task")
	}
	again.graph().checkpoint()
	_ = again.Close()
	third, _ := newTestAgent(t, &recoveryModelRecorder{}, mutate)
	row, _ := third.appRunRow(id)
	if row.State != TaskFailed || row.Waiting != "" {
		t.Fatalf("stopped waiting task came back %+v", row)
	}
	if view, _ := third.TaskRouting(strconv.FormatUint(id, 10), 0); view.State == TaskRoutingBlocked {
		t.Fatal("a stopped task reads as waiting")
	}
	if router.count() != 0 {
		t.Fatal("stop routed")
	}
}

// THE STOP EVERY SURFACE SENDS — task:N — reaches a task waiting on a model,
// before a restart and after one, and ends it as not started.
func TestAStopReachesATaskWaitingOnAModel(t *testing.T) {
	dir := t.TempDir()
	mutate := func(c *Config) {
		c.Place = Place{Dir: dir}
		c.SessionFile = filepath.Join(dir, placeTranscript)
	}
	a, _ := newTestAgent(t, &recoveryModelRecorder{}, mutate)
	none := &crewroute.NoEligibleError{Seat: crewroute.Planner, Mode: crewroute.ModeAuto}
	none.Reject("ds-flash", "deepseek", crewroute.RejectProviderUnavailable)
	live := a.graph().reserve()
	a.parkUnroutedHandoff(context.Background(), &stagedProposal{id: live, stand: taskStand{dir: dir}}, taskSpec{title: "live"}, "", nil, none)
	restored := a.graph().reserve()
	a.parkUnroutedHandoff(context.Background(), &stagedProposal{id: restored, stand: taskStand{dir: dir}}, taskSpec{title: "restored"}, "", nil, none)
	if _, err := a.CancelWithReason("task:"+strconv.FormatUint(live, 10), ""); err != nil {
		t.Fatalf("stop on a waiting task: %v", err)
	}
	if row, _ := a.appRunRow(live); row.State != TaskFailed {
		t.Fatalf("stopped waiting task is %s", row.State)
	}
	a.graph().checkpoint()
	_ = a.Close()
	b, _ := newTestAgent(t, &recoveryModelRecorder{}, mutate)
	if _, err := b.CancelWithReason(strconv.FormatUint(restored, 10), ""); err != nil {
		t.Fatalf("stop on a restored waiting task: %v", err)
	}
	if row, _ := b.appRunRow(restored); row.State != TaskFailed || b.taskRouting.isParked(restored) {
		t.Fatalf("stopped restored task is %+v", row)
	}
}
