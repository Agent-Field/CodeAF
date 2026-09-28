package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func recoveryDecision() crewroute.Decision {
	d := crewroute.Decision{Class: crewroute.Bugfix, Ladder: map[crewroute.Seat][]crewroute.Pick{}}
	for _, seat := range crewroute.Seats {
		send := "accepted/" + string(seat)
		d.Crew = append(d.Crew, crewroute.Pick{Seat: seat, Model: send, Send: send, Provider: "openrouter", Pinned: true})
	}
	return d
}

type recoveryModelRecorder struct {
	mu     sync.Mutex
	models []string
}

func (r *recoveryModelRecorder) CompleteWithMessages(_ context.Context, _ []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	var req ai.Request
	for _, opt := range opts {
		if err := opt(&req); err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	r.models = append(r.models, req.Model)
	r.mu.Unlock()
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}}}}}}, nil
}

// The profile and conversation deliberately disagree with every accepted seat.
// Reopening must use the stored policy, not ask the router for another decision.
func TestHeldRecoveryRetainsTheAcceptedCrew(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	place, repo, profile := t.TempDir(), newTestRepo(t), t.TempDir()
	routed := 0
	recorder := &recoveryModelRecorder{}
	open := func() *Agent {
		a, _ := newTestAgent(t, recorder, func(c *Config) {
			c.Place = Place{Dir: place}
			c.Workspace = repo
			c.SessionFile = filepath.Join(place, placeTranscript)
			c.ProfileDir = profile
			c.Model = "profile/different"
			c.AskConsent = false
			c.TaskMaxLoad = 0
			c.TaskMinFreeMB = 1 << 40
			c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { routed++; return recoveryDecision(), nil }
		})
		return a
	}
	first := open()
	id, _, _, err := first.StartTask(context.Background(), "keep accepted seats", false)
	if err != nil {
		t.Fatal(err)
	}
	first.beltMu.Lock()
	old := first.beltRun
	first.beltMu.Unlock()
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.over:
	case <-time.After(5 * time.Second):
		t.Fatal("old driver remained")
	}
	second := open()
	second.beltMu.Lock()
	run := second.beltRun
	second.beltMu.Unlock()
	if run == nil || run.crew == nil {
		t.Fatal("held restart lost its accepted crew")
	}
	if routed != 1 {
		t.Fatalf("restart rerouted accepted work: %d decisions", routed)
	}
	spec := second.beltRunSpec(run, run.brief)
	want := recoveryDecision()
	for seat, model := range map[crewroute.Seat]string{crewroute.Worker: spec.WorkModel, crewroute.Planner: spec.PlanModel, crewroute.Checker: spec.CheckModel} {
		if model != want.Seat(seat).Send {
			t.Fatalf("%s changed to %q", seat, model)
		}
		if _, err := spec.CompleterFor(model).CompleteWithMessages(t.Context(), nil, ai.WithModel(model)); err != nil {
			t.Fatal(err)
		}
	}
	recorder.mu.Lock()
	got := append([]string(nil), recorder.models...)
	recorder.mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("calls=%v", got)
	}
	for _, model := range got {
		if !strings.HasPrefix(model, "accepted/") {
			t.Fatalf("call used changed profile: %s", model)
		}
	}
	if engine.didRun() {
		t.Fatal("held recovery dispatched the engine before admission")
	}
	if _, err := second.Cancel("task:" + strconv.FormatUint(id, 10)); err != nil {
		t.Fatal(err)
	}
}

func TestAdmittedRecoveryRestoresRoutingAndSpendState(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	engine := newBeltRunDouble("done")
	registerBeltRunEngine(t, engine)
	place, repo, profile := t.TempDir(), newTestRepo(t), t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, &recoveryModelRecorder{}, func(c *Config) {
			c.Place = Place{Dir: place}
			c.Workspace = repo
			c.SessionFile = filepath.Join(place, placeTranscript)
			c.ProfileDir = profile
			c.Model = "profile/different"
			c.AskConsent = false
			c.TaskMaxLoad = 0
			c.TaskMinFreeMB = 0
		})
		return a
	}
	first := open()
	g := first.graph()
	id := g.reserve()
	// Seed an accepted crew with a prior fallback and an exhausted task cap.
	d := recoveryDecision()
	worker := d.Seat(crewroute.Worker).Send
	c := &taskCrew{call: "recorded-call", title: "saved task", brief: "saved brief", repo: repo, decision: d, original: map[crewroute.Seat]string{}, ladders: d.Ladder, started: map[string]bool{"accepted/replacement": true}, swaps: map[string]string{worker: "accepted/replacement"}, bad: map[string]bool{worker: true}, broke: map[string]bool{"old-account": true}, gone: map[string]bool{"old-provider": true}, freeTried: map[crewroute.Seat]int{crewroute.Worker: 2}, failed: map[string]crewFailure{worker: {kind: provider.RouteQuota, until: time.Now().Add(time.Hour), err: errors.New("resting")}}, guard: &SpendGuard{TaskCap: 2, TaskAction: "accepted task cap reached", Task: &SpendTask{spent: 2}, SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: 1}, seatSpent: map[crewroute.Seat]*SpendTask{crewroute.Checker: {spent: .5}}, modelSpent: map[string]float64{worker: .25}}}
	for _, pick := range d.Crew {
		c.original[pick.Seat] = pick.Send
	}
	c.decision.Crew[0].Send = "accepted/replacement"
	tree, err := prepareTaskTree(first.config.Place, repo, "1111bbbb1111bbbb", id, "saved task")
	if err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(g.planPath(), "task", strconv.FormatUint(id, 10), "saved task", "saved brief")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	saved := c.record()
	first.publishRunRow(g, TaskNotice{ID: id, Title: "saved task", State: TaskRunning, Copy: runCopyOf(tree), CrewState: saved})
	_ = first.Close()
	second := open()
	select {
	case <-engine.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("admitted restart never reached engine")
	}
	second.beltMu.Lock()
	run := second.beltRun
	second.beltMu.Unlock()
	if run == nil || run.crew == nil {
		t.Fatal("admitted restart lost crew")
	}
	spec := second.beltRunSpec(run, "")
	if spec.WorkModel != worker {
		t.Fatalf("factory lost original send: %s", spec.WorkModel)
	}
	if run.crew.sendFor(worker) != "accepted/replacement" || !run.crew.hasStarted("accepted/replacement") {
		t.Fatal("fallback or answered state lost")
	}
	if !reflect.DeepEqual(run.crew.bad, c.bad) || !reflect.DeepEqual(run.crew.broke, c.broke) || !reflect.DeepEqual(run.crew.gone, c.gone) || run.crew.freeTried[crewroute.Worker] != 2 {
		t.Fatal("failed-route history lost")
	}
	if _, ok := run.crew.failedHere(worker); !ok {
		t.Fatal("resting route forgotten")
	}
	_, err = spec.CompleterFor(worker).CompleteWithMessages(t.Context(), nil, ai.WithModel(worker))
	if err == nil || !strings.Contains(err.Error(), "accepted task cap reached") {
		t.Fatalf("accepted cap reset: %v", err)
	}
	if run.crew.guard.seatTally(crewroute.Checker).Total() != .5 || run.crew.guard.modelSpent[worker] != .25 {
		t.Fatal("seat/model spend reset")
	}
	endBeltRun(t, second, engine)
}

func TestRecoveryRefusesMissingOrUnsettledCrewPolicy(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, r := range []*TaskCrewRecord{nil, {Version: 2}, {Version: 1, Routed: true}, {Version: 1, InFlight: 1}} {
		if _, err := a.restoreTaskCrew(7, r); err == nil {
			t.Fatalf("unsafe policy resumed: %+v", r)
		}
	}
}

func TestCrewCheckpointPersistsCallUncertaintyBeforeReturning(t *testing.T) {
	a, g, _, _ := continueAgent(t)
	row := g.reserve()
	d := recoveryDecision()
	c := &taskCrew{decision: d, original: map[crewroute.Seat]string{}, guard: &SpendGuard{Task: &SpendTask{}}}
	for _, pick := range d.Crew {
		c.original[pick.Seat] = pick.Send
	}
	a.crews.put(row, c)
	a.bindCrewCheckpoint(row, c)
	a.publishRunRow(g, TaskNotice{ID: row, Title: "saved crew", State: TaskQueued})
	read := func() *TaskCrewRecord {
		t.Helper()
		data, err := os.ReadFile(g.store.pathOf())
		var doc taskDocument
		if err == nil {
			err = json.Unmarshal(data, &doc)
		}
		if err != nil {
			t.Fatal(err)
		}
		return doc.Runs[0].CrewState
	}
	if err := c.beginCall(); err != nil {
		t.Fatal(err)
	}
	if r := read(); r == nil || r.InFlight != 1 {
		t.Fatalf("call left before durable uncertainty: %+v", r)
	}
	c.guard.Task.settle(0, .75)
	c.endCall()
	r := read()
	if r.InFlight != 0 || r.Guard.TaskSpent != .75 {
		t.Fatalf("settlement not saved: %+v", r)
	}
	data, err := json.Marshal(runRowRecord(TaskNotice{ID: row, State: TaskRunning, CrewState: r}))
	if err != nil {
		t.Fatal(err)
	}
	var disk runRecord
	if err = json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if runRowNotice(disk).Crew == nil {
		t.Fatal("saved crew missing from reopened display")
	}
}
