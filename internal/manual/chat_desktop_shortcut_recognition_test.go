package manual

import "testing"

// Shortcut guards must be reachable in the words a person uses when editing text.
func TestDesktopShortcutRecognitionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does desktop Undo leave my textarea alone",
		"does Space Quick Look interrupt typing or activate a button",
		"is Home Up the bracket shortcut or the up arrow",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-shortcut-recognition" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-shortcut-recognition", question)
		})
	}
}
