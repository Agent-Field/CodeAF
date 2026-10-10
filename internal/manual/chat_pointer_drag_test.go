package manual

import "testing"

// Pointer drag is asked about in the words of the gesture, not the module name.
func TestTheChatManualAnswersPointerDragQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I drag a tab to reorder it",
		"how do I drag a tab onto another to group them",
		"how do I split a tab by dragging it to the edge",
		"what happens if I drag a tab out of the window",
		"how do I cancel a drag",
		"how do I reorder queued messages by dragging",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-pointer-drag" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-pointer-drag", question)
		})
	}
}
