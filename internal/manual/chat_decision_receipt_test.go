package manual

import "testing"

// Automatic decisions leave a quiet line in the conversation. These questions
// are the words a person uses for that line, checked the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestDecisionReceiptsAnswerTheQuestionsPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why does it say allowed automatically by a place", "desktop-decision-receipt"},
		{"what does Why do on a receipt", "desktop-decision-receipt"},
		{"what happens when I press an allowed automatically line", "desktop-decision-receipt"},
		{"what does did 3 things mean", "desktop-decision-receipt"},
		{"what does a policy denial look like in the chat", "desktop-decision-receipt"},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		if len(found) == 0 {
			t.Errorf("%q reaches nothing in the chat manual", ask.question)
			continue
		}
		var reached bool
		for _, section := range found {
			if section.Page == ask.page {
				reached = true
				break
			}
		}
		if !reached {
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
	const named = "desktop decision receipt"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-decision-receipt" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-decision-receipt", named)
	}
}
