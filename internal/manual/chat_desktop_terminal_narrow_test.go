package manual

import "testing"

// Narrow-window questions must reach the page that explains the hidden metadata.
func TestChatDesktopTerminalNarrowQuestions(t *testing.T) {
	for _, question := range []string{
		"where did the terminal folder and job label go in a narrow window",
		"can I ask about terminal output in a small window",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-terminal-narrow" {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q should reach desktop-terminal-narrow", question)
		}
	}
}
