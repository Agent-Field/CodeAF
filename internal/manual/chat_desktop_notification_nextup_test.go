package manual

import "testing"

func TestDesktopNotificationNextUpQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"Which desktop questions send a system notification?",
		"What happens when I click a desktop notification?",
		"Why does the desktop dock badge include my current conversation?",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-notification-nextup" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-notification-nextup", question)
		}
	}
}
