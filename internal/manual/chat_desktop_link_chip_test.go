package manual

import "testing"

func TestDesktopLinkChipQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I open a desktop link chip in a web tab",
		"how do I copy a desktop link chip URL",
		"how do I see the full URL tooltip on a desktop link chip",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-link-chip" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-link-chip", question)
		})
	}
}
