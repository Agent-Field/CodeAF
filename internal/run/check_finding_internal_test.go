package run

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestRecordCheckFindingRecordsCheckAnswerOnLeaf(t *testing.T) {
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, "plandb.db"), "p", "root", "root", "root")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "leaf", Title: "leaf", ParentID: store.RootID()},
		{ID: "review", Title: "check: leaf", ParentID: store.RootID(), Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{store: store, checkOf: map[string]string{"review": "leaf"}}
	s.recordCheckFinding(*store.Task("review"), "check: fixture reading completed")
	notes := store.Notes("leaf", 0)
	if len(notes) != 1 || notes[0].Agent != "check" || notes[0].Body != "fixture reading completed" {
		t.Fatalf("leaf notes = %#v, want the check answer recorded by check", notes)
	}
}

func TestAFindingAboutAnotherFileStillGetsItsFix(t *testing.T) {
	for _, tc := range []struct {
		name, finding string
		wantFix       int
	}{
		{"another word", "does not hold: data.go is not formatted.", 1},
		{"another path segment", "does not hold: data.a.go is not formatted.", 1},
		{"the named file", "does not hold: a.go is not formatted.", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "p", "root", "root", "root")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if _, err := store.AddMany([]plandb.TaskSpec{
				{ID: "part1", Title: "first part", ParentID: store.RootID(), Checks: []string{"gofmt -l a.go"}},
				{ID: "part2", Title: "second part", ParentID: store.RootID(), Checks: []string{"gofmt -l a.go"}},
				{ID: "review", Title: "check: first part", ParentID: store.RootID(), Role: plandb.RoleCheck},
			}); err != nil {
				t.Fatal(err)
			}
			s := &Supervisor{store: store, workspace: workspace, checkOf: map[string]string{"review": "part1"}}
			s.recordCheckFinding(*store.Task("review"), tc.finding)
			fixes := 0
			for _, task := range store.Tasks() {
				if strings.HasPrefix(task.Title, "fix: ") {
					fixes++
				}
			}
			if fixes != tc.wantFix {
				t.Errorf("finding %q started %d fixes, want %d", tc.finding, fixes, tc.wantFix)
			}
		})
	}
}
