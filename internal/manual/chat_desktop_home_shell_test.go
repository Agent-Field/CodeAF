package manual

import "testing"

// The layout answers must be reachable without knowing the component's name.
func TestDesktopHomeShellQuestions(t *testing.T) {
	for _, question := range []string{
		"How does the desktop Home fit a narrow window?",
		"Why does the desktop Home composer stay at the bottom when I scroll?",
		"Does desktop Home show skeleton sections while loading?",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-shell" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-shell", question)
		})
	}
}
