package manual

import (
	"strings"
	"testing"
)

// R9/R23/R27 keep the moved coordination material reachable while device pages
// keep their own vocabulary and the existing retrieval probes stay intact.
func TestR9R23R27LearnerAndIdleAnswersRemainReachable(t *testing.T) {
	for _, q := range []string{
		"joined computers learner capacity outdated build",
		"native relay open idle home vault polling",
		"self hosted codeaf relay persistent computer",
		"hosted relay directory upgrade older clients",
	} {
		found := false
		for _, section := range Chat().Search(q, DefaultResults) {
			if section.Page == "coordination-and-workers" {
				found = true
			}
		}
		if !found {
			t.Errorf("R9 coordination answer unreachable: %q", q)
		}
	}
	page, _ := Chat().Page("coordination-and-workers")
	for _, claim := range []string{"durable learner", "The three anchors remain the only voters", "Leadership is not offered yet on any relay", "hosted relay has no learner door"} {
		if !strings.Contains(page, claim) {
			t.Errorf("R9 missing current capability: %q", claim)
		}
	}
}
