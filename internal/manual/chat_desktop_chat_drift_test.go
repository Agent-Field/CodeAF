package manual

import "testing"

func TestTheChatManualAnswersDesktopChatDrift(t *testing.T) {
	for _, question := range []string{
		"can a chat that changes subject earn more homes",
		"does a chat changing subject move its primary home automatically",
		"when does desktop chat drift run after a settled reply",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-chat-drift" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-chat-drift", question)
		})
	}
}
