package manual

import "testing"

// Asking how to file a past conversation into a place should reach its page.
func TestDesktopHistoryAddToPlaceQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"how do I add an old conversation in History to a place", "why is there no Add to place in the History menu"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-history-add-to-place" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-history-add-to-place", question)
		}
	}
}
