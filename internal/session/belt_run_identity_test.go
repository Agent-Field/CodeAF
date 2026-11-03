package session

import "testing"

// The run a conversation starts is told who the conversation is; a run builds
// its workers outside the conversation's tree and has no other way to know.
func TestABeltRunIsToldItsConversationsDID(t *testing.T) {
	a := &Agent{}
	a.config.agentDID = "did:key:zabc"
	if got := a.beltRunSpec(&beltRun{title: "Spine"}, "write the spine").AgentDID; got != "did:key:zabc" {
		t.Fatalf("the run was told %q", got)
	}
}
