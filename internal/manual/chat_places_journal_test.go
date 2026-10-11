package manual

import "testing"

// These probes keep the organize record reachable without changing the shared question table.
func TestDesktopPlacesJournalQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where is the record of an organize run",
		"what if organizing places crashes halfway through",
		"does the places organize record grow without limit",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-places-journal" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-places-journal", question)
		})
	}
}
