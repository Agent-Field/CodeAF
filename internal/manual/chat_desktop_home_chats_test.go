package manual

import "testing"

// Home chat questions must reach the row's real data and tab behavior.
func TestDesktopHomeChatQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"what is the digest under a chat on desktop Home", "does clicking a chat on desktop Home open another tab"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-home-chats" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-home-chats", question)
		}
	}
}
