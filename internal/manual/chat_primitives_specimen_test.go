package manual

import "testing"

// These questions pin the specimen's fixture boundary as well as its discovery door.
func TestDesktopPrimitivesSpecimenQuestions(t *testing.T) {
	for _, question := range []string{
		"where can I see the desktop shared primitives",
		"how do I try a six second and ten second toast on the design system",
		"what does the tray size button look like on the design system",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-primitives-specimen" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-primitives-specimen", question)
		})
	}
}
