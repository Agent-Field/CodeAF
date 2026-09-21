package plandb

import (
	"path/filepath"
	"testing"
)

// THE SEAT OF THE FIRST PASS IS THE THING THIS TESTS, NOT THE FIELD.
//
// Asserting that the root carries RolePlan would pass on a store that set the
// field and a [roleOf] that ignored it. What matters is the answer the run
// engine asks for — the role of the root BEFORE anything has been split —
// because that is what internal/run turns into a model tier, and it is the
// answer that was wrong.
func TestTheUnsplitRootPlansOnThePlanRole(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "plan.db"), "a project", "t-root", "a job with parts", "do the job")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	root := store.Task(store.RootID())
	if root == nil {
		t.Fatal("the store opened without a root")
	}
	if root.Composite {
		t.Fatal("a freshly seeded root is not composite: the case this test exists for is the one BEFORE any split")
	}
	if got := roleOf(root); got != RolePlan {
		t.Fatalf("the first pass over an unsplit root takes role %q, want %q — this is the pass that decides the split, and %q seats it on the worker tier", got, RolePlan, got)
	}
}

// And the answer must not change once the root HAS been split, because the
// composite branch of [roleOf] already answered RolePlan there. A change that
// moved the root's later passes would be a different change from this one.
func TestASplitRootStillPlans(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "plan.db"), "a project", "t-root", "a job with parts", "do the job")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.AddMany([]TaskSpec{
		{ID: store.NextID(), Title: "first part", Description: "the first part"},
		{ID: store.NextID(), Title: "second part", Description: "the second part"},
	}); err != nil {
		t.Fatalf("split: %v", err)
	}
	root := store.Task(store.RootID())
	if !root.Composite {
		t.Fatal("the root did not become composite when it was split")
	}
	if got := roleOf(root); got != RolePlan {
		t.Fatalf("a split root takes role %q, want %q", got, RolePlan)
	}
}

// A CHILD IS STILL WORK. The seeded role belongs to the root alone; if it
// reached the parts, every leaf would be seated on the thinking tier and the
// change would be a cost increase wearing a planning argument.
func TestThePartsOfASplitAreStillWork(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "plan.db"), "a project", "t-root", "a job with parts", "do the job")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	added, err := store.AddMany([]TaskSpec{
		{ID: store.NextID(), Title: "first part", Description: "the first part"},
		{ID: store.NextID(), Title: "second part", Description: "the second part"},
	})
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("split added %d parts, want 2", len(added))
	}
	for _, child := range added {
		if child == nil {
			t.Fatal("the store answered a nil part for a split it accepted")
		}
		if got := roleOf(child); got != RoleWork {
			t.Fatalf("part %s takes role %q, want %q — the seeded role is the root's alone", child.ID, got, RoleWork)
		}
	}
}
