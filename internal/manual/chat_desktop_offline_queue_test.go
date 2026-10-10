package manual

import "testing"

// These probes keep offline sends reachable in the person's words.
// The shared table in chat_test.go is left alone.
func TestDesktopOfflineQueueQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what happens to a message I send while the engine is offline",
		"do messages I send offline go out when the engine reconnects",
		"what if I attach a file and send while offline",
		"what if the engine refuses a message after it reconnects",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-offline-queue" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-offline-queue", question)
		})
	}
}
