package manual

import "testing"

// Old saved state needs a reachable explanation so removing navigation never sounds like removing work.
func TestDesktopInboxRetirementAnswersSavedWorkspaceQuestions(t *testing.T) {
	for _, question := range []string{
		"what happens to my saved Inbox tab after updating the desktop app",
		"what happens to an old desktop Inbox link",
		"does moving my workspace to a place also move the old Inbox",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-inbox-retirement" {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q does not reach desktop-inbox-retirement", question)
		}
	}
}
