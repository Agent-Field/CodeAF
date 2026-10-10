package manual

import "testing"

// Pasted text in a queued desktop message is edited as the stored text, not as a card.
func TestDesktopQueuedPasteQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I edit a queued message that has a pasted text card",
		"does editing a queued paste show the pasted-text tags",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-queued-paste" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-queued-paste", question)
		}
	}
}
