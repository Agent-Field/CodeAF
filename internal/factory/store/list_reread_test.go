package store

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// File identity cannot see an in-place rewrite that keeps the size and time, so
// no decision may rest on it for longer than the bound.
func TestListSeesInPlaceSameTimeEditWithinItsBound(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	st.clock = func() time.Time { return now }
	made, err := st.Create(factory.Item{Title: "before"})
	if err != nil {
		t.Fatal(err)
	}
	title := func() string {
		t.Helper()
		items, err := st.List()
		if err != nil || len(items) != 1 {
			t.Fatalf("list: %v, %v", items, err)
		}
		return items[0].Title
	}
	if got := title(); got != "before" {
		t.Fatalf("title = %q", got)
	}
	path := st.ItemPath(made.ID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"before"`, `"after!"`, 1)), info.Mode()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if next, err := os.Stat(path); err != nil || !os.SameFile(info, next) || next.Size() != info.Size() || !next.ModTime().Equal(info.ModTime()) {
		t.Fatalf("the edit did not keep the file's identity: %v", err)
	}

	now = now.Add(listRereadEvery - time.Second)
	before := st.DocumentReads()
	title()
	if got := st.DocumentReads() - before; got != 0 {
		t.Fatalf("inside the bound a look read %d documents, want 0", got)
	}

	now = now.Add(time.Second)
	if got := title(); got != "after!" {
		t.Fatalf("at the bound title = %q, want the edit", got)
	}
	before = st.DocumentReads()
	title()
	if got := st.DocumentReads() - before; got != 0 {
		t.Fatalf("after the re-read a look read %d documents, want 0", got)
	}
}
