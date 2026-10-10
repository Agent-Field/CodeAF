package manual

import "testing"

// These probes keep merging two desktop places reachable without changing the shared question table.
func TestDesktopPlaceMergeQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I merge two places in the desktop app",
		"which place is kept when I merge two desktop places",
		"what happens to instructions, the model and the tint when two places merge",
		"where is Merge into in the desktop place menu",
		"does merging a desktop place delete its chats",
		"what does Merge do on a desktop place that has not been touched",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-place-merge" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-place-merge", question)
		})
	}
}
