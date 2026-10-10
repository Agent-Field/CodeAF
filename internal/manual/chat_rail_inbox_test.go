package manual

import "testing"

// The retired rail Inbox has its own probes so a person asking about the side list
// reaches this page, not a shared desktop page.
func TestTheChatManualAnswersDesktopRailInboxQuestions(t *testing.T) {
	for _, question := range []string{
		"why is Inbox missing from the desktop rail and the place switcher",
		"what does the collapsed desktop rail place switcher list",
		"what does the desktop rail show on first launch",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-rail-inbox" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-rail-inbox", question)
		})
	}
}
