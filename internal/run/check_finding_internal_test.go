package run

import (
	"path/filepath"
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
