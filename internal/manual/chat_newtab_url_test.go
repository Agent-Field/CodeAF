package manual

import "testing"

// New-tab web address. A person who pastes a URL has to land on the page that
// says it opens a web tab, the same way TestTheChatManualAnswersTheQuestionsPeopleAsk
// checks the corpus.
func TestNewTabURLQuestionsReachTheWebAddressRow(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"can I paste a URL into the new tab to open a web page", "desktop-newtab-url"},
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
