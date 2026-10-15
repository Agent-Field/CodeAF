package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// legacyDDL is the schema-1 file exactly as the build before schema 2 wrote
// it, so the migration is proved against the bytes people actually have.
const legacyDDL = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
PRAGMA application_id=1095123788; PRAGMA user_version=1;
INSERT INTO collections(id,name) VALUES ('aaaa','Product'),('bbbb','Nested');
INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection) VALUES
 ('aaaa','conversation','chat-1','',NULL),
 ('aaaa','task','3','chat-1',NULL),
 ('aaaa','collection','bbbb','','bbbb'),
 ('bbbb','artifact','/tmp/report.md','',NULL);
`

func writeLegacyStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(legacyDDL); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func userVersion(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func fixedClock(s *Store, ms int64) { s.now = func() time.Time { return time.UnixMilli(ms) } }

// A SCHEMA-1 FILE IS READ AS IT STANDS AND MIGRATED BY THE FIRST WRITE: the
// reads leave every byte alone and say nothing of when anything was added,
// and the first write rebuilds both tables in one transaction, keeping every
// row in its order with added_at unknown.
func TestSchemaOneFileIsReadThenMigratedInPlaceByTheFirstWrite(t *testing.T) {
	ctx := context.Background()
	path := writeLegacyStore(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := openTestStore(t, path)
	list, err := s.List(ctx, &Ref{Kind: ConversationKind, ID: "chat-1"})
	if err != nil || len(list) != 2 || list[0].Count != 3 || list[0].LastAddedAt != 0 || list[0].CreatedAt != 0 || !*list[0].Holds || *list[1].Holds {
		t.Fatalf("legacy list %+v, %v", list, err)
	}
	tree, err := s.Tree(ctx, "aaaa", 1, 10)
	if err != nil || len(tree.Members) != 3 || tree.Members[0].Kind != CollectionKind || tree.Members[0].Children == nil || len(tree.Members[0].Children.Members) != 1 {
		t.Fatalf("legacy tree %+v, %v", tree, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("a read changed a schema-1 file")
	}
	if userVersion(t, s) != 1 {
		t.Fatal("a read migrated the file")
	}

	fixedClock(s, 5000)
	added, already, err := s.AddRefs(ctx, "aaaa", []Addition{{Ref: Ref{Kind: CrewKind, ID: "crew-1"}}, {Ref: Ref{Kind: ConversationKind, ID: "chat-1"}}})
	if err != nil || len(added) != 1 || added[0].AddedAt != 5000 || len(already) != 1 {
		t.Fatalf("first write %+v %+v %v", added, already, err)
	}
	if userVersion(t, s) != schemaVersion {
		t.Fatal("the first write did not migrate")
	}
	got, err := s.Members(ctx, "aaaa")
	want := []Ref{{Kind: ConversationKind, ID: "chat-1"}, {Kind: TaskKind, ID: "3", SessionID: "chat-1"}, {Kind: CollectionKind, ID: "bbbb"}, {Kind: CrewKind, ID: "crew-1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("migrated members %v, %v", got, err)
	}
	// The migrated foreign keys cascade: deleting the nested collection
	// takes the membership naming it.
	parents, err := s.Delete(ctx, "bbbb")
	if err != nil || !reflect.DeepEqual(parents, []string{"aaaa"}) {
		t.Fatalf("delete after migration %v, %v", parents, err)
	}
	if got, _ := s.Members(ctx, "aaaa"); len(got) != 3 {
		t.Fatalf("the membership naming a deleted collection stayed: %v", got)
	}
	// Another handle that saw the file at schema 1 reads it at schema 2 now.
	other := openTestStore(t, path)
	if list, err := other.List(ctx, nil); err != nil || len(list) != 1 || list[0].LastAddedAt != 5000 {
		t.Fatalf("reopened list %+v, %v", list, err)
	}
}

// A handle opened on a schema-1 file keeps reading after another process
// migrated it underneath, and its own first write does not migrate twice.
func TestAStaleLegacyHandleFollowsAPeersMigration(t *testing.T) {
	ctx := context.Background()
	path := writeLegacyStore(t)
	stale, fresh := openTestStore(t, path), openTestStore(t, path)
	if err := fresh.Rename(ctx, "aaaa", "Products"); err != nil {
		t.Fatal(err)
	}
	if err := stale.Add(ctx, "aaaa", Ref{Kind: ProcessKind, ID: "chat-1:p1", SessionID: "chat-1"}); err != nil {
		t.Fatal(err)
	}
	list, err := stale.List(ctx, nil)
	if err != nil || list[0].Name != "Products" || list[0].Count != 4 {
		t.Fatalf("stale list %+v, %v", list, err)
	}
}

// NAMES ARE UNIQUE WITHOUT REGARD TO CASE, Unicode included, and a rename to
// a collection's own name in another case is allowed.
func TestCollectionNamesAreUniqueIgnoringCase(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "names.db"))
	a := createTestCollection(t, s, "Ärger")
	b := createTestCollection(t, s, "Launch")
	for _, name := range []string{"ärger", "ÄRGER"} {
		if _, err := s.Create(ctx, name); !errors.Is(err, ErrNameTaken) {
			t.Fatalf("create %q: %v", name, err)
		}
	}
	if err := s.Rename(ctx, b.ID, "äRGER"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename onto another's name: %v", err)
	}
	if _, err := s.Duplicate(ctx, b.ID, "launch"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate onto a taken name: %v", err)
	}
	if err := s.Rename(ctx, a.ID, "ÄRGER"); err != nil {
		t.Fatalf("rename to its own name in another case: %v", err)
	}
	if got, _ := s.Describe(ctx, a.ID); got.Name != "ÄRGER" {
		t.Fatalf("renamed %+v", got)
	}
}

// DELETE CASCADES: the collection, its own memberships and every membership
// naming it go, and the collections that lost one are answered.
func TestDeleteCascadesAndAnswersTheCollectionsThatChanged(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "delete.db"))
	p1, p2, child, other := createTestCollection(t, s, "P1"), createTestCollection(t, s, "P2"), createTestCollection(t, s, "Child"), createTestCollection(t, s, "Other")
	for _, parent := range []Collection{p1, p2} {
		if err := s.Add(ctx, parent.ID, Ref{Kind: CollectionKind, ID: child.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(ctx, child.ID, Ref{Kind: ConversationKind, ID: "chat"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, other.ID, Ref{Kind: ConversationKind, ID: "chat"}); err != nil {
		t.Fatal(err)
	}
	parents, err := s.Delete(ctx, child.ID)
	if err != nil || !reflect.DeepEqual(parents, []string{p1.ID, p2.ID}) {
		t.Fatalf("parents %v, %v", parents, err)
	}
	var rows int
	if err := s.db.QueryRow("SELECT count(*) FROM memberships WHERE collection_id=? OR ref_id=?", child.ID, child.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("%d memberships outlived their collection: %v", rows, err)
	}
	if got, _ := s.Members(ctx, other.ID); len(got) != 1 {
		t.Fatalf("another collection's reference to the same record went: %v", got)
	}
	if _, err := s.Delete(ctx, child.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

// A DUPLICATE HOLDS THE SAME REFERENCES with the same added_at in the same
// order, and is a collection of its own.
func TestDuplicateCopiesMembershipsWithTheirMoments(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "dup.db"))
	src, nested := createTestCollection(t, s, "Source"), createTestCollection(t, s, "Nested")
	refs := []Ref{{Kind: ConversationKind, ID: "chat"}, {Kind: CollectionKind, ID: nested.ID}, {Kind: TaskKind, ID: "2", SessionID: "chat"}}
	for i, ref := range refs {
		fixedClock(s, int64(1000*(i+1)))
		if err := s.Add(ctx, src.ID, ref); err != nil {
			t.Fatal(err)
		}
	}
	fixedClock(s, 9000)
	copy, err := s.Duplicate(ctx, src.ID, "Copy")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Tree(ctx, src.ID, 0, 10)
	b, _ := s.Tree(ctx, copy.ID, 0, 10)
	if !reflect.DeepEqual(a.Members, b.Members) || len(b.Members) != 3 || b.Members[0].AddedAt != 3000 {
		t.Fatalf("source %+v\ncopy %+v", a.Members, b.Members)
	}
	if b.Collection.CreatedAt != 9000 || b.Collection.Count != 3 || b.Collection.LastAddedAt != 3000 {
		t.Fatalf("copy summary %+v", b.Collection)
	}
	if _, err := s.Duplicate(ctx, "missing", "Other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate of nothing: %v", err)
	}
}

// AN ADD IS ALL OR NOTHING: one cycle, one missing collection or one bad
// reference refuses every reference in the call.
func TestAddOfManyIsAtomicAndRefusesACycle(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "add.db"))
	a, b, c := createTestCollection(t, s, "A"), createTestCollection(t, s, "B"), createTestCollection(t, s, "C")
	if err := s.Add(ctx, b.ID, Ref{Kind: CollectionKind, ID: c.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, c.ID, Ref{Kind: CollectionKind, ID: a.ID}); err != nil {
		t.Fatal(err)
	}
	for name, refs := range map[string][]Addition{
		"self":    {{Ref: Ref{Kind: ConversationKind, ID: "chat"}}, {Ref: Ref{Kind: CollectionKind, ID: a.ID}}},
		"deep":    {{Ref: Ref{Kind: ConversationKind, ID: "chat"}}, {Ref: Ref{Kind: CollectionKind, ID: b.ID}}},
		"missing": {{Ref: Ref{Kind: ConversationKind, ID: "chat"}}, {Ref: Ref{Kind: CollectionKind, ID: "nope"}}},
		"invalid": {{Ref: Ref{Kind: ConversationKind, ID: "chat"}}, {Ref: Ref{Kind: ProcessKind, ID: "p1"}}},
	} {
		_, _, err := s.AddRefs(ctx, a.ID, refs)
		want := map[string]error{"self": ErrCycle, "deep": ErrCycle, "missing": ErrNotFound, "invalid": ErrInvalid}[name]
		if !errors.Is(err, want) {
			t.Fatalf("%s: %v", name, err)
		}
		if got, _ := s.Members(ctx, a.ID); len(got) != 0 {
			t.Fatalf("%s left %v behind", name, got)
		}
	}
	added, already, err := s.AddRefs(ctx, a.ID, []Addition{{Ref: Ref{Kind: ConversationKind, ID: "chat"}}, {Ref: Ref{Kind: ConversationKind, ID: "chat"}}, {Ref: Ref{Kind: CrewKind, ID: "crew"}}})
	if err != nil || len(added) != 2 || !reflect.DeepEqual(already, []Ref{{Kind: ConversationKind, ID: "chat"}}) {
		t.Fatalf("added %v already %v: %v", added, already, err)
	}
}

// REMOVE ANSWERS WHAT WAS THERE WITH ITS MOMENT, AND ADDING IT BACK WITH
// THAT MOMENT PUTS IT WHERE IT WAS: the Undo of a removal.
func TestRemoveThenRestoreKeepsThePlace(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "undo.db"))
	c := createTestCollection(t, s, "Inbox")
	for i, id := range []string{"first", "second", "third"} {
		fixedClock(s, int64(100*(i+1)))
		if err := s.Add(ctx, c.ID, Ref{Kind: ConversationKind, ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := s.RemoveRefs(ctx, c.ID, []Ref{{Kind: ConversationKind, ID: "second"}, {Kind: ConversationKind, ID: "never"}})
	if err != nil || !reflect.DeepEqual(removed, []Member{{Ref: Ref{Kind: ConversationKind, ID: "second"}, AddedAt: 200}}) {
		t.Fatalf("removed %v, %v", removed, err)
	}
	fixedClock(s, 999)
	added, _, err := s.AddRefs(ctx, c.ID, []Addition{{Ref: removed[0].Ref, AddedAt: removed[0].AddedAt}})
	if err != nil || added[0].AddedAt != 200 {
		t.Fatalf("restore %v, %v", added, err)
	}
	tree, _ := s.Tree(ctx, c.ID, 0, 10)
	var order []string
	for _, n := range tree.Members {
		order = append(order, n.ID)
	}
	if !reflect.DeepEqual(order, []string{"third", "second", "first"}) {
		t.Fatalf("restored order %v", order)
	}
}

// AN EXPANSION is every live-shaped reference once, at its outermost place,
// to its depth, reading each nested collection once.
func TestExpandDedupesAndStopsAtItsDepth(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "expand.db"))
	root, l1, l2, l3 := createTestCollection(t, s, "Root"), createTestCollection(t, s, "L1"), createTestCollection(t, s, "L2"), createTestCollection(t, s, "L3")
	add := func(c Collection, refs ...Ref) {
		t.Helper()
		for _, ref := range refs {
			if err := s.Add(ctx, c.ID, ref); err != nil {
				t.Fatal(err)
			}
		}
	}
	shared := Ref{Kind: ConversationKind, ID: "shared"}
	add(root, shared, Ref{Kind: CollectionKind, ID: l1.ID}, Ref{Kind: CollectionKind, ID: l2.ID})
	add(l1, shared, Ref{Kind: ArtifactKind, ID: "/a"}, Ref{Kind: CollectionKind, ID: l2.ID})
	add(l2, Ref{Kind: TaskKind, ID: "1", SessionID: "x"}, Ref{Kind: CollectionKind, ID: l3.ID})
	add(l3, Ref{Kind: CrewKind, ID: "deep"})
	keys := func(e Expansion) []string {
		var out []string
		for _, m := range e.Refs {
			out = append(out, m.ID)
		}
		return out
	}
	e, err := s.Expand(ctx, root.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(e); !reflect.DeepEqual(got, []string{"shared", "1", "/a", "deep"}) || e.Collections != 3 || e.More {
		t.Fatalf("depth 3: %v, %d collections, more %v", got, e.Collections, e.More)
	}
	e, _ = s.Expand(ctx, root.ID, 1)
	if got := keys(e); !reflect.DeepEqual(got, []string{"shared"}) || e.Collections != 0 {
		t.Fatalf("depth 1: %v %d", got, e.Collections)
	}
	e, _ = s.Expand(ctx, root.ID, 2)
	if got := keys(e); !reflect.DeepEqual(got, []string{"shared", "1", "/a"}) || e.Collections != 2 {
		t.Fatalf("depth 2: %v %d", got, e.Collections)
	}
	if _, err := s.Expand(ctx, "missing", 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

// A TREE IS BOUNDED: limit per level with more said, depth honoured, and the
// node cap ends it whatever the file holds.
func TestTreeHonoursLimitDepthAndTheNodeCap(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "tree.db"))
	root, mid, leaf := createTestCollection(t, s, "Root"), createTestCollection(t, s, "Mid"), createTestCollection(t, s, "Leaf")
	if err := s.Add(ctx, root.ID, Ref{Kind: CollectionKind, ID: mid.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, mid.ID, Ref{Kind: CollectionKind, ID: leaf.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, leaf.ID, Ref{Kind: ConversationKind, ID: "x"}); err != nil {
		t.Fatal(err)
	}
	many := make([]Addition, 0, 1700)
	for i := 0; i < 1700; i++ {
		many = append(many, Addition{Ref: Ref{Kind: ConversationKind, ID: fmt.Sprintf("chat-%04d", i)}, AddedAt: int64(i + 1)})
	}
	if _, _, err := s.AddRefs(ctx, mid.ID, many); err != nil {
		t.Fatal(err)
	}
	tree, err := s.Tree(ctx, root.ID, 0, 500)
	if err != nil || len(tree.Members) != 1 || tree.Members[0].Children != nil || tree.Members[0].Count != 1701 || tree.Members[0].Name != "Mid" {
		t.Fatalf("depth 0 %+v, %v", tree.Members, err)
	}
	tree, _ = s.Tree(ctx, root.ID, 1, 2)
	kids := tree.Members[0].Children
	if kids == nil || len(kids.Members) != 2 || !kids.More || kids.Members[0].ID != leaf.ID || kids.Members[1].ID != "chat-1699" {
		t.Fatalf("depth 1 limit 2 %+v", kids)
	}
	tree, _ = s.Tree(ctx, root.ID, 2, 500)
	total := 0
	var count func(l *Level)
	count = func(l *Level) {
		for _, n := range l.Members {
			total++
			if n.Children != nil {
				count(n.Children)
			}
		}
	}
	count(&tree.Level)
	if total > TreeNodeMax {
		t.Fatalf("%d nodes, past the cap", total)
	}
	// A cycle written by hand (no door writes one) is not followed.
	if _, err := s.db.Exec("INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection) VALUES (?,?,?,?,?)", leaf.ID, "collection", root.ID, "", root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tree(ctx, root.ID, 2, 1); err != nil {
		t.Fatal(err)
	}
	if e, err := s.Expand(ctx, root.ID, 3); err != nil || e.Collections != 2 {
		t.Fatalf("expand through a cycle %+v, %v", e, err)
	}
}

// A process carries its conversation like a task; a crew carries nothing.
func TestProcessAndCrewReferencesValidate(t *testing.T) {
	for _, tc := range []struct {
		ref Ref
		ok  bool
	}{
		{Ref{Kind: ProcessKind, ID: "chat:p1.123", SessionID: "chat"}, true},
		{Ref{Kind: ProcessKind, ID: "chat:p1"}, false},
		{Ref{Kind: CrewKind, ID: "crew-1"}, true},
		{Ref{Kind: CrewKind, ID: "crew-1", SessionID: "chat"}, false},
	} {
		if err := tc.ref.Validate(); (err == nil) != tc.ok {
			t.Errorf("%+v: %v", tc.ref, err)
		}
	}
}
