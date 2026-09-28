package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// TestARunSetAsideAsInterruptedReadsInterruptedOnItsPage: a run nothing was
// driving is set aside when the next request arrives, and its rows are read as
// they stand for good. They read `interrupted` — not running, which nothing will
// ever move, and not incomplete or stopped, which nothing decided.
func TestARunSetAsideAsInterruptedReadsInterruptedOnItsPage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	left, err := plandb.Open(path, "first", "71", "the first task", "FIRST BRIEF")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := left.AddMany([]plandb.TaskSpec{{ID: "part", Title: "a part", ParentID: "71"}}); err != nil {
		t.Fatalf("add a part: %v", err)
	}
	_ = left.Close()

	if err := setAsideRunStore(path); err != nil {
		t.Fatalf("set aside: %v", err)
	}
	archived, err := plandb.Open(path+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("open the set-aside store: %v", err)
	}
	defer archived.Close()
	for _, id := range []string{"71", "part"} {
		row := planTaskRow(archived, dir, archived.Task(id), nil, nil)
		if !row.Interrupted || row.Stopped {
			t.Fatalf("row %s of a run set aside reads %+v, want interrupted and not stopped", id, row)
		}
	}
	// A STORE WHOSE RUN HAD ENDED IS MOVED AS IT ENDED.
	done, err := plandb.Open(path, "second", "72", "the second task", "SECOND BRIEF")
	if err != nil {
		t.Fatalf("seed the second: %v", err)
	}
	if err := done.CompleteRoot("finished"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	_ = done.Close()
	if err := setAsideRunStore(path); err != nil {
		t.Fatalf("set aside the second: %v", err)
	}
	second, err := plandb.Open(path+".2", "", "", "", "")
	if err != nil {
		t.Fatalf("open the second set-aside store: %v", err)
	}
	defer second.Close()
	if root := second.Task("72"); root.Status != plandb.StatusDone || root.Error != "" {
		t.Fatalf("a finished run was rewritten on the way aside: %s %q", root.Status, root.Error)
	}
}
