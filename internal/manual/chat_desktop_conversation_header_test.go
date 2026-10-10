package manual

import "testing"

// The header's rename and count buttons must be reachable in a person's own words.
func TestDesktopConversationHeaderQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"how do I rename a conversation in the desktop app", "what does clicking N running in the desktop header do", "why does the dot next to running pulse"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-conversation-header" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-conversation-header", question)
		}
	}
}
