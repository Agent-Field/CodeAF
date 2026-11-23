package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/trace"
)

func TestConversationDebugKeepsItsExistingRunAndOnlyItsOwnRecord(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var runs []string
	var enabled []bool
	complete := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		runs = append(runs, trace.RunFrom(ctx))
		enabled = append(enabled, trace.EnabledRun(ctx))
		return textResponse("answer"), nil
	}
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{complete, complete}}, nil)
	collect(t, mustSubmit(t, a, "before"))
	folder, _, err := a.DebugConversation()
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := a.DebugConversation()
	if err != nil || folder != again {
		t.Fatalf("repeat: %s %v", again, err)
	}
	collect(t, mustSubmit(t, a, "after"))
	other, _ := newTestAgent(t, &scriptedCompleter{steps: []step{complete}}, nil)
	collect(t, mustSubmit(t, other, "other conversation"))
	if runs[0] != runs[1] || runs[0] == runs[2] || enabled[0] || !enabled[1] || enabled[2] || folder != trace.Dir(runs[0]) {
		t.Fatalf("runs %v, enabled %v, folder %s", runs, enabled, folder)
	}
}
