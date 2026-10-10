package manual

import "testing"

// Place marks and the retained closed row must be reachable in the person's own words.
func TestDesktopPlaceRailRowQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what do the square and dot beside a desktop place mean",
		"how do I close a desktop place from its rail row",
		"why does a closed desktop place say closed still running",
		"can I use the keyboard to focus desktop place rail rows",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-rail-row" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-rail-row", question)
		})
	}
}
