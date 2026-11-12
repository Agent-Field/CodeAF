package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// TestListDoesNotDecodeAnUnchangedFloor is the idle-engine read. Triage,
// intake and a poll each ask for every job on their own clocks. Asking
// again while the files are the same must not open them again. An edit
// still shows up on the next ask.
func TestListDoesNotDecodeAnUnchangedFloor(t *testing.T) {
	st := open(t)
	created, err := st.Create(factory.Item{Title: "finished", State: factory.StateShipped})
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.List()
	if err != nil || len(first) != 1 || first[0].Title != "finished" {
		t.Fatalf("first list: %+v %v", first, err)
	}
	allocs := testing.AllocsPerRun(3, func() {
		second, err := st.List()
		if err != nil || len(second) != 1 || second[0].Title != "finished" {
			t.Fatalf("unchanged list: %+v %v", second, err)
		}
	})
	// Metadata reads are allowed; decoding a full item again exceeds this budget.
	if allocs > 100 {
		t.Fatalf("unchanged list allocated %.0f objects, want at most 100", allocs)
	}
	if err := st.Update(created.ID, func(it *factory.Item) error {
		it.Title = "edited"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	third, err := st.List()
	if err != nil || len(third) != 1 || third[0].Title != "edited" {
		t.Fatalf("edited list: %+v %v", third, err)
	}
}

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// A document goes in, comes back the same, lists newest first, and a save is
// the next revision.
func TestCreateGetListSaveRoundTrip(t *testing.T) {
	st := open(t)
	t0 := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	a, err := st.Create(factory.Item{Repo: "agentfield/codeaf", Title: "first", Created: t0})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Create(factory.Item{Repo: "agentfield/codeaf", Title: "second", Created: t0.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("ids = %d, %d; want 1, 2", a.ID, b.ID)
	}
	if a.Changed != t0 {
		t.Fatalf("Changed = %v, want it stamped from Created", a.Changed)
	}
	got, err := st.Get(a.ID)
	if err != nil || got.Title != "first" || got.Repo != "agentfield/codeaf" || !got.Created.Equal(t0) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	list, err := st.List()
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("List = %+v, %v; want newest first", list, err)
	}
	got.Title = "first, renamed"
	if err := st.Save(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.Get(a.ID); again.Title != "first, renamed" {
		t.Fatalf("after Save, Title = %q", again.Title)
	}
	if rev, _ := st.Revision(a.ID); rev != 2 {
		t.Fatalf("revision after one Save = %d, want 2", rev)
	}
	if _, err := st.Get(99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(99) = %v, want ErrNotFound", err)
	}
	if _, err := st.Get(0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(0) = %v, want ErrNotFound", err)
	}
	if _, err := st.Create(factory.Item{ID: 7, Title: "brought its own id"}); err == nil {
		t.Fatal("Create kept a caller's id")
	}
}

// Ten creators at once mint ten different ids, and every document is there.
func TestConcurrentCreatesMintDistinctIDs(t *testing.T) {
	st := open(t)
	const n = 10
	ids := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			it, err := st.Create(factory.Item{Title: "one of ten"})
			ids[i], errs[i] = it.ID, err
		}(i)
	}
	wg.Wait()
	seen := map[int]bool{}
	for i, id := range ids {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[id] {
			t.Fatalf("id %d minted twice: %v", id, ids)
		}
		seen[id] = true
	}
	list, err := st.List()
	if err != nil || len(list) != n {
		t.Fatalf("List = %d items, %v; want %d", len(list), err, n)
	}
}

// A document a newer build wrote is refused by Get, skipped by List, and its
// id is never minted again.
func TestNewerSchemaRefused(t *testing.T) {
	st := open(t)
	if err := os.WriteFile(st.ItemPath(5), []byte(`{"schema": 99, "revision": 1, "item": {"Title": "from the future"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(5); !errors.Is(err, ErrNewer) {
		t.Fatalf("Get = %v, want ErrNewer", err)
	}
	if list, err := st.List(); err != nil || len(list) != 0 {
		t.Fatalf("List = %+v, %v; want the newer document skipped", list, err)
	}
	if err := st.Update(5, func(*factory.Item) error { return nil }); !errors.Is(err, ErrNewer) {
		t.Fatalf("Update = %v, want ErrNewer", err)
	}
	it, err := st.Create(factory.Item{Title: "now"})
	if err != nil || it.ID != 6 {
		t.Fatalf("Create = %d, %v; want 6, past the document it cannot read", it.ID, err)
	}
}

// Add fills what a conversation does not say.
func TestAddFillsTheChatDefaults(t *testing.T) {
	st := open(t)
	id, err := st.Add(context.Background(), factory.Item{Repo: "agentfield/codeaf", Title: "split from chat: the write half", State: factory.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Origin != factory.OriginChat || it.State != factory.StateNew || it.Tier != factory.TierOwner || it.Kind != factory.KindIssue {
		t.Fatalf("Add = %+v", it)
	}
	if it.Created.IsZero() || it.Changed.IsZero() {
		t.Fatalf("Add left the times zero: %+v", it)
	}
	if len(it.Stages) == 0 || it.Stages[0].Name != "plan" {
		t.Fatalf("Add stages = %+v, want the default issue recipe", it.Stages)
	}
	issue, err := st.Add(context.Background(), factory.Item{Title: "an issue", Kind: factory.KindIssue, Origin: factory.OriginTerminal, Tier: factory.TierCollab})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(issue)
	if got.Origin != factory.OriginTerminal || got.Tier != factory.TierCollab || got.Stages[0].Name != "plan" {
		t.Fatalf("Add overrode the caller: %+v", got)
	}
	if _, err := st.Add(context.Background(), factory.Item{Title: "old PR job", Kind: factory.KindPR}); err == nil {
		t.Fatal("chat created a standalone PR job")
	}

	if _, err := st.Add(context.Background(), factory.Item{}); err == nil {
		t.Fatal("Add made an item with no title")
	}
	var none *Store
	if _, err := none.Add(context.Background(), factory.Item{Title: "x"}); err == nil {
		t.Fatal("a nil store added an item")
	}
}

func TestListSeesPreservedMtimeWrites(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%t", replace), func(t *testing.T) {
			st := open(t)
			it, err := st.Create(factory.Item{Title: "before"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.List(); err != nil {
				t.Fatal(err)
			}
			path := st.ItemPath(it.ID)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.ReplaceAll(data, []byte("before"), []byte("after!"))
			target := path
			if replace {
				target = path + ".tmp"
			}
			if err := os.WriteFile(target, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if replace {
				if err := os.Rename(target, path); err != nil {
					t.Fatal(err)
				}
			}
			items, err := st.List()
			if err != nil || len(items) != 1 || items[0].Title != "after!" {
				t.Fatalf("preserved-mtime write hidden: %+v, %v", items, err)
			}
		})
	}
}
