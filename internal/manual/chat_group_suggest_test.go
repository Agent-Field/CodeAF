package manual

import "testing"

// These probes keep the workspace group-suggestion rule reachable in the person's words.
func TestDesktopGroupSuggestQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"when does the desktop offer a group suggestion for tabs on one workspace",
		"does a group suggestion compare tab titles or the workspace folder",
		"dismissing a group suggestion for this set of tabs",
		"a pinned tab or a tab already in a group and the group suggestion",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-group-suggest" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-group-suggest", question)
		})
	}
}
