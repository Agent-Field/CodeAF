package store

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// idleStore is a floor of n jobs, each carrying a log, a thread and the
// slices and maps a busy floor's documents hold.
func idleStore(t *testing.T, n int) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		it := factory.Item{
			Title: fmt.Sprintf("job %d", i), Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew,
			Labels: []string{"bug", "p1"}, Notes: []string{"a note"}, BriefSeen: map[string]int{"ana": 1},
			Stream: &factory.Stream{Log: []factory.LogLine{{Text: "line one"}, {Text: "line two"}}},
		}
		if _, err := st.Create(it); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// THE IDLE-COST LAW (PERF.md): a listing of a floor nobody changed decodes no
// document. Every factory request settles the floor, every worker pull walks
// it and every job chat's watch looks at it, and on a floor of hundreds of
// jobs, decoding each document on each of those looks kept an idle engine on
// a core or more.
func TestAnUnchangedFloorIsListedWithoutReadingAJob(t *testing.T) {
	const jobs, looks = 60, 50
	st := idleStore(t, jobs)
	for _, door := range []struct {
		name string
		list func(*Store) ([]factory.Item, error)
	}{
		{"ListSaved", (*Store).ListSaved},
		{"List", (*Store).List},
		{"Get", func(st *Store) ([]factory.Item, error) { it, err := st.Get(jobs / 2); return []factory.Item{it}, err }},
	} {
		if _, err := door.list(st); err != nil {
			t.Fatal(err)
		}
		before := st.Reads()
		for range looks {
			// A fresh store each look, as every door opens its own.
			again, err := Open(st.Root())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := door.list(again); err != nil {
				t.Fatal(err)
			}
			before -= again.Reads()
		}
		got := st.Reads() - before
		t.Logf("%s: %d looks at an unchanged floor of %d jobs decoded %d documents", door.name, looks, jobs, got)
		if got != 0 {
			t.Fatalf("%s: %d looks at an unchanged floor of %d jobs decoded %d documents, want 0", door.name, looks, jobs, got)
		}
	}
}

// Every way a document changes is seen on the very next read, and nothing
// that was read can be changed through the copy a caller was handed.
func TestEveryChangeToAJobIsSeenOnTheNextRead(t *testing.T) {
	st := idleStore(t, 5)
	first, err := st.ListSaved()
	if err != nil || len(first) != 5 {
		t.Fatalf("%v %v", len(first), err)
	}
	// A caller editing what it was handed reaches nothing kept.
	for i := range first {
		first[i].Title = "edited"
		first[i].Labels[0] = "edited"
		first[i].BriefSeen["ana"] = 99
		first[i].Stream.Log[0].Text = "edited"
	}
	again, err := st.Get(3)
	if err != nil {
		t.Fatal(err)
	}
	if again.Title != "job 2" || again.Labels[0] != "bug" || again.BriefSeen["ana"] != 1 || again.Stream.Log[0].Text != "line one" {
		t.Fatalf("an edit to a handed-out copy reached the next read: %+v", again)
	}

	// A store write.
	if err := st.Update(3, func(it *factory.Item) error { factory.MarkPersonStopped(it); return nil }); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(3); !factory.StoppedByPerson(got) {
		t.Fatal("a Stop written by the store was not seen on the next read")
	}
	// A write by another process, in place, dated back to the old time.
	path := st.ItemPath(4)
	info, _ := os.Stat(path)
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"job 3"`, `"job 3, renamed by hand"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(4); got.Title != "job 3, renamed by hand" {
		t.Fatalf("an edit in place was not seen: %q", got.Title)
	}
	// A document replaced by another of exactly the same size and time.
	other := st.ItemPath(5)
	oinfo, _ := os.Stat(other)
	odata, _ := os.ReadFile(other)
	tmp := other + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Replace(string(odata), `"job 4"`, `"job X"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, oinfo.ModTime(), oinfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, other); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(5); got.Title != "job X" {
		t.Fatalf("a renamed-in replacement of the same size and time was not seen: %q", got.Title)
	}
	// A document taken away.
	if err := os.Remove(st.ItemPath(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(1); err != ErrNotFound {
		t.Fatalf("a removed document still read: %v", err)
	}
	if items, _ := st.ListSaved(); len(items) != 4 {
		t.Fatalf("a removed document still listed: %d", len(items))
	}
}
