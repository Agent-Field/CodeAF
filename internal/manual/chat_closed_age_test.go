package manual

import "testing"

func TestDesktopClosedAgeQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"recently closed rows say closed 1h ago",
		"why does a recently closed tab just say closed",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-closed-age" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-closed-age", question)
		}
	}
}
