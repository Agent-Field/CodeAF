package manual

import "testing"

// These probes use the reader's words so the desktop rail remains findable independently of the shared places page.
func TestDesktopPlaceRailQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I switch places with the desktop rail hidden",
		"how do I reorder or pin places in the desktop rail",
		"how do I use the desktop rail with a keyboard",
		"what happens when I close a place in the desktop rail",
		"can I drop a file or a tab onto the desktop rail",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-rail" {
					return
				}
			}
			t.Fatal("desktop-place-rail did not answer the question")
		})
	}
}
