package session

import (
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestUserMessageCountUsesReplayAndCompactionSeam(t *testing.T) {
	agent := &Agent{
		messages: []ai.Message{textMessage("system", "hidden"), textMessage("user", "old"), textMessage("assistant", "answer"), textMessage("user", "new")},
		earlier:  []DisplayEntry{{Role: "user"}, {Role: "assistant"}}, earlierFloor: 2,
	}
	if got := agent.UserMessageCount(); got != 2 {
		t.Fatalf("count = %d, want 2 across the compaction seam", got)
	}
	agent.earlier = nil
	if got := agent.UserMessageCount(); got != 2 {
		t.Fatalf("uncompacted count = %d, want 2", got)
	}
}
