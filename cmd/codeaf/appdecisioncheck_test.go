package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE DECISION CHECK'S VERB REACHES THE DESK (live2/E6b). ws_task_conflict is
// never registered, so the model never holds it, but a set that serves
// ws_decisions is sent it; any other set is not.
func TestClientToolsTheDecisionCheckVerbReachesASetThatKeepsDecisions(t *testing.T) {
	b, door, emitter := clientToolBackend(t)
	if _, err := b.SetClientTools(clientToolParams(door.agent, "work", session.WSDecisionsTool)); err != nil {
		t.Fatal(err)
	}
	emitter.answer = func(event appwire.AgentsToolCall) {
		if event.Name != session.WSTaskConflictVerb {
			t.Errorf("sent %q", event.Name)
		}
		if _, err := b.ClientToolResult(appwire.ClientToolResultParams{Call: event.Call, Result: json.RawMessage(`{"asked":"you"}`)}); err != nil {
			t.Error(err)
		}
	}
	conflict := session.ClientToolCall{Set: "work", Name: session.WSTaskConflictVerb, TurnCall: "provider-1", Args: json.RawMessage(`{"task":"tk_1","decision":"dc_1","why":"w"}`)}
	if answer := clientToolAwait(t, clientToolStart(context.Background(), door, conflict)); answer.IsError || !strings.Contains(answer.Text, "you") {
		t.Fatalf("the conflict verb: %+v", answer)
	}
	if _, err := b.SetClientTools(clientToolParams(door.agent, "notes", "app_note")); err != nil {
		t.Fatal(err)
	}
	conflict.Set = "notes"
	if answer := clientToolAwait(t, clientToolStart(context.Background(), door, conflict)); !answer.IsError {
		t.Fatalf("a set without decisions was sent the verb: %+v", answer)
	}
}
