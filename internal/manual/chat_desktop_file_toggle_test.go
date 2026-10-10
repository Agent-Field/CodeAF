package manual

import "testing"

// The keyboard behaviour must be reachable without knowing the control's implementation name.
func TestDesktopFileToggleQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I switch Changes and File with the keyboard in a desktop file tab",
		"why does the desktop file toggle show a focus ring only with the keyboard",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-file-toggle" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-file-toggle", question)
		})
	}
}
