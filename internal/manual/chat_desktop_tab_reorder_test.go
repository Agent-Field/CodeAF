package manual

import "testing"

func TestDesktopKeyboardTabReorderIsReachable(t *testing.T) {
	for _, question := range []string{
		"how do I move desktop tabs left or right with the keyboard",
		"what does Moved to position N of M mean when reordering desktop tabs",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-tab-reorder" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-tab-reorder", question)
		}
	}
}
