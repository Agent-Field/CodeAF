package manual

import "testing"

// Animation questions must reach the step's own page without knowing its component name.
func TestDesktopStepShimmerQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"why does the running step title shimmer", "why does the step clock stop when work finishes", "stop the live step animation with reduced motion"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-step-shimmer" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-step-shimmer", question)
		}
	}
}
