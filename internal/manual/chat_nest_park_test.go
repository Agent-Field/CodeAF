package manual

import "testing"

// These probes keep "where did organize put this chat" reachable in the
// person's words. The shared table in chat_test.go is left alone.
func TestDesktopNestParkQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why is this chat in the broader place instead of the smaller one",
		"when does a chat stay in Now instead of a place",
		"does organize put every chat in the smallest folder",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-nest-park" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-nest-park", question)
		})
	}
}
