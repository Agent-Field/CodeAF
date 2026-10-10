package manual

import "testing"

// These probes keep the focus-scoped desktop shortcuts reachable in the person's words.
func TestDesktopNavigationShortcutQuestions(t *testing.T) {
	for _, question := range []string{
		"what do Command J Command brackets and Command Up mean in the desktop app",
		"desktop navigation shortcuts Ctrl J Alt Left Alt Right",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-navigation-shortcuts" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-navigation-shortcuts", question)
		})
	}
}
