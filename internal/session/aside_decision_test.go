package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/decide"
)

// receiptTurn answers n permission questions through a deciding place on an
// agent with a journal, then runs one turn so the drain writes the aside.
func receiptTurn(t *testing.T, n int) (*Agent, string) {
	t.Helper()
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	_, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 95)
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = journal })
	for i := 0; i < n; i++ {
		q := decideQuestion(StakesReversible, AskPermission)
		q.ID = uint64(10 + i)
		if got := gate.apply(agent, &q); got.Outcome != DecideTake {
			t.Fatalf("question %d not decided: %+v", i, got)
		}
	}
	turn(t, agent, completer, "go")
	return agent, journal
}

func decisionEntries(entries []DisplayEntry) []DisplayEntry {
	var out []DisplayEntry
	for _, e := range entries {
		if e.Role == "aside" && e.AsideKind == NoteKindDecision {
			out = append(out, e)
		}
	}
	return out
}

func TestOneDecisionDrawsOneReceiptAside(t *testing.T) {
	agent, _ := receiptTurn(t, 1)
	got := decisionEntries(agent.Transcript())
	if len(got) != 1 || got[0].Decision == nil {
		t.Fatalf("asides = %+v", got)
	}
	d := got[0].Decision
	want := "Allowed automatically by Marketing · matches what Marketing knows · Why?"
	if got[0].Text != want || d.Text != want || len(d.Children) != 0 {
		t.Fatalf("text = %q aside = %+v", got[0].Text, d)
	}
	if d.Why == nil || d.Why.By != "Marketing" || d.Why.Percent != 95 || !d.Why.Reversible || d.Why.Because != "matches what Marketing knows" {
		t.Fatalf("why = %+v", d.Why)
	}
}

func TestThreeDecisionsOfOneBatchGroup(t *testing.T) {
	agent, _ := receiptTurn(t, 3)
	got := decisionEntries(agent.Transcript())
	if len(got) != 1 || got[0].Text != "Did 3 things" || len(got[0].Decision.Children) != 3 {
		t.Fatalf("asides = %+v", got)
	}
	for _, c := range got[0].Decision.Children {
		if c.Why.By != "Marketing" || c.DecisionID == "" || c.Question.ID == "" {
			t.Fatalf("child = %+v", c)
		}
	}
}

func TestReopeningRedrawsTheReceiptsFromTheJournal(t *testing.T) {
	for _, n := range []int{1, 3} {
		live, journal := receiptTurn(t, n)
		want := decisionEntries(live.Transcript())
		if err := live.Close(); err != nil {
			t.Fatal(err)
		}
		got := decisionEntries(reopen(t, journal).Transcript())
		if len(got) != 1 || len(want) != 1 || got[0].Text != want[0].Text || len(got[0].Decision.Children) != len(want[0].Decision.Children) {
			t.Fatalf("n=%d replay %+v != live %+v", n, got, want)
		}
	}
}
