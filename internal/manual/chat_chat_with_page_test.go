package manual

import "testing"

func TestDesktopChatWithPageReachesItsPage(t *testing.T) {
	for _, question := range []string{
		"how do I start a conversation about this web page",
		"does chat-plus send the page",
		"does codeaf read the page body",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-chat-with-page" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-chat-with-page", question)
		})
	}
}
