package manual

import "testing"

// A finished job's tab right-click offers Run again and Remove job. A person
// who asks in those words has to land on that page, the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestTheFinishedJobTabMenuPageAnswersTheRightClick(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"right-clicking a finished job tab", "desktop-finished-job-tab-menu"},
		{"run again from the tab menu", "desktop-finished-job-tab-menu"},
		{"remove job under close tab", "desktop-finished-job-tab-menu"},
		{"open log on a finished job tab", "desktop-finished-job-tab-menu"},
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
