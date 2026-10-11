package manual

import "testing"

// The first pin and last unpin must be discoverable without the shared desktop pages.
func TestDesktopRailDropTargetQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I drag the first place into Pinned",
		"how do I unpin the last place with a pointer drag",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-rail-drop-targets" {
					return
				}
			}
			t.Fatal("desktop-rail-drop-targets did not answer the question")
		})
	}
}
