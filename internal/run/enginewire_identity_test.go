package run

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A chat's run hands its conversation's name to every worker it builds, so the
// name a worker acts under comes from the conversation and not from a test.
func TestAChatsRunHandsItsWorkersTheConversationsDID(t *testing.T) {
	worker := &BashWorker{}
	for _, option := range engineWorkerOptions(session.RunSpec{AgentDID: "did:key:zabc"}) {
		option(worker)
	}
	if worker.agentDID != "did:key:zabc" {
		t.Fatalf("the worker acts for %q", worker.agentDID)
	}
}
