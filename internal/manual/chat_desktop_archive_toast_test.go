package manual

import "testing"

// Archive notification questions must reach the shared toast's timing and actions.
func TestDesktopArchiveToastQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"how long does the desktop archive notification stay", "can I dismiss the archive notification with Escape"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-archive-toast" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-archive-toast", question)
		}
	}
}
