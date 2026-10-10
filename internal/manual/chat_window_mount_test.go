package manual

import "testing"

// The window mount (no Inbox row, Next up on the strip, back through the
// window's own history) is its own page. A person who asks in those words
// has to land there, the same way TestTheChatManualAnswersTheQuestionsPeopleAsk
// checks the corpus.
func TestTheChatManualAnswersWindowMountQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"does the desktop side list still have an Inbox row", "desktop-window-mount"},
		{"where did the desktop Inbox row go", "desktop-window-mount"},
		{"what does the desktop window remember when I go back", "desktop-window-mount"},
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
