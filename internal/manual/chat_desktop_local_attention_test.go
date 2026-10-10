package manual

import "testing"

// These probes keep the local header's scope and tray action reachable by their visible words.
func TestDesktopLocalAttentionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what does needs you here count in the desktop chat header",
		"how do I open the first question from the desktop chat header",
		"what is the Next up progress chip beside the desktop chat title",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-local-attention" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-local-attention", question)
		})
	}
}
