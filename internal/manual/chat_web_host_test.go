package manual

import "testing"

// The web page follows its sheet. These questions are the words a person uses
// for that, and they have to reach this page rather than a neighbour that only
// mentions a web tab.
func TestTheChatManualAnswersWebHostQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"does resizing the window, the split or the sidebar move the web page", "desktop-web-host"},
		{"does the filmstrip open a web page on the side cards", "desktop-web-host"},
		{"does closing a web tab close its page", "desktop-web-host"},
		{"what happens to a web page opened in a split or moved to another window", "desktop-web-host"},
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
