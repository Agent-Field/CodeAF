package session

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workstream"
)

// WP-F2 (e): the last second of day 89 fits every grant clock on today's UTC day.
func TestWorkstreamGoAheadDateEdgeIs89Days(t *testing.T) {
	for _, hour := range []int{0, 12, 23} {
		now := time.Date(2026, 11, 3, hour, 0, 0, 0, time.UTC)
		ask := WorkstreamGoAheadAsk{Scope: "job", Factory: "f", Job: 7, CapUSD: 1, Why: "Review"}
		ask.EndsOn = now.AddDate(0, 0, 89).Format("2006-01-02")
		if err := WorkstreamValidateGoAhead(ask, now); err != nil {
			t.Fatal(err)
		}
		ask.EndsOn = now.AddDate(0, 0, 90).Format("2006-01-02")
		if err := WorkstreamValidateGoAhead(ask, now); err == nil || !strings.Contains(err.Error(), "at most 89 days ahead") {
			t.Fatalf("90-day edge: %v", err)
		}
	}
	if !strings.Contains(workstreamGoAheadSchema, "at most 89 days ahead") {
		t.Fatal("schema date bound drifted")
	}
}

// WP-F2 (e): resolve once before retry, stamp every added draft, preserve old items.
func TestWorkstreamDraftSourcesSurviveTheSingleRetry(t *testing.T) {
	for _, source := range []*workstream.Source{nil, {Kind: "page", ID: "page", Label: "Spec"}} {
		d := &workstreamFakeDoor{conflicts: 1, plan: WorkstreamPlanState{Waves: []workstream.Wave{{N: 1, Items: []workstream.PlanItem{{Kind: "draft", DraftID: "old", Title: "Old", Size: "L", BudgetUSD: 10, Source: &workstream.Source{Kind: "message", ID: "m", ChatID: "chat"}}}}}}}
		before := d.plan.Waves[0]
		args := `{"jobs":[{"title":"One"},{"title":"Two"}]}`
		want := &workstream.Source{Kind: "chat", ID: "agent-chat"}
		if source != nil {
			args = `{"jobs":[{"title":"One"},{"title":"Two"}],"source":{"kind":"page","id":"page","label":"Spec"}}`
			want = source
		}
		workstreamCallForTest(t, d, "propose_jobs", args, false)
		if d.sourceCalls != 1 || len(d.saves) != 2 {
			t.Fatalf("resolve/retry calls: %+v", d)
		}
		for _, saved := range d.saves {
			if !reflect.DeepEqual(saved.Waves[0], before) {
				t.Fatal("old item metadata changed")
			}
			wave := saved.Waves[len(saved.Waves)-1]
			for _, item := range wave.Items {
				if !reflect.DeepEqual(item.Source, want) || item.Size != "" || item.BudgetUSD != 0 {
					t.Fatalf("draft: %+v", item)
				}
			}
		}
	}
}

// WP-F2 (e): invalid explicit source refuses without a plan write.
func TestWorkstreamDraftSourceRefusalAddsNothing(t *testing.T) {
	d := &workstreamFakeDoor{sourceError: errors.New("source page does not exist")}
	workstreamCallForTest(t, d, "propose_jobs", `{"jobs":[{"title":"Draft"}],"source":{"kind":"page","id":"missing"}}`, true)
	if d.sourceCalls != 1 || d.reads != 0 || len(d.saves) != 0 {
		t.Fatalf("refusal wrote: %+v", d)
	}
	for _, args := range []string{
		`{"jobs":[{"title":"Draft","size":"S"}]}`,
		`{"jobs":[{"title":"Draft","budget_usd":1}]}`,
		`{"jobs":[{"title":"Draft"}],"source":{"kind":"message","id":"m"}}`,
		`{"jobs":[{"title":"Draft"}],"source":{"kind":"page","id":"p","extra":true}}`,
		`{"jobs":[{"title":"Draft"}],"source":{"kind":"page","id":"p","factory":""}}`,
	} {
		d := &workstreamFakeDoor{}
		workstreamCallForTest(t, d, "propose_jobs", args, true)
		if d.sourceCalls != 0 || d.reads != 0 || len(d.saves) != 0 {
			t.Fatal("invalid source reached the door")
		}
	}
}

// WP-F2 (f): the agent's read grammar accepts the structured proposal kind.
func TestWorkstreamReadAcceptsGoAheadKind(t *testing.T) {
	d := &workstreamFakeDoor{read: []byte(`{"records":[]}`)}
	workstreamCallForTest(t, d, "workstream_read", `{"what":"records","kind":"goahead"}`, false)
	if d.readAsk.Kind != "goahead" {
		t.Fatalf("kind: %+v", d.readAsk)
	}
}
