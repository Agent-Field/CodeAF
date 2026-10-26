package syncsetup

import (
	"context"
	"net/url"
	"os"
	"slices"
	"testing"
)

// R6/R9/R23 hold on the default relay too: the REAL codeaf client, talking to
// the hosted relay inside the local Worker runtime, joins its identity's
// cluster as a durable learner beside the three hosted anchors, and leading
// returns only an elected, committed and freshly read receipt for itself.
func TestR6R9R23HostedPersonalMemberLifecycle(t *testing.T) {
	endpoint := os.Getenv("CODEAF_HOSTED_PROOF_URL")
	if endpoint == "" {
		t.Skip("local hosted proof runner only")
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" {
		t.Fatal("hosted proof requires loopback HTTP")
	}
	home := machine(t, endpoint)
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
	if !slices.Contains(status.Configuration.Learners, status.Self) || slices.Contains(status.Configuration.Voters, status.Self) {
		t.Fatalf("R9 joining made this computer a voter instead of a learner: %+v", status)
	}
	receipt, err := client.LeadCluster(context.Background(), 0)
	if err != nil {
		t.Fatalf("R6 actual personal leadership: %v", err)
	}
	if receipt.Node != status.Self || receipt.Term == 0 || receipt.Barrier == 0 {
		t.Fatalf("R6 pending intent presented as leadership: %+v", receipt)
	}
}
