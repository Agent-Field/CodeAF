package manual

import "testing"

// These probes keep the group suggestion pill reachable in the person's words.
func TestDesktopSuggestPillQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where is the group suggestion pill",
		"what does the group suggestion pill say",
		"how do I use the Group button on the suggestion pill",
		"how do I dismiss the group suggestion pill",
		"does the group suggestion pill wrap on a narrow window",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-suggest-pill" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-suggest-pill", question)
		})
	}
}
