package manual

import "testing"

// These probes keep the source list and its removal reachable in the person's words.
func TestDesktopHomeSourcesQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where are the files folders and URLs used by a desktop place",
		"how do I remove a source from desktop Home and undo it",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-sources" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-sources", question)
		})
	}
}
