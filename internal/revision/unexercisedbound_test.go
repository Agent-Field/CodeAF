package revision

// One coverage round, and no more.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// coverageRoundJob splices one job into a private store so a gate can be read
// back off its ledger.
func coverageRoundJob(t *testing.T, session string) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "coverage-round.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "task-1", Brief: "add the flag",
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "add the flag", SessionID: session}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// anUnexercisedFinding is the finding the acceptance gate raises on its own:
// behaviours the request states that no check exercises, sourced because nobody
// has to ask for them to be checked.
func anUnexercisedFinding() Judgment {
	return Judgment{Pass: false, Checked: true, Sourced: true,
		Gaps:        "1 the request states has no check that exercises it.",
		Unexercised: []string{"the flag turns on"}}
}

// A SECOND UNEXERCISED-ONLY FINDING BUYS NO ROUND. The finding stands until a
// measurement closes it, so without this bound it raises the same round at every
// later gate — the run whose named fix was committed minutes in and spent the
// rest of its wall writing more tests. One round is bought for it and no more,
// and the finding is recorded unclosed so the delivery says why.
func TestASecondUnexercisedFindingBuysNoRound(t *testing.T) {
	graph := coverageRoundJob(t, "s1")
	// The earlier round: the request's behaviours had no check, a round was
	// bought for it, and the row says so.
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
		Gap:         "2 the request states have no check that exercises them.",
		Unexercised: []string{"the flag turns on", "the flag turns off"},
		Round:       1, Extended: true,
	}); err != nil {
		t.Fatal(err)
	}

	node := store.Node{ID: "task-1", Provenance: store.Provenance{Intent: "add the flag"}}
	planned := 0
	extension := ExtendForGap(context.Background(), graph, node, "the flag is added",
		anUnexercisedFinding(), nil, 10,
		func(context.Context, string, string) (store.Subtree, error) {
			planned++
			return store.Subtree{}, nil
		})

	if planned != 0 {
		t.Fatalf("a second coverage round was planned: %d calls", planned)
	}
	if extension.Spliced != 0 || !extension.Unclosed {
		t.Fatalf("the finding did not stay open: %+v", extension)
	}
	if !strings.Contains(extension.Refused, "coverage round was already bought once") {
		t.Fatalf("the refusal does not say what it refused: %q", extension.Refused)
	}
}

// THE BOUND IS THE SECOND ROUND. A job that has never bought a coverage round
// buys its first, which is the whole point of the round existing; the bound may
// not refuse work the finding has not already paid for.
func TestAFirstUnexercisedFindingStillBuysItsRound(t *testing.T) {
	graph := coverageRoundJob(t, "s2")
	node := store.Node{ID: "task-1", Provenance: store.Provenance{Intent: "add the flag"}}
	planned := 0
	extension := ExtendForGap(context.Background(), graph, node, "the flag is added",
		anUnexercisedFinding(), nil, 10,
		func(context.Context, string, string) (store.Subtree, error) {
			planned++
			return store.Subtree{}, nil
		})

	if planned != 1 {
		t.Fatalf("the first coverage round was not even planned: %d calls", planned)
	}
	if strings.Contains(extension.Refused, "coverage round was already bought once") {
		t.Fatalf("the bound refused a round the finding had not paid for: %q", extension.Refused)
	}
}
