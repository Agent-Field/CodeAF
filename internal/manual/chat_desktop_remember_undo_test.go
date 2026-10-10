package manual

import "testing"

// Remember confirmations must be reachable using the words drawn in the transcript.
func TestDesktopRememberUndoQuestions(t *testing.T) {
	for _, question := range []string{"Remember something in Marketing Saved to Marketing", "Undo a remembered line Removed"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-remember-undo" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-remember-undo", question)
		}
	}
}
