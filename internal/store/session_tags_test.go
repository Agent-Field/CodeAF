package store

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The subjects a room is filed under (see [Session.Tags]).
//
// They are one half of what the naming pass produces and they are governed by
// one rule the tests below state from every side: THE PASS FILES, A PERSON
// NAMES. A rename is a statement about what a room is called and says nothing
// about what it is about, so it may never write over the filing — and the
// filing lives in the journal rather than only in the projection, because a
// projection is rebuilt and anything not in an event does not survive that.

// The naming pass files the room and names it in one event, and both halves
// survive a rebuild.
func TestTheNamingPassFilesARoomAndTheFilingSurvivesARebuild(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "audit billing"}); err != nil {
		t.Fatal(err)
	}
	named, err := s.RenameSessionTagged("chat-1", "Billing code audit",
		[]string{"Billing", " proration ", "invoices"})
	if err != nil {
		t.Fatalf("name and file: %v", err)
	}
	want := []string{"billing", "proration", "invoices"}
	if !slices.Equal(named.Tags, want) {
		t.Fatalf("the room is filed under %v, want %v lower-cased and trimmed", named.Tags, want)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilt, ok, err := s.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read rebuilt session: %v (found %v)", err, ok)
	}
	if !sameSession(rebuilt, named) {
		t.Fatalf("a rebuild produced %+v, want %+v", rebuilt, named)
	}
}

// A rename states a NAME. It is not a statement about what the conversation is
// about, so the filing is left exactly as the naming pass left it.
func TestARenameLeavesTheFilingAlone(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "audit billing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameSessionTagged("chat-1", "Billing code audit", []string{"billing", "proration"}); err != nil {
		t.Fatal(err)
	}
	renamed, err := s.RenameSession("chat-1", "quarterly numbers")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	want := []string{"billing", "proration"}
	if renamed.Title != "quarterly numbers" || !slices.Equal(renamed.Tags, want) {
		t.Fatalf("the room is %q filed under %v, want the new name and %v", renamed.Title, renamed.Tags, want)
	}
	// And a naming pass that produced no subjects says nothing about them
	// either — an absent list is not an empty one.
	empty, err := s.RenameSessionTagged("chat-1", "quarterly numbers II", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(empty.Tags, want) {
		t.Fatalf("a nameless filing rewrote the tags to %v, want %v", empty.Tags, want)
	}
	// The filing alone is still a change worth journaling: a room whose title
	// is already right and whose subjects are not must not be un-fileable.
	refiled, err := s.RenameSessionTagged("chat-1", "quarterly numbers II", []string{"taxes"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(refiled.Tags, []string{"taxes"}) {
		t.Fatalf("re-filing an already-named room produced %v", refiled.Tags)
	}
}

// Three, and no duplicates: a filter's index stops helping the moment its
// entries are a summary.
func TestTheFilingIsBoundedAndDeduplicated(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "audit billing"}); err != nil {
		t.Fatal(err)
	}
	filed, err := s.RenameSessionTagged("chat-1", "Billing code audit",
		[]string{"billing", "BILLING", "", "proration", "invoices", "taxes"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"billing", "proration", "invoices"}
	if !slices.Equal(filed.Tags, want) {
		t.Fatalf("the room is filed under %v, want %v", filed.Tags, want)
	}
	long := strings.Repeat("a", MaxSessionTagBytes+40)
	bounded, err := s.RenameSessionTagged("chat-1", "Billing code audit II", []string{long})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Tags) != 1 || len(bounded.Tags[0]) > MaxSessionTagBytes {
		t.Fatalf("a long tag landed as %v, want one bounded to %d bytes", bounded.Tags, MaxSessionTagBytes)
	}
}

// A database written by the previous build gains the column when a newer one
// opens it, and the events that predate it replay into it.
func TestTheTagsColumnArrivesByMigrationAndSurvivesRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-tags-column.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := graph.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "audit billing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RenameSessionTagged("chat-1", "Billing code audit", []string{"billing", "proration"}); err != nil {
		t.Fatal(err)
	}
	// Take the column away, exactly as a database written by the previous build
	// would have it, and let the migration put it back.
	if _, err := graph.db.Exec(`ALTER TABLE sessions DROP COLUMN tags`); err != nil {
		t.Fatalf("drop the column: %v", err)
	}
	if err := graph.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	if found, err := tableHasColumn(reopened.db, "sessions", "tags"); err != nil || !found {
		t.Fatalf("tags column migration: found=%t err=%v", found, err)
	}
	// The recovered column is EMPTY, which is the honest state of a filing that
	// was never written down. The event is the truth and the table is a view of
	// it, so the replay is what makes it say the right thing again.
	recovered, ok, err := reopened.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read migrated session: %v (found %v)", err, ok)
	}
	if len(recovered.Tags) != 0 {
		t.Fatalf("the migrated column already held %v, want the default", recovered.Tags)
	}
	if err := reopened.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	replayed, ok, err := reopened.Session("chat-1")
	if err != nil || !ok {
		t.Fatalf("read after rebuild: %v (found %v)", err, ok)
	}
	want := []string{"billing", "proration"}
	if replayed.Title != "Billing code audit" || !slices.Equal(replayed.Tags, want) {
		t.Fatalf("the replay produced %q filed under %v, want the name and %v",
			replayed.Title, replayed.Tags, want)
	}
}

// The switcher's projection carries the filing, because the switcher's filter
// is the one place it is ever read.
func TestTheThreadIndexCarriesTheFiling(t *testing.T) {
	s := openThreadStore(t)
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleUser, Body: "audit billing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostMessage(Message{SessionID: "chat-1", Role: RoleAgent, Body: "On it."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameSessionTagged("chat-1", "Billing code audit", []string{"billing", "proration"}); err != nil {
		t.Fatal(err)
	}
	arcs, err := s.ThreadIndex(8)
	if err != nil {
		t.Fatalf("thread index: %v", err)
	}
	if len(arcs) != 1 {
		t.Fatalf("the index listed %d threads, want one", len(arcs))
	}
	if want := []string{"billing", "proration"}; !slices.Equal(arcs[0].Tags, want) {
		t.Fatalf("the arc carries %v, want %v", arcs[0].Tags, want)
	}
}
