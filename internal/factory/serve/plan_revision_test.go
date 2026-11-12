package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// POST /v1/jobs/{id}/reject_plan over the real handler, store, record and
// lease desk. The cmd/codeaf suite drives the same route through the app's
// wire; these pin the route's own contract and that /answer is unchanged.

func revisablePlan(person string, firstGate bool) factory.Item {
	it := factory.Item{
		State: factory.StateNeedsYou, Assignee: person,
		Question: factory.ApproveQuestion("plan"), QKind: factory.QKindApprove,
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
		}},
	}
	if firstGate {
		it.Stream.Cur = 0
		it.Stream.Phases = []factory.Phase{
			{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
			{Name: "plan", Kind: factory.StageChat, State: factory.PhasePending},
		}
	}
	return it
}

func rejectBody(version int, words string) string {
	b, _ := json.Marshal(map[string]any{"words": words, "expect": map[string]any{"version": version, "state": "needs_you"}})
	return string(b)
}

func (f *v2Jobs) document(t *testing.T, id int) string {
	t.Helper()
	data, err := os.ReadFile(f.st.ItemPath(id))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *v2Jobs) receipts(kind string) int {
	events, _ := f.rec.Since(context.Background(), time.Time{})
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestRejectPlanRouteSendsBackOnlyARevisablePlan(t *testing.T) {
	f := newV2Jobs(t)
	plan := f.create(t, revisablePlan("sam", false))
	j := f.read(t, plan)
	if !j.Can.RejectPlan || j.NeedsYou == nil || j.NeedsYou.BackTo != "plan" {
		t.Fatalf("read: can %+v needs %+v", j.Can, j.NeedsYou)
	}
	for _, body := range []string{
		`{"words":"redo"}`,
		fmt.Sprintf(`{"words":"redo","expect":{"version":%d}}`, j.Version),
		fmt.Sprintf(`{"words":"redo","expect":{"version":%d,"state":"running"}}`, j.Version),
		rejectBody(j.Version, "  "),
		rejectBody(j.Version, strings.Repeat("é", api.MaxRejectPlanWords+1)),
		fmt.Sprintf(`{"words":"redo","yes":false,"expect":{"version":%d,"state":"needs_you"}}`, j.Version),
	} {
		was := f.document(t, plan.ID)
		f.post(t, plan, "reject_plan", body, 422)
		if f.document(t, plan.ID) != was {
			t.Fatalf("a refused body changed the job: %s", body)
		}
	}
	f.post(t, plan, "reject_plan", rejectBody(j.Version-1, "redo"), 409)
	f.post(t, plan, "reject_plan", rejectBody(j.Version, "redo it"), 200)
	got, _ := f.st.Get(plan.ID)
	if got.State != factory.StateQueued || got.Stream.Cur != 0 || got.Stream.Phases[0].Round != 2 || strings.Join(got.Notes, "|") != "redo it" {
		t.Fatalf("not sent back: %+v", got)
	}
	if f.receipts(record.Answered) != 1 || f.receipts(record.Approved) != 0 {
		t.Fatalf("receipts: answered %d approved %d", f.receipts(record.Answered), f.receipts(record.Approved))
	}
}

// THE ANSWER ROUTE IS UNCHANGED: its no on a first gate still stops the job,
// which is exactly why the typed route refuses one.
func TestRejectPlanRefusesAFirstGateTheAnswerRouteWouldStop(t *testing.T) {
	f := newV2Jobs(t)
	typed := f.create(t, revisablePlan("sam", true))
	j := f.read(t, typed)
	if j.Can.RejectPlan || j.NeedsYou == nil || j.NeedsYou.BackTo != "" {
		t.Fatalf("a first gate is offered: can %+v needs %+v", j.Can, j.NeedsYou)
	}
	was := f.document(t, typed.ID)
	w := f.post(t, typed, "reject_plan", rejectBody(j.Version, "redo"), 409)
	if !strings.Contains(w.Body.String(), "no finished step before it") || f.document(t, typed.ID) != was || f.receipts(record.Answered) != 0 {
		t.Fatalf("refusal %s, or it wrote something", w.Body)
	}

	generic := f.create(t, revisablePlan("sam", true))
	f.post(t, generic, "answer", `{"yes":false,"words":"redo"}`, 200)
	if got, _ := f.st.Get(generic.ID); got.State != factory.StateNew {
		t.Fatalf("the answer route's no on a first gate is %s, not the stop it always was", got.State)
	}
}

// SOLO: the one caller answers every plan, so only the run decides; and a
// factory with no store has no route, which the read says.
func TestRejectPlanInSoloAndWithoutAStore(t *testing.T) {
	f := newV2Jobs(t)
	cfg := f.cfg
	cfg.Mode = api.FactorySolo
	solo := serve.New(f.seam, cfg)
	t.Cleanup(func() { solo.Close() })
	plan := f.create(t, revisablePlan("kim", false))
	w := verbRequest(solo, "GET", fmt.Sprintf("/v1/jobs/%d", plan.ID), "")
	checkJSON(t, w, 200)
	var j api.Job
	_ = json.Unmarshal(w.Body.Bytes(), &j)
	if !j.Can.RejectPlan {
		t.Fatalf("solo can = %+v", j.Can)
	}
	checkJSON(t, verbRequest(solo, "POST", fmt.Sprintf("/v1/jobs/%d/reject_plan", plan.ID), rejectBody(j.Version, "redo")), 200)

	bare := serve.New(f.seam, serve.Config{Mode: api.FactoryTeam, Address: config.Address, Version: config.Version, Now: f.cfg.Now})
	t.Cleanup(func() { bare.Close() })
	other := f.create(t, revisablePlan("sam", false))
	checkJSON(t, verbRequest(bare, "POST", fmt.Sprintf("/v1/jobs/%d/reject_plan", other.ID), rejectBody(1, "redo")), 501)
	w = verbRequest(bare, "GET", fmt.Sprintf("/v1/jobs/%d", other.ID), "")
	checkJSON(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &j)
	if j.Can.RejectPlan {
		t.Fatal("a factory with no route offers it")
	}
}
