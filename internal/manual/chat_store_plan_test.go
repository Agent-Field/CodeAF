package manual

import "testing"

func TestTheChatManualAnswersPlacePlanQuestions(t *testing.T) {
	for _, question := range []string{
		"can I undo a whole place plan in one step",
		"what happens if a place plan contains an invalid change",
		"what are the organizing calls per run and cost per run",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-store-plan" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-store-plan", question)
		}
	}
}
