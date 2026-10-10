package manual

import "testing"

// These probes keep the live decision wiring reachable without editing the shared table.
func TestTheChatManualAnswersLiveDecisionQuestions(t *testing.T) {
	for _, question := range []string{
		"will a place allow go test without showing me the question",
		"does answering a question teach the place",
		"why is a decided question not in needs you",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-live-decision" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-live-decision", question)
		})
	}
}
