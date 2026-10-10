package manual

import "testing"

// The web page hides under a menu. These questions are the words a person uses
// for that, and they have to reach this page rather than a neighbour that only
// mentions a web tab.
func TestTheChatManualAnswersWebOcclusionQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why is the page title on the web sheet when I open a menu", "desktop-web-occlusion"},
		{"does a menu that does not cover the web page hide it", "desktop-web-occlusion"},
		{"when does the hidden web page come back", "desktop-web-occlusion"},
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
