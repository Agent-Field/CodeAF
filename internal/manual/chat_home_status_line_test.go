package manual

import "testing"

// These probes keep the place Home status line reachable in the person's words.
func TestDesktopHomeStatusLineQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does home say deciding automatically",
		"what does learning 14 of 20 agreed mean under the home title",
		"what does always asks you mean under the home title",
		"when is the place status line hidden",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-home-status-line" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-home-status-line", question)
		})
	}
}
