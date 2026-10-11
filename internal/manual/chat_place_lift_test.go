package manual

import "testing"

// These probes keep the false-parent offer reachable in the person's words.
// The shared table in chat_test.go is left alone.
func TestDesktopPlaceLiftQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why did organize take these places out from under the place they were in",
		"two projects were filed under one place and they are not the same project",
		"why did these places stay inside the one above them when the top is already full",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-lift" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-lift", question)
		})
	}
}
