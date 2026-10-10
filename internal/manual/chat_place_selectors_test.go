package manual

import "testing"

// A person asks about colour, counts and the rail in these words, so those questions must reach this page.
func TestDesktopPlaceSelectorQuestions(t *testing.T) {
	for _, question := range []string{
		"why is a child place the same colour as its parent",
		"why a place in two parents does not mix colours",
		"what does 4 inside mean",
		"what does 28 chats mean",
		"what does 2 need you in Config parser mean",
		"why is a closed place still in the sidebar",
		"what do control 1 to 9 do for places",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-selectors" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-selectors", question)
		})
	}
}
