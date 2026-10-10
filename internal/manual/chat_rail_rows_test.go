package manual

import "testing"

// Questions about the desktop rail's Now row and the missing Inbox row must reach their page.
func TestDesktopRailRowQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"what is the number next to Now in the desktop sidebar", "is there an inbox row in the desktop sidebar", "why is there an amber dot on the Now row"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-rail-rows" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-rail-rows", question)
		}
	}
}
