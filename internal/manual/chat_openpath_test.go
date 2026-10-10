package manual

import "testing"

// Open and reveal refuse with fixed sentences. A person who sees one of those
// sentences and asks about it has to land on the page that says what it means,
// the same way TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestOpenPathQuestionsReachTheRefusalSentences(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"that file is outside the workspace", "desktop-open-path"},
		{"that file no longer exists", "desktop-open-path"},
		{"the workspace is unavailable", "desktop-open-path"},
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
	const named = "desktop open path"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-open-path" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-open-path", named)
	}
}
