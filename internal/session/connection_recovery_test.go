package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The adapter already waited for connectivity. A model hop cannot repair DNS,
// so the session must report that ending once and retain the next user turn.
func TestASpentConnectionWaitDoesNotRestartTheModelLadder(t *testing.T) {
	completer := chained([]string{"other/model"},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, &provider.ConnectionUnavailableError{}
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("back online"), nil },
	)
	agent, _ := newTestAgent(t, completer, nil)
	events := collect(t, mustSubmit(t, agent, "answer"))
	if completer.requests() != 1 || countOfKind(events, EventRetrying) != 0 || countOfKind(events, EventError) != 1 {
		t.Fatalf("spent recovery repeated: calls=%d events=%v", completer.requests(), kinds(events))
	}
	events = collect(t, mustSubmit(t, agent, "try again"))
	if completer.requests() != 2 || countOfKind(events, EventError) != 0 || messageText(lastMessage(agent)) != "back online" {
		t.Fatalf("next user turn did not recover: calls=%d events=%v", completer.requests(), kinds(events))
	}
}
