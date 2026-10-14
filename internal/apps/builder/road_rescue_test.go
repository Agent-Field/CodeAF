package builder

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// rawFailure is the text of the failed calls attempt. The owner must never read it.
const rawFailure = "RAW-CALLS-ERROR agent is missing"

// secondPassChecks fails every check until the owner has been told "Taking a
// second pass", which is the moment the calls attempt is over. After that it
// plays `after` in order, and repeats the last answer.
type secondPassChecks struct {
	hub   *memHub
	after []bool // pass or fail, one per check after the second pass began
	mu    sync.Mutex
	n     int
}

func (c *secondPassChecks) Run(_ context.Context, _ string, _ spec.Files, round int) (bridge.CheckReport, error) {
	passes := false
	if c.hub.saw(secondPass) {
		c.mu.Lock()
		passes = c.after[min(c.n, len(c.after)-1)]
		c.n++
		c.mu.Unlock()
	}
	if passes {
		return bridge.CheckReport{Pass: true, Round: round}, nil
	}
	return bridge.CheckReport{Round: round, Issues: []spec.Issue{{File: spec.AppFile, Rule: "agent", Message: rawFailure}}}, nil
}

func (h *memHub) saw(line string) bool {
	for _, ev := range h.events() {
		if ev.Log == line {
			return true
		}
	}
	return false
}

// newRescueRig is the rig on the rescue road with a scripted worker and a
// check that fails the calls attempt and then plays after.
func newRescueRig(t *testing.T, worker *workerScript, after ...bool) *rig {
	t.Helper()
	r := newLoopRig(t, worker)
	r.svc.road, r.svc.roadName = rescueRoad, roadRescue
	r.svc.all.Check = &secondPassChecks{hub: r.hub, after: after}
	r.model.fixes = []string{goldenFiles([]string{spec.AppFile})}
	return r
}

// writePlanned creates the app, waits for the calls plan card, and writes.
func writePlanned(t *testing.T, r *rig) {
	t.Helper()
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	waitFor(t, "idle", func() bool { b, _ := r.svc.buildFor(context.Background(), "crm"); return !b.isWorking() })
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
}

// failedCards are the events that end a build in the failed card: a failed
// write, or the check giving up. A failed check round is progress, not the end.
func (r *rig) failedCards() []bridge.BuildEvent {
	var out []bridge.BuildEvent
	for _, ev := range r.hub.events() {
		giveUp := ev.Step == bridge.StepCheck && strings.Contains(ev.Log, "did not pass")
		if ev.Status == bridge.BuildFailed && (ev.Step == bridge.StepWrite || giveUp) {
			out = append(out, ev)
		}
	}
	return out
}

func (r *rig) neverShowed(t *testing.T, text string) {
	t.Helper()
	for _, ev := range r.hub.events() {
		if strings.Contains(ev.Log, text) {
			t.Errorf("the owner was shown %q in %+v", text, ev)
		}
	}
}

// bareEvents is the build events with the clock and the running cost removed,
// in a fixed order: the calls write runs its files side by side, so the order
// of the "Wrote" lines and the cost at each one differ from run to run.
func bareEvents(r *rig) []string {
	var out []string
	for _, ev := range r.hub.events() {
		ev.At, ev.Cost = "", bridge.Cost{}
		raw, _ := json.Marshal(ev)
		out = append(out, string(raw))
	}
	sort.Strings(out)
	return out
}

func TestRescueThatPassesTheCallsWriteIsTheCallsRoadByteForByte(t *testing.T) {
	run := func(name string, chosen road) (*rig, string) {
		r := newRig(t)
		root, err := os.MkdirTemp("", "bl")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		r.svc.root = root
		r.svc.road, r.svc.roadName = chosen, name
		writePlanned(t, r)
		r.waitState(t, bridge.StatePreview)
		return r, root
	}
	calls, _ := run(roadCalls, callsRoad)
	rescue, root := run(roadRescue, rescueRoad)

	if a, b := bareEvents(calls), bareEvents(rescue); !reflect.DeepEqual(a, b) {
		t.Errorf("events differ:\ncalls  %+v\nrescue %+v", a, b)
	}
	_, want := calls.store.current()
	_, got := rescue.store.current()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("files differ: calls %v, rescue %v", want.Paths(), got.Paths())
	}
	if _, err := os.Stat(filepath.Join(root, buildsDir)); !os.IsNotExist(err) {
		t.Errorf("a calls pass made a draft folder (stat err %v)", err)
	}
	if got := rescue.road(t); got != roadCalls {
		t.Errorf("road = %q, want %q", got, roadCalls)
	}
}

func TestRescueRunsTheLoopBuildFromThePlanWhenTheCallsWriteFails(t *testing.T) {
	r := newRescueRig(t, script(crmBuild(t, ""), finishTask("build")), true)
	writePlanned(t, r)
	r.waitState(t, bridge.StatePreview)

	if got := r.road(t); got != roadRescueLoop {
		t.Errorf("road = %q, want %q", got, roadRescueLoop)
	}
	if !r.hub.saw(secondPass) {
		t.Error("the owner was not told about the second pass")
	}
	if n := len(r.failedCards()); n != 0 {
		t.Errorf("%d failed cards were shown on a build that was rescued", n)
	}
	r.neverShowed(t, rawFailure)
	_, files := r.store.current()
	for _, want := range goldenPaths() {
		if files[want] == nil {
			t.Errorf("the draft is missing %s", want)
		}
	}
	if _, ok := r.hub.find(bridge.StepPreview, bridge.BuildDone); !ok {
		t.Error("no preview-ready event")
	}
	if n := r.model.calls["plan"]; n != 1 {
		t.Errorf("plan calls = %d: the second pass must start from the approved plan", n)
	}
}

func TestRescueThatStallsIsFinishedByTheFixRounds(t *testing.T) {
	steps := script(crmBuild(t, "# 5\\."), finishTask("build"))
	r := newRescueRig(t, steps, false, true)
	writePlanned(t, r)
	r.waitState(t, bridge.StatePreview)

	if got := r.road(t); got != roadRescueLoopFix {
		t.Errorf("road = %q, want %q", got, roadRescueLoopFix)
	}
	if n := len(r.failedCards()); n != 0 {
		t.Errorf("%d failed cards were shown on a build that was rescued", n)
	}
}

func TestRescueThatFailsEverywhereShowsTheFailedCardOnce(t *testing.T) {
	r := newRescueRig(t, script(finishTask("build")), false)
	writePlanned(t, r)
	r.waitState(t, bridge.StateFailed)
	waitFor(t, "idle", func() bool { b, _ := r.svc.buildFor(context.Background(), "crm"); return !b.isWorking() })

	cards := r.failedCards()
	if len(cards) != 1 {
		t.Fatalf("failed cards = %d, want 1: %+v", len(cards), cards)
	}
	if !r.hub.saw(secondPass) {
		t.Error("the second pass never started")
	}
	if strings.Contains(cards[0].Log, "RAW-CALLS") || strings.Contains(strings.ToLower(cards[0].Log), "error") {
		t.Errorf("the failed card reads like an error: %q", cards[0].Log)
	}
}

func TestRescueNeedsTheTaskBeltLikeTheLoopRoad(t *testing.T) {
	if name, _ := chooseRoad("rescue", false); name != roadCalls {
		t.Errorf("without the belt, road = %q, want calls", name)
	}
	if name, _ := chooseRoad("rescue", true); name != roadRescue {
		t.Errorf("with the belt, road = %q, want rescue", name)
	}
}

// TestRescueSeedFitsLongCallsPlanLines pins the seed against the plans that
// need a rescue most: the calls road allows plan lines to the schema's 200
// characters, the draft verbs take far fewer, and an empty agent line would
// stop `plan ready`. The seed must land every line and reach a ready plan.
func TestRescueSeedFitsLongCallsPlanLines(t *testing.T) {
	store, err := draft.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("Deliverable · brand, fee, due date, status, link, notes; ", 4)[:199]
	plan := prompt.Plan{
		Name:       "Sponsor deals tracker for my channel and its many sponsors",
		Data:       []prompt.Line{{ID: "deal", Line: long}},
		Automation: []prompt.Line{{ID: "chase", Line: long}},
		Screens:    []prompt.Line{{ID: "deals", Line: long}},
	}
	st := &seat{drafts: store, feed: &feed{drafts: store}}
	if err := (&Service{}).seedPlan(st, plan); err != nil {
		t.Fatalf("seed refused: %v", err)
	}
	state, err := store.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != draft.PhasePlanned {
		t.Errorf("phase = %q, want %q", state.Phase, draft.PhasePlanned)
	}
	for _, l := range state.Plan.Data {
		if n := len([]rune(l.Line)); n > draft.MaxPlanLine {
			t.Errorf("seeded line is %d characters, over %d", n, draft.MaxPlanLine)
		}
	}
}

// A worker that never stops hits the step bound; the ladder then finds an
// empty draft and the failed card shows once, in the second pass's own words.
func TestRescueStepBoundStopsTheWorkerAndShowsTheSecondPassCard(t *testing.T) {
	var steps []func(context.Context) *ai.Response
	for i := 0; i < rescueSteps+20; i++ {
		steps = append(steps, types("true"))
	}
	worker := script(steps...)
	r := newRescueRig(t, worker, false)
	writePlanned(t, r)
	r.waitState(t, bridge.StateFailed)

	if n := worker.used(); n > rescueSteps+1 {
		t.Errorf("the worker made %d calls, bound is %d", n, rescueSteps)
	}
	cards := r.failedCards()
	if len(cards) != 1 || cards[0].Log != couldNotFinish {
		t.Errorf("failed cards = %+v, want one saying %q", cards, couldNotFinish)
	}
}

// The wall bound is shortened to 200 ms: a worker that is slow is stopped.
func TestRescueWallBoundStopsASlowWorker(t *testing.T) {
	old := rescueWall
	rescueWall = 200 * time.Millisecond
	t.Cleanup(func() { rescueWall = old })
	slow := func(ctx context.Context) *ai.Response {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return types("true")(ctx)
	}
	r := newRescueRig(t, script(slow, slow), false)
	writePlanned(t, r)
	r.waitState(t, bridge.StateFailed)
	if cards := r.failedCards(); len(cards) != 1 || cards[0].Log != couldNotFinish {
		t.Errorf("failed cards = %+v", cards)
	}
}

// The spend bound stops the worker at the cap and no later than one call past.
func TestASpendCapStopsTheWorkerOnce(t *testing.T) {
	b := &build{budget: 20}
	stopped := 0
	bank := &spendBank{b: b, stop: func() { stopped++ }, cap: rescueSpend}
	bank.add(rescueSpend / 2)
	if stopped != 0 {
		t.Fatal("stopped below the cap")
	}
	bank.add(rescueSpend)
	if stopped == 0 || b.spentNow() > rescueSpend {
		t.Errorf("stopped %d, spent %.2f", stopped, b.spentNow())
	}
}

func TestPluralsInTheCheckLine(t *testing.T) {
	one := bridge.CheckReport{Issues: make([]spec.Issue, 1)}
	two := bridge.CheckReport{Issues: make([]spec.Issue, 2)}
	if got := checkLog(one); got != "1 thing to fix" {
		t.Errorf("one: %q", got)
	}
	if got := checkLog(two); got != "2 things to fix" {
		t.Errorf("two: %q", got)
	}
}

func TestUnsetRoadIsRescueWhenTheBeltIsOn(t *testing.T) {
	if name, _ := chooseRoad("", true); name != roadRescue {
		t.Errorf("default = %q", name)
	}
	if name, _ := chooseRoad("", false); name != roadCalls {
		t.Errorf("default without belt = %q", name)
	}
}

// The rescue's twin of TestAPreviewNeverFollowsAFailedFinalCheck: the worker
// edits the draft after done, so the done phase no longer matches a passing
// Check of these files, and the fix rung must run before any preview.
func TestARescuePreviewNeverFollowsAFailedFinalCheck(t *testing.T) {
	steps := script(crmBuild(t, ""), types(`codeaf app plan data extra "Extra · name"`), finishTask("build"))
	r := newRescueRig(t, steps, true, true, true, false, true)
	writePlanned(t, r)
	r.waitState(t, bridge.StatePreview)
	if got := r.road(t); got == roadRescueLoop {
		t.Errorf("road = %q: the preview followed a draft whose last check was not a pass", got)
	}
}
