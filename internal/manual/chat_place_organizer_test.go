package manual

import "testing"

// These probes keep the desktop organizer's own answers reachable. They are
// separate from the Organize now page so a question about a second run, a
// guess, a shortcut or a view still lands here.
func TestDesktopPlaceOrganizerQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"a second Organize now while places are already being organized",
		"guessing a place while I type a new chat",
		"also in names a shortcut, not a second home",
		"views of one place, such as by month",
		"organizing a place says I did not understand that",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-organizer" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-organizer", question)
		})
	}
}
