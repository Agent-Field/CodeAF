package manual

import "testing"

// A person asks how desktop notifications are grouped, so those questions must reach this page.
func TestDesktopNotifyGroupQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how are desktop notifications grouped by place",
		"what group is a notification for a chat in no place",
		"which place groups a notification when a chat is in several places",
		"does a failed item get a desktop notification with its place",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-notify-group" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-notify-group", question)
		})
	}
}
