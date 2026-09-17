package resident

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// shareRequest is the ordinary overrun round every cap is tested against: one
// node's worth of work, measured, that the free checks would otherwise admit.
func shareRequest(t *testing.T, graph *store.Store) GrowRequest {
	t.Helper()
	node := jobNode(t, graph, "job")
	node.Provenance.SessionID = "s1"
	return GrowRequest{JobRoot: "job", Node: node, Reason: GrowOverrun, Adding: 1, Measured: true, Produced: 1}
}

// GROWTH IS REFUSED ONCE IT HAS SPENT MORE THAN THE REQUESTED WORK COST. The
// rail is measured from the moment a gate first found the work done, and it is
// a share of the bill rather than a count of rounds — the bound a count could
// not give a run whose named fix landed early and whose growth came after.
func TestGrowthIsRefusedWhenSpendAfterTheGateDoublesTheBill(t *testing.T) {
	graph := crowdedJob(t, "s1", 3)
	// The spend that got the requested work done, and the gate that found it
	// done.
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "job", Cost: 1.0}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job", store.DeliveryGate{Pass: true}); err != nil {
		t.Fatal(err)
	}
	// And the growth since, which has already spent twice the work cost.
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "job", Cost: 2.0}); err != nil {
		t.Fatal(err)
	}

	verdict, err := growJob(context.Background(), graph, nil, shareRequest(t, graph))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Allow || verdict.Cause != CauseSpendShare {
		t.Fatalf("growth past the share was admitted: %+v", verdict)
	}
	if verdict.Refused != RefusedSpendShare {
		t.Fatalf("the refusal is not the share rail's own words: %q", verdict.Refused)
	}
}

// THE RAIL ONLY APPLIES WHERE IT CAN BE MEASURED. A job whose gate has never
// found its core work done has no anchor, so growth is bounded by the caps and
// the rail refuses nothing — the fail-safe direction, because a rail that
// guessed at an anchor would stop work it cannot account for.
func TestAJobWithNoGateIsNotRefusedByTheShareRail(t *testing.T) {
	graph := crowdedJob(t, "s2", 3)
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "job", Cost: 5.0}); err != nil {
		t.Fatal(err)
	}

	verdict, err := growJob(context.Background(), graph, nil, shareRequest(t, graph))
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Allow || verdict.Cause == CauseSpendShare {
		t.Fatalf("a job with no gate was refused by the share rail: %+v", verdict)
	}
}
