package manual

import "testing"

// These questions pin the notification page to the words a person uses while reaching for an action.
func TestDesktopToastQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"can I pause a desktop toast while reaching for Undo",
		"how do I dismiss a desktop toast with Escape",
		"why is the desktop toast message cut short in a narrow window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-toast" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-toast", question)
		})
	}
}
