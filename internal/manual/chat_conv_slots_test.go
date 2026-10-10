package manual

import "testing"

// The conversation header's Using chip and the membership line are their own
// page. A person who asks in those words has to land there, the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestTheChatManualAnswersConvSlotQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"where does the Using chip sit beside the running counts", "desktop-conv-slots"},
		{"does a place change show twice in the transcript", "desktop-conv-slots"},
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
}
