package manual

import "testing"

// The desktop answer binding must be reachable using the words on its chip.
func TestDesktopAnswerInComposerQuestions(t *testing.T) {
	for _, question := range []string{
		"How do I answer a desktop review or clarification in the composer?",
		"Why did my existing desktop draft not become an answer?",
		"How do I dismiss Replying to and send an ordinary desktop message?",
		"Does paging the desktop tray change which card my composer answers?",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-answer-in-composer" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-answer-in-composer", question)
		}
	}
}
