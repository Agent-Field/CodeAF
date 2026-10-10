package manual

import "testing"

// These probes keep the desktop appearance settings reachable in the person's words.
func TestDesktopAppearanceQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I switch the desktop app to dark theme",
		"does the theme choice stay after I reload the app",
		"where is the reduce motion setting in the desktop app",
		"how do I turn off animations in the desktop app",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-appearance" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-appearance", question)
		})
	}
}
