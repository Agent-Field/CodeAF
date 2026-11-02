package syncsetup

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// R6/R9/R23 require the REAL codeaf client and production relay to activate a
// durable personal learner before status/lead can report it. An internal core
// fixture or a hosted actor standing in for this computer cannot prove this.
func TestR6R9R23PersonalMemberProductionLifecycle(t *testing.T) {
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer service.Close()
	server := httptest.NewServer(service.Handler)
	defer server.Close()
	home := machine(t, server.URL)
	client, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatal(err)
	}
	status, err := client.ClusterStatus(context.Background())
	if err != nil {
		t.Fatalf("R6/R9 personal activation/status door: %v", err)
	}
	if status.Self == 0 || len(status.Configuration.Voters) != 3 || len(status.Configuration.Learners) == 0 {
		t.Fatalf("R6/R9 personal home did not join as a learner: %+v", status)
	}
	receipt, err := client.LeadCluster(context.Background(), 0)
	if err != nil {
		t.Fatalf("R6 actual personal leadership: %v", err)
	}
	if receipt.Node != status.Self || receipt.Term == 0 || receipt.Barrier == 0 {
		t.Fatalf("R6 pending intent presented as leadership: %+v", receipt)
	}
}
