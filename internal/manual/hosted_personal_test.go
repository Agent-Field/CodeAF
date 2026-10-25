package manual

import (
	"strings"
	"testing"
)

// R9/R23 keep current hosted enrollment and status distinct from the older
// relay fallback, while leaving leadership availability explicit.
func TestR9R23HostedLearnerManualTruth(t *testing.T) {
	for _, query := range []string{"hosted relay learner cluster status", "paired open home hosted learner leadership"} {
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
		"hosted relay admits paired computers as learners",
		"`codeaf cluster status`",
		"this relay does not offer cluster leadership yet; your saved chats are still here",
	} {
		if !strings.Contains(page, claim) {
			t.Errorf("R9/R23 current hosted capability missing: %q", claim)
		}
	}
	for _, obsolete := range []string{"The hosted relay has no learner door", "The hosted relay has no cluster control door"} {
		if strings.Contains(page, obsolete) {
			t.Errorf("R9 obsolete current-hosted denial: %q", obsolete)
		}
	}
}
