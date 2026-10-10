package manual

import "testing"

// A person sees the refusal sentence, so those words must retrieve its explanation.
func TestDesktopPlaceConflictQuestions(t *testing.T) {
	for _, question := range []string{"Your places changed in another window. Reload and try again.", "places changed in another window"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-place-conflicts" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-place-conflicts", question)
		}
	}
}
