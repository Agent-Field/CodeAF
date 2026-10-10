package manual

import "testing"

// Narrow-window and touch questions about the desktop tab preview have to land
// on the page that says what the card does, the same way
// TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestDesktopPreviewNarrowQuestionsReachTheirPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"does the tab preview stay on screen on a small window", "desktop-preview-narrow"},
		{"why is there no preview when I touch a tab", "desktop-preview-narrow"},
		{"how wide is the hover preview on a narrow window", "desktop-preview-narrow"},
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
	// The page's file name with the dashes read as words, among eight sections.
	const named = "desktop preview narrow"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-preview-narrow" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-preview-narrow", named)
	}
}
