package workspace

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func createTestCollection(t *testing.T, s *Store, name string) Collection {
	t.Helper()
	c, err := s.Create(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSharedReferencesSurviveReopenAndKeepTheirOwners(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "collections ?# .db")
	s := openTestStore(t, path)
	product := createTestCollection(t, s, "Product")
	marketing := createTestCollection(t, s, "Marketing")
	refs := []Ref{
		{Kind: ConversationKind, ID: "shared-discussion"},
		{Kind: TaskKind, ID: "1", SessionID: "code-chat"},
		{Kind: TaskKind, ID: "1", SessionID: "marketing-chat"},
		{Kind: StandingKind, ID: "email-watch"},
		{Kind: ArtifactKind, ID: filepath.Join(t.TempDir(), "not-currently-mounted.md")},
	}
	for _, c := range []Collection{product, marketing} {
		for _, ref := range refs {
			if err := s.Add(ctx, c.ID, ref); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, c.ID, ref); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path)
	if err := s.Rename(ctx, product.ID, "Engineering"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, marketing.ID, refs[1]); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, marketing.ID, refs[1]); err != nil {
		t.Fatal(err)
	}
	got, err := s.Members(ctx, product.ID)
	if err != nil || !reflect.DeepEqual(got, refs) {
		t.Fatalf("members %v, %v", got, err)
	}
	got, err = s.Members(ctx, marketing.ID)
	want := []Ref{refs[0], refs[2], refs[3], refs[4]}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("members %v, %v", got, err)
	}
	parents, err := s.CollectionsFor(ctx, refs[0])
	if err != nil || len(parents) != 2 || parents[0].ID != product.ID || parents[0].Name != "Engineering" {
		t.Fatalf("parents %v, %v", parents, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions %o", info.Mode().Perm())
	}
}

func TestNestedCollectionsAllowSharedParentsButRejectCycles(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "collections.db"))
	a, b, c := createTestCollection(t, s, "A"), createTestCollection(t, s, "B"), createTestCollection(t, s, "C")
	for _, edge := range [][2]string{{a.ID, b.ID}, {b.ID, c.ID}, {a.ID, c.ID}} {
		if err := s.Add(ctx, edge[0], Ref{Kind: CollectionKind, ID: edge[1]}); err != nil {
			t.Fatal(err)
		}
	}
	for _, edge := range [][2]string{{c.ID, a.ID}, {b.ID, a.ID}, {a.ID, a.ID}} {
		if err := s.Add(ctx, edge[0], Ref{Kind: CollectionKind, ID: edge[1]}); !errors.Is(err, ErrCycle) {
			t.Fatalf("cycle: %v", err)
		}
	}
	parents, err := s.CollectionsFor(ctx, Ref{Kind: CollectionKind, ID: c.ID})
	if err != nil || len(parents) != 2 {
		t.Fatalf("parents %v, %v", parents, err)
	}
	if err := s.Remove(ctx, b.ID, Ref{Kind: CollectionKind, ID: c.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, c.ID, Ref{Kind: CollectionKind, ID: b.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentHandlesCannotCreateCycleOrLoseMemberships(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	first, second := openTestStore(t, path), openTestStore(t, path)
	a, b := createTestCollection(t, first, "A"), createTestCollection(t, first, "B")
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i, s := range []*Store{first, second} {
		go func(i int, s *Store) {
			<-start
			from, to := a.ID, b.ID
			if i == 1 {
				from, to = to, from
			}
			errs <- s.Add(ctx, from, Ref{Kind: CollectionKind, ID: to})
		}(i, s)
	}
	close(start)
	e1, e2 := <-errs, <-errs
	if !((e1 == nil && errors.Is(e2, ErrCycle)) || (e2 == nil && errors.Is(e1, ErrCycle))) {
		t.Fatalf("opposite edits %v, %v", e1, e2)
	}
	var wg sync.WaitGroup
	for _, s := range []*Store{first, second} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			for _, id := range []string{"one", "two", "three", "four"} {
				if err := s.Add(ctx, a.ID, Ref{Kind: ConversationKind, ID: id}); err != nil {
					t.Error(err)
				}
			}
		}(s)
	}
	wg.Wait()
	members, err := first.Members(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	chats := 0
	for _, ref := range members {
		if ref.Kind == ConversationKind {
			chats++
		}
	}
	if chats != 4 {
		t.Fatalf("lost or duplicated edits: %v", members)
	}
}

func TestInvalidAndMissingReferencesDoNotMutateCollections(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "collections.db"))
	c := createTestCollection(t, s, "Inbox")
	for _, ref := range []Ref{
		{}, {Kind: "worker", ID: "x"}, {Kind: TaskKind, ID: "1"},
		{Kind: TaskKind, ID: "01", SessionID: "s"}, {Kind: TaskKind, ID: "0", SessionID: "s"},
		{Kind: ConversationKind, ID: "x", SessionID: "s"}, {Kind: ConversationKind, ID: "a\nb"},
		{Kind: ArtifactKind, ID: "relative.md"},
	} {
		if err := s.Add(ctx, c.ID, ref); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", ref, err)
		}
	}
	if err := s.Add(ctx, c.ID, Ref{Kind: CollectionKind, ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Add(ctx, "missing", Ref{Kind: ConversationKind, ID: "chat"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Members(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Rename(ctx, "missing", "New name"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	got, err := s.Members(ctx, c.ID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("invalid writes leaked: %v, %v", got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Add(cancelled, c.ID, Ref{Kind: ConversationKind, ID: "cancelled"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

// C7 — Future, foreign and corrupt files must remain distinguishable from an
// empty store, because accepting one would let collections rewrite another owner.
func TestOpenRefusesFutureForeignAndDamagedDatabases(t *testing.T) {
	for _, fixture := range []struct{ name, sql string }{
		{"future", "PRAGMA application_id=1095123788; PRAGMA user_version=99"},
		{"foreign", "CREATE TABLE memories(id INTEGER)"},
		{"missing-schema", "PRAGMA application_id=1095123788; PRAGMA user_version=1"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(fixture.sql); err != nil {
				t.Fatal(err)
			}
			_ = db.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if s, err := Open(path); err == nil {
				s.Close()
				t.Fatal("accepted incompatible store")
			}
			after, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("changed rejected store", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("accepted corrupt store")
	}
}

// C4 — A real write that exhausts the configured wait must return ErrBusy in
// the same stable sentence while preserving everything committed before it.
func TestBusyWriterReturnsWithoutLosingPriorState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, other := openTestStore(t, path), openTestStore(t, path)
	c := createTestCollection(t, s, "Inbox")
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = other.Add(context.Background(), c.ID, Ref{Kind: ConversationKind, ID: "blocked"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("write passed or misreported another writer: %v", err)
	}
	if want := ErrBusy.Error() + " (waited " + busyTimeout.String() + ")"; err.Error() != want {
		t.Fatalf("busy answer %q, want %q", err, want)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	members, err := s.Members(context.Background(), c.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("failed write changed state: %v, %v", members, err)
	}
}
