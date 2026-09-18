package plandb

// Tests for the finish check: CanFinish's two refusals and its pass, and Done
// answering with the reason CanFinish gave.

import (
	"strings"
	"testing"
)

// CanFinish refuses a task whose child has not finished or whose hard
// dependency is not done, naming the first task in the way, and Done refuses
// with that same reason.
func TestPlandbCliCanFinishGatesDone(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store,
		TaskSpec{ID: "p", Title: "Parent"},
		TaskSpec{ID: "kid", Title: "Child", ParentID: "p"},
	)
	// A live child blocks its parent, and the reason names it.
	if ok, reason := store.CanFinish("p"); ok || !strings.Contains(reason, `"kid"`) {
		t.Fatalf("CanFinish with a live child = %v %q, want false naming kid", ok, reason)
	}
	// The leaf itself has nothing in its way.
	if ok, reason := store.CanFinish("kid"); !ok || reason != "" {
		t.Fatalf("CanFinish on the leaf = %v %q, want true and no reason", ok, reason)
	}
	// Finishing the child clears the parent's child block.
	planFinish(t, store, "kid", "w", "done")
	if ok, reason := store.CanFinish("p"); !ok || reason != "" {
		t.Fatalf("CanFinish after the child finished = %v %q, want true", ok, reason)
	}

	// A running task that gains an unfinished hard dependency is refused, and
	// the reason names the dependency.
	planAdd(t, store, planSpec("later", "Later"))
	if _, err := store.Claim("later", "w"); err != nil {
		t.Fatalf("claim later: %v", err)
	}
	planAdd(t, store, planSpec("upstream", "Upstream"))
	if _, err := store.AddDep("later", "upstream", DepFeedsInto); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	if ok, reason := store.CanFinish("later"); ok || !strings.Contains(reason, `"upstream"`) {
		t.Fatalf("CanFinish behind an unfinished dep = %v %q, want false naming upstream", ok, reason)
	}
	// Done refuses with that reason while the dependency is unfinished.
	if _, err := store.Done("later", "w", "trying anyway", nil, nil); err == nil || !strings.Contains(err.Error(), `"upstream"`) {
		t.Fatalf("Done behind an unfinished dep = %v, want the CanFinish reason", err)
	}
	// A `suggests` edge is advice, not a gate: it does not block a finish.
	if _, err := store.AddDep("later", "p", DepSuggests); err != nil {
		t.Fatalf("add suggests dep: %v", err)
	}
	if ok, _ := store.CanFinish("later"); ok {
		t.Fatal("CanFinish passed while a hard dependency was unfinished")
	}
	// Finishing the dependency unblocks it, and then the finish passes.
	planFinish(t, store, "upstream", "w", "done")
	if ok, reason := store.CanFinish("later"); !ok || reason != "" {
		t.Fatalf("CanFinish after the dep finished = %v %q, want true", ok, reason)
	}
	if _, err := store.Done("later", "w", "finished at last", nil, nil); err != nil {
		t.Fatalf("Done after the dep finished: %v", err)
	}
	// The unknown-id miss is the store's one plain sentence.
	if ok, reason := store.CanFinish("ghost"); ok || !strings.Contains(reason, "not found") {
		t.Fatalf("CanFinish on an unknown task = %v %q, want false and not found", ok, reason)
	}
}
