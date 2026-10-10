package manual

import "testing"

// Place tiles are asked about in the words on the tile, not in the component's name.
func TestTheChatManualAnswersDesktopPlaceTileQuestions(t *testing.T) {
	for _, question := range []string{
		"what does a place tile show on the desktop home",
		"why is there an amber or red dot on a place tile",
		"what does Add here mean on a place tile",
		"how do I add a new place from the tile",
		"how do I tell which place tile is selected",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-tile" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-place-tile", question)
		})
	}
}
