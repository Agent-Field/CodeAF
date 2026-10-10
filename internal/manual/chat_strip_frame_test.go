package manual

import "testing"

// The strip's frame pill, banner and back chip are a desktop surface of their
// own. A person who sees one and asks where it sits has to land on the page
// that says so, the same way TestTheChatManualAnswersTheQuestionsPeopleAsk
// checks the corpus.
func TestStripFrameQuestionsReachTheStripFramePage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"where the frame pill sits on the desktop tab strip", "desktop-strip-frame"},
		{"banner under the frame pill", "desktop-strip-frame"},
		{"where the back chip is on the tab strip", "desktop-strip-frame"},
		{"back chip before the home tab", "desktop-strip-frame"},
		{"focus mode and the collapsed rail", "desktop-strip-frame"},
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
