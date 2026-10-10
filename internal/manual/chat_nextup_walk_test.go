package manual

import "testing"

// The desktop walk is found by the person's question, rather than by its implementation name.
func TestDesktopNextUpWalkQuestionsReachItsPage(t *testing.T) {
	for _, question := range []string{
		"how do I walk through questions elsewhere with Next up",
		"does answering Next up load the next question what does Skip do",
		"how do I exit Next up and get my draft and scroll back",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-nextup-walk" {
				reached = true
				break
			}
		}
		if !reached {
			t.Errorf("%q should reach desktop-nextup-walk", question)
		}
	}
}
