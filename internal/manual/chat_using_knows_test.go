package manual

import "testing"

func TestChatManualAnswersUsingKnowsQuestions(t *testing.T) {
	for _, question := range []string{
		"what does the Using list show for what a place knows",
		"where does a learned line show in the Using popover",
		"why is a replaced line struck in the Using list",
		"does Now also using change when a place knows something",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-using-knows" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-using-knows", question)
		})
	}
}
