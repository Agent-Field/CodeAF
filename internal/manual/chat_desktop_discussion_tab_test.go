package manual

import "testing"

func TestDesktopDiscussionTabQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I open a council discussion from Live or History",
		"who said each line in a council tab",
		"does typing in a council pause it",
		"why is the message box disabled in a finished council",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-discussion-tab" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-discussion-tab", question)
		}
	}
}
