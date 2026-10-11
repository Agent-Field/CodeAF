package manual

import "testing"

func TestRailRecentQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I get back a tab I closed",
		"where are my recent chats",
		"I closed a conversation by accident how do I reopen it from the rail",
		"how do I remove something from the recent list without deleting the chat",
		"how do I hide the recent section in the sidebar",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-rail-recent" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-rail-recent", question)
		}
	}
}
