package manual

import (
	"fmt"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R26/R27/C30 keep hosted idle costs and the fresh-read wake reachable.
func TestR26R27C30HostedQuietManual(t *testing.T) {
	for _, query := range []string{"hosted idle presence sweeps sleeping connections", "hosted open home idle memory preferences"} {
		found := false
		for _, section := range Chat().Search(query, DefaultResults) {
			found = found || section.Page == "coordination-and-workers"
		}
		if !found {
			t.Errorf("R27 quiet answer unreachable: %q", query)
		}
	}
	page, ok := Chat().Page("coordination-and-workers")
	if !ok {
		t.Fatal("missing coordination page")
	}
	for _, claim := range []string{"zero HTTP requests", "zero peer or forwarding frames", "zero client attempts", "no recurring alarms", "neither wakes the anchors nor reconnects", "first real request obtains a fresh majority", fmt.Sprintf("every %d seconds", cs.PresenceEveryMillis/1000), fmt.Sprintf("every %d seconds", cs.RenewEveryMillis/1000)} {
		if !strings.Contains(page, claim) {
			t.Errorf("R27 missing hosted idle behavior %q", claim)
		}
	}
}
