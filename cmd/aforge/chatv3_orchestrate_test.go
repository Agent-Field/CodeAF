package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE SEAM THAT WAS NEVER FILLED. Config.OrchestrateRunner is how a turn or a
// tool reaches the adaptive engine, and until this wiring existed nothing in any
// shipping door assigned it — so the engine had no caller outside its own tests
// and the model was never handed the verb.

// A CONVERSATION THIS DOOR OPENS CAN RUN ONE. The runner is on the config the
// agent is built from, whatever the launch left there.
func TestTheV3DoorWiresTheAdaptiveRunner(t *testing.T) {
	wired, runs := v3Adaptive(session.Config{Workspace: t.TempDir(), Model: "test/model"})
	if wired.OrchestrateRunner == nil {
		t.Fatal("the config went to the session with no runner on it, which is orchestration off")
	}
	if runs == nil {
		t.Fatal("nothing was handed back to bind to the agent")
	}
}

// AND IT REACHES THE AGENT IT BELONGS TO. The seam exists because the config
// builds the agent, so there is nothing to close over when the closure is made;
// what it must never do is reach some other conversation's session.
func TestTheWiredRunnerReachesThisConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	agent, err := v3OpenSession(session.Config{
		Workspace: workspace,
		Model:     "test/model",
		APIKey:    "test-key",
		BaseURL:   "https://example.invalid/v1",
	})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	// Closed first, so the run started below is cancelled before this test
	// returns: it is a real run and it would otherwise sit trying to think.
	defer func() { _ = agent.Close() }()

	_, runs := v3Adaptive(session.Config{})
	runs.bind(agent)
	id, err := runs.start(context.Background(), "audit the pricing code", "", 1)
	if err != nil {
		t.Fatalf("the runner refused: %v", err)
	}
	if id == "" {
		t.Fatal("the runner started nothing")
	}
	// The id names a run THIS session holds, which is the whole point of binding
	// per conversation: the room draws what it can look up.
	if _, known := agent.OrchestrateSnapshot(id); !known {
		t.Fatalf("run %q is not registered in the session that started it", id)
	}
}

// AN UNBOUND SEAM SAYS SO. It is a bug in this file rather than a state a
// person can reach, and a conversation is not worth crashing over.
func TestAnUnboundRunnerRefusesRatherThanPanics(t *testing.T) {
	_, runs := v3Adaptive(session.Config{})
	id, err := runs.start(context.Background(), "audit the pricing code", "", 1)
	if err == nil {
		t.Fatalf("an unbound seam started run %q", id)
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("the refusal says %q", err)
	}
}
