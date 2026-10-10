package manual

import "testing"

// The address controls must be reachable by the words someone uses when a button disappears.
func TestDesktopWebAddressQuestions(t *testing.T) {
	for _, question := range []string{
		"how do I edit the web address and cancel with Escape",
		"where did Open in browser and Start a conversation with this page go",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-web-address" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-web-address", question)
		})
	}
}
