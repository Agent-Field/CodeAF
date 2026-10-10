package manual

import "testing"

// Bubble clamp, paste cards and code-block scrolling are their own page.
// A person who asks in those words has to land there, the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestTheChatManualAnswersConversationMessageQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why does my desktop message stop at Show more", "desktop-conversation-messages"},
		{"why is a desktop message faded until the engine records it", "desktop-conversation-messages"},
		{"when does Copy appear on a desktop message", "desktop-conversation-messages"},
		{"what does a pasted text card in a desktop message show", "desktop-conversation-messages"},
		{"why does a desktop code block fade on the right", "desktop-conversation-messages"},
		{"how do I copy a desktop code block", "desktop-conversation-messages"},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			found := Chat().Search(ask.question, DefaultResults)
			if len(found) == 0 {
				t.Fatalf("%q reaches nothing in the chat manual", ask.question)
			}
			for _, section := range found {
				if section.Page == ask.page {
					return
				}
			}
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Fatalf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		})
	}
}
