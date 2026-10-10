package manual

import "testing"

// Live's own probes keep its new page reachable without changing the shared desktop corpus table.
func TestDesktopHomeLiveAnswersItsQuestions(t *testing.T) {
	for _, question := range []string{
		"what does Live on a place Home show",
		"how do I reopen closed running work from Home Live",
		"does Home Live show discussion topics and council turns",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-home-live" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach desktop-home-live", question)
		}
	}
}
