package manual

import "testing"

// Navigation needs its own probes so the person can find the distinction between closing a view and stopping work.
func TestDesktopPlaceNavigationQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I go to a place and restore its tabs in the desktop app",
		"how do I close a desktop place and reopen its saved tabs",
		"how do I go up a level from a desktop place Home",
		"how do I open a place in a new window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-navigation" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-navigation", question)
		})
	}
}
