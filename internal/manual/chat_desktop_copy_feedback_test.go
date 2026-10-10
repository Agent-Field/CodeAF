package manual

import "testing"

// Copy feedback questions should reach the clipboard confirmation's timing and failure behavior.
func TestDesktopCopyFeedbackQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"why does the desktop Copy button briefly show a check", "do desktop menus animate with reduced motion"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-copy-feedback" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-copy-feedback", question)
		}
	}
}
