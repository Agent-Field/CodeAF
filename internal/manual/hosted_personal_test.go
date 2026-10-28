package manual

import (
	"strings"
	"testing"
)

// R6/R9/R23 keep hosted enrollment, personal leadership and forwarding reachable.
func TestR6R9R23HostedPersonalManualTruth(t *testing.T) {
	for _, query := range []string{"hosted relay learner cluster status", "paired open home hosted learner leadership", "hosted relay personal leader forwarding"} {
		found := false
		for _, section := range Chat().Search(query, DefaultResults) {
			found = found || section.Page == "coordination-and-workers"
		}
		if !found {
			t.Errorf("R9 hosted learner answer unreachable: %q", query)
		}
	}
	page, ok := Chat().Page("coordination-and-workers")
	if !ok {
		t.Fatal("R9 coordination page is missing")
	}
	for _, claim := range []string{
		"joins the hosted relay",
		"as a durable learner",
		"`codeaf cluster lead`",
		"The voters are then two anchors and this computer",
		"routes signed coordination requests",
		"waits for its own durable copy before replying",
		"`codeaf cluster status`",
	} {
		if !strings.Contains(page, claim) {
			t.Errorf("R9/R23 current hosted capability missing: %q", claim)
		}
	}
	for _, obsolete := range []string{"The hosted relay has no learner door", "The hosted relay has no cluster control door", "this relay does not offer cluster leadership yet"} {
		if strings.Contains(page, obsolete) {
			t.Errorf("R9 obsolete current-hosted denial: %q", obsolete)
		}
	}
}
