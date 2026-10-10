package manual

import "testing"

// The place picker is the palette in pick mode; its probes keep "add to a place" reachable in a person's own words.
func TestDesktopPlacePickerQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I add a conversation to a place in the desktop app",
		"which places can I not choose in the add to another place picker",
		"can I create a new place while picking one to add to",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-picker" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-picker", question)
		})
	}
}
