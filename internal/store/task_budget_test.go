package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// spliceTaskTree is the shape every task-ceiling test needs: a job root with a
// child that itself has a leaf, so "nearest governed ancestor" has more than
// one answer to choose between.
func spliceTaskTree(t *testing.T, graph *Store) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "deliver the thing", Stage: 3},
		{ID: "stage", Parent: "job", Brief: "one stage of it", Stage: 2},
		{ID: "leaf", Parent: "stage", Brief: "the actual work", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "task-session", Intent: "measure a task"}); err != nil {
		t.Fatalf("splice task tree: %v", err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "other-job", Brief: "an unrelated job", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "task-session", Intent: "another task"}); err != nil {
		t.Fatalf("splice sibling job: %v", err)
	}
}

// TestTaskRailIsSilentWithoutACeiling is the default-path contract: no ceiling
// has been set, so nothing about the gate is observable — no stop, no question,
// no governed root — for every node in the graph.
func TestTaskRailIsSilentWithoutACeiling(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "ungoverned.db"))
	spliceTaskTree(t, graph)
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", Cost: 99}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{RootID, "job", "stage", "leaf", "other-job"} {
		rail, posted, err := graph.PauseTaskRail(id, "task-session", 0)
		if err != nil {
			t.Fatalf("pause %s: %v", id, err)
		}
		if rail.Set || rail.Reached || posted || rail.Root != "" {
			t.Fatalf("ungoverned pause on %s = %+v posted=%t, want the zero rail", id, rail, posted)
		}
	}
	messages, err := graph.Messages("task-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("ungoverned graph posted %d messages, want none", len(messages))
	}
}

// TestPauseTaskRailStopsOnlyItsOwnSubtree covers the whole failure mode: the
// governed task trips, asks once, keeps refusing while it waits, and a raise
// re-arms both the work and the question — while a sibling job never notices.
func TestPauseTaskRailStopsOnlyItsOwnSubtree(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "governed.db"))
	spliceTaskTree(t, graph)
	if err := graph.SetTaskCeiling("job", 1, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", Cost: 0.40}); err != nil {
		t.Fatal(err)
	}

	// Under the ceiling the governed task runs exactly like an ungoverned one.
	rail, posted, err := graph.PauseTaskRail("leaf", "task-session", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !rail.Set || rail.Root != "job" || rail.Reached || posted {
		t.Fatalf("under ceiling = %+v posted=%t", rail, posted)
	}

	if err := graph.RecordUsage(NodeUsage{NodeID: "stage", Cost: 0.70}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		rail, posted, err := graph.PauseTaskRail("leaf", "task-session", 0)
		if err != nil {
			t.Fatal(err)
		}
		if !rail.Reached || posted != (attempt == 0) {
			t.Fatalf("pause %d = %+v posted=%t, want reached with one question", attempt, rail, posted)
		}
	}
	messages, err := graph.Messages("task-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "Task budget reached -- job has spent $1.10 of $1.00. " +
		"Say the word and I'll continue (raises this task's ceiling by $1.10)."
	if len(messages) != 1 || messages[0].Role != RoleAgent || messages[0].Body != want {
		t.Fatalf("task rail questions = %+v, want exactly %q", messages, want)
	}

	// The stop is scoped: the sibling job is ungoverned and unaffected.
	if rail, posted, err := graph.PauseTaskRail("other-job", "task-session", 0); err != nil ||
		rail.Set || rail.Reached || posted {
		t.Fatalf("sibling job = %+v posted=%t err=%v, want untouched", rail, posted, err)
	}

	// A raise re-arms the work and the question together.
	if err := graph.RaiseTaskCeiling("job", 1.10, "test:user"); err != nil {
		t.Fatal(err)
	}
	if rail, posted, err := graph.PauseTaskRail("leaf", "task-session", 0); err != nil ||
		rail.Reached || posted || rail.Ceiling != 2.10 {
		t.Fatalf("after raise = %+v posted=%t err=%v, want clear at $2.10", rail, posted, err)
	}
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", Cost: 1.00}); err != nil {
		t.Fatal(err)
	}
	if _, posted, err := graph.PauseTaskRail("leaf", "task-session", 0); err != nil || !posted {
		t.Fatalf("question after raise posted=%t err=%v, want asked again", posted, err)
	}
}

// TestTaskRailCountsPendingSpendWithoutPromisingIt mirrors the daily rail's
// honesty contract: the gate decides on cost that has not been journaled, and
// the sentence quotes only what a later consent can actually raise against.
func TestTaskRailCountsPendingSpendWithoutPromisingIt(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "pending.db"))
	spliceTaskTree(t, graph)
	if err := graph.SetTaskCeiling("job", 1, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", Cost: 0.80}); err != nil {
		t.Fatal(err)
	}
	rail, posted, err := graph.PauseTaskRail("leaf", "task-session", 0.30)
	if err != nil {
		t.Fatal(err)
	}
	if !posted || !rail.Reached || rail.Spend < 1.099 || rail.Spend > 1.101 ||
		rail.Pending < 0.299 || rail.Pending > 0.301 {
		t.Fatalf("pending rail = %+v posted=%t", rail, posted)
	}
	messages, err := graph.Messages("task-session", 0, 0)
	if err != nil || len(messages) != 1 {
		t.Fatalf("pending rail messages = %+v err=%v", messages, err)
	}
	want := "Task budget reached -- job has spent $0.80 of $1.00, and the next step costs $0.30. " +
		"Say the word and I'll continue (raises this task's ceiling by $1.00)."
	if messages[0].Body != want {
		t.Fatalf("pending rail message = %q, want %q", messages[0].Body, want)
	}
}

// TestSubtreeSpendSumsEachRunOnce is the no-double-counting contract: a run is
// one row against one node, so a parent's total is the sum of the rows beneath
// it and a node that ran twice contributes both runs and no more.
func TestSubtreeSpendSumsEachRunOnce(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "subtree-spend.db"))
	spliceTaskTree(t, graph)
	for _, usage := range []NodeUsage{
		{NodeID: "job", Cost: 0.10},
		{NodeID: "stage", Cost: 0.20},
		{NodeID: "leaf", Cost: 0.30},
		{NodeID: "leaf", Cost: 0.05}, // a second attempt at the same node
		{NodeID: "other-job", Cost: 5},
	} {
		if err := graph.RecordUsage(usage); err != nil {
			t.Fatal(err)
		}
	}
	for _, expect := range []struct {
		root string
		want float64
	}{
		{"job", 0.65}, {"stage", 0.55}, {"leaf", 0.35}, {"other-job", 5},
	} {
		got, err := graph.SubtreeSpend(expect.root)
		if err != nil {
			t.Fatalf("subtree spend %s: %v", expect.root, err)
		}
		if got < expect.want-0.0001 || got > expect.want+0.0001 {
			t.Fatalf("subtree spend %s = %.4f, want %.4f", expect.root, got, expect.want)
		}
	}
}

// TestTaskRailUsesTheNearestGovernedAncestor settles which ceiling applies when
// a task is nested inside another, and what happens when the inner one is
// cleared: the outer ceiling resumes governing without a second decision.
func TestTaskRailUsesTheNearestGovernedAncestor(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "nested.db"))
	spliceTaskTree(t, graph)
	if err := graph.SetTaskCeiling("job", 10, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetTaskCeiling("stage", 1, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", Cost: 2}); err != nil {
		t.Fatal(err)
	}
	rail, err := graph.TaskRailFor("leaf")
	if err != nil {
		t.Fatal(err)
	}
	if rail.Root != "stage" || !rail.Reached {
		t.Fatalf("nested rail = %+v, want the stage ceiling reached", rail)
	}
	if err := graph.ClearTaskCeiling("stage", "test:user"); err != nil {
		t.Fatal(err)
	}
	rail, err = graph.TaskRailFor("leaf")
	if err != nil {
		t.Fatal(err)
	}
	if rail.Root != "job" || rail.Reached {
		t.Fatalf("rail after clearing the inner ceiling = %+v, want the job ceiling clear", rail)
	}
	if err := graph.ClearTaskCeiling("stage", "test:user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clearing an unset ceiling = %v, want ErrNotFound", err)
	}
}

// TestTaskCeilingsSurviveRebuild keeps the projection honest: the ceiling is
// journal-native, so replay alone must reproduce both the set and the clear.
func TestTaskCeilingsSurviveRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "ceiling-rebuild.db"))
	spliceTaskTree(t, graph)
	if err := graph.SetTaskCeiling("job", 2, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetTaskCeiling("other-job", 3, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.ClearTaskCeiling("other-job", "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if ceiling, set, err := graph.TaskCeilingOf("job"); err != nil || !set || ceiling != 2 {
		t.Fatalf("job ceiling after rebuild = %.2f set=%t err=%v, want $2.00", ceiling, set, err)
	}
	if _, set, err := graph.TaskCeilingOf("other-job"); err != nil || set {
		t.Fatalf("cleared ceiling after rebuild set=%t err=%v, want gone", set, err)
	}
}

// TestTaskCeilingRefusesNonsense keeps the writers from journaling policy that
// cannot be enforced: an unknown root, a zero or negative ceiling, an anonymous
// decision, or a raise against a task nobody has capped.
func TestTaskCeilingRefusesNonsense(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "invalid-ceiling.db"))
	spliceTaskTree(t, graph)
	if err := graph.SetTaskCeiling("nowhere", 1, "test:user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ceiling on an unknown root = %v, want ErrNotFound", err)
	}
	for _, bad := range []struct {
		ceiling float64
		origin  string
	}{{0, "test:user"}, {-1, "test:user"}, {1, "  "}} {
		if err := graph.SetTaskCeiling("job", bad.ceiling, bad.origin); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ceiling %.2f origin %q = %v, want ErrInvalid", bad.ceiling, bad.origin, err)
		}
	}
	if err := graph.RaiseTaskCeiling("job", 1, "test:user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("raise without a ceiling = %v, want ErrNotFound", err)
	}
	if _, _, err := graph.PauseTaskRail("", "task-session", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pause with no node = %v, want ErrInvalid", err)
	}
	if _, _, err := graph.PauseTaskRail("leaf", "task-session", -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pause with negative additional spend = %v, want ErrInvalid", err)
	}
}
