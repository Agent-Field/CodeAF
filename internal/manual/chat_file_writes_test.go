package manual

import "testing"

// The file tab is a viewer. These questions are how a person asks whether
// that tab can change a file, which is a different door from the conversation's
// own write tools.
func TestDesktopFileWriteQuestionsReachTheirPage(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"can I edit text in the desktop file tab", "desktop-file-writes"},
		{"can I rename a file from the desktop file tab", "desktop-file-writes"},
		{"can I delete a file from the desktop file tab", "desktop-file-writes"},
		{"does the desktop file tab save", "desktop-file-writes"},
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
				pages = append(pages, section.Page+" / "+section.Title)
			}
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
	// The same query TestEveryChatManualPageIsReachableByItsOwnName uses: the
	// page's file name with the dashes read as words, among eight sections.
	const named = "desktop file writes"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-file-writes" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-file-writes", named)
	}
}
