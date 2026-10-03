package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/store"
)

// THE FIRST TURN HOLDS FOR THE SHELF, BOUNDED (#1659). The import pass a
// launch used to run before anything could build is off the open now; the
// first message still must not miss foreign skills, so the turn's skill
// resolve waits for the pass — up to the bound, never past it, and never at
// all once the pass is done. A gate nobody handed us is no gate.
func TestTheFirstTurnHoldsForTheShelfBounded(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	never := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("hello"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Skills = st
		config.SkillsReady = never
		config.SkillsReadyWait = 30 * time.Millisecond
	})
	start := time.Now()
	block, _ := agent.turnSkills("hello")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the gate held %v, want around the 30ms bound", elapsed)
	}
	if block != "" {
		t.Fatalf("an empty shelf still rendered a block: %q", block)
	}
	// AND ONCE THE PASS IS DONE, NO TURN WAITS AT ALL: a closed channel
	// answers in the select's first tick.
	closed := make(chan struct{})
	close(closed)
	agent.config.SkillsReady = closed
	start = time.Now()
	_, _ = agent.turnSkills("hello")
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("a closed gate still held %v", elapsed)
	}
}

// AND NO GATE, NO WAIT. A door that ran the pass itself hands no channel;
// every turn composes straight away, exactly as it did before the gate
// existed.
func TestATurnWithoutAGateNeverWaits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("hello"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Skills = st
	})
	start := time.Now()
	_, _ = agent.turnSkills("hello")
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("a gate-less turn held %v", elapsed)
	}
}
