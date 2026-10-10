package manual

import "testing"

// Parent names must be discoverable in the words a person uses to ask about returning.
func TestDesktopBackHeaderQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"how do I go back from a task in the desktop app", "why did the desktop Back chip disappear"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-back-header" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-back-header", question)
		}
	}
}
