package manual

import "testing"

// Root Home has its own probes so its vocabulary stays reachable without editing the shared desktop table.
func TestDesktopRootHomeAnswersItsQuestions(t *testing.T) {
	for _, question := range []string{
		"what is All places in the desktop app",
		"how do I search places by name or ancestor path",
		"where are chats not in any place",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-root-home" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-root-home", question)
		}
	}
}
