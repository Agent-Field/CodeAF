package manual

import "testing"

// New Window has to be reachable in the words a person uses for the chord and the browser.
func TestDesktopNewWindowQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"does command N open another codeaf window on Now",
		"why does Ctrl N stay with the browser",
		"what if opening a new window is refused",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-new-window" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-new-window", question)
		})
	}
}
