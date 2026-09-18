package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plan"
	"github.com/Agent-Field/codeaf/internal/resident"
	"github.com/Agent-Field/codeaf/internal/store"
)

// countingPlanClient is the planner seam under observation: a remainder that
// reuses the plan it already has never reaches it, and one that re-plans from
// scratch always does.
type countingPlanClient struct {
	model string
	calls int
}

func (c *countingPlanClient) Model() string { return c.model }

func (c *countingPlanClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	return nil, errors.New("the planner was asked to draw a remainder the job already had a plan for")
}

// NOTHING TO CONTINUE: the two facts the predicate needs, checked apart. A
// remainder continues only when the lineage has recorded turns AND the job
// still has the plan it drew; either one missing is the full re-plan's ground.
func TestContinueRemainderNeedsBothTurnsAndAPlan(t *testing.T) {
	graph := openContinuationJob(t)
	anchor := resident.PlanAnchor{NodeID: "task-7", SessionID: "s1"}
	withPlan := &jobPlans{graphs: map[string]plannedJob{"task-7": {graph: retainedPlan(), root: "task-7"}}}
	noPlan := &jobPlans{graphs: map[string]plannedJob{}}

	// Recorded turns and the plan: this is a continuation.
	if _, ok := continueRemainder(graph, withPlan, anchor, true, "task-7-n1-x1", "finish it"); !ok {
		t.Fatal("recorded turns and an existing plan were not read as a continuation")
	}
	// The plan, but no recorded work: nothing has happened to continue.
	if _, ok := continueRemainder(noTurnsStore(t), noPlan, anchor, true, "task-7-n1-x1", "finish it"); ok {
		t.Fatal("a job with no recorded turns claimed a continuation")
	}
	// Recorded work, but no plan to continue: the full re-plan's ground.
	if _, ok := continueRemainder(graph, noPlan, anchor, true, "task-7-n1-x1", "finish it"); ok {
		t.Fatal("a job with no plan claimed a continuation")
	}
	// No anchor at all: nothing to look a plan up by.
	if _, ok := continueRemainder(graph, withPlan, resident.PlanAnchor{}, false, "t1", "finish it"); ok {
		t.Fatal("a remainder with no anchor claimed a continuation")
	}
}

// THE PROMISE: an overrun continuation of work that already has recorded turns
// and a plan continues it. It does not buy a fresh full planning pass, does not
// re-emit the planner's phases on the stream, and does not drop the plan.
func TestAContinuationContinuesItsPlanWithoutARepass(t *testing.T) {
	graph := openContinuationJob(t)
	node, claimed, err := graph.Claim("task-7-n1", "worker-1")
	if err != nil || !claimed {
		t.Fatalf("claim: %v (claimed=%v)", err, claimed)
	}
	if err := graph.Start(node); err != nil {
		t.Fatal(err)
	}
	leaf, _, err := graph.Node("task-7-n1")
	if err != nil {
		t.Fatal(err)
	}

	plans := &jobPlans{graphs: map[string]plannedJob{"task-7": {graph: retainedPlan(), root: "task-7"}}}
	planner := &countingPlanClient{model: "plan/model"}
	planClient := adoptLiveClient(config.Config{}, "plan/model", planner)
	workClient := adoptLiveClient(config.Config{}, "work/model", &countingPlanClient{model: "work/model"})
	settings := config.Config{NodeBudget: 40, MaxDepth: 2}

	spliced, sink, err := resident.ReplanOverrun(context.Background(), graph, leaf,
		"half of it exists", "", []string{"module.go"}, 20,
		replanRemainder(settings, planClient, workClient, plans, graph, ""))
	if err != nil {
		t.Fatalf("overrun: %v", err)
	}
	if spliced == 0 || sink == "" {
		t.Fatalf("the continuation spliced nothing: spliced=%d sink=%q", spliced, sink)
	}
	if planner.calls != 0 {
		t.Fatalf("the continuation ran a full plan.Build %d time(s)", planner.calls)
	}
	for _, message := range storeMessages(t, graph, "s1") {
		for _, phase := range []string{"reading the request", "choosing the shape", "breaking it into steps"} {
			if strings.Contains(message, phase) {
				t.Fatalf("the continuation re-emitted a planner phase %q in the record:\n%s", phase, message)
			}
		}
	}
	if entry, ok := plans.get("task-7"); !ok || entry.graph == nil {
		t.Fatal("the continuation discarded the plan it was continuing")
	}
}

// openContinuationJob is a job with a lineage that has recorded turns to
// continue from: one leaf that ran, under the bare job root the plan is keyed
// by.
func openContinuationJob(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-7", Brief: "the whole job"},
		{ID: "task-7-n1", Parent: "task-7", Brief: "the first step"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "the whole job"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTranscript("task-7-n1", "work/model", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptNote, Text: "read the module"},
		{Turn: 2, Kind: store.TranscriptNote, Text: "wrote half of it"},
	}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// noTurnsStore is the same job with nothing recorded yet: a cold start.
func noTurnsStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "cold.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-7", Brief: "the whole job"},
		{ID: "task-7-n1", Parent: "task-7", Brief: "the first step"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "the whole job"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// retainedPlan is a job's plan with one step landed and two still to run — the
// remaining steps a continuation has to carry.
func retainedPlan() *plan.Graph {
	document := &plan.Graph{Goal: "the whole job", NextID: 1}
	document.Add(plan.Node{Title: "Gather the parts", Kind: plan.KindWork})
	document.Add(plan.Node{Title: "Write the module", Kind: plan.KindWork})
	document.Add(plan.Node{Title: "Run its tests", Kind: plan.KindWork})
	document.Nodes[0].State = plan.StateDone
	return document
}

func storeMessages(t *testing.T, graph *store.Store, session string) []string {
	t.Helper()
	messages, err := graph.Messages(session, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 0, len(messages))
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	return bodies
}
