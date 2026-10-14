package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A cap reached between two calls of one turn is the same stop as a cap reached
// before the turn: the typed rail, and the rail's own sentence — never the
// transport taxonomy's words for a request that failed on the wire.
func TestAppCapReachedMidTurnEndsOnTheRailsOwnSentence(t *testing.T) {
	profile := t.TempDir()
	ledger := filepath.Join(profile, "v3", UsageLedgerName)
	var a *Agent
	c := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			a.mu.Lock()
			id := a.appAgentIDLocked()
			a.mu.Unlock()
			appLedgerFixture(t, ledger, UsageLine{At: time.Now(), Session: "child", Root: id, USD: 5})
			return toolResponse("call_1", "read", `{"path":"missing.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Fatal("capped call reached provider")
			return nil, nil
		},
	}}
	a, _ = newTestAgent(t, c, func(cfg *Config) { cfg.ProfileDir = profile; cfg.usageLedger = ledger })
	if err := a.SetAppBudget(5); err != nil {
		t.Fatal(err)
	}
	events := collect(t, mustSubmit(t, a, "Keep working"))
	last := events[len(events)-1]
	if last.Kind != EventError || !errors.Is(last.Err, ErrSpendRail) {
		t.Fatalf("turn ended on %v (%v), want the spend rail", last.Kind, last.Err)
	}
	if said := last.Err.Error(); !strings.HasPrefix(said, "agent cap reached") {
		t.Fatalf("turn ended on %q, want the rail's own sentence", said)
	}
	if c.requests() != 1 || !a.NeedsPerson() {
		t.Fatalf("calls %d waiting %v", c.requests(), a.NeedsPerson())
	}
}
