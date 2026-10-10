package manual

import "testing"

func TestDesktopSearchFieldQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I clear and leave the desktop search field with Escape",
		"why does the desktop search shortcut hint disappear",
		"why does the desktop search field have a ring only with keyboard focus",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-search-field" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-search-field", question)
		})
	}
}
