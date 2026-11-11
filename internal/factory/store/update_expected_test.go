package store

import (
	"errors"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// UPDATE EXPECTED WRITES ONLY ON THE REVISION READ. The matching revision is
// an ordinary Update (the next revision, Changed stamped); any other one is a
// StaleError naming both, with the document byte for byte as it was and the
// change never called.
func TestUpdateExpectedWritesOnlyOnTheRevisionRead(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "a job"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.Title = "moved"; return nil }); err != nil {
		t.Fatal(err)
	}
	was, _ := os.ReadFile(st.ItemPath(it.ID))
	called := false
	err = st.UpdateExpected(it.ID, 1, func(*factory.Item) error { called = true; return nil })
	var stale *StaleError
	if !errors.As(err, &stale) || stale.ID != it.ID || stale.Expected != 1 || stale.Revision != 2 || called {
		t.Fatalf("err = %v, called %v; want a stale refusal at revision 2", err, called)
	}
	if now, _ := os.ReadFile(st.ItemPath(it.ID)); string(now) != string(was) {
		t.Fatal("a stale update changed the document")
	}
	if err := st.UpdateExpected(it.ID, 2, func(cur *factory.Item) error { cur.Title = "revised"; return nil }); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(it.ID)
	if rev, _ := st.Revision(it.ID); rev != 3 || got.Title != "revised" {
		t.Fatalf("revision %d title %q", rev, got.Title)
	}
	if err := st.UpdateExpected(404, 1, func(*factory.Item) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing item answered %v", err)
	}
}
