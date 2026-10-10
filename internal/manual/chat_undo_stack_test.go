package manual

import "testing"

// These probes keep the desktop tab undo stack reachable in the person's words.
func TestDesktopUndoStackQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"undo a closed desktop tab and put it back in its group",
		"how many structural undos does one desktop window keep",
		"is draft typing undoable in the desktop tab strip",
		"closing toast undo for a desktop tab I just closed",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-undo-stack" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-undo-stack", question)
		})
	}
}
