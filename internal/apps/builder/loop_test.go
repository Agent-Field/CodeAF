package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// newLoopRig is the rig on the loop road with a scripted worker. The root is a
// short path of its own because the build's socket lives under it and a unix
// socket's path is short-limited.
func newLoopRig(t *testing.T, worker *workerScript, reports ...bridge.CheckReport) *rig {
	t.Helper()
	armDoor(t)
	r := newRig(t, reports...)

	root, err := os.MkdirTemp("", "bl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	r.svc.root = root
	r.svc.road, r.svc.roadName = loopRoad, roadLoop
	r.svc.completerFor = func(string) (session.Completer, error) { return worker, nil }
	r.showTrajectories(t)
	return r
}

func (b *build) isWorking() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.working
}

func (r *rig) draftEvents() []bridge.DraftEvent {
	r.hub.mu.Lock()
	defer r.hub.mu.Unlock()
	return append([]bridge.DraftEvent(nil), r.hub.drafts...)
}

// showTrajectories logs what each task's worker ran and what came back, when
// the test failed: the quickest way to read why a script stopped.
func (r *rig) showTrajectories(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		st := r.seat(t)
		for _, id := range []string{"plan", "build", "replan"} {
			steps, _ := run.Trajectory(st.dir, id)
			for _, step := range steps {
				out := step.Observation
				if raw, err := os.ReadFile(step.FullOutput); err == nil {
					out = string(raw)
				}
				t.Logf("%s step %d: %.120s => ...%s", id, step.Step, step.Command, tail(out, 1500))
			}
		}
	})
}

func (r *rig) road(t *testing.T) string {
	t.Helper()
	b, err := r.svc.buildFor(context.Background(), "crm")
	if err != nil {
		t.Fatal(err)
	}
	return b.result().Road
}

func (r *rig) seat(t *testing.T) *seat {
	t.Helper()
	b, err := r.svc.buildFor(context.Background(), "crm")
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.svc.openSeat(b)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestLoopPlanFillsTheCardFromTheJournal(t *testing.T) {
	worker := script(planScript()...)
	r := newLoopRig(t, worker)
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })

	ev, _ := r.hub.find(bridge.StepPlan, bridge.BuildDone)
	if ev.Plan == nil || ev.Plan.Name != "CRM" || len(ev.Plan.Data) != 1 || len(ev.Plan.Screens) != 1 {
		t.Fatalf("plan = %+v", ev.Plan)
	}
	if !strings.Contains(ev.Plan.Ask, "A CRM for our sales pipeline") {
		t.Errorf("plan.ask lost the owner's words: %q", ev.Plan.Ask)
	}
	if len(ev.Needs) == 0 || !strings.Contains(ev.Needs[0].Line, "deal") {
		t.Errorf("needs = %+v", ev.Needs)
	}
	waitFor(t, "draft events", func() bool { return len(r.draftEvents()) >= 5 })
	for _, d := range r.draftEvents() {
		if d.Line == "" || strings.Contains(d.Line, "codeaf app") || strings.HasPrefix(d.Line, "error") {
			t.Errorf("a draft line reads like a verb or an error: %+v", d)
		}
	}
	if state, files := r.store.current(); state != bridge.StatePlanning || files[spec.PlanFile] == nil {
		t.Errorf("state %s, plan stored %v", state, files[spec.PlanFile] != nil)
	}
}

func TestLoopPlanThatNeverGetsReadyFallsBackToTheCallsPlan(t *testing.T) {
	r := newLoopRig(t, script(types(`codeaf app name "CRM"`)))
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })

	ev, _ := r.hub.find(bridge.StepPlan, bridge.BuildDone)
	if ev.Plan == nil || len(ev.Plan.Screens) != 3 {
		t.Fatalf("the calls plan should be on the card, got %+v", ev.Plan)
	}
	if r.model.calls["plan"] != 1 {
		t.Errorf("plan calls = %d, want 1", r.model.calls["plan"])
	}
	b, _ := r.svc.buildFor(context.Background(), "crm")
	if !b.isDemoted() {
		t.Error("a build whose plan came from the calls road must stay on it")
	}
}

// buildToPlan creates the app on the loop road and waits for its card.
func buildToPlan(t *testing.T, r *rig) {
	t.Helper()
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	waitFor(t, "idle", func() bool { b, _ := r.svc.buildFor(context.Background(), "crm"); return !b.isWorking() })
}

func TestLoopWriteWithAnEmptyDraftTakesTheCallsRoad(t *testing.T) {
	steps := append(planScript(), finishTask("build"))
	r := newLoopRig(t, script(steps...))
	buildToPlan(t, r)
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)
	if got := r.road(t); got != roadCalls {
		t.Errorf("road = %q, want %q", got, roadCalls)
	}
	if r.model.calls["write"] == 0 {
		t.Error("the calls pipeline should have written the files")
	}
}

func TestAnOwnerEditMidBuildLeavesANoteOnTheWorkersTask(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release")
	steps := append(planScript(),
		types("while [ ! -f "+release+" ]; do sleep 0.02; done; echo waited"),
		finishTask("build"))
	r := newLoopRig(t, script(steps...))
	buildToPlan(t, r)
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	st := r.seat(t)
	waitFor(t, "a live build worker", func() bool { return st.liveTask() == "build" })

	_, err := r.svc.EditPlan(context.Background(), r.scope(), bridge.PlanEditParams{Edits: []bridge.PlanEdit{
		{Section: "screens", ID: "pipeline", Text: "Pipeline · deals by stage, newest first"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	notes := st.tasks.Notes("build", 0)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Owner changed screens pipeline") {
		t.Fatalf("notes on the build task = %+v", notes)
	}
	waitFor(t, "owner draft event", func() bool {
		for _, d := range r.draftEvents() {
			if d.Author == string(draft.AuthorOwner) && strings.Contains(d.Line, "newest first") {
				return true
			}
		}
		return false
	})
	if got := r.svc.mustPlan(t, r).Screens[0].Line; !strings.Contains(got, "newest first") {
		t.Errorf("the plan card still reads %q", got)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)
}

func (s *Service) mustPlan(t *testing.T, r *rig) spec.Plan {
	t.Helper()
	b, err := s.buildFor(context.Background(), "crm")
	if err != nil {
		t.Fatal(err)
	}
	return specPlan(b.currentPlan())
}

func TestAFreeRequestWithNoLiveWorkerStartsAReplan(t *testing.T) {
	steps := append(planScript(),
		types(`codeaf app plan data contact "Contact · name, email"`),
		types(`codeaf app plan ready`),
		finishTask("replan"))
	r := newLoopRig(t, script(steps...))
	buildToPlan(t, r)
	before := len(r.hub.events())

	_, err := r.svc.EditPlan(context.Background(), r.scope(), bridge.PlanEditParams{Edits: []bridge.PlanEdit{
		{Text: "Add contacts too"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a second plan card", func() bool {
		n := 0
		for _, ev := range r.hub.events()[before:] {
			if ev.Step == bridge.StepPlan && ev.Status == bridge.BuildDone {
				n++
			}
		}
		return n == 1
	})
	if plan := r.svc.mustPlan(t, r); len(plan.Data) != 2 {
		t.Errorf("data lines after the replan = %+v", plan.Data)
	}
}

func TestACancelledBuildReleasesItsTaskAndReturnsToPlanning(t *testing.T) {
	steps := append(planScript(), types("sleep 30"))
	r := newLoopRig(t, script(steps...))
	buildToPlan(t, r)
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	st := r.seat(t)
	waitFor(t, "a live build worker", func() bool { return st.liveTask() == "build" })
	if err := r.svc.Cancel(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePlanning)
	waitFor(t, "the task to be released", func() bool {
		task := st.tasks.Task("build")
		return task != nil && task.Status != "running"
	})
	if task := st.tasks.Task("build"); task.Status == "failed" {
		t.Error("a cancelled task must be left to start again, not failed")
	}
}

func TestARestartRebuildsThePlanFromTheJournal(t *testing.T) {
	r := newLoopRig(t, script(planScript()...))
	buildToPlan(t, r)

	r.store.mu.Lock()
	r.store.files = spec.Files{} // the store forgot plan.json; only the journal remembers
	r.store.mu.Unlock()
	again := New(svc.Config{Ctx: context.Background(), Root: r.svc.root}, r.svc.all)
	again.road, again.roadName = loopRoad, roadLoop
	b, err := again.buildFor(context.Background(), "crm")
	if err != nil {
		t.Fatal(err)
	}
	plan := b.currentPlan()
	if plan.Name != "CRM" || len(plan.Screens) != 1 || !strings.Contains(plan.Ask, "A CRM for our sales pipeline") {
		t.Fatalf("plan after restart = %+v", plan)
	}
}

// crmPart is a slice of the golden CRM verb script as one shell command: the
// lines from the one that starts with from up to and including the one that
// starts with to ("" means the end of the script).
func crmPart(t *testing.T, from, to string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crm.sh")
	if err := os.WriteFile(path, []byte(draft.CRMScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	end := "$"
	if to != "" {
		end = "/^" + to + "/"
	}
	// The script is handed to bash as its argument and not on stdin: verbs such
	// as command read stdin, and a piped script would feed them its own lines.
	return `bash -c "$(sed -n '/^` + from + `/,` + end + `p' ` + path + `)" < /dev/null`
}

// crmPlan types the plan half of the CRM script; crmBuild the rest.
func crmPlan(t *testing.T) func(context.Context) *ai.Response {
	return types(crmPart(t, "codeaf app name", "codeaf app plan ready"))
}

func crmBuild(t *testing.T, to string) func(context.Context) *ai.Response {
	return types(crmPart(t, "# 3\\.", to))
}

func goldenPaths() []string {
	files, _ := spec.GoldenCRM()
	return files.Paths()
}

func TestLoopTypingTheCRMScriptReachesPreviewOnTheLoopRoad(t *testing.T) {
	r := newLoopRig(t, script(crmPlan(t), finishTask("plan"), crmBuild(t, ""), finishTask("build")))
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	waitFor(t, "idle", func() bool { b, _ := r.svc.buildFor(context.Background(), "crm"); return !b.isWorking() })
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)

	if got := r.road(t); got != roadLoop {
		t.Errorf("road = %q, want %q", got, roadLoop)
	}
	if n := r.model.calls["write"] + r.model.calls["plan"] + r.model.calls["fix"]; n != 0 {
		t.Errorf("the single-call pipeline ran %d times on a build that finished by itself", n)
	}
	_, files := r.store.current()
	for _, want := range goldenPaths() {
		if files[want] == nil {
			t.Errorf("the draft is missing %s", want)
		}
	}
	if len(r.checks.seen) != 1 {
		t.Errorf("check ran %d times; check, try and done on one render must share one run", len(r.checks.seen))
	}
	if _, ok := r.hub.find(bridge.StepPreview, bridge.BuildDone); !ok {
		t.Error("no preview-ready event")
	}
}

func TestLoopThatStopsHalfwayIsFixedByTheCallsFixRounds(t *testing.T) {
	steps := []func(context.Context) *ai.Response{
		crmPlan(t), finishTask("plan"), crmBuild(t, "# 5\\."), finishTask("build"),
	}
	r := newLoopRig(t, script(steps...), bridge.CheckReport{Pass: false, Issues: []spec.Issue{{File: spec.AppFile, Rule: "agent", Message: "the agent is missing"}}},
		bridge.CheckReport{Pass: true})
	r.model.fixes = []string{goldenFiles(goldenPaths())}
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	waitFor(t, "idle", func() bool { b, _ := r.svc.buildFor(context.Background(), "crm"); return !b.isWorking() })
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)

	if got := r.road(t); got != roadLoopFix {
		t.Errorf("road = %q, want %q", got, roadLoopFix)
	}
	if r.model.calls["fix"] != 1 || r.model.calls["write"] != 0 {
		t.Errorf("fix calls %d, write calls %d", r.model.calls["fix"], r.model.calls["write"])
	}
}

func tail(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[len(text)-n:]
}

func TestPlanJSONWaitsForPlanReadyAndSurvivesAReplan(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release")
	r := newLoopRig(t, script(
		types(`codeaf app name "CRM"`),
		types(`codeaf app plan data deal "Deal · name"`),
		types("while [ ! -f "+release+" ]; do sleep 0.02; done"),
		types(`codeaf app plan screen pipeline "Pipeline · deals"`),
		types(`codeaf app plan agent "Helps"`),
		types(`codeaf app plan ready`),
		finishTask("plan")))
	r.create(t, false)
	st := r.seat(t)
	waitFor(t, "two lines", func() bool { return len(r.draftEvents()) >= 2 })
	if _, files := r.store.current(); files[spec.PlanFile] != nil {
		t.Fatal("plan.json reached the app before the plan was ready")
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "plan.json", func() bool { _, f := r.store.current(); return f[spec.PlanFile] != nil })
	_ = st
}

func TestHoldPlanKeepsTheLastReadyPlanThroughAReplan(t *testing.T) {
	f := &feed{}
	ready := spec.Files{spec.PlanFile: []byte("ready")}
	if got := f.holdPlan(draft.State{Phase: draft.PhasePlanned}, ready); string(got[spec.PlanFile]) != "ready" {
		t.Fatal("a ready plan must be written")
	}
	early := spec.Files{spec.PlanFile: []byte("half"), spec.AppFile: []byte("{}")}
	got := f.holdPlan(draft.State{Phase: draft.PhasePlanning}, early)
	if string(got[spec.PlanFile]) != "ready" || got[spec.AppFile] == nil {
		t.Fatalf("a replan must keep the last ready plan and stream the rest: %v", got)
	}
}

func TestTintIsTheFirstUnusedAndFallsBackToTheLeastUsed(t *testing.T) {
	palette := []string{"a", "b", "c"}
	if got := leastUsed(palette, map[string]int{"a": 1}); got != "b" {
		t.Errorf("first unused = %q, want b", got)
	}
	if got := leastUsed(palette, map[string]int{"a": 2, "b": 1, "c": 2}); got != "b" {
		t.Errorf("least used = %q, want b", got)
	}
	if line := tintLine("b", []string{"a"}); line != "Tint hint: b (other apps use: a)" {
		t.Errorf("line = %q", line)
	}
}

// seqChecks answers Check from a list of reports in the order of the calls,
// whatever round each call names.
type seqChecks struct {
	mu      sync.Mutex
	reports []bridge.CheckReport
	n       int
}

func (c *seqChecks) Run(context.Context, string, spec.Files, int) (bridge.CheckReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.reports[min(c.n, len(c.reports)-1)]
	c.n++
	return r, nil
}

func TestAFixRungThatKeepsFailingFallsToTheCallsRungBeforeTheCard(t *testing.T) {
	steps := []func(context.Context) *ai.Response{
		crmPlan(t), finishTask("plan"), crmBuild(t, "# 5\\."), finishTask("build"),
	}
	r := newLoopRig(t, script(steps...))
	bad := bridge.CheckReport{Issues: []spec.Issue{{File: spec.AppFile, Rule: "agent", Message: "still wrong"}}}
	// Three failed rounds end the fix rung; the calls rung's first check passes.
	r.svc.all.Check = &seqChecks{reports: []bridge.CheckReport{bad, bad, bad, {Pass: true}}}
	r.model.fixes = []string{goldenFiles(goldenPaths())}
	buildToPlan(t, r)
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)

	if got := r.road(t); got != roadCalls {
		t.Errorf("road = %q, want %q", got, roadCalls)
	}
	if r.model.calls["fix"] != 2 || r.model.calls["write"] == 0 {
		t.Errorf("fix calls %d, write calls %d", r.model.calls["fix"], r.model.calls["write"])
	}
	for _, ev := range r.hub.events() {
		if ev.Status == bridge.BuildFailed && ev.Step == bridge.StepWrite {
			t.Errorf("the failed card showed before the last rung: %+v", ev)
		}
	}
}

func TestAFreeRequestWhileTheBuilderIsBusyIsQueuedAsANote(t *testing.T) {
	r := newLoopRig(t, script(planScript()...))
	buildToPlan(t, r)
	b, _ := r.svc.buildFor(context.Background(), "crm")
	if _, err := b.begin(context.Background()); err != nil { // busy, with no worker live
		t.Fatal(err)
	}
	defer b.finish()
	if _, err := r.svc.EditPlan(context.Background(), r.scope(), bridge.PlanEditParams{Edits: []bridge.PlanEdit{{Text: "Add contacts"}}}); err != nil {
		t.Fatalf("a busy Builder must queue the request, got %v", err)
	}
	notes := r.seat(t).tasks.Notes("plan", 0)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Add contacts") {
		t.Fatalf("notes = %+v", notes)
	}
}

// slowChecks counts its runs and holds each one until released.
type slowChecks struct {
	mu      sync.Mutex
	runs    int
	release chan struct{}
}

func (c *slowChecks) Run(context.Context, string, spec.Files, int) (bridge.CheckReport, error) {
	c.mu.Lock()
	c.runs++
	c.mu.Unlock()
	<-c.release
	return bridge.CheckReport{Pass: true}, nil
}

func (c *slowChecks) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs
}

func TestTwoVerbsOnOneRenderRunOneCheckAndShareItsReport(t *testing.T) {
	r := newLoopRig(t, script(planScript()...))
	buildToPlan(t, r)
	checks := &slowChecks{release: make(chan struct{})}
	r.svc.all.Check = checks
	st := r.seat(t)
	b, _ := r.svc.buildFor(context.Background(), "crm")
	handle := r.svc.verbHandler(b, st)

	replies := make(chan draft.Reply, 3)
	for _, verb := range []string{"check", "check", "done"} {
		go func() { replies <- handle(context.Background(), draft.Request{Verb: verb, Args: []string{"x"}}) }()
	}
	waitFor(t, "the first check to start", func() bool { return checks.count() == 1 })
	close(checks.release)
	for i := 0; i < 3; i++ {
		if reply := <-replies; !reply.OK {
			t.Errorf("reply = %+v", reply)
		}
	}
	if checks.count() != 1 {
		t.Errorf("checks ran %d times, want 1", checks.count())
	}
}

func TestCheckTryAndDoneRefuseWhileThePlanIsStillBeingMade(t *testing.T) {
	r := newLoopRig(t, script(types(`codeaf app name "CRM"`), types("sleep 20")))
	r.create(t, false)
	st := r.seat(t)
	b, _ := r.svc.buildFor(context.Background(), "crm")
	waitFor(t, "a live plan worker", func() bool { return st.liveTask() == "plan" })
	handle := r.svc.verbHandler(b, st)
	for _, verb := range []string{"check", "try", "done"} {
		reply := handle(context.Background(), draft.Request{Verb: verb, Args: []string{"x"}})
		if reply.OK || reply.Text != "error: check runs after plan ready — finish the plan first" {
			t.Errorf("%s answered %+v", verb, reply)
		}
	}
	if err := r.svc.Cancel(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
}
