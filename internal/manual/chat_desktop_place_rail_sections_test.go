package manual

import "testing"

func TestDesktopPlaceRailSectionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what is on the desktop place rail",
		"how do I move between rows on the desktop place rail",
		"where is the desktop inbox row",
		"what is the number next to Now on the desktop place rail",
		"how do I close every open place on the desktop rail",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-rail-sections" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-rail-sections", question)
		})
	}
}
