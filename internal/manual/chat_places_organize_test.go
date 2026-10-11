package manual

import "testing"

// These probes keep Organize now reachable in the person's words.
func TestDesktopPlacesOrganizeQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where is Organize now",
		"what is the difference between add to what's there and start fresh",
		"what does the progress line say while organizing places",
		"how do I cancel organizing places",
		"how do I uncheck a change when I review the plan",
		"what does Not now do on the organize review",
		"how do I undo organizing places",
		"organize my old chats into places",
		"sort existing conversations into places",
		"codeaf asked if I want to organize my chats and I pressed Not now, when does it come back",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-places-organize" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-places-organize", question)
		})
	}
}
