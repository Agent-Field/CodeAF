package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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
	spliced, sink, err = ReplanOverrun(context.Background(), graph, repair, "more partial", "the docker half is missing", nil, 20, planned)
	if err != nil || spliced != 2 || sink != "job-a-x2-n9" {
		t.Fatalf("round two: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	repair, _, _ = graph.Node("job-a-x2-n5")
	spliced, sink, err = ReplanOverrun(context.Background(), graph, repair, "last partial", "", nil, 20, planned)
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
	messages, err := graph.Messages("s1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	noticed := false
	for _, message := range messages {
		if strings.Contains(message.Body, "split as many times") {
			noticed = true
		}
	}
	if !noticed {
		t.Fatalf("round cap left no receipt in the thread")
	}
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
	messages, err := graph.Messages("s9", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	noticed := false
	for _, message := range messages {
		if strings.Contains(message.Body, "grown as large") {
			noticed = true
		}
	}
	if !noticed {
		t.Fatalf("job ceiling left no receipt in the thread")
	}
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
