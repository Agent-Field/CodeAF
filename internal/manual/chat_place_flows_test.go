package manual

import "testing"

func TestChatManualAnswersPlaceFlowQuestions(t *testing.T) {
	for _, question := range []string{
		"add the first folder on a populated place home",
		"open a folder or repo and land on its home",
		"undo a saved-to remember line",
		"open tabs that share a topic",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-flows" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-place-flows", question)
		})
	}
}
