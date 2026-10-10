package manual

import "testing"

// A page's new window is a background web tab. These are the words a person
// uses for that, and they have to reach this page rather than one that only
// mentions a window or a web tab.
func TestTheChatManualAnswersWebNewWindowQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what happens when a page opens a new window or a target=_blank link", "desktop-web-new-window"},
		{"does target=_blank open another window in the desktop app", "desktop-web-new-window"},
		{"why did a page only open five new tabs", "desktop-web-new-window"},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			for _, section := range Chat().Search(ask.question, DefaultResults) {
				if section.Page == ask.page {
					return
				}
			}
			t.Fatalf("%q did not reach %s", ask.question, ask.page)
		})
	}
}
