package manual

import "testing"

func TestDesktopEmptyStartQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does the empty desktop chat use my place's tint",
		"what are we building empty desktop conversation",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-empty-start" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-empty-start", question)
		}
	}
}
