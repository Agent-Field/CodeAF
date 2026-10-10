package manual

import "testing"

// These questions keep the desktop queue's direct actions reachable from the chat manual.
func TestDesktopQueuedRowActionsQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I edit a queued message in the desktop app",
		"how do I move send now or remove queued messages",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-queued-row-actions" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-queued-row-actions", question)
		}
	}
}
