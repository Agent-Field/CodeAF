package manual

import "testing"

// Dragging a tab out of the strip is its own page. These questions are the
// words a person uses for that gesture, checked the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestDraggingATabOutOfTheStripReachesItsPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what happens when I drag a tab out of the window", "desktop-tab-tearoff"},
		{"can I drag a pinned tab or the Inbox into its own window", "desktop-tab-tearoff"},
		{"does dragging a tab out of the strip stop what it was doing", "desktop-tab-tearoff"},
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
	// The same query TestEveryChatManualPageIsReachableByItsOwnName uses: the
	// page's file name with the dashes read as words, among eight sections.
	const named = "desktop tab tearoff"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-tab-tearoff" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-tab-tearoff", named)
	}
}
