package manual

import "testing"

// These probes keep the desktop's place mutations reachable in the person's words.
func TestDesktopPlaceActionsQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I archive a place in the desktop app",
		"what does undo do after I archive, move or delete a place on the desktop",
		"why can't a place sit inside itself",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-actions" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-actions", question)
		})
	}
}
