package manual

import "testing"

// Home "Decided automatically" is its own page so the shared chat table stays untouched.
func TestDesktopHomeDecidedQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where does the desktop home list decisions it made automatically",
		"what does All do beside Decided automatically",
		"how do I open why a desktop decision was made automatically",
		"how do I show fewer decided rows on the desktop home",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-decided" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-decided", question)
		})
	}
}
