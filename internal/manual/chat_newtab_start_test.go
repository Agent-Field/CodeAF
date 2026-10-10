package manual

import "testing"

// New-tab Start rows. A person who asks in those words has to land on the page
// that says New terminal turns this tab into a shell and command O asks for
// part of a file name, the same way TestTheChatManualAnswersTheQuestionsPeopleAsk
// checks the corpus.
func TestNewTabStartQuestionsReachTheStartRows(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"how do I open a new terminal from the new tab", "desktop-newtab-start"},
		{"what does command O do", "desktop-newtab-start"},
		{"type part of a file name", "desktop-newtab-start"},
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
