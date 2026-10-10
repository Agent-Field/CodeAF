package manual

import "testing"

func TestDesktopFileChipQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I open a file chip in an editor",
		"how do I copy a file chip path",
		"how do I see the full path on a file chip",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-file-chip" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-file-chip", question)
		})
	}
}
