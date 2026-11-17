package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

// bulkCollections files n collections in one transaction, named "<prefix> i",
// with nothing in them; the store's own Create is proved elsewhere, and a
// thousand fsyncs prove nothing more.
func bulkCollections(t *testing.T, s *Store, prefix string, n int) []string {
	t.Helper()
	if err := s.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%05d", prefix, i)
		if _, err := tx.Exec("INSERT INTO collections(id,name,created_at) VALUES (?,?,1)", ids[i], fmt.Sprintf("%s %d", prefix, i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// bulkMembers files one conversation reference per moment given, in that
// order, so ties and unknown (0) moments can be laid down on purpose.
func bulkMembers(t *testing.T, s *Store, id, prefix string, moments []int64) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i, at := range moments {
		if _, err := tx.Exec("INSERT INTO memberships(collection_id,kind,ref_id,session_id,added_at) VALUES (?,?,?,?,?)",
			id, ConversationKind, fmt.Sprintf("%s-%05d", prefix, i), "", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func summaryIDs(list []Summary) []string {
	out := make([]string, len(list))
	for i, c := range list {
		out[i] = c.ID
	}
	return out
}

func nodeIDs(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}

// walkList reads every page of a listing at limit, calling between after
// each page, and answers the ids in the order read.
func walkList(t *testing.T, s *Store, limit int, between func(page int)) []string {
	t.Helper()
	var ids []string
	after := ""
	for page := 0; ; page++ {
		p, err := s.ListPage(context.Background(), nil, after, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Collections) > max(limit, 0) && limit > 0 {
			t.Fatalf("page %d answered %d, more than %d", page, len(p.Collections), limit)
		}
		ids = append(ids, summaryIDs(p.Collections)...)
		if p.Next == "" {
			return ids
		}
		if between != nil {
			between(page)
		}
		after = p.Next
		if page > 10000 {
			t.Fatal("the walk did not end")
		}
	}
}

// A LISTING IS NEVER CUT SILENTLY: more than ListMax collections are all
// reachable, a page that stops short says where the next one starts, total
// counts them all, and pages walked at any size equal one read of the lot.
func TestListPagesReachEveryCollection(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "list.db"))
	ids := bulkCollections(t, s, "c", ListMax+205)
	first, err := s.ListPage(ctx, nil, "", 0)
	if err != nil || len(first.Collections) != ListMax || first.Next == "" || first.Total != len(ids) {
		t.Fatalf("first page %d, next %q, total %d, %v", len(first.Collections), first.Next, first.Total, err)
	}
	second, err := s.ListPage(ctx, nil, first.Next, ListMax)
	if err != nil || len(second.Collections) != 205 || second.Next != "" || second.Total != len(ids) {
		t.Fatalf("second page %d, next %q, %v", len(second.Collections), second.Next, err)
	}
	all, err := s.List(ctx, nil)
	if err != nil || !reflect.DeepEqual(summaryIDs(all), ids) {
		t.Fatalf("List read %d of %d, %v", len(all), len(ids), err)
	}
	for _, limit := range []int{1, 7, 500, ListMax} {
		if got := walkList(t, s, limit, nil); !reflect.DeepEqual(got, ids) {
			t.Fatalf("pages of %d read %d of %d", limit, len(got), len(ids))
		}
	}
	// A page that ends exactly at the end has no next.
	exact, err := s.ListPage(ctx, nil, "", len(ids)-ListMax)
	if err != nil {
		t.Fatal(err)
	}
	if last, err := s.ListPage(ctx, nil, encodeCursor(cursor{K: listCursor, S: exact.Collections[len(exact.Collections)-1].seq}), ListMax); err != nil || last.Next != "" {
		t.Fatalf("last page %+v, %v", last.Next, err)
	}
}

// A WALK UNDER CONCURRENT CREATES AND DELETES NEVER REPEATS OR SKIPS A
// COLLECTION THAT WAS THERE WHEN IT BEGAN: every one not deleted is read
// exactly once, a deleted one not yet read is not read, and one created
// during the walk, if read at all, is read after every pre-existing one.
func TestListPagesAreStableUnderConcurrentCreatesAndDeletes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "list.db")
	s, peer := openTestStore(t, path), openTestStore(t, path)
	ids := bulkCollections(t, s, "c", 300)
	deleted := map[string]bool{}
	created := map[string]bool{}
	got := walkList(t, s, 25, func(page int) {
		// Delete one already read and one not yet read, and create two.
		for _, id := range []string{ids[page*25], ids[min(len(ids)-1, page*25+40)]} {
			if !deleted[id] {
				if _, err := peer.Delete(ctx, id); err != nil {
					t.Fatal(err)
				}
				deleted[id] = true
			}
		}
		for i := 0; i < 2; i++ {
			c, err := peer.Create(ctx, fmt.Sprintf("new %d %d", page, i))
			if err != nil {
				t.Fatal(err)
			}
			created[c.ID] = true
		}
	})
	seen := map[string]int{}
	lastOld := -1
	for i, id := range got {
		seen[id]++
		if !created[id] {
			lastOld = i
		}
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("%s read %d times", id, n)
		}
	}
	for i, id := range ids {
		if seen[id] == 0 && !deleted[id] {
			t.Fatalf("pre-existing collection %d (%s) was skipped", i, id)
		}
	}
	for i, id := range got {
		if created[id] && i < lastOld {
			t.Fatalf("a collection created during the walk was read before a pre-existing one")
		}
	}
}

// walkTree reads every page of a collection's top level at limit, calling
// between after each page, and answers the ids in the order read.
func walkTree(t *testing.T, s *Store, id string, limit int, between func(page int)) []string {
	t.Helper()
	var ids []string
	after := ""
	for page := 0; ; page++ {
		tree, err := s.Tree(context.Background(), id, 0, limit, after)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, nodeIDs(tree.Members)...)
		if tree.Next == "" {
			if tree.More {
				t.Fatal("a last page says more")
			}
			return ids
		}
		if !tree.More {
			t.Fatal("a page with a next does not say more")
		}
		if between != nil {
			between(page)
		}
		after = tree.Next
		if page > 10000 {
			t.Fatal("the walk did not end")
		}
	}
}

// AN OPENED COLLECTION'S PAGES EQUAL ONE READ IN ITS ORDER: newest added
// first, ties broken newest inserted first, rows with no moment (0, a
// migrated schema-1 row) last, and more than LevelMax members all reachable.
func TestOpenPagesWalkTheWholeOrder(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "open.db"))
	c := createTestCollection(t, s, "Big")
	moments := make([]int64, 0, 1200)
	for i := 0; i < 1200; i++ {
		switch {
		case i < 100:
			moments = append(moments, 0) // unknown: last of all
		case i%3 == 0:
			moments = append(moments, 5000) // a long run of ties
		default:
			moments = append(moments, int64(1000+i))
		}
	}
	bulkMembers(t, s, c.ID, "m", moments)
	rows, err := s.db.Query("SELECT ref_id FROM memberships WHERE collection_id=? ORDER BY added_at DESC, seq DESC", c.ID)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	rows.Close()
	if want[len(want)-1] != "m-00000" {
		t.Fatalf("the order the test expects is wrong: %v", want[len(want)-1])
	}
	for _, limit := range []int{1, 13, LevelMax} {
		if got := walkTree(t, s, c.ID, limit, nil); !reflect.DeepEqual(got, want) {
			t.Fatalf("pages of %d read %d of %d", limit, len(got), len(want))
		}
	}
}

// AN OPENED COLLECTION'S WALK IS STABLE UNDER CONCURRENT CHANGE: members
// added during the walk are newer than any cursor and never appear on a
// later page, a member removed before its page is not read, and every other
// pre-existing member is read exactly once.
func TestOpenPagesAreStableUnderConcurrentAddsAndRemoves(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "open.db")
	s, peer := openTestStore(t, path), openTestStore(t, path)
	c := createTestCollection(t, s, "Busy")
	moments := make([]int64, 600)
	for i := range moments {
		moments[i] = int64(1000 + i/2) // pairs of ties
	}
	bulkMembers(t, s, c.ID, "m", moments)
	fixedClock(peer, 1_000_000)
	removed, added := map[string]bool{}, map[string]bool{}
	got := walkTree(t, s, c.ID, 40, func(page int) {
		id := fmt.Sprintf("new-%d", page)
		if err := peer.Add(ctx, c.ID, Ref{Kind: ConversationKind, ID: id}); err != nil {
			t.Fatal(err)
		}
		added[id] = true
		// The member 30 places past this page's end in the read order.
		victim := fmt.Sprintf("m-%05d", 599-(page*40+40+30))
		if page*40+70 < 600 {
			if err := peer.Remove(ctx, c.ID, Ref{Kind: ConversationKind, ID: victim}); err != nil {
				t.Fatal(err)
			}
			removed[victim] = true
		}
	})
	seen := map[string]int{}
	for _, id := range got {
		if added[id] {
			t.Fatalf("%s was added during the walk and appeared on a later page", id)
		}
		seen[id]++
	}
	for i := 0; i < 600; i++ {
		id := fmt.Sprintf("m-%05d", i)
		switch {
		case removed[id] && seen[id] != 0:
			t.Fatalf("%s was removed before its page and still read", id)
		case !removed[id] && seen[id] != 1:
			t.Fatalf("%s read %d times", id, seen[id])
		}
	}
}

// A CURSOR THIS STORE DID NOT GIVE IS REFUSED, NEVER GUESSED AT: garbage, a
// listing's cursor spent on a collection, one collection's cursor spent on
// another, and an opened collection's spent on the listing.
func TestForeignAndMalformedCursorsAreRefused(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "cursor.db"))
	a, b := createTestCollection(t, s, "A"), createTestCollection(t, s, "B")
	bulkMembers(t, s, a.ID, "m", []int64{1, 2, 3})
	tree, err := s.Tree(ctx, a.ID, 0, 1, "")
	if err != nil || tree.Next == "" {
		t.Fatalf("tree %+v, %v", tree, err)
	}
	page, err := s.ListPage(ctx, nil, "", 1)
	if err != nil || page.Next == "" {
		t.Fatalf("page %+v, %v", page, err)
	}
	for name, try := range map[string]func() error{
		"garbage list":         func() error { _, err := s.ListPage(ctx, nil, "!!not a cursor", 1); return err },
		"garbage open":         func() error { _, err := s.Tree(ctx, a.ID, 0, 1, "e30"); return err },
		"list cursor on open":  func() error { _, err := s.Tree(ctx, a.ID, 0, 1, page.Next); return err },
		"open cursor on list":  func() error { _, err := s.ListPage(ctx, nil, tree.Next, 1); return err },
		"another collection's": func() error { _, err := s.Tree(ctx, b.ID, 0, 1, tree.Next); return err },
		"trailing bytes": func() error {
			_, err := s.ListPage(ctx, nil, encodeCursor(cursor{K: listCursor, S: 1})+"x", 1)
			return err
		},
	} {
		if err := try(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// THE NODE BUDGET NEVER STRANDS THE TOP LEVEL: when it cuts the lists below,
// the tree says more, each cut or unopened child list says more, and the top
// level's cursor still continues exactly after its own last row.
func TestTheNodeBudgetCutsChildrenButTheTopLevelContinues(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "budget.db"))
	root := createTestCollection(t, s, "Root")
	kids := bulkCollections(t, s, "k", LevelMax)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i, kid := range kids {
		// Children newest, so the first page is all collections.
		if _, err := tx.Exec("INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection,added_at) VALUES (?,?,?,?,?,?)",
			root.ID, CollectionKind, kid, "", kid, 10_000+i); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 4; j++ {
			if _, err := tx.Exec("INSERT INTO memberships(collection_id,kind,ref_id,session_id,added_at) VALUES (?,?,?,?,?)",
				kid, ConversationKind, fmt.Sprintf("chat-%d-%d", i, j), "", 1+j); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	bulkMembers(t, s, root.ID, "older", []int64{5, 4, 3, 2, 1})
	tree, err := s.Tree(ctx, root.ID, 1, LevelMax, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Members) != LevelMax || !tree.More || tree.Next == "" {
		t.Fatalf("first page %d, more %v, next %q", len(tree.Members), tree.More, tree.Next)
	}
	nodes, opened, unopened := LevelMax, 0, 0
	for _, n := range tree.Members {
		if n.Children == nil {
			t.Fatalf("%s within depth carries no children at all", n.ID)
		}
		nodes += len(n.Children.Members)
		if len(n.Children.Members) == 4 && !n.Children.More {
			opened++
		} else if !n.Children.More {
			t.Fatalf("%s's cut list does not say more: %+v", n.ID, n.Children)
		} else {
			unopened++
		}
	}
	if nodes > TreeNodeMax || opened == 0 || unopened == 0 {
		t.Fatalf("%d nodes, %d opened in full, %d cut", nodes, opened, unopened)
	}
	next, err := s.Tree(ctx, root.ID, 1, LevelMax, tree.Next)
	if err != nil || next.Next != "" || next.More || !reflect.DeepEqual(nodeIDs(next.Members), []string{"older-00000", "older-00001", "older-00002", "older-00003", "older-00004"}) {
		t.Fatalf("second page %v, more %v, next %q, %v", nodeIDs(next.Members), next.More, next.Next, err)
	}
}

// EXPANSION COUNTS WHAT IT DOES NOT ANSWER: a nested collection past the
// depth is unread, each once, and a nested reference to a collection that is
// gone is deleted. Cascading deletes mean only a damaged or foreign-written
// file can hold the latter, so the test writes one with foreign keys off.
func TestExpandCountsUnreadAndDeletedNestedCollections(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "expand.db"))
	root, a, b, c := createTestCollection(t, s, "Root"), createTestCollection(t, s, "A"), createTestCollection(t, s, "B"), createTestCollection(t, s, "C")
	for _, edge := range [][2]string{{root.ID, a.ID}, {a.ID, b.ID}, {b.ID, c.ID}, {root.ID, b.ID}} {
		if err := s.Add(ctx, edge[0], Ref{Kind: CollectionKind, ID: edge[1]}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(ctx, c.ID, Ref{Kind: ConversationKind, ID: "deep"}); err != nil {
		t.Fatal(err)
	}
	for depth, want := range map[int]int{1: 2, 2: 1, 3: 0} {
		e, err := s.Expand(ctx, root.ID, depth)
		if err != nil || e.Unread != want || e.Deleted != 0 {
			t.Fatalf("depth %d: unread %d deleted %d, want %d unread, %v", depth, e.Unread, e.Deleted, want, err)
		}
	}
	if _, err := s.db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection,added_at) VALUES (?,?,?,?,?,1)",
		a.ID, CollectionKind, "gone", "", "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if e, err := s.Expand(ctx, root.ID, 3); err != nil || e.Deleted != 1 || e.Unread != 0 || len(e.Refs) != 1 {
		t.Fatalf("with a gone nested collection %+v, %v", e, err)
	}
}

// RELINK PUTS ONE REFERENCE IN ANOTHER'S PLACE IN ONE WRITE: the new one
// keeps the old one's moment and place; one already there just loses the
// old; a non-member is ErrNotMember; a collection is checked as Add checks
// one; relinking a member to itself changes nothing.
func TestRelinkKeepsThePlaceAndRefusesWhatAddRefuses(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "relink.db"))
	box, parent, child := createTestCollection(t, s, "Box"), createTestCollection(t, s, "Parent"), createTestCollection(t, s, "Child")
	old, other := Ref{Kind: ArtifactKind, ID: "/old/report.md"}, Ref{Kind: ConversationKind, ID: "chat"}
	for i, ref := range []Ref{other, old, {Kind: ConversationKind, ID: "last"}} {
		fixedClock(s, int64(1000*(i+1)))
		if err := s.Add(ctx, box.ID, ref); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := s.Tree(ctx, box.ID, 0, 10, "")
	moved := Ref{Kind: ArtifactKind, ID: "/new/report.md"}
	m, already, err := s.Relink(ctx, box.ID, old, moved)
	if err != nil || already || m.Ref != moved || m.AddedAt != 2000 {
		t.Fatalf("relink %+v %v %v", m, already, err)
	}
	after, _ := s.Tree(ctx, box.ID, 0, 10, "")
	if after.Members[1].Ref != moved || after.Members[1].AddedAt != 2000 || after.Members[1].seq != before.Members[1].seq {
		t.Fatalf("the relinked member moved: %+v", after.Members)
	}
	if members, _ := s.Members(ctx, box.ID); members[1] != moved {
		t.Fatalf("insertion order moved: %v", members)
	}
	if _, _, err := s.Relink(ctx, box.ID, old, moved); !errors.Is(err, ErrNotMember) {
		t.Fatalf("relink of a non-member: %v", err)
	}
	m, already, err = s.Relink(ctx, box.ID, moved, other)
	if err != nil || !already || m.Ref != other || m.AddedAt != 1000 {
		t.Fatalf("relink onto a member %+v %v %v", m, already, err)
	}
	if members, _ := s.Members(ctx, box.ID); len(members) != 2 {
		t.Fatalf("relink onto a member left %v", members)
	}
	if m, already, err := s.Relink(ctx, box.ID, other, other); err != nil || !already || m.AddedAt != 1000 {
		t.Fatalf("relink to itself %+v %v %v", m, already, err)
	}
	if err := s.Add(ctx, child.ID, Ref{Kind: CollectionKind, ID: parent.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, parent.ID, Ref{Kind: ConversationKind, ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Relink(ctx, parent.ID, Ref{Kind: ConversationKind, ID: "x"}, Ref{Kind: CollectionKind, ID: child.ID}); !errors.Is(err, ErrCycle) {
		t.Fatalf("relink making a cycle: %v", err)
	}
	if _, _, err := s.Relink(ctx, parent.ID, Ref{Kind: ConversationKind, ID: "x"}, Ref{Kind: CollectionKind, ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("relink to a collection that is not there: %v", err)
	}
	if members, _ := s.Members(ctx, parent.ID); len(members) != 1 || members[0].ID != "x" || hasCycle(t, s) {
		t.Fatalf("a refused relink changed %v", members)
	}
	if _, _, err := s.Relink(ctx, "nope", other, moved); !errors.Is(err, ErrNotFound) {
		t.Fatalf("relink in no collection: %v", err)
	}
}
