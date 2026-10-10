package manual

import "testing"

// Image questions must reach the desktop's own account of safe picture rendering and its read limit.
func TestDesktopImageViewQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I open an image file or SVG in the desktop File view",
		"why cannot my desktop image be shown",
		"how do I open an oversized picture",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-image-view" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-image-view", question)
		}
	}
}
