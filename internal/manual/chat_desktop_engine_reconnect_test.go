package manual

import "testing"

// These probes keep the conversation's engine line reachable in the person's words.
// The shared table in chat_test.go is left alone.
func TestDesktopEngineReconnectQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"reconnecting to the engine under the conversation header",
		"can't reach the engine after 30 seconds and retry",
		"the engine line under the header disappears when it reconnects",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-engine-reconnect" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-engine-reconnect", question)
		})
	}
}
