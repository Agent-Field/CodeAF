package manual

import "testing"

// These probes keep the decision gate reachable without editing the shared table.
func TestChatManualDecisionGate(t *testing.T) {
	for _, question := range []string{
		"does a place decide a question before I see it",
		"why is the choice already selected while a place is learning",
		"why did a parent place allow this",
		"why did it still ask me",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-decision-gate" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-decision-gate", question)
		})
	}
}
