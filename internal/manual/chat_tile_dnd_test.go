package manual

import "testing"

// Tile drag is asked about in the words of the gesture, not the module name.
func TestTheChatManualAnswersTileDndQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I drag a chat onto a place tile",
		"how do I move a chat or a place with the option key",
		"how do I add a place to another place without dragging",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-tile-dnd" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-tile-dnd", question)
		})
	}
}
