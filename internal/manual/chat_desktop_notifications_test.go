package manual

import "testing"

// These probes keep the desktop's notification rules reachable in the person's words.
func TestDesktopNotificationsQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does the desktop app send system notifications",
		"what does the desktop dock badge count",
		"when does the desktop ask for notification permission",
		"what happens when I click a desktop notification",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-notifications" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-notifications", question)
		})
	}
}
