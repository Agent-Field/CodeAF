package plandb

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

// A READ OF AN UNCHANGED PLAN DOES NOT LOAD IT AGAIN. Every read used to
// reload the whole plan from the database, so listing a run with hundreds of
// tasks one task at a time read the plan hundreds of times over: gigabytes of
// allocation for one list. A read now loads when the database has changed, and
// only then.
func TestRepeatedReadsOfAnUnchangedPlanLoadItOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	store, err := Open(path, "program", "1", "root", "the root")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const tasks = 200
	specs := make([]TaskSpec, tasks)
	for i := range specs {
		specs[i] = TaskSpec{ID: fmt.Sprintf("s%d", i), Title: fmt.Sprintf("task %d", i), Description: "a step with some words in it to give the row a size", ParentID: "1"}
	}
	added, err := store.AddMany(specs)
	if err != nil {
		t.Fatal(err)
	}

	// One read to settle whatever the first load costs.
	if _, err := store.Show(added[0].ID); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, task := range added {
		if _, err := store.Show(task.ID); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	t.Logf("%d unchanged plan reads allocated %d bytes", tasks, after.TotalAlloc-before.TotalAlloc)
	const budget = 3 << 20
	if spent := after.TotalAlloc - before.TotalAlloc; spent > budget {
		t.Fatalf("%d reads of an unchanged plan allocated %d bytes, want under %d", tasks, spent, budget)
	}
}

// A WRITE BY ANOTHER HANDLE IS STILL SEEN BY THE NEXT READ, which is the whole
// reason a read refreshes at all.
func TestAReadSeesAnotherHandlesWriteAfterAnUnchangedRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	reader, err := Open(path, "program", "1", "root", "the root")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.Show("1"); err != nil {
		t.Fatal(err)
	}
	writer, err := Open(path, "program", "1", "root", "the root")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	added, err := writer.AddMany([]TaskSpec{{ID: "later", Title: "later", Description: "added by the other handle", ParentID: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Show(added[0].ID); err != nil {
		t.Fatalf("a task another handle committed was not seen: %v", err)
	}
	if _, err := writer.AddNote(added[0].ID, "agent", "a note"); err != nil {
		t.Fatal(err)
	}
	if notes := reader.Notes(added[0].ID, 10); len(notes) != 1 {
		t.Fatalf("reader saw %d notes after another handle's note, want 1", len(notes))
	}
}
