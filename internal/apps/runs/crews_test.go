package runs

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type crewsGate struct {
	allowed      bool
	app, trigger string
	at           time.Time
	calls        int
}

func (g *crewsGate) Allow(app, trigger string, at time.Time) bool {
	g.app, g.trigger, g.at = app, trigger, at
	g.calls++
	return g.allowed
}

type crewsCap struct {
	month float64
	ok    bool
}

func (c crewsCap) Month(string) (float64, bool) { return c.month, c.ok }

func TestCrewsHooksAreNilSafe(t *testing.T) {
	for _, tt := range []struct {
		name string
		all  *svc.Services
	}{
		{"no services", nil}, {"no hooks", &svc.Services{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := &engine{all: tt.all}
			if !e.crewsAllow("crm", "follow-up") {
				t.Fatal("nil gate denied")
			}
			app := &spec.App{Budget: spec.Budget{Month: 20}}
			e.crewsBudget("crm", app)
			e.crewsBudget("crm", nil)
			if app.Budget.Month != 20 {
				t.Fatal("nil cap changed app.json")
			}
		})
	}
}

func TestCrewsTriggerGateUsesEngineClock(t *testing.T) {
	h := newHarness(t)
	for _, allowed := range []bool{false, true} {
		gate := &crewsGate{allowed: allowed}
		h.all.Crews.TriggerGate = gate
		if got := h.eng.crewsAllow("crm", "follow-up"); got != allowed {
			t.Fatalf("allowed=%v", got)
		}
		if gate.app != "crm" || gate.trigger != "follow-up" || !gate.at.Equal(h.clock.get()) {
			t.Fatalf("gate=%+v", gate)
		}
	}
}

func TestCrewsBudgetOverrideIsUsedByRuns(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cap    crewsCap
		want   float64
		denied bool
	}{
		{"absent override", crewsCap{month: .1}, 20, false},
		{"override denies start", crewsCap{month: .1, ok: true}, .1, true},
		{"override is journaled", crewsCap{month: 10, ok: true}, 10, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.bundle.App.Budget.Month = 20
			h.all.Crews.BudgetCap = tt.cap
			state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "follow-up"})
			if tt.denied {
				var typed *bridge.Error
				if !errors.As(err, &typed) || typed.Code != bridge.CodeBudget {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			job, err := h.eng.loadJob("crm", state.ID)
			if err != nil || job.Month != tt.want {
				t.Fatalf("job=%+v error=%v", job, err)
			}
		})
	}
}

func TestCrewsRecordTriggerHookSkipsPausedEvents(t *testing.T) {
	for _, tt := range []struct {
		name string
		gate *crewsGate
		want int
	}{
		{"nil gate", nil, 1}, {"allowed", &crewsGate{allowed: true}, 1}, {"paused", &crewsGate{}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			if tt.gate != nil {
				h.all.Crews.TriggerGate = tt.gate
			}
			h.bundle.App.Triggers = []spec.Trigger{{ID: "crew-test", On: spec.TriggerRecordCreated, Type: "deal", Run: "crew-test"}}
			h.bundle.Runs["crew-test"] = &spec.Run{Run: "crew-test"}
			diff := bridge.RecordDiff{Op: bridge.DiffUpsert, Type: "deal", ID: "new-deal", Record: &bridge.Record{ID: "new-deal", Type: "deal", Version: 1}}
			h.eng.handleRecord(h.ctx, "crm", diff)
			if tt.gate != nil {
				if tt.gate.calls != 1 || tt.gate.trigger != "crew-test" {
					t.Fatalf("gate=%+v", tt.gate)
				}
				tt.gate.allowed = true
			}
			// Resume does not replay the event that was skipped while paused.
			rows, err := h.runs.List(h.ctx, h.scope(), bridge.ListRunsParams{Run: "crew-test"})
			if err != nil || len(rows.Runs) != tt.want {
				t.Fatalf("runs=%+v error=%v", rows, err)
			}
		})
	}
}
