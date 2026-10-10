package manual

import "testing"

// The highlight's explanation must be reachable using the words someone reads on screen.
func TestDesktopTextSelectionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does selected text use the place tint in Light and Dark",
		"desktop text selection highlight",
		"why is the background dimmer behind an overlay in Dark appearance",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-text-selection" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-text-selection", question)
		}
	}
}
