package manual

import "testing"

// These probes keep a collapsed group's pill reachable in the person's words.
func TestDesktopCollapsedGroupPageAnswersItsQuestions(t *testing.T) {
	for _, question := range []string{
		"where is my collapsed group when I am in another tab",
		"the group pill shows Needs you when a hidden tab needs you",
		"how do I open a collapsed group of desktop tabs",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-collapsed-group" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-collapsed-group", question)
		})
	}
}
