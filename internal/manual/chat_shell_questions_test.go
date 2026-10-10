package manual

import "testing"

func TestDesktopShellQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where does the Inbox tab sit next to Home",
		"what does middle-click do on a desktop tab",
		"why doesn't right-click on Inbox or Now open a menu",
		"how many notices sit at the bottom of the desktop window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-shell-questions" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-shell-questions", question)
		})
	}
}
