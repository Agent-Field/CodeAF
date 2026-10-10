package manual

import "testing"

func TestChatManualAnswersUsingChipQuestions(t *testing.T) {
	for _, question := range []string{
		"what is the Using chip in the conversation header",
		"why does the header chip show only a place name",
		"why is there no Using chip on my conversation",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-using-chip" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-using-chip", question)
		})
	}
}
