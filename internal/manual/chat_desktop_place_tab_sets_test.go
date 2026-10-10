package manual

import "testing"

// These questions keep the saved strip and Home's behavior reachable in the person's words.
func TestDesktopPlaceTabSetQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"are my desktop window tab sets saved when I switch places",
		"why does Now have no pinned Home tab",
		"where does a chat started from the Home composer open",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-tab-sets" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-tab-sets", question)
		})
	}
}
