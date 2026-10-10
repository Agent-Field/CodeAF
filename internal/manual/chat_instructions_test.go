package manual

import "testing"

// Place instructions on Home must be reachable in the reader's own words.
func TestDesktopInstructionsQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I write instructions for a place",
		"how do I add a note or drop a file or link",
		"what happens if I press escape while editing instructions",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-instructions" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-instructions", question)
		}
	}
}
