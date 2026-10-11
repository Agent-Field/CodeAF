package manual

import "testing"

// The new controls must be reachable in the words a person uses beside them.
func TestDesktopDraftPlaceQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why is there an arrow place pill while I type in the desktop composer",
		"what does also in mean on a desktop Home chat row",
		"how do I filter the chats with views chips on a desktop place Home",
		"how do I steer a desktop place with Organize this by",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-draft-place" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-draft-place", question)
		}
	}
}
