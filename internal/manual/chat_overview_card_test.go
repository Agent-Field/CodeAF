package manual

import "testing"

func TestDesktopOverviewCardQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what does an overview card show",
		"why does an overview card say 4 running",
		"how do I allow all from the tab overview",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-overview-card" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-overview-card", question)
		}
	}
}
