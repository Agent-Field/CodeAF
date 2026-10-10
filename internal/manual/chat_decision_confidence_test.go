package manual

import "testing"

// These probes keep the confidence rules reachable without editing the shared table.
func TestChatManualDecisionConfidence(t *testing.T) {
	for _, question := range []string{
		"why did it allow go test without asking me",
		"how sure does a place have to be before it decides",
		"will it decide something I cannot undo",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-decision-confidence" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-decision-confidence", question)
		})
	}
}
