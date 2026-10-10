package manual

import "testing"

// Retired rail rows have their own probes so a person asking about the side list
// reaches this page, not a shared desktop page.
func TestDesktopRailLegacyQuestions(t *testing.T) {
	for _, question := range []string{
		"where did Find anything go on the desktop rail",
		"how do I open Settings from the desktop sidebar",
		"where is the theme on the desktop rail",
		"where is Activity on the desktop rail",
		"why does the desktop rail show a Design system row",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-rail-legacy" {
					return
				}
			}
			t.Fatalf("%q should reach desktop-rail-legacy", question)
		})
	}
}
