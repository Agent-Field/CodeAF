package manual

import "testing"

func TestDesktopScrollMemoryQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"does reloading the desktop keep my position in a conversation",
		"do two desktop windows share my scroll position",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-scroll-memory" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-scroll-memory", question)
		}
	}
}
