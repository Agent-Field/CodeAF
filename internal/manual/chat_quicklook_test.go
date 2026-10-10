package manual

import "testing"

// Quick Look must be reachable in the words a person uses while browsing Home.
func TestChatManualQuickLook(t *testing.T) {
	for _, question := range []string{
		"how do I preview a place without switching my tabs",
		"how do I close Quick Look with Space or Escape",
		"how do I go to the place from Quick Look",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-quicklook" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-quicklook", question)
		})
	}
}
