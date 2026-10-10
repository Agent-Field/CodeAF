package manual

import "testing"

// The editor handoff must be reachable in the words used at the file tab.
func TestDesktopEditorMenuQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I open a desktop file in an editor",
		"why does a remote engine only offer copy path in the desktop editor menu",
		"how do I use the desktop editor menu with the keyboard",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-editor-menu" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-editor-menu", question)
		})
	}
}
