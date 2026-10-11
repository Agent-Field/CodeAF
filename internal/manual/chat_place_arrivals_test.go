package manual

import "testing"

// These probes keep arrival and Now-birth answers on the arrivals page.
// The headings avoid "where did the places go" and "start a new chat", which
// already belong to the places page and the screen page.
func TestDesktopPlaceArrivalsQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"after the first reply, which place gets the chat",
		"when codeaf asks which place a chat belongs in",
		"why a chat stays in Now instead of a place",
		"when Now offers a new place",
		"a single arriving chat does not begin a place",
		"while a draft is still being typed, which place would it fit",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-arrivals" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-arrivals", question)
		})
	}
}
