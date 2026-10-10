package manual

import (
	"strings"
	"testing"
)

// The desktop's delete-with-undo section must be reachable in a person's own
// words, and must sit on the history page rather than the terminal's delete.
func TestTheDesktopDeleteSectionIsReachable(t *testing.T) {
	for _, question := range []string{
		"how do I delete an archived conversation in the desktop app",
		"can I undo deleting a conversation in History",
		"why can't I delete a conversation that is not archived",
		"how long is a deleted conversation kept in the trash",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "history" && strings.Contains(section.Title, "Delete a conversation") {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q does not reach the history page's delete section", question)
		}
	}
}
