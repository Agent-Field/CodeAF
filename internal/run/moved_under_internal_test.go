package run

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// WHAT A SIBLING DOES IS NOT A WORKER'S PROGRESS, AND WHAT HAPPENS UNDER ITS
// OWN TASK IS. The same-action law reads movement through [movedUnder], and
// this pins its three answers on a real store with no worker and no clock of
// its own: a note on a sibling moves the store and not this worker's part of
// it; a note on the worker's own task does; a note on a row under it does, at
// any depth. The whole-store reading this replaced kept a broken worker alive
// for as long as the rest of the run went on working.
func TestOnlyAMoveUnderAWorkersOwnTaskCountsAsItsProgress(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", "The run", "drive")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "mine", Title: "this worker's task", ParentID: "root"},
		{ID: "theirs", Title: "a sibling's task", ParentID: "root"},
		{ID: "under", Title: "a part of mine", ParentID: "mine"},
		{ID: "deeper", Title: "a part of that part", ParentID: "under"},
	}); err != nil {
		t.Fatalf("add the tasks: %v", err)
	}

	if !movedUnder(store, "mine", time.Time{}) {
		t.Fatal("the first look of a turn has nothing to compare with and must answer moved")
	}
	for _, look := range []struct {
		note string
		want bool
		why  string
	}{
		{"theirs", false, "a sibling's row moved, which is not this worker's progress"},
		{"root", false, "the row above moved, which is not under this worker's task"},
		{"mine", true, "the worker's own row moved"},
		{"under", true, "a row under the worker's task moved"},
		{"deeper", true, "a row two levels under the worker's task moved"},
	} {
		since := time.Now()
		if _, err := store.AddNote(look.note, "somebody", "tick"); err != nil {
			t.Fatalf("note %s: %v", look.note, err)
		}
		if got := movedUnder(store, "mine", since); got != look.want {
			t.Errorf("after a note on %q movedUnder = %v, want %v: %s", look.note, got, look.want, look.why)
		}
	}
}
