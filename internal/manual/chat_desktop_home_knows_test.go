package manual

import "testing"

// Home knowledge controls must be reachable in the reader's own words.
func TestDesktopHomeKnowsQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I edit or remove what Marketing knows on Home",
		"how do I add something Marketing should know",
		"why is a knowledge line crossed out or asking still true",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-home-knows" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-home-knows", question)
		}
	}
}
