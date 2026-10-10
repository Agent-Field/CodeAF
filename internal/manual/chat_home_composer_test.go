package manual

import "testing"

// The Home composer answers must be reachable without knowing the component's name.
func TestDesktopHomeComposerQuestions(t *testing.T) {
	for _, question := range []string{
		"How do I start a chat from a place's Home on the desktop?",
		"What does the Home composer say?",
		"Does Enter on Home open a new tab, and what does Command Enter do?",
		"Does Home turn into the chat?",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-composer" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-composer", question)
		})
	}
}
