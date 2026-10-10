package manual

import "testing"

// These probes keep the desktop place-delete confirm reachable in the person's words.
func TestTheChatManualAnswersDeletePlaceQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I delete a place on the desktop",
		"what does chats become unplaced and places move up mean",
		"undo deleting a desktop place within 10 seconds",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-delete-place" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-delete-place", question)
		})
	}
}
