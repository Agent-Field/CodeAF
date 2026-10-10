package manual

import "testing"

func TestChatManualAnswersRememberPlaceQuestions(t *testing.T) {
	for _, question := range []string{
		"remember this in my place where does it get saved",
		"undo something I asked a chat to remember",
		"remember with several direct places first parent most",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-remember-place" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-remember-place", question)
		})
	}
}
