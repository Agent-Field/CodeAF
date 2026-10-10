package manual

import "testing"

// These probes keep the walk's completion and return action reachable in the person's words.
func TestDesktopNextUpEndCardQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"what does You're clear mean at the end of Next up", "how do I return from the Next up end card"} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-nextup-end-card" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-nextup-end-card", question)
		})
	}
}
