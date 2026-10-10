package manual

import "testing"

// These questions describe the keyboard path that must remain usable after attaching a saved conversation.
func TestDesktopConversationTabFocusQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I switch background conversations with the keyboard",
		"why does the composer not take focus when I use the tab strip",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-conversation-tab-focus" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-conversation-tab-focus", question)
		}
	}
}
