package manual

import "testing"

// Opening a place is the touch the untouched-place line reads. Its own page, so the shared chat table stays untouched.
func TestTheChatManualAnswersPlaceTouchQuestions(t *testing.T) {
	for _, question := range []string{
		"does going to a place count as touching it",
		"I opened a place from Go to and the hasn't been touched line stayed",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-touch" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-touch", question)
		})
	}
}
