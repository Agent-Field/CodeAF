package manual

import "testing"

// The desktop receipt vocabulary must reach its account of the actual integration limits.
func TestDesktopTranscriptDecisionQuestions(t *testing.T) {
	for _, question := range []string{
		"Why does the desktop show Allowed automatically by a place?",
		"What does Did 2 things mean in the desktop transcript?",
		"Where are Go Edit Cancel on a desktop coordination plan?",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-transcript-decisions" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-transcript-decisions", question)
		}
	}
}
