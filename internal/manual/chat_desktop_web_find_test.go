package manual

import "testing"

// The native capability limit must be reachable in the words someone asks it.
func TestDesktopWebFindQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I find text in a desktop web page with Command F",
		"why does web page find show no count or no n of m",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-web-find" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-web-find", question)
		}
	}
}
