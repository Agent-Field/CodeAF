package manual

import "testing"

// A click inside a page opens a tab and only the rail switches places (owner rule, 10 Oct). The probes use the words
// of the person who lost their tabs, not the page's own headings.
func TestDesktopPlaceClicksQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"I clicked a place on All places and my tabs disappeared",
		"what happens when I click a place tile in the desktop app",
		"how do I open a place in the background without leaving my tab",
		"how do I get my old tabs back after switching to a place and clicking Now",
		"does the rail or a page click switch the window to a place",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-clicks" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-clicks", question)
		})
	}
}
