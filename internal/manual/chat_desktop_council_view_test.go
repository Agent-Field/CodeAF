package manual

import "testing"

func TestDesktopCouncilViewQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what does a council discussion look like in the desktop app",
		"what is the Decided card at the end of a council",
		"can I steer or add to a council discussion",
		"what does Writing here pauses it until you send mean",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-council-view" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-council-view", question)
		}
	}
}
