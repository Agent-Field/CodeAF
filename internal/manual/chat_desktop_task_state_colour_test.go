package manual

import "testing"

func TestDesktopTaskStateColourQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why is an incomplete desktop task marked with a red dot",
		"what do the colours on desktop task dots mean",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-task-state-colour" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-task-state-colour", question)
		})
	}
}
