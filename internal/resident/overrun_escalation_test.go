package resident

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// exhaustionLeafFixture splices a two-node job whose leaf carries the named
// subharness, then claims and starts the leaf so it looks like a worker that
// ran and stopped. The leaf's node record carries its subharness — the
// envelope the continuation's escalation is decided from.
func exhaustionLeafFixture(t *testing.T, subharness string) (*store.Store, store.Node) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "the part that exhausted", Subharness: subharness},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("job-a", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("job-a")
	if node.Subharness != subharness {
		t.Fatalf("fixture leaf is on %q, want %q", node.Subharness, subharness)
	}
	return graph, node
}

// A bare exhaustion escalates its continuation to the generalist. bare is the
// smallest envelope, and a leaf that ran out of budget inside it has already
// proved the sitting was bigger than bare — re-running bare is paying to learn
// the same lesson twice. The dead leaf's structured state rides into the
// continuation's replan brief under the state header, so the escalation
// changes the envelope without losing what the bare leaf already found.
func TestABareExhaustionEscalatesItsContinuationToLinear(t *testing.T) {
	graph, node := exhaustionLeafFixture(t, "bare")

	var capturedGoal string
	planned := func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		capturedGoal = goal
		return twoNodeRemainder(ctx, goal, prefix)
	}
	spliced, sink, err := ReplanOverrunAs(context.Background(), graph, node,
		"the partial result", "", nil, 0, "",
		Growth{Reason: GrowOverrun, State: "2 files changed; ran `go test` (pass)"},
		planned)
	if err != nil || spliced != 2 {
		t.Fatalf("spliced=%d err=%v", spliced, err)
	}
	continued, _, _ := graph.Node(sink)
	if continued.Subharness != "linear" {
		t.Fatalf("a bare exhaustion continued on %q, want the generalist (linear)", continued.Subharness)
	}
	if continued.Provenance.Subharness != "linear" {
		t.Fatalf("the escalated envelope was not journaled onto the provenance: %q",
			continued.Provenance.Subharness)
	}
	// The state handover is intact: the continuation's replan brief carries the
	// dead leaf's findings under the state header, so it resumes from there
	// instead of re-discovering them in the larger envelope.
	if !strings.Contains(capturedGoal, ContinuationStateHeader) {
		t.Fatalf("the continuation goal lost the state header:\n%s", capturedGoal)
	}
	if !strings.Contains(capturedGoal, "2 files changed") {
		t.Fatalf("the continuation goal lost the state body:\n%s", capturedGoal)
	}
}

// The measured defect, pinned: a bare leaf that exhausted and whose judge
// re-chose bare used to run bare again. The escalation overrides that — the
// judge naming the same envelope that just failed is exactly the lesson
// already paid for, so the continuation takes the generalist regardless.
func TestABareExhaustionEscalatesEvenWhenTheJudgeReNamesBare(t *testing.T) {
	graph, node := exhaustionLeafFixture(t, "bare")

	spliced, sink, err := ReplanOverrunOn(context.Background(), graph, node,
		"the partial result", "", nil, 0, "bare", twoNodeRemainder)
	if err != nil || spliced != 2 {
		t.Fatalf("spliced=%d err=%v", spliced, err)
	}
	continued, _, _ := graph.Node(sink)
	if continued.Subharness != "linear" {
		t.Fatalf("a bare exhaustion the judge re-named bare continued on %q, want linear",
			continued.Subharness)
	}
}

// A linear exhaustion keeps linear. linear is the generalist ceiling — the
// largest single-agent envelope — so an exhaustion there has no higher
// generalist rung to climb to, and repeating it is correct rather than a
// reflex of the clock. The continuation names the generalist it keeps.
func TestALinearExhaustionKeepsTheGeneralist(t *testing.T) {
	graph, node := exhaustionLeafFixture(t, "linear")

	spliced, sink, err := ReplanOverrun(context.Background(), graph, node,
		"the partial result", "", nil, 0, twoNodeRemainder)
	if err != nil || spliced != 2 {
		t.Fatalf("spliced=%d err=%v", spliced, err)
	}
	continued, _, _ := graph.Node(sink)
	if continued.Subharness != "linear" {
		t.Fatalf("a linear exhaustion continued on %q, want linear (the generalist ceiling)",
			continued.Subharness)
	}
}

// A swe exhaustion is left to the frozen engine's own continuation semantics.
// Exhausting a swe leaf says nothing about the envelope — the engine resumes
// from its own checkpoints — so the ladder does not touch it: the continuation
// runs on the worker the caller judged, unchanged.
func TestASweExhaustionIsLeftToTheFrozenEngine(t *testing.T) {
	graph, node := exhaustionLeafFixture(t, "swe")

	spliced, sink, err := ReplanOverrunOn(context.Background(), graph, node,
		"the partial result", "", nil, 0, "swe", twoNodeRemainder)
	if err != nil || spliced != 2 {
		t.Fatalf("spliced=%d err=%v", spliced, err)
	}
	continued, _, _ := graph.Node(sink)
	if continued.Subharness != "swe" {
		t.Fatalf("a swe exhaustion the judge named swe continued on %q, want swe (frozen)",
			continued.Subharness)
	}
}

// A linear exhaustion on a job the compiler named for a specialist climbs the
// third rung: the shape judgment was made at admission, and two exhausted
// single-agent envelopes are the size evidence it was waiting for. The
// provenance is the only place that judgment survives — the leaf's own record
// says linear after the first escalation — so the ladder reads it there.
func TestALinearExhaustionClimbsToTheProvenancesSpecialist(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "the part that exhausted", Subharness: "linear"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test", Subharness: "swe"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("job-a", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("job-a")

	spliced, sink, err := ReplanOverrun(context.Background(), graph, node,
		"the partial result", "", nil, 0, twoNodeRemainder)
	if err != nil || spliced != 2 {
		t.Fatalf("spliced=%d err=%v", spliced, err)
	}
	continued, _, _ := graph.Node(sink)
	if continued.Subharness != "swe" {
		t.Fatalf("a linear exhaustion on a swe-shaped job continued on %q, want swe (the third rung)",
			continued.Subharness)
	}
}

// The cheap-first inheritance: a node that made no choice of its own on a job
// the compiler named for a specialist starts on the cheap whole-taker, and
// the compiler's choice survives untouched in the provenance for the third
// rung to read. A node with its own verdict is never rewritten.
func TestCheapFirstOnSubtree(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "bare", Purpose: "one agent, one sitting, minimal loop"})
	r := &Reconciler{}

	specialistJob := store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "unjudged"},
		{ID: "job-b", Parent: "job", Brief: "judged", Subharness: "swe"},
	}}
	out := r.cheapFirstOnSubtree(specialistJob, Compiled{Subharness: "swe"})
	if out.Nodes[0].Subharness != "bare" || out.Nodes[1].Subharness != "bare" {
		t.Fatalf("unjudged nodes = %q, %q, want bare", out.Nodes[0].Subharness, out.Nodes[1].Subharness)
	}
	if out.Nodes[2].Subharness != "swe" {
		t.Fatalf("judged node = %q, want its own verdict kept", out.Nodes[2].Subharness)
	}

	generalistJob := store.Subtree{Nodes: []store.NodeSpec{{ID: "job", Brief: "the whole job"}}}
	out = r.cheapFirstOnSubtree(generalistJob, Compiled{Subharness: "linear"})
	if out.Nodes[0].Subharness != "" {
		t.Fatalf("generalist-compiled node = %q, want the inheritance left alone", out.Nodes[0].Subharness)
	}
}

// A WORKER OFF THE PROFILE'S ROSTER IS A RUNG THE LADDER CANNOT REACH.
//
// The third rung is the one place the ladder climbs to a worker nobody has run
// yet — the compiler's admission judgment, cashed in after two exhausted
// single-agent envelopes — and it is gated on the registration and not on the
// name. So an install whose roster leaves the specialist out keeps the
// generalist, named, exactly as it does on a job whose provenance judged
// nothing: there is no higher rung here to climb to.
func TestALinearExhaustionKeepsTheGeneralistWhenTheSpecialistIsNotInstalled(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.ForgetSubharnesses()

	if got := escalateContinuation(exec.LinearSubharness, "", "swe"); got != exec.LinearSubharness {
		t.Fatalf("the ladder climbed to %q with swe off the roster, want the generalist", got)
	}
	// And the same job on a build that has the worker does climb, so the test
	// above is about the registration and not about the ladder being broken.
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	if got := escalateContinuation(exec.LinearSubharness, "", "swe"); got != "swe" {
		t.Fatalf("the ladder did not climb to an installed specialist: %q", got)
	}
}
