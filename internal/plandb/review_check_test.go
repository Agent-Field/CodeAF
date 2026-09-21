package plandb

import (
	"path/filepath"
	"testing"
)

// A REVIEW IS SEATED BENEATH ITS PARENT WHATEVER THE PARENT WROTE ABOUT ITSELF
// MEANWHILE, and every done task above it goes back to waiting on it with the
// result it earned. The tree here is the one a loaded run produced: a task still
// in its turn finished the moment its child's row read done, the root above it
// did the same, and only then did the child's worker come home to be reviewed.
func TestAReviewCheckReopensEveryDoneAncestorAndKeepsWhatEachEarned(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "plan.db"), "review-check", "root", "the run", "finish over a child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID := store.RootID()
	must := func(_ *Task, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AddMany([]TaskSpec{{ID: "mid", Title: "the task between", ParentID: rootID}}); err != nil {
		t.Fatal(err)
	}
	must(store.Claim("mid", "mid", "owner"))
	if _, err := store.AddMany([]TaskSpec{{ID: "leaf", Title: "the leaf", ParentID: "mid"}}); err != nil {
		t.Fatal(err)
	}
	must(store.Claim("leaf", "leaf", "owner"))
	must(store.Done("leaf", "leaf", "the leaf's word", nil, nil))
	must(store.Done("mid", "mid", "the middle's word", nil, nil))
	must(store.Done(rootID, rootID, "the root's word", nil, nil))

	// NOTHING BUT A CHECK COMES IN THIS WAY, and a check needs a parent that is
	// there.
	if _, err := store.AddReviewCheck(TaskSpec{ID: "extra", Title: "more work", ParentID: "mid"}); err == nil {
		t.Fatal("a child that is not a check was seated beneath a done task")
	}
	if _, err := store.AddReviewCheck(TaskSpec{ID: "lost", Title: "check: nothing", ParentID: "nowhere", Role: RoleCheck}); err == nil {
		t.Fatal("a check was seated beneath a parent that does not exist")
	}
	for _, id := range []string{"mid", rootID} {
		if got := store.Task(id).Status; got != StatusDone {
			t.Fatalf("%s = %s after two refusals, want it still done", id, got)
		}
	}

	must(store.AddReviewCheck(TaskSpec{ID: "chk", Title: "check: the leaf", ParentID: "mid", Role: RoleCheck}))
	for id, word := range map[string]string{"mid": "the middle's word", rootID: "the root's word"} {
		task := store.Task(id)
		if terminal(task.Status) || !task.Composite || !task.CompletedAt.IsZero() {
			t.Fatalf("%s = %s composite=%v completed=%v, want it open again over the check", id, task.Status, task.Composite, task.CompletedAt)
		}
		if task.Result != word {
			t.Fatalf("%s result = %q, want the %q it earned kept", id, task.Result, word)
		}
	}
	if got := store.Task("leaf").Status; got != StatusDone {
		t.Fatalf("leaf = %s, want the reviewed work itself left done", got)
	}

	// THE CHECK LANDS AND THE TASK BETWEEN CLOSES BY ITSELF, with its own word;
	// the root is the run's to complete.
	must(store.Claim("chk", "chk", "owner"))
	must(store.Done("chk", "chk", "holds: reviewed", nil, nil))
	if mid := store.Task("mid"); mid.Status != StatusDone || mid.Result != "the middle's word" {
		t.Fatalf("mid = %s %q, want done again with its own word", mid.Status, mid.Result)
	}
	if got := store.Task(rootID).Status; got == StatusDone {
		t.Fatal("the root closed by itself; its completion is the run's to write")
	}
	if err := store.CompleteRoot("the root's word"); err != nil {
		t.Fatal(err)
	}
	if root := store.Task(rootID); root.Status != StatusDone || root.Result != "the root's word" {
		t.Fatalf("root = %s %q, want done with its own word", root.Status, root.Result)
	}
}
