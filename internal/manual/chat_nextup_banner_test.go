package manual

import "testing"

// The banner is a desktop surface of its own. A person who sees one and asks
// what it did has to land on the page that says so, the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestNextUpBannerQuestionsReachTheBannerPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"desktop nextup banner", "desktop-nextup-banner"},
		{"banner slides out of the frame pill", "desktop-nextup-banner"},
		{"folds into the pill after 4 seconds", "desktop-nextup-banner"},
		{"when the banner stays off", "desktop-nextup-banner"},
		{"allow 3 git actions", "desktop-nextup-banner"},
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
