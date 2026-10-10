package manual

import "testing"

func TestDesktopQuestionFocusQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why do desktop question buttons and fields have a ring when I use the keyboard",
		"does clicking the optional desktop question field show a focus ring",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-question-focus" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-question-focus", question)
		})
	}
}
