package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

const capSentence = "agent cap reached · $0.01 spent of $0.01 · raise the cap before continuing"

// overTheEngineWire is the error as the app really receives it: every engine is
// a remote one, so the type has to survive JSON or it is not there.
func overTheEngineWire(t *testing.T, err error) error {
	t.Helper()
	payload, e := json.Marshal(remote.WireEvent(session.Event{Kind: session.EventError, Err: err}))
	if e != nil {
		t.Fatal(e)
	}
	var wire remote.EventWire
	if e := json.Unmarshal(payload, &wire); e != nil {
		t.Fatal(e)
	}
	return wire.Unwire().Err
}

func TestTurnStopHasOneReasonPerTypedError(t *testing.T) {
	rail := session.TurnStopError(session.TurnStopSpendRail, capSentence)
	launch := session.TurnStopError(session.TurnStopLaunchBudget, "conversation launch limit reached")
	rows := []struct {
		name   string
		err    error
		reason turnStopReason
		words  string
	}{
		{"no error", nil, turnStopFailed, turnFailedWords},
		{"spend rail", rail, turnStopBudget, "Stopped · " + capSentence},
		{"spend rail over the engine wire", overTheEngineWire(t, rail), turnStopBudget, "Stopped · " + capSentence},
		{"spend rail under another sentence", fmt.Errorf("after 2 retries: %w", rail), turnStopBudget, "Stopped · after 2 retries: " + capSentence},
		{"launch limit", launch, turnStopLimit, "Stopped · conversation launch limit reached"},
		{"launch limit over the engine wire", overTheEngineWire(t, launch), turnStopLimit, "Stopped · conversation launch limit reached"},
		{"cancel", fmt.Errorf("turn: %w", context.Canceled), turnStopCancelled, "Stopped"},
		{"transport", errors.New("the connection to the model dropped"), turnStopFailed, "Stopped · the connection to the model dropped"},
		{"transport over the engine wire", overTheEngineWire(t, errors.New("the request did not get through")), turnStopFailed, "Stopped · the request did not get through"},
		{"provider refusal", &provider.APIError{Status: 402, Message: "insufficient credits"}, turnStopFailed, ""},
		{"a sentence that only reads like a cap", errors.New(capSentence), turnStopFailed, "Stopped · " + capSentence},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got := turnStopOf(row.err)
			if got.reason != row.reason || got.failed() != (row.reason == turnStopFailed) {
				t.Fatalf("reason %q failed %v, want %q", got.reason, got.failed(), row.reason)
			}
			if row.words != "" && got.words != row.words {
				t.Fatalf("words %q, want %q", got.words, row.words)
			}
		})
	}
}

// THE DEFAULT CASE IS A FAILURE, so a typed stop the mapping has not been taught
// would be drawn as one. Every kind the engine can name must have its own case.
func TestEveryTypedTurnStopHasAReason(t *testing.T) {
	seen := map[turnStopReason]session.TurnStop{}
	for _, kind := range session.TurnStops() {
		got := turnStopOf(overTheEngineWire(t, session.TurnStopError(kind, "stopped for a reason")))
		if got.failed() {
			t.Errorf("typed stop %q falls to the generic failure; give it a case in turnStopOf", kind)
		}
		if other, taken := seen[got.reason]; taken {
			t.Errorf("typed stops %q and %q share the reason %q", other, kind, got.reason)
		}
		seen[got.reason] = kind
	}
}

func TestTurnStoppedAtTheCapMidTurnSaysSoAndCarriesTheRaise(t *testing.T) {
	f := newTurnFixture(t)
	asked := ""
	f.registry.budgetAsk = func(agent string) *Ask {
		asked = agent
		return &Ask{ID: "budget:1:agent:" + agent, Kind: "budget", State: "waiting", Title: "agent cap reached · $0.01 spent of $0.01",
			Options: []AskOption{{ID: "raise", Label: "Raise the cap"}, {ID: "stop", Label: "Stop"}}}
	}
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"work"}`), 200)
	f.engine.stream(0) <- session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "call", Output: "ran"}
	f.engine.stream(0) <- session.Event{Kind: session.EventError, Err: overTheEngineWire(t, session.TurnStopError(session.TurnStopSpendRail, capSentence))}
	close(f.engine.stream(0))
	f.waitState(t, "idle")

	var ended TurnLifecycleEvent
	records, _, _, _, _ := f.server.events.read(0)
	for _, record := range records {
		if record.kind == EventTurnEnded {
			if err := json.Unmarshal(record.data, &ended); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ended.How != "stopped" || ended.Receipt == nil || ended.Receipt.Text != "Stopped · "+capSentence || ended.Receipt.By != "codeaf" {
		t.Fatalf("turn.ended: %+v receipt %+v", ended, ended.Receipt)
	}
	card := ended.Receipt.Card
	if asked != f.id || card == nil || card.Kind != "budget" || card.ID != "budget:1:agent:"+f.id || len(card.Options) == 0 || card.Options[0].ID != "raise" {
		t.Fatalf("asked %q card %+v", asked, card)
	}
	entries := f.snapshot(t).Snapshot.Entries
	last := entries[len(entries)-1]
	if last.Text != "Stopped · "+capSentence || last.Card != nil {
		t.Fatalf("saved receipt: %+v", last)
	}
}

func TestTurnThatFailedCarriesNoBudgetCard(t *testing.T) {
	f := newTurnFixture(t)
	f.registry.budgetAsk = func(string) *Ask {
		t.Error("a failed turn asked for a budget card")
		return nil
	}
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"work"}`), 200)
	f.engine.stream(0) <- session.Event{Kind: session.EventError, Err: errors.New("the request did not get through")}
	close(f.engine.stream(0))
	f.waitState(t, "failed")
}

func TestAgentBudgetAskNamesTheAgentsOwnCapBeforeTheDays(t *testing.T) {
	home, now := t.TempDir(), time.Now()
	backend := &fakeResourceSpending{cap: usdMoney(20), rows: []MeasuredSpendingRow{{SpendingRow: SpendingRow{At: now, Who: SpendingWho{Kind: "agent", ID: "a"}}, USD: 20}}}
	s := resourceServer(t, home, &now, WithSpendingBackend(context.Background(), backend))
	rows := []struct {
		agent, source string
	}{
		{"a", "agent"},
		{"b", "spending"},
	}
	for _, row := range rows {
		ask := s.agentBudgetAsk(row.agent)
		if ask == nil || ask.Kind != "budget" || ask.Source.Kind != row.source || ask.Options[0].ID != "raise" {
			t.Fatalf("agent %q: %+v", row.agent, ask)
		}
	}
	backend.rows = nil
	if ask := s.agentBudgetAsk("a"); ask != nil {
		t.Fatalf("nothing is at its cap, got %+v", ask)
	}
}
