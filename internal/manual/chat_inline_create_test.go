package manual

import "testing"

// The inline tile's keyboard and destination rules must be reachable in the chat's own manual.
func TestChatManualDesktopInlineCreate(t *testing.T) {
	for _, question := range []string{
		"how do I create a new desktop place with a name and tint",
		"how do I cancel inline desktop creation with Escape",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-inline-create" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-inline-create", question)
		})
	}
}
