package manual

import "testing"

// The pinned home tab has to answer in the words a person uses for that first strip tab.
func TestTheChatManualAnswersHomeTabQuestions(t *testing.T) {
	for _, question := range []string{
		"where is the pinned home tab in the desktop strip",
		"how do I open the place switcher from the home tab",
		"why is there an amber mark on the home tab swatch",
		"how do I open the place menu on the home tab",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-tab" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-home-tab", question)
		})
	}
}
