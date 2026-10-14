package builder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
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
