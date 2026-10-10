package manual

import "testing"

// These probes keep the desktop Engine section reachable in the person's words.
// The shared table in chat_test.go is left alone.
func TestDesktopEngineConnectionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where does the desktop engine run on this mac",
		"what is the dev transport address in desktop settings",
		"settings says reconnecting while it tries to reach the engine",
		"can't reach the engine retry in desktop settings",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-engine-connection" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-engine-connection", question)
		})
	}
}
