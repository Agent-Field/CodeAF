package manual

import "testing"

func TestDesktopNewTabKeyQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what does cmd K do in the desktop app",
		"where did the command palette go",
		"does ctrl+k open a new empty tab every time",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-new-tab-key" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-new-tab-key", question)
		})
	}
}
