package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The scenario the mechanism exists for: worker A runs out of budget with B
// waiting on it. The repair subtree must consume A's partial, live under the
// same job, and hold B until the remainder actually lands.
func TestReplanOverrunSplicesRepairAndRewiresWaiters(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "the oversized part"},
		{ID: "job-b", Parent: "job", Brief: "consumes a", Needs: []store.Need{{NodeID: "job-a", Kind: store.FeedsInto}}},
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

	nodeA, _, _ := graph.Node("job-a")
	planned := func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		anchor, ok := PlanAnchorFromContext(ctx)
		if !ok || anchor.NodeID != "job" || anchor.SessionID != "s1" || anchor.CommandSeq != 0 {
			t.Fatalf("replan anchor = %+v ok=%t", anchor, ok)
		}
		if prefix == "job-a-x1" && (!strings.Contains(goal, "partial progress text") || !strings.Contains(goal, "/tmp/partial.md")) {
			t.Fatalf("replan goal does not carry the partial result:\n%s", goal)
		}
		if prefix == "job-a-x2" && !strings.Contains(goal, "the docker half is missing") {
			t.Fatalf("replan goal does not carry the reviewer's gap:\n%s", goal)
		}
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: prefix + "-n9", Brief: "finish it", Title: "Finish"},
			{ID: prefix + "-n5", Parent: prefix + "-n9", Brief: "remaining piece", Title: "Remaining piece"},
		}}, nil
	}
	spliced, sink, err := ReplanOverrun(context.Background(), graph, nodeA, "partial progress text", "", []string{"/tmp/partial.md"}, 20, planned)
	if err != nil {
		t.Fatal(err)
	}
	if spliced != 2 || sink != "job-a-x1-n9" {
		t.Fatalf("spliced=%d sink=%q", spliced, sink)
	}

	// The repair belongs to the same job and consumes A's digest.
	repairEntry, ok, err := graph.Node("job-a-x1-n5")
	if err != nil || !ok {
		t.Fatalf("repair entry missing: %v", err)
	}
	sinkNode, _, _ := graph.Node(sink)
	if sinkNode.Parent != "job" || repairEntry.Parent != sink {
		t.Fatalf("repair parents wrong: sink under %q, entry under %q", sinkNode.Parent, repairEntry.Parent)
	}

	// B now waits for the finished remainder as well as A; nothing repair-side
	// is ready until A's partial lands.
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	sinkFeedsB, aFeedsEntry := false, false
	for _, edge := range edges {
		if edge.From == sink && edge.To == "job-b" {
			sinkFeedsB = true
		}
		if edge.From == "job-a" && edge.To == "job-a-x1-n5" {
			aFeedsEntry = true
		}
	}
	if !sinkFeedsB || !aFeedsEntry {
		t.Fatalf("wiring incomplete: sink→b=%t a→entry=%t\n%v", sinkFeedsB, aFeedsEntry, edges)
	}
	ready, err := graph.Ready(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range ready {
		if node.ID == "job-b" || strings.HasPrefix(node.ID, "job-a-x1") {
			t.Fatalf("%s is ready while A still runs", node.ID)
		}
	}

	// A lands its partial; the repair entry becomes ready, B still waits.
	if err := graph.Complete(claim, "partial progress text"); err != nil {
		t.Fatal(err)
	}
	ready, _ = graph.Ready(10)
	readyIDs := map[string]bool{}
	for _, node := range ready {
		readyIDs[node.ID] = true
	}
	if !readyIDs["job-a-x1-n5"] || readyIDs["job-b"] {
		t.Fatalf("after A lands: ready=%v, want repair entry ready and b held", readyIDs)
	}

	// The journal reproduces the added edges.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild after edge additions: %v", err)
	}

	// Repair leaves may split for MaxOverrunRounds rounds; the counter
	// replaces the old suffix instead of stacking markers.
	repair, _, _ := graph.Node("job-a-x1-n5")
	// Each round names a file it wrote: a lineage that changes nothing twice
	// running is refused before the round counter is reached, and this test is
	// about the counter and the splice.
	spliced, sink, err = ReplanOverrun(context.Background(), graph, repair, "more partial", "the docker half is missing",
		[]string{"docker/Dockerfile"}, 20, planned)
	if err != nil || spliced != 2 || sink != "job-a-x2-n9" {
		t.Fatalf("round two: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	repair, _, _ = graph.Node("job-a-x2-n5")
	spliced, sink, err = ReplanOverrun(context.Background(), graph, repair, "last partial", "",
		[]string{"docker/compose.yml"}, 20, planned)
	if err != nil || spliced != 2 || sink != "job-a-x3-n9" {
		t.Fatalf("round three: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if strings.HasPrefix(node.ID, "job-a-x") && strings.Count(node.ID, "-x") != 1 {
			t.Fatalf("overrun id stacked round suffixes: %q", node.ID)
		}
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := graph.Node("job-a-x3-n9"); err != nil || !ok {
		t.Fatalf("rebuilt third round missing: ok=%t err=%v", ok, err)
	}

	// Round four is where the governor draws the line: 27 real rounds under
	// one leaf is the incident this cap exists for. No splice, no planner
	// call, and the thread carries a receipt instead of silence.
	plansBefore := 0
	counting := func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		plansBefore++
		return planned(ctx, goal, prefix)
	}
	repair, _, _ = graph.Node("job-a-x3-n5")
	spliced, sink, err = ReplanOverrun(context.Background(), graph, repair, "still partial", "", nil, 20, counting)
	if err != nil || spliced != 0 || sink != "" {
		t.Fatalf("capped round: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	if plansBefore != 0 {
		t.Fatalf("planner called %d times past the round cap", plansBefore)
	}
	// The receipt lands on the work's own record and never in the conversation
	// (13.18): how many times a job was allowed to divide is the machinery's own
	// arithmetic, and the delivery that follows says what the person got.
	assertRecordOnly(t, graph, "s1", "job-a-x3-n5", "split as many times")
}

// The job-lifetime ceiling: many siblings can each split within the round
// allowance, and the sum is the sprawl the round cap alone cannot see.
func TestReplanOverrunStopsAtJobCeiling(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "ceiling.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	specs := []store.NodeSpec{{ID: "big-job", Brief: "the whole job"}}
	for i := 0; i < maxJobNodes-2; i++ {
		specs = append(specs, store.NodeSpec{ID: fmt.Sprintf("big-job-n%d", i), Parent: "big-job", Brief: "piece"})
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: specs}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s9", Intent: "test",
	}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("big-job-n0", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("big-job-n0")
	planned := func(_ context.Context, _, prefix string) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: prefix + "-n1", Brief: "finish it"},
			{ID: prefix + "-n2", Parent: prefix + "-n1", Brief: "remaining piece"},
		}}, nil
	}
	spliced, sink, err := ReplanOverrun(context.Background(), graph, node, "partial", "", nil, 20, planned)
	if err != nil || spliced != 0 || sink != "" {
		t.Fatalf("ceiling replan: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	if _, ok, err := graph.Node("big-job-n0-x1-n1"); err != nil || ok {
		t.Fatalf("repair landed past the job ceiling: ok=%t err=%v", ok, err)
	}
	assertRecordOnly(t, graph, "s9", node.ID, "grown as large")
}

func TestReplanOverrunPausesBeforeSpliceAtDailyRail(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "rail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "rail-job", Brief: "finish it", Stage: 2},
		{ID: "oversized", Parent: "rail-job", Brief: "finish the oversized task", Stage: 1},
		{ID: "consumer", Parent: "rail-job", Brief: "assemble the result", Stage: 2, Needs: []store.Need{{NodeID: "oversized", Kind: store.FeedsInto}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "rail-session", Intent: "finish it"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 1}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("oversized", "rail-worker")
	if err != nil || !won {
		t.Fatalf("claim oversized: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("oversized")
	plans := 0
	plan := func(_ context.Context, _, prefix string) (store.Subtree, error) {
		plans++
		return store.Subtree{Nodes: []store.NodeSpec{{ID: prefix, Brief: "finish deferred remainder"}}}, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		spliced, sink, err := ReplanOverrun(context.Background(), graph, node, "partial", "", nil, 1, plan)
		if err != nil || spliced != 0 || sink != "" {
			t.Fatalf("rail replan %d = spliced %d sink %q err=%v", attempt, spliced, sink, err)
		}
	}
	if plans != 0 {
		t.Fatalf("planner called %d times at the rail", plans)
	}
	if _, ok, err := graph.Node("must-not-land"); err != nil || ok {
		t.Fatalf("repair node landed at rail: ok=%t err=%v", ok, err)
	}
	messages, err := graph.Messages("rail-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	for _, message := range messages {
		if strings.HasPrefix(message.Body, store.DailyRailQuestionPrefix) {
			questions++
		}
	}
	if questions != 1 {
		t.Fatalf("rail questions = %d, want one", questions)
	}
	pending, err := graph.PendingOverruns(0)
	if err != nil || len(pending) != 1 || pending[0].Prefix != "oversized-x1" {
		t.Fatalf("deferred overruns = %+v err=%v", pending, err)
	}
	if err := graph.Complete(claim, "partial"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RaiseDailyRail(1, "test:yes"); err != nil {
		t.Fatal(err)
	}
	runs := 0
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		runs++
		return ExecResult{Summary: "done"}, nil
	}, "deferred-runner", 1).WithDailyBudgetUSD(1)
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 0 || runs != 0 {
		t.Fatalf("claim raced deferred replan: dispatched=%d runs=%d err=%v", dispatched, runs, err)
	}
	reconciler := New(graph, nil, nil).WithOverrunPlanner(1, plan)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if plans != 1 {
		t.Fatalf("planner calls after raise = %d, want one", plans)
	}
	if _, ok, err := graph.Node("oversized-x1"); err != nil || !ok {
		t.Fatalf("deferred repair missing after raise: ok=%t err=%v", ok, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	pending, err = graph.PendingOverruns(0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("rebuilt pending overruns = %+v err=%v, want none", pending, err)
	}
}

// The split counter replaces its suffix rather than stacking markers, so every
// round of one node's continuation is a sibling in the same namespace. A reader
// following the chain has to walk it in round order and — when it starts from a
// piece that itself split — never walk backwards into the round it is standing
// on.
func TestSplitContinuationWalksTheNamespaceForward(t *testing.T) {
	graph := openStore(t)
	stamp := "ran out mid-thought.\n\n[" + OverrunContinuationMessage(1) + "]"
	provenance := store.Provenance{Origin: store.OriginUser, SessionID: "split", Intent: "do the job"}
	for _, spec := range []store.NodeSpec{
		{ID: "job", Title: "Job", Brief: "do the job", Stage: 1},
		{ID: "job-x1", Title: "Round one", Brief: "finish the remainder", Stage: 1},
		{ID: "job-x2", Title: "Round two", Brief: "finish the remainder again", Stage: 1},
	} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{spec}}, provenance); err != nil {
			t.Fatal(err)
		}
	}
	// A part of round one: same namespace, not a round of it.
	if err := graph.Splice("job-x1", store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job-x1-n2", Title: "A part", Brief: "a part of round one", Stage: 1},
	}}, provenance); err != nil {
		t.Fatal(err)
	}
	completeNode := func(id, summary string) store.Node {
		t.Helper()
		claim, won, err := graph.Claim(id, "worker")
		if err != nil || !won {
			t.Fatalf("claim %s won=%t err=%v", id, won, err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatal(err)
		}
		if err := graph.Complete(claim, summary); err != nil {
			t.Fatal(err)
		}
		node, _, err := graph.Node(id)
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	completeNode("job-x1-n2", "the part landed")
	root := completeNode("job", stamp)
	piece := completeNode("job-x1", stamp)

	chain, continued := SplitContinuation(graph, root)
	if !continued || len(chain) != 2 || chain[0].ID != "job-x1" || chain[1].ID != "job-x2" {
		t.Fatalf("chain from the root = %+v continued=%t", chain, continued)
	}
	// A part of round one shares the namespace but is not a round of it.
	fromPiece, continued := SplitContinuation(graph, piece)
	if !continued || len(fromPiece) != 1 || fromPiece[0].ID != "job-x2" {
		t.Fatalf("chain from job-x1 = %+v continued=%t", fromPiece, continued)
	}
	if _, continued := SplitContinuation(graph, completeNode("job-x2", "finished properly")); continued {
		t.Fatal("an unstamped landing claimed a continuation")
	}
}

// The §6 defect on the remainder path: a replan that re-derives its criterion
// writes a new one, and a new one written from a partial result is aimed at the
// work that happened rather than at the work that was asked for. The criterion
// travels instead — verbatim, and saying so of itself.
func TestOverrunGoalCarriesTheOriginalCriterion(t *testing.T) {
	spec := plan.Spec{
		Instruction: "write the module the request named",
		Method:      "read what exists before writing anything",
		Done: plan.Done{
			Produces: []string{"the named module file"},
			Conditions: []plan.Check{
				{Kind: plan.CheckRun, Check: "the stated build command", Expect: "it completes with no error"},
				{Kind: plan.CheckRead, Check: "the named constructor", Expect: "it is present and exported"},
			},
		},
	}
	node := store.Node{ID: "task-2", Brief: "write the module the request named", Spec: EncodeSpec(spec)}

	goal := OverrunGoal(node, "half of it exists", nil, "", "")
	for _, want := range []string{
		SpecUnchangedNotice,
		"the named module file",
		"the stated build command",
		"it completes with no error",
		"the named constructor",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("the replan goal dropped %q:\n%s", want, goal)
		}
	}

	// And a node with no criterion phrases the goal exactly as it always did.
	bare := store.Node{ID: "task-2", Brief: "write the module the request named"}
	if before, after := OverrunGoal(bare, "half of it exists", nil, "", ""), OverrunGoal(bare, "half of it exists", nil, "", ""); before != after {
		t.Fatal("the goal is not deterministic")
	}
	if strings.Contains(OverrunGoal(bare, "half of it exists", nil, "", ""), SpecUnchangedNotice) {
		t.Fatal("a criterion notice appeared for a node that has no criterion")
	}
}

// The object has to make the round trip through the store's opaque bytes, or
// the criterion is only ever as durable as the process that authored it.
func TestSpecEncodesAndDecodesThroughTheStore(t *testing.T) {
	spec := plan.Spec{Instruction: "do it", Done: plan.Done{Produces: []string{"a result"}}}
	if restored := DecodeSpec(EncodeSpec(spec)); restored.Render(0) != spec.Render(0) {
		t.Fatalf("round trip changed the spec:\n%s\n---\n%s", spec.Render(0), restored.Render(0))
	}
	if encoded := EncodeSpec(plan.Spec{}); encoded != nil {
		t.Fatalf("an empty spec encoded to %q, want nothing at all", encoded)
	}
	if restored := DecodeSpec([]byte("not json")); !restored.Empty() {
		t.Fatalf("unreadable bytes decoded to %+v, want the empty spec", restored)
	}
}

// THE NESTING WRAPPER. A remainder's goal is composed from the original
// assignment plus the current finding, and the node a remainder plans carries
// the whole composed goal as its brief (plan.Build's undivided shortcut writes
// Brief: goal). So a second round wrapped a first round's text: measured on the
// canary, round two's goal read "Finish work… The original assignment: Finish
// work… The original assignment: Implement issue #4031 …" at 28,468 characters
// with the issue text twice over and the current finding buried at the end —
// and the spine planned the issue again instead of the finding.
func TestARemainderOfARemainderCarriesOneWrapperAndTheOriginalAssignment(t *testing.T) {
	const original = "Implement issue #4031: parse the unit prefixes and add the tests for them."

	first := OverrunGoal(store.Node{ID: "job-1", Brief: original}, "half of it exists", nil,
		"the prefixes are parsed but nothing exercises them", "")
	// The node the first round planned is handed the goal it was planned from,
	// which is exactly how the nesting happened.
	second := OverrunGoal(store.Node{ID: "job-1-x1", Brief: first}, "the tests are written", nil,
		"2 unexercised behaviours", "")

	if got := strings.Count(second, OverrunPreamble); got != 1 {
		t.Fatalf("a remainder of a remainder carries %d wrappers, want 1:\n%s", got, second)
	}
	if got := strings.Count(second, original); got != 1 {
		t.Fatalf("the original assignment appears %d times, want 1:\n%s", got, second)
	}
	if !strings.HasPrefix(second, OverrunPreamble+original) {
		t.Fatalf("the second round did not open on the original assignment:\n%.400s", second)
	}
	// And the finding for THIS round is the one that travels; the previous
	// round's is gone with the wrapper that carried it.
	if !strings.Contains(second, "2 unexercised behaviours") {
		t.Fatal("the current round's finding was lost with the unwrap")
	}
	if strings.Contains(second, "the prefixes are parsed but nothing exercises them") {
		t.Fatalf("the previous round's finding was carried into the next round:\n%s", second)
	}

	// The cooperative twin unwraps the same way and through the same function:
	// a division asked for on a node a remainder already planned is wrapped
	// once, not twice.
	divided := CooperativeGoal(store.Node{ID: "job-1-x1", Brief: first}, twoParts(), "nothing yet")
	if strings.Contains(divided, OverrunPreamble) {
		t.Fatalf("a cooperative division carried the remainder's wrapper:\n%s", divided)
	}
	if got := strings.Count(divided, CooperativePreamble); got != 1 {
		t.Fatalf("a cooperative division carries %d wrappers, want 1", got)
	}
	if !strings.HasPrefix(divided, CooperativePreamble+original) {
		t.Fatalf("the division did not open on the original assignment:\n%.400s", divided)
	}
}

// AND THE SIZE OF A REMAINDER GOAL IS THE SIZE OF ITS FINDING, NOT OF ITS
// ROUNDS. Every prompt of a round grows with the goal; the round-two gate call
// on the canary cost 116,674 prompt tokens because the goal it was composed
// from had swallowed the round before it. Three rounds of the same shape are
// the same length.
func TestARemainderGoalDoesNotGrowWithItsRounds(t *testing.T) {
	const original = "Implement issue #4031: parse the unit prefixes and add the tests for them."
	// Three findings of the SAME length, so any growth measured below is the
	// composition's and not the finding's.
	findings := []string{
		"1 unexercised behaviour: prefix parsing",
		"2 unexercised behaviours: totals, dates",
		"1 failing check: tests/test_units.py::milli",
	}

	first := OverrunGoal(store.Node{ID: "job-1", Brief: original}, "half of it", nil, findings[0], "")
	second := OverrunGoal(store.Node{ID: "job-1-x1", Brief: first}, "half of it", nil, findings[1], "")
	third := OverrunGoal(store.Node{ID: "job-1-x2", Brief: second}, "half of it", nil, findings[2], "")

	// The whole of the allowance is the finding this round carries. A round that
	// nested would be longer than its predecessor by the entire assignment.
	for round, goal := range []string{first, second, third} {
		grown := len(goal) - len(first)
		if allowed := len(findings[round]) - len(findings[0]); grown > allowed {
			t.Fatalf("round %d grew by %d characters over round one, allowed %d — the goal is nesting:\n%.600s",
				round+1, grown, allowed, goal)
		}
	}
}

// The section table is the one place that says where an assignment stops, and
// a block added to either wrapper without a header here would stay inside the
// next round's "original assignment" — the same defect, one section at a time.
// So the fully loaded goal is composed and unwrapped, and what comes back is
// the brief exactly.
func TestEverySectionOfARemainderGoalEndsTheAssignment(t *testing.T) {
	const original = "Implement issue #4031: parse the unit prefixes.\n\nIt has blank lines of its own."
	node := store.Node{
		ID:    "job-1",
		Brief: original,
		Spec: EncodeSpec(plan.Spec{Done: plan.Done{
			Produces:   []string{"the parser"},
			Conditions: []plan.Check{{Kind: plan.CheckRun, Check: "pytest", Expect: "it passes"}},
		}}),
	}
	loaded := overrunGoal(node, "a partial result", []string{"/jobs/x/a.md"},
		"what the reviewer found", "what the leaf did", "the attempt's own turns",
		OpenFindings{Unexercised: []string{"prefix parsing"}, Gap: "still short"},
		"/jobs/x/record.md")

	if got := originalAssignment(loaded); got != original {
		t.Fatalf("unwrapping a fully loaded remainder goal gave:\n%q\nwant:\n%q", got, original)
	}
	// Every header in the table is a header the wrappers can actually write, and
	// the loaded goal above is where that is checked: a stale entry is dead
	// weight in the one place that must not go stale.
	for _, section := range remainderSections {
		if section == CooperativeFindingHeader {
			continue
		}
		if !strings.Contains(loaded, "\n\n"+section) {
			t.Fatalf("remainderSections names a block the remainder wrapper never writes: %.60s…", section)
		}
	}

	// A brief that never went through either wrapper comes back untouched, which
	// is every fresh job.
	if got := originalAssignment(original); got != original {
		t.Fatalf("a fresh brief was rewritten by the unwrap: %q", got)
	}
}
