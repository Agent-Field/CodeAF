package manual

import "testing"

// Quick Look questions should reach the sheet's page in the asker's own words.
func TestDesktopQuickLookQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"how do I close the desktop quick look preview", "why does space type a space instead of closing quick look", "does the quick look sheet shrink on a small window"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-quick-look" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-quick-look", question)
		}
	}
}
