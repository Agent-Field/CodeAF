package manual

import "testing"

// Opening a file in one listed editor. A person who asks in those words has to
// land on the page that says the id is looked up and a browser has no opener,
// the same way TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestOpenWithQuestionsReachTheChosenEditor(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"open this file in a chosen editor", "desktop-open-with"},
		{"that editor is not one this machine just listed", "desktop-open-with"},
		{"why is open with missing in the browser", "desktop-open-with"},
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
	const named = "desktop open with"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-open-with" {
			reached = true
			break
		}
	}
	if !reached {
		pages := make([]string, 0, len(found))
		for _, section := range found {
			pages = append(pages, section.Page+": "+section.Title)
		}
		t.Errorf("%q should reach desktop-open-with; it reached %v", named, pages)
	}
}
