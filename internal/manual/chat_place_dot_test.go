package manual

import "testing"

// The rail's words must be reachable by the questions people ask about its marks.
func TestTheChatManualAnswersDesktopPlaceDotQuestions(t *testing.T) {
	for _, question := range []string{
		"what does the amber or red dot beside a desktop place mean",
		"why is there no dot when a desktop place is only running",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-dot" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-place-dot", question)
		})
	}
}
