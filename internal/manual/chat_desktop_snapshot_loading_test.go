package manual

import "testing"

func TestDesktopSnapshotLoadingQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"does reconnecting the desktop stop a reply or lose earlier messages",
		"why is a large tool output missing when I reopen a desktop conversation",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-snapshot-loading" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-snapshot-loading", question)
		}
	}
}
