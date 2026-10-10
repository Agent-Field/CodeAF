package manual

import "testing"

// These probes keep the window frame's tint reachable in the person's words.
func TestDesktopWindowTintQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why did the window colour change when I went to a place",
		"what colour is Now",
		"does the Mac title bar change colour with the place",
		"why doesn't the window fade when I switch places",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-window-tint" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-window-tint", question)
		})
	}
}
