package manual

import "testing"

// These probes keep the running-close toast's Undo reachable in the words a person uses.
func TestDesktopCloseToastUndoQuestionsReachTheirPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"command z while the running tab close toast is still up", "desktop-close-toast-undo"},
		{"does a second undo reopen a desktop tab the close toast already put back", "desktop-close-toast-undo"},
		{"stop it on the closed and still running toast", "desktop-close-toast-undo"},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			for _, section := range Chat().Search(ask.question, DefaultResults) {
				if section.Page == ask.page {
					return
				}
			}
			t.Fatalf("%q did not reach %s", ask.question, ask.page)
		})
	}
}
