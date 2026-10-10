package manual

import "testing"

func TestChatManualAnswersPlaceKnowledgeStorageQuestions(t *testing.T) {
	for _, question := range []string{
		"what happened to my place instructions and paragraphs",
		"where do knowledge line sources come from",
		"can knowledge line edits and deletion be undone",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-knowledge" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-place-knowledge", question)
		})
	}
}
