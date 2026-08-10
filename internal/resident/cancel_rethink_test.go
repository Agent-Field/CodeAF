package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A cascade cancels the whole subtree, and the sentinel is owed one event
// naming the root — not one per node, racing each other over the same
// remainder on the same budget.
func TestACancelledSubtreeConvenesTheSentinelExactlyOnce(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "release", Brief: "ship the release", Title: "Release", Stage: 2},
		{ID: "docs", Parent: "release", Brief: "write the docs", Title: "Docs", Stage: 1},
		{ID: "tests", Parent: "release", Brief: "run the tests", Title: "Tests", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "cancel", Intent: "ship the release"}); err != nil {
		t.Fatal(err)
	}
	var rethought []string
	reconciler := New(graph, nil, nil).WithCancelRethink(
		func(_ context.Context, node store.Node, _ string) {
			rethought = append(rethought, node.ID)
		})
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "cancel", Kind: store.CommandCancel, Target: "release", Instruction: "stop this",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rethought) != 1 || rethought[0] != "release" {
		t.Fatalf("a three-node cascade convened the sentinel over %v, want [release]", rethought)
	}
	// A second tick reads no new events, so nothing is reconsidered twice.
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rethought) != 1 {
		t.Fatalf("the settle lane replayed the cancellation: %v", rethought)
	}
}

// Only the user's own cancellations. A revision's own removals cancel pending
// nodes too, and convening the sentinel over its own edit is a loop with a
// budget attached.
func TestARevisionsOwnRemovalNeverConvenesTheSentinel(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship it", Title: "Ship"},
		{ID: "extra", Parent: "job", Brief: "the step the sentinel cut"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "cancel", Intent: "ship it"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reconciler := New(graph, nil, nil).WithCancelRethink(
		func(context.Context, store.Node, string) { calls++ })
	if err := graph.CancelPending("extra", "revision: the API makes this unnecessary"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("the sentinel was convened over its own edit %d time(s)", calls)
	}
}

// A leaf that had started belongs to the leaf wrapper, which holds the partial
// and revises before it hands the claim back. Letting the settle lane fire too
// would spend twice to say one thing.
func TestACancelledRunningLeafIsLeftToTheLeafWrapper(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship it", Title: "Ship"},
		{ID: "leaf", Parent: "job", Brief: "the step a worker was inside"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "cancel", Intent: "ship it"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("leaf", "runner")
	if err != nil || !won {
		t.Fatalf("Claim: won=%v err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.RequestNodeCancel("leaf", store.UserCancelReason); err != nil {
		t.Fatal(err)
	}
	if err := graph.Release(claim); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reconciler := New(graph, nil, nil).WithCancelRethink(
		func(context.Context, store.Node, string) { calls++ })
	runner := NewRunner(graph, nil, "runner", 1)
	node, _, _ := graph.Node("leaf")
	runner.finishCancellation(node, "half of the report is written")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("the settle lane revised a leaf the wrapper already revised (%d call(s))", calls)
	}
	settled, _, _ := graph.Node("leaf")
	if settled.Status != store.Cancelled || settled.Summary != "half of the report is written" {
		t.Fatalf("cancelled leaf = %+v", settled)
	}
}

// The event the sentinel actually reads. A cancellation is a decision, and the
// one edit it must never buy is a node that redoes or checks the work the user
// just stopped paying for.
func TestTheCancelledEventForbidsRedoingTheCancelledWork(t *testing.T) {
	node := store.Node{ID: "job-n3", Title: "Draft the findings"}
	event := CancelledRevisionEvent(node, "sections 1 and 2 are written", store.UserCancelReason)
	for _, wanted := range []string{
		"CANCELLED by the user", "Draft the findings", "sections 1 and 2 are written",
		"unstarted remainder", "Never add a node that redoes",
	} {
		if !strings.Contains(event, wanted) {
			t.Fatalf("cancellation event is missing %q:\n%s", wanted, event)
		}
	}
	if strings.Contains(event, "FAILED") {
		t.Fatalf("a cancellation was phrased as a failure:\n%s", event)
	}
}
