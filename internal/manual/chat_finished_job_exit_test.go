package manual

import "testing"

// A finished job's tab reads exit N after its name. A person who asks in those
// words has to land on that page, the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestTheFinishedJobExitPageAnswersTheTab(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"exit 0 after the name on a finished job tab", "desktop-finished-job-exit"},
		{"a running job tab shows nothing after its name", "desktop-finished-job-exit"},
		{"a shell tab shows no exit code", "desktop-finished-job-exit"},
		{"non-zero exit on a finished job tab", "desktop-finished-job-exit"},
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
