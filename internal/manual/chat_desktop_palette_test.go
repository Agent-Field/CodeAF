package manual

import "testing"

// The go-to palette needs its own probes so its search, keys and narrow sheet stay reachable in a person's own words.
func TestDesktopPaletteQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I search for a place or create one in the desktop palette",
		"what do the keys do in the desktop go-to palette",
		"what does enter and command enter do in the go to palette",
		"how does the desktop go-to palette look in a narrow window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-palette" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-palette", question)
		})
	}
}
