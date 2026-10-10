package manual

import "testing"

func TestChatManualAnswersDesktopHomeSections(t *testing.T) {
	for _, question := range []string{
		"what is on my place Home and what order is it in",
		"where are learning proposals on Home",
		"add something this place should know from Home",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-sections" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-home-sections", question)
		})
	}
}
