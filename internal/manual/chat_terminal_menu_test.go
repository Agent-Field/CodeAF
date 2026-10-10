package manual

import "testing"

// The menu must be findable through the words a person uses at its terminal header.
func TestChatTerminalMenuQuestions(t *testing.T) {
	for _, question := range []string{
		"what is in a finished job's terminal menu",
		"why is Stop missing from the running terminal menu",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-terminal-menu" {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q should reach desktop-terminal-menu", question)
		}
	}
}
