package manual

import "testing"

// A parent Home opens a child place's chat. The question should reach that page.
func TestChatManualDesktopHomeDescendant(t *testing.T) {
	for _, question := range []string{
		"Where does the desktop find the journal when a parent home shows a chat in a child place",
		"What happens when I click a live row that says in Config parser",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-descendant" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-descendant", question)
		})
	}
}
