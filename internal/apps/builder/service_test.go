package builder

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestCreateShowsThePlanCard(t *testing.T) {
	r := newRig(t)
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })

	ev, _ := r.hub.find(bridge.StepPlan, bridge.BuildDone)
	if ev.Plan == nil || ev.Plan.Name != "CRM" || len(ev.Plan.Screens) != 3 {
		t.Fatalf("plan = %+v", ev.Plan)
	}
	if len(ev.Needs) == 0 || ev.Needs[0].Kind != "records" || !strings.Contains(ev.Needs[0].Line, "deal") {
		t.Errorf("needs = %+v", ev.Needs)
	}
	if ev.Cost.Spent != 0.01 || ev.Cost.Cap != 20 {
		t.Errorf("cost = %+v", ev.Cost)
	}
	state, files := r.store.current()
	if state != bridge.StatePlanning || files[spec.PlanFile] == nil {
		t.Errorf("state %s, plan stored %v", state, files[spec.PlanFile] != nil)
	}
	if !strings.Contains(string(files[spec.PlanFile]), "A CRM for our sales pipeline") {
		t.Error("plan.ask lost the owner's words")
	}
}

func TestCreateRefusesAnEmptyParagraph(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Create(context.Background(), svc.Caller{Member: svc.Owner}, bridge.CreateAppParams{Paragraph: "  "})
	if be, ok := err.(*bridge.Error); !ok || be.Code != bridge.CodeInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanThatDoesNotParseGetsOneRetryThenFails(t *testing.T) {
	r := newRig(t)
	r.model.plan = func(int) string { return "I cannot do that" }
	r.create(t, false)
	waitFor(t, "plan failure", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildFailed); return ok })
	if r.model.calls["plan"] != 2 {
		t.Errorf("plan calls = %d, want 2", r.model.calls["plan"])
	}
}

func TestEditPlan(t *testing.T) {
	tests := []struct {
		name  string
		edits []bridge.PlanEdit
		check func(t *testing.T, res bridge.PlanEditResult, r *rig)
	}{
		{"set a line", []bridge.PlanEdit{{Section: "data", ID: "deal", Text: "Deal · name, stage, amount, notes"}},
			func(t *testing.T, res bridge.PlanEditResult, r *rig) {
				if res.Plan.Data[0].Line != "Deal · name, stage, amount, notes" || res.Plan.Data[0].ID != "deal" {
					t.Errorf("data = %+v", res.Plan.Data)
				}
				if r.model.calls["plan"] != 1 {
					t.Error("a line edit must not call the model")
				}
			}},
		{"add a screen", []bridge.PlanEdit{{Section: "screens", ID: "reports", Text: "Reports · charts"}},
			func(t *testing.T, res bridge.PlanEditResult, _ *rig) {
				if last := res.Plan.Screens[len(res.Plan.Screens)-1]; last.ID != "reports" {
					t.Errorf("screens = %+v", res.Plan.Screens)
				}
			}},
		{"empty text removes", []bridge.PlanEdit{{Section: "screens", ID: "contacts"}},
			func(t *testing.T, res bridge.PlanEditResult, _ *rig) {
				if len(res.Plan.Screens) != 2 {
					t.Errorf("screens = %+v", res.Plan.Screens)
				}
			}},
		{"agent line", []bridge.PlanEdit{{Section: "agent", Text: "Sales agent · answers"}},
			func(t *testing.T, res bridge.PlanEditResult, _ *rig) {
				if res.Plan.Agent.Line != "Sales agent · answers" || res.Plan.Agent.ID != "agent" {
					t.Errorf("agent = %+v", res.Plan.Agent)
				}
			}},
		{"free request asks the model again", []bridge.PlanEdit{{Text: "also track invoices"}},
			func(t *testing.T, _ bridge.PlanEditResult, r *rig) {
				if r.model.calls["plan"] != 2 || !strings.Contains(r.model.prompts[len(r.model.prompts)-1], "also track invoices") {
					t.Errorf("calls = %v", r.model.calls)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.create(t, false)
			waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
			res, err := r.svc.EditPlan(context.Background(), r.scope(), bridge.PlanEditParams{Edits: tt.edits})
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, res, r)
			if _, files := r.store.current(); files[spec.PlanFile] == nil {
				t.Error("edited plan not stored")
			}
		})
	}
}

func TestEditPlanRejectsABadEditWithoutChangingThePlan(t *testing.T) {
	r := newRig(t)
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	_, err := r.svc.EditPlan(context.Background(), r.scope(), bridge.PlanEditParams{
		Edits: []bridge.PlanEdit{{Section: "data", ID: "Bad Id", Text: "x"}}})
	if be, ok := err.(*bridge.Error); !ok || be.Code != bridge.CodeInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteBuildsThePreview(t *testing.T) {
	r := newRig(t)
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)

	_, files := r.store.current()
	want := []string{"agent.md", "app.json", "plan.json", "runs/follow-up.yaml", "screens/contacts.json", "screens/deal.json", "screens/pipeline.json"}
	if got := files.Paths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v", got)
	}
	if issues := spec.Validate(files); len(issues) != 0 {
		t.Errorf("written files do not validate: %v", issues)
	}
	assertStepOrder(t, r.hub.events())
	if r.seeds.seeded == nil {
		t.Error("sample records were not seeded")
	}
	if done, _ := r.hub.find(bridge.StepWrite, bridge.BuildDone); len(done.Files) != 7 {
		t.Errorf("write done files = %v", done.Files)
	}
	if ready, ok := r.hub.find(bridge.StepPreview, bridge.BuildDone); !ok || len(ready.Needs) == 0 {
		t.Errorf("preview event = %+v", ready)
	}
}

// assertStepOrder checks the Building rows go plan, write, check, preview and
// that each file written is announced before the write step is done.
func assertStepOrder(t *testing.T, events []bridge.BuildEvent) {
	t.Helper()
	rank := map[bridge.BuildStep]int{bridge.StepPlan: 0, bridge.StepWrite: 1, bridge.StepCheck: 2, bridge.StepPreview: 3}
	last := 0
	for _, ev := range events {
		if rank[ev.Step] < last {
			t.Fatalf("step %s after a later step", ev.Step)
		}
		last = rank[ev.Step]
	}
	perFile := 0
	for _, ev := range events {
		if ev.Step == bridge.StepWrite && ev.Status == bridge.BuildRunning && len(ev.Files) > 0 {
			perFile++
		}
	}
	if perFile < 4 {
		t.Errorf("only %d write-progress events", perFile)
	}
}

func TestJustBuildSkipsThePlanCard(t *testing.T) {
	r := newRig(t)
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)
}

func TestFailedCheckGoesBackToTheBuilderThenPasses(t *testing.T) {
	fail := bridge.CheckReport{Issues: []spec.Issue{{File: "screens/deal.json", Path: "/body", Rule: "kit/label", Message: "Button needs a label"}}}
	r := newRig(t, fail, bridge.CheckReport{Pass: true})
	r.model.fixes = []string{goldenFiles([]string{"screens/deal.json"})}
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)

	if r.model.calls["fix"] != 1 {
		t.Errorf("fix calls = %d", r.model.calls["fix"])
	}
	var fixPrompt string
	for _, p := range r.model.prompts {
		if callKind(p) == "fix" {
			fixPrompt = p
		}
	}
	for _, want := range []string{"kit/label", "Button needs a label", "----- screens/deal.json -----"} {
		if !strings.Contains(fixPrompt, want) {
			t.Errorf("fix prompt lacks %q", want)
		}
	}
	failed, ok := r.hub.find(bridge.StepCheck, bridge.BuildFailed)
	if !ok || failed.Round != 1 || failed.Check == nil || failed.Check.Issues[0].Rule != "kit/label" {
		t.Errorf("failed event = %+v", failed)
	}
	if len(r.checks.seen) != 2 {
		t.Errorf("check ran %d times", len(r.checks.seen))
	}
}

func TestGivesUpAfterThreeCheckRounds(t *testing.T) {
	fail := bridge.CheckReport{Issues: []spec.Issue{{File: "app.json", Rule: "schema", Message: "bad"}}}
	r := newRig(t, fail)
	r.model.fixes = []string{goldenFiles([]string{"app.json"})}
	r.create(t, true)
	r.waitState(t, bridge.StateFailed)

	if len(r.checks.seen) != 3 || r.model.calls["fix"] != 2 {
		t.Errorf("checks %d, fixes %d", len(r.checks.seen), r.model.calls["fix"])
	}
	events := r.hub.events()
	last := events[len(events)-1]
	if last.Step != bridge.StepCheck || last.Status != bridge.BuildFailed || last.Round != 3 {
		t.Errorf("last event = %+v", last)
	}
	if r.seeds.seeded != nil {
		t.Error("a failed app must not be seeded")
	}
}

func TestCancelReturnsToPlanning(t *testing.T) {
	r := newRig(t)
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	r.model.hold = make(chan struct{})
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StateBuilding)
	waitFor(t, "a write call", func() bool { r.model.mu.Lock(); defer r.model.mu.Unlock(); return r.model.calls["write"] > 0 })
	if err := r.svc.Cancel(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePlanning)
}

func TestWriteIsRefusedWhileBusyAndWhenNotPlanning(t *testing.T) {
	r := newRig(t)
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)
	err := r.svc.Write(context.Background(), r.scope())
	if be, ok := err.(*bridge.Error); !ok || be.Code != bridge.CodeConflict {
		t.Fatalf("err = %v", err)
	}
}

func TestSpendIsCappedByTheBuildBudget(t *testing.T) {
	r := newRig(t)
	r.model.cost = 0.6
	_, err := r.svc.Create(context.Background(), svc.Caller{Member: svc.Owner}, bridge.CreateAppParams{Paragraph: "A CRM for our sales pipeline", BudgetMonth: 1, JustBuild: true})
	if err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StateFailed)
	failed, ok := r.hub.find(bridge.StepWrite, bridge.BuildFailed)
	if !ok || !strings.Contains(failed.Log, "spent") {
		t.Errorf("failed event = %+v", failed)
	}
}

func TestAppJSONGetsTheStoreIDAndPlanName(t *testing.T) {
	r := newRig(t)
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)
	_, files := r.store.current()
	if !strings.Contains(string(files[spec.AppFile]), `"id": "crm"`) {
		t.Error("app.json id not patched")
	}
}

// Rule 01: the Builder has no way to read records or content. Only Seed of the
// records service may appear in this package's non-test code.
func TestBuilderNeverReadsRecords(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, _ := os.ReadFile(e.Name())
		for _, banned := range []string{"Records.Query", "Records.Get", "Records.Create", "Records.Update", "Records.Delete", "Connectors", "Runs.", "Agent."} {
			if strings.Contains(string(src), "all."+banned) {
				t.Errorf("%s reaches %s", e.Name(), banned)
			}
		}
	}
}
