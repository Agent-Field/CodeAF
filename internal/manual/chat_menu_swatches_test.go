package manual

import "testing"

func TestDesktopMenuSwatchesQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I change a place's colour in the desktop menu",
		"why is graphite missing from the tint menu",
		"what do the arrow keys do on the tint squares",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-menu-swatches" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-menu-swatches", question)
		})
	}
}
