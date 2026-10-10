package manual

import "testing"

func TestDesktopRailMenuQuestions(t *testing.T) {
	for _, question := range []string{
		"what is in the right-click menu of a place in the rail",
		"how do I open the rail row menu from the keyboard with shift f10",
		"how do I unpin a place from the rail",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-rail-menu" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-rail-menu", question)
		})
	}
}
