package manual

import "testing"

// The queue popover is a desktop surface of its own. Questions about the list under
// the frame pill have to land on its page, the way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestQueuePopoverQuestionsReachTheQueuePopoverPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"desktop queue popover", "desktop-queue-popover"},
		{"hover the frame pill to see the list of questions waiting", "desktop-queue-popover"},
		{"move through the queue with the arrow keys", "desktop-queue-popover"},
		{"accept 3 suggestions button", "desktop-queue-popover"},
		{"what does Blocking or Suggested mean on the right of the queue row", "desktop-queue-popover"},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		var reached bool
		pages := make([]string, 0, len(found))
		for _, section := range found {
			pages = append(pages, section.Page)
			if section.Page == ask.page {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
}
