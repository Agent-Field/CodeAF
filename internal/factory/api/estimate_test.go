package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAnOpenJobCarriesItsMeasuredRangeAndTheQueueItsHitRate(t *testing.T) {
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	r := &factory.Rubric{From: factory.RubricFromRead}
	var items []factory.Item
	for i := range 6 {
		items = append(items, factory.Item{ID: i + 1, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateShipped, DoneHow: "answered",
			Triage: factory.Triage{Rubric: r},
			Stream: &factory.Stream{Spent: 0.5, Started: start.Add(time.Duration(i) * time.Hour), Ended: start.Add(time.Duration(i)*time.Hour + 20*time.Minute)}})
	}
	items = append(items, factory.Item{ID: 9, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Triage: factory.Triage{Rubric: r}},
		factory.Item{ID: 10, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew})
	q := FromSnapshot(factory.Snapshot{Now: start.Add(24 * time.Hour), Items: items}, "")
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range q.Jobs {
		switch {
		case j.ID == 9 && (j.Estimate == nil || j.Estimate.N != 6 || j.Estimate.USDLo != 0.5 || j.Estimate.MinHi != 20 || j.Estimate.Repo != "acme/api"):
			t.Errorf("open scored job estimate = %+v", j.Estimate)
		case j.ID == 10 && j.Estimate != nil:
			t.Errorf("an unscored job got a figure: %+v", j.Estimate)
		case j.ID < 9 && j.Estimate != nil:
			t.Errorf("a finished job carries an estimate: %+v", j.Estimate)
		}
	}
	if c := q.Desk.Estimates; c == nil || c.Jobs != 6 || c.Of != 1 || c.Hit != 1 {
		t.Fatalf("queue calibration = %+v, want 6 jobs, 1 of 1 backtested inside", c)
	}
	if !strings.Contains(string(raw), `"estimate":{"usd_lo":0.5,"usd_hi":0.5,"min_lo":20,"min_hi":20,"n":6,"repo":"acme/api"}`) {
		t.Errorf("wire = %s", raw)
	}
}
