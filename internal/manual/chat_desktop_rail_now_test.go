package manual

import "testing"

// The row's gestures need to be retrievable independently of place navigation.
func TestDesktopRailNowQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I switch the desktop window to Now and my unplaced tabs",
		"how do I command-click or middle-click Now to open a new window",
		"what does command-click Now do in a browser",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-rail-now" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-rail-now", question)
		}
	}
}
