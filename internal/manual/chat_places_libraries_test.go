package manual

import "testing"

// Places are libraries (owner decision 2026-10-11). These probes keep the rail, Focus and drag-to-add questions
// reaching their own page in the words a person brings to them.
func TestDesktopPlaceLibraryQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"does clicking a place in the desktop rail replace my tabs",
		"what is focus on a place in the desktop app",
		"how do I turn focus off in the desktop app",
		"which tabs belong to a place",
		"how do I add a tab to a place in the desktop app",
		"which place does the rail highlight in the desktop app",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-libraries" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-libraries", question)
		})
	}
}
