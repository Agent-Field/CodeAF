package manual

import "testing"

// These probes keep the desktop History inline delete reachable in the person's words.
func TestDesktopHistoryDeleteQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I delete a chat from History",
		"what does Delete 14 chats mean",
		"undo deleted chats within 10 seconds",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-history-delete" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-history-delete", question)
		})
	}
}
