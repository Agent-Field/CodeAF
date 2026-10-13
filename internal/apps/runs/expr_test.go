package runs

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func TestEval(t *testing.T) {
	env := map[string]any{
		"stage": "Proposal", "me": "me",
		"params":   map[string]any{"deals": "stage:Proposal"},
		"steps":    map[string]any{"draft": map[string]any{"output": map[string]any{"subject": "Hi"}}},
		"_changed": map[string]bool{"stage": true},
	}
	tests := []struct {
		expr string
		want any
	}{
		{"stage == 'Proposal'", true},
		{"changed('stage') && stage == 'Proposal'", true},
		{"changed('amount')", false},
		{"$steps.draft.output.subject", "Hi"},
		{"$me", "me"},
		{"false", false},
	}
	for _, test := range tests {
		got, err := eval(test.expr, env)
		if err != nil || got != test.want {
			t.Errorf("%s = %#v err %v", test.expr, got, err)
		}
	}
	if render("Hello {{steps.draft.output.subject}}", env) != "Hello Hi" {
		t.Fatalf("render = %q", render("Hello {{steps.draft.output.subject}}", env))
	}
}

func TestMatchQuery(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	row := bridge.Record{ID: "dl_umbrella", Fields: map[string]any{
		"stage": "Proposal", "name": "Umbrella", "last_touch": "2026-09-25T16:45:00Z", "amount": 9800.0,
	}}
	fresh := bridge.Record{ID: "dl_globex", Fields: map[string]any{
		"stage": "Negotiation", "last_touch": "2026-10-10T14:00:00Z",
	}}
	tests := []struct {
		query string
		row   bridge.Record
		want  bool
	}{
		{"stage:Proposal,Negotiation last_touch<-14d", row, true},
		{"stage:Proposal,Negotiation last_touch<-14d", fresh, false},
		{"id:dl_umbrella", row, true},
		{"stage:-Lost", row, true},
		{"amount>10000", row, false},
		{"name:Um*", row, true},
	}
	for _, test := range tests {
		if got := matchQuery(test.row, test.query, now); got != test.want {
			t.Errorf("%s on %s = %v", test.query, test.row.ID, got)
		}
	}
}

func TestCronDue(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 5, 0, 0, time.UTC)
	if !due("* * * * *", now, time.Time{}) {
		t.Fatal("first minute should be due")
	}
	if due("* * * * *", now, now) {
		t.Fatal("same minute should not fire twice")
	}
	if !due("5 12 * * *", now, time.Time{}) {
		t.Fatal("12:05 should match")
	}
	if due("6 12 * * *", now, time.Time{}) {
		t.Fatal("12:06 should not match 12:05")
	}
	if _, ok := parseCron("not a cron"); ok {
		t.Fatal("bad cron parsed")
	}
	if backoff("30s", 3) != 2*time.Minute {
		t.Fatalf("backoff = %s", backoff("30s", 3))
	}
}
