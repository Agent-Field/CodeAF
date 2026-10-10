package manual

import "testing"

// These questions pin the specimen's fixture boundary as well as its discovery door.
func TestDesktopFoundationsSpecimenQuestions(t *testing.T) {
	for _, question := range []string{
		"where can I see the desktop Foundations design tokens",
		"why are there light and dark swatches together in Foundations",
		"why does the Foundations shimmer or running dot move",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-foundations-specimen" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-foundations-specimen", question)
		})
	}
}
