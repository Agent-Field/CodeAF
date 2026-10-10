package manual

import "testing"

// The popover must answer its own file-drop and filing questions rather than borrowing the broader places page.
func TestDesktopUsingPopoverQuestions(t *testing.T) {
	for _, question := range []string{
		"can I drop files on the desktop Using popover for this chat only",
		"how do I add this conversation to a place from Using",
		"how do I close the desktop Using popover on a small window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-using-popover" {
					return
				}
			}
			t.Fatalf("%q must reach desktop-using-popover", question)
		})
	}
}
