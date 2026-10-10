package manual

import "testing"

func TestChatManualAnswersKnowsListPresentationQuestions(t *testing.T) {
	for _, question := range []string{
		"why does Home show only three things a place knows",
		"what do the sources under things a place knows mean",
		"why is a replaced knowledge line struck out for seven days",
		"when does an unused knowledge line say still true",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-knows-list" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-knows-list", question)
		})
	}
}
