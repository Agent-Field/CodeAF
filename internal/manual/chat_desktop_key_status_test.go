package manual

import "testing"

// These probes keep the desktop provider key row reachable in the person's words.
func TestDesktopKeyStatusQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where does the desktop app get my provider key from",
		"does the desktop settings page show my API key",
		"the provider key row is missing from desktop settings",
		"what does Saved in your profile mean in the desktop settings key row",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-key-status" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-key-status", question)
		})
	}
}
