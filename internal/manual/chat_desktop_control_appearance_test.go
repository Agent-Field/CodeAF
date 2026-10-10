package manual

import "testing"

// These questions keep the shared desktop appearance rules reachable in the person's words.
func TestDesktopControlAppearanceQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why do desktop chrome buttons have the same keyboard focus ring",
		"why are added line counts different in Light and Dark appearance",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-control-appearance" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-control-appearance", question)
		})
	}
}
