package manual

import "testing"

// These probes keep the desktop tab's full-title tooltip and press fill reachable
// in the words a person would use. The shared chat table is left alone.
func TestDesktopTabTitleQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where is the full title when a desktop tab name is cut off",
		"how long is the desktop tab full title tooltip",
		"what does a pressed desktop tab look like",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-tab-title" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-tab-title", question)
		})
	}
}
