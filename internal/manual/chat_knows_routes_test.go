package manual

import "testing"

func TestTheChatManualAnswersKnowsRoutesQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I edit or delete what a place knows",
		"what happens when edited knowledge lines contradict each other",
		"does saying yes to still true reset a knowledge line's last use",
		"does Using include edited knowledge and exclude replaced lines",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-knows-routes" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-knows-routes", question)
		})
	}
}
