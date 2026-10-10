package manual

import "testing"

func TestDesktopOverviewCardMenuQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I right-click an overview card",
		"why does a terminal card in the overview show no lines",
		"what does an empty conversation card show in the overview",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-overview-card-menu" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-overview-card-menu", question)
		}
	}
}
