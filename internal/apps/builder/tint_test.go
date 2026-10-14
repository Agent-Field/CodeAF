package builder

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// neighbours is a store that also lists other apps, one of which wears the
// palette's first tint.
type neighbours struct{ *memStore }

func (n neighbours) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	return []bridge.AppSummary{{ID: "crm"}, {ID: "other"}}, nil
}

func (n neighbours) Get(ctx context.Context, app string) (bridge.AppDetail, error) {
	if app == "other" {
		return bridge.AppDetail{AppSummary: bridge.AppSummary{ID: app},
			Files: map[string]string{spec.AppFile: `{"tint":"` + spec.Tints[0] + `"}`}}, nil
	}
	return n.memStore.Get(ctx, app)
}

func tintEdit(text string) bridge.PlanEditParams {
	return bridge.PlanEditParams{Edits: []bridge.PlanEdit{{Section: "tint", Text: text}}}
}

func planTint(t *testing.T, r *rig) string {
	t.Helper()
	b, err := r.svc.buildFor(context.Background(), "crm")
	if err != nil {
		t.Fatal(err)
	}
	return b.currentPlan().Tint
}

func TestCallsRoadDefaultsTheTintToOneNoNeighbourWears(t *testing.T) {
	r := newRig(t)
	r.svc.all.Store = neighbours{r.store}
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	ev, _ := r.hub.find(bridge.StepPlan, bridge.BuildDone)
	if ev.Plan.Tint == "" || ev.Plan.Tint == spec.Tints[0] {
		t.Fatalf("first plan event tint = %q, want a tint the neighbour does not wear", ev.Plan.Tint)
	}
}

func TestCallsRoadTintEditSetsItAndABadOneIsRefused(t *testing.T) {
	r := newRig(t)
	r.create(t, false)
	waitFor(t, "plan card", func() bool { _, ok := r.hub.find(bridge.StepPlan, bridge.BuildDone); return ok })
	res, err := r.svc.EditPlan(context.Background(), r.scope(), tintEdit("rose"))
	if err != nil || res.Plan.Tint != "rose" {
		t.Fatalf("tint edit = %+v, %v", res.Plan, err)
	}
	if _, files := r.store.current(); !strings.Contains(string(files[spec.PlanFile]), `"tint": "rose"`) {
		t.Error("plan.json does not carry the tint")
	}
	_, err = r.svc.EditPlan(context.Background(), r.scope(), tintEdit("purple"))
	be, ok := err.(*bridge.Error)
	if !ok || be.Code != bridge.CodeInvalid || !strings.Contains(be.Message, "tint must be one of iris, sage, sand, tide, rose, graphite") {
		t.Fatalf("bad tint error = %v", err)
	}
	if planTint(t, r) != "rose" {
		t.Error("a refused tint changed the plan")
	}
}

func TestLoopRoadDefaultsAndEditsTheTint(t *testing.T) {
	r := newLoopRig(t, script(planScript()...))
	r.svc.all.Store = neighbours{r.store}
	buildToPlan(t, r)
	ev, _ := r.hub.find(bridge.StepPlan, bridge.BuildDone)
	if ev.Plan.Tint == "" || ev.Plan.Tint == spec.Tints[0] {
		t.Fatalf("first plan event tint = %q", ev.Plan.Tint)
	}
	if _, err := r.svc.EditPlan(context.Background(), r.scope(), tintEdit("sand")); err != nil {
		t.Fatal(err)
	}
	if planTint(t, r) != "sand" {
		t.Errorf("tint after edit = %q", planTint(t, r))
	}
	found := false
	for _, d := range r.draftEvents() {
		found = found || (d.Kind == "tint" && d.Author == "owner")
	}
	if !found {
		t.Error("no owner tint draft event")
	}
	if _, err := r.svc.EditPlan(context.Background(), r.scope(), tintEdit("purple")); err == nil {
		t.Error("a bad tint must be refused")
	}
}
