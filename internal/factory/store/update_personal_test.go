package store

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PERSON'S OWN FACT IS NOT THE JOB MOVING. UpdatePersonal writes the change
// on the document on disk, keeps the revision another person's page read and
// leaves Changed as it was, so nobody's open page goes stale over it.
func TestUpdatePersonalKeepsTheRevisionAndChanged(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "a job"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.Title = "moved"; return nil }); err != nil {
		t.Fatal(err)
	}
	was, _ := st.Get(it.ID)
	st.clock = func() time.Time { return was.Changed.Add(time.Hour) }
	if err := st.UpdatePersonal(it.ID, func(cur *factory.Item) bool { cur.Title = "personal"; return true }); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(it.ID)
	if rev, _ := st.Revision(it.ID); rev != 2 || got.Title != "personal" || !got.Changed.Equal(was.Changed) {
		t.Fatalf("revision %d title %q changed %v (was %v)", rev, got.Title, got.Changed, was.Changed)
	}
	before, _ := os.ReadFile(st.ItemPath(it.ID))
	if err := st.UpdatePersonal(it.ID, func(cur *factory.Item) bool { cur.Title = "unsaid"; return false }); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(st.ItemPath(it.ID)); string(after) != string(before) {
		t.Fatal("a personal change that changed nothing rewrote the document")
	}
	if err := st.UpdateExpected(it.ID, 2, func(cur *factory.Item) error { cur.Title = "after"; return nil }); err != nil {
		t.Fatalf("a page read at revision 2 went stale: %v", err)
	}
}
