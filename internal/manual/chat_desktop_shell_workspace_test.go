package manual

import "testing"

// These probes keep the workspace's shared and window-local behavior reachable in the reader's words.
func TestDesktopShellWorkspaceQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does the desktop suggest grouping tabs from the same folder",
		"how do I undo a desktop tab change without undoing my typing",
		"do two desktop windows share my active tab or scroll position",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-shell-workspace" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-shell-workspace", question)
		})
	}
}
