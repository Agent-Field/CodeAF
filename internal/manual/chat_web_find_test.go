package manual

import "testing"

// Find in a desktop web page has its own page. These are the words a person
// uses for it. The shared probe table in chat_test.go is left alone.
func TestDesktopWebFindAnswersHowToSearchAPage(t *testing.T) {
	for _, question := range []string{
		"how do I find text in a desktop web page with Command F or Control F",
		"why does web page find show no count or no n of m",
	} {
		t.Run(question, func(t *testing.T) {
			found := Chat().Search(question, DefaultResults)
			if len(found) == 0 {
				t.Fatalf("%q reaches nothing in the chat manual", question)
			}
			for _, section := range found {
				if section.Page == "desktop-web-find" {
					return
				}
			}
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Errorf("%q should reach desktop-web-find; it reached %v", question, pages)
		})
	}
}
