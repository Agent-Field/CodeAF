package manual

import "testing"

// These probes keep the desktop filter tabs reachable in the person's words.
func TestDesktopFilterTabsQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do filter tabs work in the desktop app",
		"how do I move between filter tabs with the arrow keys",
		"what happens to filter tabs on a narrow window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-filter-tabs" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-filter-tabs", question)
		})
	}
}
