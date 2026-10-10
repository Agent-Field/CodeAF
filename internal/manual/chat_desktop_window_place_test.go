package manual

import "testing"

// These probes keep the window's remembered destination reachable in the person's words.
func TestDesktopWindowPlaceQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"does each desktop window remember its own place when I relaunch",
		"why does a deleted or archived place fall back to Now",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-window-place" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-window-place", question)
		})
	}
}
