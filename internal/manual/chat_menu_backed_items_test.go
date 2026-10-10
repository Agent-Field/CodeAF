package manual

import "testing"

// The tab menu leaves Move to new window and Copy link off when nothing backs
// them. These questions are the words a person uses for that, checked the same
// way TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestTheTabMenuLeavesUnbackedItemsOff(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why is Move to new window missing from the tab menu", "desktop-menu-backed-items"},
		{"why is Copy link missing", "desktop-menu-backed-items"},
		{"what does the copy link shortcut do when a tab has no link", "desktop-menu-backed-items"},
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
	const named = "desktop menu backed items"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-menu-backed-items" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-menu-backed-items", named)
	}
}
