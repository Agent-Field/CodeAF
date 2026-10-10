package manual

import "testing"

// These probes keep place Undo reachable without changing the shared desktop question table.
func TestDesktopPlaceUndoQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"undo closing a desktop place after its toast disappears",
		"undo archiving moving deleting or filing in desktop places",
		"do desktop tabs and places share the same twenty undo steps",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-undo" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-undo", question)
		})
	}
}
