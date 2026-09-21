package plandb

import (
	"strings"
	"testing"
)

func TestQuestionRoundTripsAndCanBePatched(t *testing.T) {
	path := t.TempDir() + "/plan.db"
	store := planOpen(t, path)
	planAdd(t, store, TaskSpec{ID: "owed", Title: "Answer later", Question: "Which branch did it land on?"})
	changed := "What changed, and where did it land?"
	if _, err := store.Revise("owed", TaskPatch{Question: &changed}); err != nil {
		t.Fatalf("revise question: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened := planReopen(t, path)
	defer reopened.Close()
	if got := reopened.Task("owed").Question; got != changed {
		t.Fatalf("reopened question = %q, want %q", got, changed)
	}
}

func TestQuestionColumnMigratesAndShowPrintsOnlyWhenSet(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	store, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := store.Task(store.RootID())
	question := "Did the repair preserve the old rows?"
	if _, err := store.Revise(root.ID, TaskPatch{Question: &question}); err != nil {
		t.Fatalf("set question: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if code := h.run("--db", h.db, "show", "t-"+root.ID); code != 0 {
		t.Fatalf("show exited %d: %s", code, h.errb.String())
	}
	if !strings.Contains(h.out.String(), "question: "+question) {
		t.Fatalf("show omitted question: %q", h.out.String())
	}

	db, err := openDatabase(h.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"tasks", "archived_tasks"} {
		if _, err := db.Exec("ALTER TABLE " + table + " DROP COLUMN question"); err != nil {
			t.Fatalf("drop %s.question: %v", table, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatalf("migrate question: %v", err)
	}
	defer migrated.Close()
	if got := migrated.Task(root.ID).Question; got != "" {
		t.Fatalf("migrated old question = %q, want empty", got)
	}
}
