package manual

import "testing"

func TestDesktopGroupSelectedQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I group the selected tabs with the keyboard",
		"what does command G do when no tabs are selected",
		"new group in the tab menu groups which tabs",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-group-selected" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-group-selected", question)
		}
	}
}
