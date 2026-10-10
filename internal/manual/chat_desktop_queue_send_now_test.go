package manual

import "testing"

func TestDesktopQueueSendNowQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what happens when a queued message is sent now in the desktop",
		"why does the desktop say that message has already been sent",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-queue-send-now" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-queue-send-now", question)
		}
	}
}
