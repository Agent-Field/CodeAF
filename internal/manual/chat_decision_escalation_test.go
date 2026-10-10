package manual

import "testing"

// These probes keep the routing limits reachable without editing shared tables.
func TestChatManualDecisionEscalation(t *testing.T) {
	for _, question := range []string{
		"why does a place pass a question to a parent",
		"which place decides when my chat is in two places",
		"how many parent hops can a desktop decision question take",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-decision-escalation" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-decision-escalation", question)
		})
	}
}
