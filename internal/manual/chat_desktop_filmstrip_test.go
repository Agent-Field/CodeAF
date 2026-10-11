package manual

import "testing"

// Preview performance must remain reachable in the words a desktop user asks.
func TestTheChatManualAnswersDesktopFilmstripQuestions(t *testing.T) {
	for _, question := range []string{
		"why are All tabs filmstrip previews faster now",
		"does reopening the desktop filmstrip fetch every tab again",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-filmstrip" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q must reach desktop-filmstrip", question)
		}
	}
}
