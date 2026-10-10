package manual

import "testing"

// These probes keep the row's input gestures reachable in the packed chat manual.
func TestDesktopHistoryRowMenuQuestions(t *testing.T) {
	for _, question := range []string{
		"right-click a desktop History row or open its menu with Shift F10",
		"command-click or middle-click a History row in a background tab",
		"delete an archived conversation from desktop History",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-history-row-menu" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-history-row-menu", question)
		}
	}
}
