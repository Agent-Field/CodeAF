package manual

import "testing"

// Questions about a finger on the desktop shell must reach the touch-target page.
// The shared chat table stays untouched; this file is the only probe list for that page.
func TestDesktopTouchTargetsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I close a tab with my finger",
		"how do I open a tab menu without a right click",
		"how do I open a group menu on a tablet",
		"why are the sidebar rows taller on my phone",
		"how do I resize a split with my finger",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-touch-targets" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-touch-targets", question)
		})
	}
}
