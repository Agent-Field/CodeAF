package manual

import "testing"

// These probes keep the desktop choice menu reachable in the person's words.
func TestDesktopSelectQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I pick from a desktop dropdown",
		"what do Home, End, Enter and Escape do in a desktop dropdown",
		"why does the desktop dropdown highlight a whole row",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-select" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-select", question)
		})
	}
}
