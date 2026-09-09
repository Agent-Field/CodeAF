package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// EVERY DURABILITY GUARANTEE THIS STORE MAKES LIVES IN ONE DSN STRING, and a
// renamed driver parameter or a reordered query would switch all of them off
// while every other test in this package still passed. So the settings are read
// back from the live connection, and the foreign key is proven by making the
// database refuse a membership whose owning collection does not exist.
func TestConnectionPragmasStayInForceSoWritesAreDurableAndBounded(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "pragmas.db"))
	for _, want := range []struct {
		pragma string
		value  int64
	}{
		{"foreign_keys", 1},
		{"busy_timeout", busyTimeout.Milliseconds()},
		{"synchronous", 2}, // 2 is FULL: the commit is on the disk before it is reported.
	} {
		var got int64
		if err := s.db.QueryRow("PRAGMA " + want.pragma).Scan(&got); err != nil {
			t.Fatalf("%s: %v", want.pragma, err)
		}
		if got != want.value {
			t.Errorf("%s is %d, want %d", want.pragma, got, want.value)
		}
	}
	_, err := s.db.Exec(`INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES ('no-such-collection','conversation','chat','',NULL)`)
	if err == nil {
		t.Fatal("stored a membership under a collection that does not exist")
	}
}

// Nesting is the one relationship whose correctness depends on reading state
// back off the disk: the cycle check consults the stored edges, not a graph the
// process happens to remember. A reopened store must refuse what it refused.
func TestNestedCollectionsPersistAndStillRefuseTheOppositeEdgeAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nesting.db")
	s := openTestStore(t, path)
	parent, child := createTestCollection(t, s, "Parent"), createTestCollection(t, s, "Child")
	if err := s.Add(ctx, parent.ID, Ref{Kind: CollectionKind, ID: child.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestStore(t, path)
	members, err := reopened.Members(ctx, parent.ID)
	if err != nil || len(members) != 1 || members[0] != (Ref{Kind: CollectionKind, ID: child.ID}) {
		t.Fatalf("nesting did not survive the reopen: %v, %v", members, err)
	}
	if err := reopened.Add(ctx, child.ID, Ref{Kind: CollectionKind, ID: parent.ID}); !errors.Is(err, ErrCycle) {
		t.Fatalf("reopened store forgot the edge it must not close: %v", err)
	}
}

// A shortcut in the fixture can conceal a shallow cycle check. This chain has
// no shortcuts when its closing edge is attempted, so the check must traverse it.
func TestCycleCheckFollowsALongChainOfNesting(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "chain.db"))
	var chain []Collection
	for i := 0; i < 12; i++ {
		chain = append(chain, createTestCollection(t, s, fmt.Sprintf("Step %d", i)))
	}
	for i := 0; i+1 < len(chain); i++ {
		if err := s.Add(ctx, chain[i].ID, Ref{Kind: CollectionKind, ID: chain[i+1].ID}); err != nil {
			t.Fatal(err)
		}
	}
	last, first := chain[len(chain)-1], chain[0]
	if err := s.Add(ctx, last.ID, Ref{Kind: CollectionKind, ID: first.ID}); !errors.Is(err, ErrCycle) {
		t.Fatalf("closed a twelve-step chain: %v", err)
	}
	// A sideways edge that shares parents is not a cycle and must still be allowed.
	if err := s.Add(ctx, first.ID, Ref{Kind: CollectionKind, ID: last.ID}); err != nil {
		t.Fatalf("refused a legitimate shortcut edge: %v", err)
	}
}

// A cancelled caller must reach every door as a refusal, not as a partial write.
// Only Add was covered before, and the read doors are the ones a future surface
// will call from a context that is routinely cancelled by a keystroke.
func TestEveryDoorRefusesACancelledContextWithoutWriting(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "cancel.db"))
	inbox := createTestCollection(t, s, "Inbox")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doors := map[string]func() error{
		"Create":         func() error { _, err := s.Create(ctx, "Later"); return err },
		"Rename":         func() error { return s.Rename(ctx, inbox.ID, "Later") },
		"Add":            func() error { return s.Add(ctx, inbox.ID, Ref{Kind: ConversationKind, ID: "chat"}) },
		"Remove":         func() error { return s.Remove(ctx, inbox.ID, Ref{Kind: ConversationKind, ID: "chat"}) },
		"Collections":    func() error { _, err := s.Collections(ctx); return err },
		"Members":        func() error { _, err := s.Members(ctx, inbox.ID); return err },
		"CollectionsFor": func() error { _, err := s.CollectionsFor(ctx, Ref{Kind: ConversationKind, ID: "chat"}); return err },
	}
	for name, door := range doors {
		if err := door(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s answered a cancelled caller with %v", name, err)
		}
	}
	live := context.Background()
	kept, err := s.Collections(live)
	if err != nil || len(kept) != 1 || kept[0] != inbox {
		t.Fatalf("a cancelled call changed the store: %v, %v", kept, err)
	}
	members, err := s.Members(live, inbox.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("a cancelled add left a membership: %v, %v", members, err)
	}
}

// Create and Rename enforce the name rule at the write boundary so a command
// adapter cannot be the only thing standing between a control character and
// durable storage. A refused rename must leave the stored name untouched.
func TestMalformedNamesAreRefusedAndLeaveTheStoredNameAlone(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "names.db"))
	empty, err := s.Collections(ctx)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("a new store must list an empty set, not nothing: %v, %v", empty, err)
	}
	inbox := createTestCollection(t, s, "Inbox")
	for _, name := range []string{"", "   ", " leading", "trailing ", "two\nlines", strings.Repeat("x", 257)} {
		if _, err := s.Create(ctx, name); !errors.Is(err, ErrInvalid) {
			t.Errorf("Create(%q): %v", name, err)
		}
		if err := s.Rename(ctx, inbox.ID, name); !errors.Is(err, ErrInvalid) {
			t.Errorf("Rename(%q): %v", name, err)
		}
	}
	got, err := s.Collections(ctx)
	if err != nil || len(got) != 1 || got[0] != inbox {
		t.Fatalf("a refused name reached storage: %v, %v", got, err)
	}
}

// REMOVING A MEMBERSHIP REMOVES A MEMBERSHIP. The referenced work keeps its own
// owner and its own lifetime, so an artifact that is ungrouped is still on the
// disk, and a task that is ungrouped from one collection stays in the others.
func TestRemoveDetachesTheReferenceAndLeavesTheRecordAlone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	artifact := filepath.Join(dir, "report.md")
	if err := os.WriteFile(artifact, []byte("still here"), 0600); err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, filepath.Join(dir, "remove.db"))
	product, archive := createTestCollection(t, s, "Product"), createTestCollection(t, s, "Archive")
	refs := []Ref{{Kind: ArtifactKind, ID: artifact}, {Kind: TaskKind, ID: "7", SessionID: "chat"}}
	for _, c := range []Collection{product, archive} {
		for _, ref := range refs {
			if err := s.Add(ctx, c.ID, ref); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, ref := range refs {
		if err := s.Remove(ctx, product.ID, ref); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(artifact)
	if err != nil || string(content) != "still here" {
		t.Fatalf("ungrouping touched the artifact: %q, %v", content, err)
	}
	kept, err := s.Members(ctx, archive.ID)
	if err != nil || len(kept) != 2 {
		t.Fatalf("ungrouping from one collection emptied another: %v, %v", kept, err)
	}
	for _, ref := range refs {
		parents, err := s.CollectionsFor(ctx, ref)
		if err != nil || len(parents) != 1 || parents[0].ID != archive.ID {
			t.Fatalf("%v is filed under %v, %v", ref, parents, err)
		}
	}
}

// A damaged or hand-edited schema can still claim our version. The store must
// say so rather than treat the missing half as an
// empty set of memberships, and it must not repair the file underneath the owner.
func TestOpenRefusesAHalfBuiltSchemaAtOurOwnVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "half.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, name TEXT NOT NULL);
PRAGMA application_id=%d; PRAGMA user_version=%d`, applicationID, schemaVersion)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		_ = s.Close()
		t.Fatal("accepted a store with no memberships table")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rewrote a store it had refused", err)
	}
}

// The file provisioned by Open is private. A dangling database symlink must
// not let SQLite bypass that provisioning and create its target by itself.
func TestOpenDoesNotCreateADanglingDatabaseLinkTarget(t *testing.T) {
	root := t.TempDir()
	path, target := filepath.Join(root, "linked.db"), filepath.Join(root, "target.db")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("opened a dangling database link")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created link target: %v", err)
	}
}

// Several sessions share one working tree and one home, so the very first open
// of a collections database is routinely a race. Exactly one schema must result,
// and no handle may be told the store is unusable because a peer got there first.
func TestConcurrentFirstOpensAgreeOnOneSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "first.db")
	const handles = 8
	var wg sync.WaitGroup
	failures := make([]error, handles)
	for i := 0; i < handles; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				failures[i] = err
				return
			}
			defer s.Close()
			_, failures[i] = s.Create(context.Background(), fmt.Sprintf("Collection %d", i))
		}(i)
	}
	wg.Wait()
	for i, err := range failures {
		if err != nil {
			t.Errorf("handle %d lost the first-open race: %v", i, err)
		}
	}
	s := openTestStore(t, path)
	got, err := s.Collections(context.Background())
	if err != nil || len(got) != handles {
		t.Fatalf("racing opens kept %d of %d collections: %v", len(got), handles, err)
	}
}

// Insertion order is the whole ordering story: nothing here infers relevance.
// A reference that is removed and added again is a new insertion, so it belongs
// at the end, and that is a promise the documentation makes out loud.
func TestReAddedReferenceLandsAtTheEndOfTheOrder(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "order.db"))
	inbox := createTestCollection(t, s, "Inbox")
	first := Ref{Kind: ConversationKind, ID: "first"}
	second := Ref{Kind: ConversationKind, ID: "second"}
	third := Ref{Kind: StandingKind, ID: "third"}
	for _, ref := range []Ref{first, second, third} {
		if err := s.Add(ctx, inbox.ID, ref); err != nil {
			t.Fatal(err)
		}
	}
	// Adding again is a no-op, so it must not reorder anything either.
	if err := s.Add(ctx, inbox.ID, first); err != nil {
		t.Fatal(err)
	}
	got, err := s.Members(ctx, inbox.ID)
	if err != nil || !reflect.DeepEqual(got, []Ref{first, second, third}) {
		t.Fatalf("a repeated add moved a reference: %v, %v", got, err)
	}
	if err := s.Remove(ctx, inbox.ID, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, inbox.ID, first); err != nil {
		t.Fatal(err)
	}
	got, err = s.Members(ctx, inbox.ID)
	if err != nil || !reflect.DeepEqual(got, []Ref{second, third, first}) {
		t.Fatalf("a re-added reference did not land at the end: %v, %v", got, err)
	}
}
