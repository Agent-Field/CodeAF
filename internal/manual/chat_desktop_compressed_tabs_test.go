package manual

import "testing"

func TestDesktopCompressedTabsQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why are the desktop tabs only icons on a narrow window",
		"how do I read a compressed tab's full title in the desktop app",
		"where do desktop tabs that do not fit on a narrow strip go",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-compressed-tabs" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-compressed-tabs", question)
		})
	}
}
