package manual

import "testing"

// Window questions must reach the shell's own page rather than a resident window command.
func TestDesktopWindowShellQuestions(t *testing.T) {
	for _, question := range []string{
		"Which place opens when I launch a desktop window?",
		"Why does an inactive desktop window lose its glass frame?",
		"Where did the desktop Activity page go?",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-window-shell" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-window-shell", question)
		})
	}
}
