package manual

import "testing"

func TestDesktopTabCloseMotionQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what happens when I close a desktop tab",
		"do the other desktop tabs slide when one closes",
		"why does a new desktop tab fade in",
		"how do I stop the desktop tab close animation",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-tab-close-motion" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-tab-close-motion", question)
		})
	}
}
