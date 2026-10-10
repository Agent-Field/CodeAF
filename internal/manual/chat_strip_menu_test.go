package manual

import "testing"

func TestDesktopStripMenuQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I right-click empty tab strip space in the desktop app",
		"how do I open the desktop tab strip background menu with Shift F10",
		"why is Reopen closed tab disabled in the desktop strip background menu",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-strip-menu" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-strip-menu", question)
		})
	}
}
