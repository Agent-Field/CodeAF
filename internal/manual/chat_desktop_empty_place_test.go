package manual

import "testing"

// Empty places are found through the words on the page and the actions beside them.
func TestTheChatManualAnswersDesktopEmptyPlaceQuestions(t *testing.T) {
	for _, question := range []string{
		"why does my desktop place say Nothing here yet",
		"how do I add files or links to an empty desktop place",
		"how do I write instructions in an empty desktop place",
		"why does my empty desktop place say Uses Marketing's context",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-empty-place" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-empty-place", question)
		})
	}
}
