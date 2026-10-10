package manual

import "testing"

// These questions use the input gestures someone needs to find again.
func TestDesktopTabSelectionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I select several desktop tabs for grouping",
		"how do I clear selected desktop tabs",
		"does middle-click close a desktop tab or stop running work",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-tab-selection" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-tab-selection", question)
		}
	}
}
