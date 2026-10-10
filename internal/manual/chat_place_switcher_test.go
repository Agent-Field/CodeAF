package manual

import "testing"

// The collapsed place switcher has its own probes so a person asking how to move
// through it reaches this page, not a shared desktop page.
func TestTheChatManualAnswersPlaceSwitcherQuestions(t *testing.T) {
	for _, question := range []string{
		"how do the arrow keys work in the desktop place switcher",
		"what does control 2 do from the home tab",
		"which place needs you when the rail is hidden",
		"why is one place row filled",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-switcher" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-place-switcher", question)
		})
	}
}
