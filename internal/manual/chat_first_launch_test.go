package manual

import "testing"

// First launch needs its own probes because a new person's vocabulary starts with folders, not the place graph.
func TestChatManualDesktopFirstLaunch(t *testing.T) {
	for _, question := range []string{
		"how do I make my first desktop place from a folder or repo",
		"how do I name my first desktop place without a folder",
		"how do I move Now's matching tabs into my first desktop place",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-first-launch" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-first-launch", question)
		})
	}
}
