package session

import "testing"

// CRITICAL: REQUEST WORDS DO NOT DETERMINE WHETHER DELEGATION IS ALLOWED.
// The model may have context that makes a short request substantial. Existing
// approval and dependency checks still apply to its explicit proposal.
func TestExplicitDelegationIsNotRefusedByRequestKeywords(t *testing.T) {
	for _, ask := range []string{"commit", "undo", "read this file", "summarize the findings"} {
		t.Run(ask, func(t *testing.T) {
			completer := &routedCompleter{parent: []step{
				proposeCall("Requested work", "Complete the requested work with the supplied context."),
				finalText("The proposal is ready."),
			}}
			agent, _ := newTestAgent(t, completer, func(config *Config) {
				config.AskConsent = true
				config.TaskAutoApproveSeconds = 0
			})
			stubbedGraph(agent, func(*TaskNode) {})
			events := approveTasks(t, agent, mustSubmit(t, agent, ask))
			if _, ok := firstOfKind(events, EventTaskProposal); !ok {
				t.Fatalf("explicit proposal was suppressed for %q: %v", ask, kinds(events))
			}
		})
	}
}
