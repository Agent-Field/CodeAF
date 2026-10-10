package manual

import "testing"

// These probes keep moving a tab group reachable in the person's words.
func TestDesktopGroupMoveQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I move a whole tab group in the desktop strip",
		"drag the group label to reorder a group of tabs",
		"keyboard shortcut to move a tab group left or right",
		"can I drop a group before a pinned tab",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-group-move" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-group-move", question)
		})
	}
}
