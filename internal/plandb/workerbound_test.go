package plandb

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// L-LOOP2 C3a, C3b, C3c, C3f: each creation verb obeys the caller's own task.
func TestWorkerCreationStaysUnderItsOwnTask(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"default", []string{"add", "New work", "--as", "new"}, true},
		{"explicit", []string{"add", "New work", "--as", "new", "--parent", "t-own"}, true},
		{"root", []string{"add", "New work", "--parent", "root"}, false},
		{"sibling", []string{"add", "New work", "--parent", "other"}, false},
		{"child", []string{"add", "New work", "--parent", "child"}, false},
		{"split", []string{"split", "other", "--into", "Part one, Part two"}, false},
		{"pivot", []string{"task", "pivot", "other", "--subtasks", "Part one, Part two"}, false},
		{"pivot root", []string{"task", "pivot", "root", "--subtasks", "Part one, Part two"}, false},
		{"insert sibling", []string{"task", "insert", "--after", "other", "--title", "New work"}, false},
		{"insert child", []string{"task", "insert", "--after", "child", "--title", "New work"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s := treeBoundStore(t)
			treeBoundAdd(t, s, TaskSpec{ID: "own", Title: "Own work"}, TaskSpec{ID: "other", Title: "Other work"}, TaskSpec{ID: "child", ParentID: "own", Title: "Existing child"})
			t.Setenv(RunEnv, s.RootID())
			t.Setenv(TaskEnv, "own")
			before := s.Tasks()
			code := h.run(append([]string{"--db", h.db}, tc.args...)...)
			if tc.ok {
				if code != 0 || len(s.Tasks()) != len(before)+1 {
					t.Fatalf("own work refused: %s", h.errb)
				}
				for _, task := range s.Tasks() {
					if task.Title == "New work" && (task.ParentID != "own" || task.AddedByTask != "own") {
						t.Fatalf("wrong parent or author: %+v", task)
					}
				}
			} else if code == 0 || !strings.Contains(h.errb.String(), "a worker adds tasks only under its own task (t-own)") || !reflect.DeepEqual(before, s.Tasks()) {
				t.Fatalf("out-of-scope creation changed the store or lacked its explanation: %d %s", code, h.errb)
			}
		})
	}
	t.Run("unbound", func(t *testing.T) {
		h, s := treeBoundStore(t)
		t.Setenv(TaskEnv, "ignored-without-run")
		h.cliAdd("Person's work", "person")
		if task := s.Task("person"); task.ParentID != s.RootID() || task.AddedByTask != "" {
			t.Fatalf("unbound creation changed: %+v", task)
		}
	})
}

// L-LOOP2 C3d: only stored worker additions consume that worker's five slots.
func TestWorkerAllowanceCountsOnlyItsOwnAdditions(t *testing.T) {
	h, s := treeBoundStore(t)
	treeBoundAdd(t, s, TaskSpec{ID: "own", Title: "Own work"})
	treeBoundAdd(t, s, TaskSpec{ID: "joined", ParentID: "own", Title: "Joined work"}, TaskSpec{ID: "fix", ParentID: "own", Title: "Fix work"})
	if _, err := s.AddReviewCheck(TaskSpec{ID: "review", ParentID: "own", Title: "Review work", Role: RoleCheck}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RunEnv, s.RootID())
	t.Setenv(TaskEnv, "own")
	for i := 0; i < 5; i++ {
		h.cliAdd(fmt.Sprintf("Part %d", i), fmt.Sprintf("part%d", i), "--role", "check")
	}
	before := s.Tasks()
	if code := h.run("--db", h.db, "add", "Sixth part"); code == 0 || !strings.Contains(h.errb.String(), "already added 5 tasks") || !reflect.DeepEqual(before, s.Tasks()) {
		t.Fatalf("sixth addition escaped the allowance: %d %s", code, h.errb)
	}
	if _, err := s.Cancel("part0", "no longer needed"); err != nil {
		t.Fatal(err)
	}
	h.cliAdd("Replacement part", "replacement")
	if task := s.Task("replacement"); task.AddedByTask != "own" {
		t.Fatalf("worker marker did not survive a fresh CLI handle: %+v", task)
	}
}

// L-LOOP2 C3d: a batch cannot partly cross a worker's remaining allowance.
func TestWorkerAllowanceRefusesWholeBatch(t *testing.T) {
	h, s := treeBoundStore(t)
	treeBoundAdd(t, s, TaskSpec{ID: "own", Title: "Own work"})
	t.Setenv(RunEnv, s.RootID())
	t.Setenv(TaskEnv, "own")
	for i := 0; i < 4; i++ {
		h.cliAdd(fmt.Sprintf("Part %d", i), fmt.Sprintf("part%d", i))
	}
	before := s.Tasks()
	if code := h.run("--db", h.db, "split", "own", "--into", "Fifth, Sixth"); code == 0 || !strings.Contains(h.errb.String(), "5 tasks") || !reflect.DeepEqual(before, s.Tasks()) {
		t.Fatalf("partial batch admitted: %d %s", code, h.errb)
	}
}

// L-LOOP2 C3d: archiving cannot replenish a worker's allowance or lose authorship.
func TestArchivedAdditionsKeepTheirWorkerMarker(t *testing.T) {
	h, s := treeBoundStore(t)
	t.Setenv(RunEnv, s.RootID())
	t.Setenv(TaskEnv, s.RootID())
	now := time.Now()
	s.now = func() time.Time { return now.Add(-2 * time.Hour) }
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("part%d", i)
		if _, err := s.AddManyForWorker(s.RootID(), []TaskSpec{{ID: id, ParentID: s.RootID(), Title: fmt.Sprintf("Part %d", i)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Claim(id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Done(id, id, "finished", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	s.now = func() time.Time { return now }
	archived, err := s.Archive(time.Hour)
	if err != nil || len(archived) != 5 {
		t.Fatalf("archive: %+v %v", archived, err)
	}
	stored, err := s.Archived()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range stored {
		if task.AddedByTask != s.RootID() {
			t.Fatalf("archive lost author: %+v", task)
		}
	}
	if code := h.run("--db", h.db, "add", "Sixth part"); code == 0 || !strings.Contains(h.errb.String(), "5 tasks") {
		t.Fatalf("archive replenished worker slots: %d %s", code, h.errb)
	}
}

// L-LOOP2 C3d: concurrent store handles cannot spend the same worker slot twice.
func TestWorkerAllowanceIsAtomicAcrossHandles(t *testing.T) {
	_, s := treeBoundStore(t)
	for i := 0; i < 4; i++ {
		if _, err := s.AddManyForWorker(s.RootID(), []TaskSpec{{ID: fmt.Sprintf("part%d", i), ParentID: s.RootID(), Title: fmt.Sprintf("Part %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	other, err := Open(s.Path(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, handle := range []*Store{s, other} {
		wg.Add(1)
		go func(i int, handle *Store) {
			defer wg.Done()
			_, err := handle.AddManyForWorker(s.RootID(), []TaskSpec{{ID: fmt.Sprintf("extra%d", i), ParentID: s.RootID(), Title: fmt.Sprintf("Extra %d", i)}})
			errs <- err
		}(i, handle)
	}
	wg.Wait()
	close(errs)
	accepted := 0
	for err := range errs {
		if err == nil {
			accepted++
		} else if !strings.Contains(err.Error(), "5 tasks") {
			t.Fatalf("unexpected refusal: %v", err)
		}
	}
	if accepted != 1 || len(s.Tasks()) != 6 {
		t.Fatalf("worker allowance raced: accepted=%d tasks=%d", accepted, len(s.Tasks()))
	}
}

// L-LOOP2 C3e: default parenting makes the depth refusal reachable.
func TestGrandchildWorkerCannotHandOutMoreWork(t *testing.T) {
	h, s := treeBoundStore(t)
	treeBoundAdd(t, s, TaskSpec{ID: "child", Title: "Child"}, TaskSpec{ID: "deep", ParentID: "child", Title: "Grandchild"})
	t.Setenv(RunEnv, s.RootID())
	t.Setenv(TaskEnv, "deep")
	before := s.Tasks()
	if code := h.run("--db", h.db, "add", "Renamed deeper work"); code == 0 || !strings.Contains(h.errb.String(), "2 levels below") || !reflect.DeepEqual(before, s.Tasks()) {
		t.Fatalf("grandchild escaped the depth bound: %d %s", code, h.errb)
	}
}

// L-LOOP2 C3d, C6: old stores migrate without guessing who added their rows
// or inventing a person's stop.
func TestOldStoresDefaultToUnattributedAdditionsAndNoStop(t *testing.T) {
	h, s := treeBoundStore(t)
	treeBoundAdd(t, s, TaskSpec{ID: "old", Title: "Earlier work"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(h.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"tasks", "archived_tasks"} {
		if _, err := db.Exec("ALTER TABLE " + table + " DROP COLUMN added_by_task"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("ALTER TABLE meta DROP COLUMN stopped"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if migrated.Task("old").AddedByTask != "" || migrated.Stopped() {
		t.Fatal("migration invented a worker author or a person's stop")
	}
	if _, err := migrated.AddManyForWorker(migrated.RootID(), []TaskSpec{{ID: "new", ParentID: migrated.RootID(), Title: "New work"}}); err != nil {
		t.Fatal(err)
	}
}
