package manual

import "testing"

// The overview must remain reachable without knowing the implementation's names.
func TestDesktopIterationTwoQuestions(t *testing.T) {
	for _, question := range []string{
		"where did the desktop Inbox go and how do I open Next up",
		"how do I go back after Next up or go up a level on desktop Home",
		"when do desktop places decide automatically instead of asking me",
		"what are automatic decision receipts and can I overturn them",
		"what does a desktop place know and where does remember save it",
		"why is there a desktop discussion called Marketing with Software",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-iteration-two" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-iteration-two", question)
		}
	}
}
