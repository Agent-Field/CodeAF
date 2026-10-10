package manual

import "testing"

// These probes keep the fixed Home and closable root discoverable in the person's words.
func TestDesktopHomePinningQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why can't I close the first place Home tab with Command W",
		"is the All places tab also pinned or can I close it",
		"can I use arrow keys to reach the desktop Home tab",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-pinning" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-pinning", question)
		})
	}
}
