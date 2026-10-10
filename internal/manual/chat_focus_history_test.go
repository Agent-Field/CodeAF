package manual

import "testing"

// Focus history is retrieved by the words a reader uses for back, scroll and the chip.
func TestDesktopFocusHistoryQuestions(t *testing.T) {
	for _, question := range []string{
		"Does the desktop remember the tabs and places I visited?",
		"How do I go back to the tab, place, scroll and draft I left?",
		"When does the back chip show, and when does it go away?",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-focus-history" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-focus-history", question)
		})
	}
}
