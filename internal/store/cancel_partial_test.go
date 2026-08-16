package store

import (
	"strings"
	"testing"
)

// The partial a worker had already written used to be computed on the cancel
// path and then dropped: the cancelled node kept a reason and nothing else, so
// the restart that wires a fresh subtree to the dead attempt's digest had
// nothing to read and retyped work that was already on disk. It survives the
// write, the rebuild, and the digest.
func TestCancellationKeepsWhatTheWorkerHadWritten(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "write the report", Title: "Report"},
		{ID: "draft", Parent: "job", Brief: "draft the findings", Title: "Draft"},
		{ID: "polish", Parent: "job", Brief: "polish the findings", Title: "Polish",
			Needs: []Need{{NodeID: "draft", Kind: FeedsInto}}},
	}}, Provenance{Origin: OriginUser, SessionID: "cancel", Intent: "write the report"}); err != nil {
		t.Fatal(err)
	}

	partial := "Section 1 and 2 are written to /tmp/report.md; section 3 was never started."
	if err := graph.CancelPendingWithPartial("draft", UserCancelReason, partial); err != nil {
		t.Fatalf("CancelPendingWithPartial: %v", err)
	}
	cancelled, found, err := graph.Node("draft")
	if err != nil || !found {
		t.Fatalf("draft after cancel: found=%v err=%v", found, err)
	}
	if cancelled.Status != Cancelled || cancelled.Error != UserCancelReason {
		t.Fatalf("cancelled node = %+v", cancelled)
	}
	if cancelled.Summary != partial {
		t.Fatalf("the partial was dropped: summary = %q", cancelled.Summary)
	}

	// The journal is the truth, so the rebuilt view has to say the same thing.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	rebuilt, _, _ := graph.Node("draft")
	if rebuilt.Summary != partial || rebuilt.Status != Cancelled {
		t.Fatalf("rebuilt cancellation lost the partial: %+v", rebuilt)
	}

	// And the one reader that matters: whoever comes next, including the retry
	// a restart splices against the dead attempt.
	inputs, err := graph.DependencyInputs("polish", 4096)
	if err != nil {
		t.Fatalf("DependencyInputs: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("dependency inputs = %+v", inputs)
	}
	if !strings.Contains(inputs[0].Digest, "cancelled midway") {
		t.Fatalf("digest does not say where the work stopped: %q", inputs[0].Digest)
	}
	if !strings.Contains(inputs[0].Digest, "Section 1 and 2 are written") {
		t.Fatalf("digest does not carry the partial: %q", inputs[0].Digest)
	}
	if !containsPath(inputs[0].Artifacts, "/tmp/report.md") {
		t.Fatalf("the files the cancelled worker left are invisible: %v", inputs[0].Artifacts)
	}
}

// A cancellation with nothing written behind it still reads as work that never
// ran — the old wording, kept for the case it was actually true of.
func TestCancellationWithNothingWrittenStillReadsAsNotRun(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "ship it", Title: "Ship"},
		{ID: "first", Parent: "job", Brief: "the first step"},
		{ID: "second", Parent: "job", Brief: "the second step",
			Needs: []Need{{NodeID: "first", Kind: FeedsInto}}},
	}}, Provenance{Origin: OriginUser, SessionID: "cancel", Intent: "ship it"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.CancelPending("first", "the user no longer wants this"); err != nil {
		t.Fatal(err)
	}
	inputs, err := graph.DependencyInputs("second", 4096)
	if err != nil {
		t.Fatalf("DependencyInputs: %v", err)
	}
	if len(inputs) != 1 || !strings.Contains(inputs[0].Digest, "(not run)") {
		t.Fatalf("digest for a node that never ran = %+v", inputs)
	}
}

// The invisible permanent park: the runner releases the claim first and cancels
// second, so a failed cancel leaves the node Pending with cancel_requested set
// — refused by Ready, refused by Claim, and its parent waiting on it forever.
// The startup sweep finishes the row, and does not call it recovered work.
func TestStartupSweepFinishesAParkedCancellation(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "ship it", Title: "Ship"},
		{ID: "parked", Parent: "job", Brief: "the step the user stopped"},
	}}, Provenance{Origin: OriginUser, SessionID: "park", Intent: "ship it"}); err != nil {
		t.Fatal(err)
	}
	claim := mustClaim(t, graph, "parked", "runner")
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.RequestNodeCancel("parked", UserCancelReason); err != nil {
		t.Fatal(err)
	}
	// Exactly the state the dropped error left behind: released, still asked to
	// cancel, and cancelled by nobody.
	if err := graph.Release(claim); err != nil {
		t.Fatal(err)
	}
	before, _, _ := graph.Node("parked")
	if before.Status != Pending || !before.CancelRequested {
		t.Fatalf("the park was not reproduced: %+v", before)
	}
	ready, err := graph.Ready(20)
	if err != nil {
		t.Fatal(err)
	}
	if nodeListed(ready, "parked") {
		t.Fatal("a parked cancellation was offered as ready work")
	}

	released, err := graph.ReleaseOrphans()
	if err != nil {
		t.Fatalf("ReleaseOrphans: %v", err)
	}
	for _, id := range released {
		if id == "parked" {
			t.Fatal("a parked cancellation was announced as work picked up again")
		}
	}
	after, _, _ := graph.Node("parked")
	if after.Status != Cancelled || after.CancelRequested {
		t.Fatalf("the parked row was left where it was: %+v", after)
	}
}

func containsPath(paths []string, wanted string) bool {
	for _, path := range paths {
		if path == wanted {
			return true
		}
	}
	return false
}
