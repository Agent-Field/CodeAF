package placegraph

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestVirtualRootAndNowSemantics(t *testing.T) {
	s, _ := newStore(t)
	top, _, err := s.CreatePlace(NewPlace{Name: "Top", Parents: []string{RootID}})
	if err != nil || len(top.Parents) != 0 {
		t.Fatalf("explicit root parent should normalise to top-level: %v %v", top.Parents, err)
	}
	if _, _, err := s.CreatePlace(NewPlace{Name: "X", Parents: []string{NowID}}); !errors.Is(err, ErrReservedID) {
		t.Fatalf("now as parent = %v", err)
	}
	if _, err := s.Rename(RootID, "Everything"); !errors.Is(err, ErrReservedID) {
		t.Fatalf("rename root = %v", err)
	}
	if _, err := s.Archive(NowID); !errors.Is(err, ErrReservedID) {
		t.Fatalf("archive now = %v", err)
	}
	sn := snap(t, s)
	if got := ids(sn.Children(RootID, false)); !eq(got, []string{top.ID}) {
		t.Fatalf("root children = %v", got)
	}
	for _, v := range []string{RootID, NowID} {
		if tint, ok := sn.EffectiveTint(v); !ok || tint != TintGraphite {
			t.Fatalf("%s tint = %v %v", v, tint, ok)
		}
	}
	if _, ok := sn.EffectiveTint("pl_nope"); ok {
		t.Fatal("unknown place reported a tint")
	}
}

func TestIDsAreStableAcrossRenameAndMove(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	b := mk(t, s, "B")
	ok(t)(s.Rename(a.ID, "Renamed"))
	ok(t)(s.AddParent(a.ID, b.ID))
	got, found := snap(t, s).Place(a.ID)
	if !found || got.Name != "Renamed" {
		t.Fatal("id no longer finds the place after rename and reparent")
	}
	c := mk(t, s, "C")
	if _, _, err := s.DeletePlace(c.ID); err != nil {
		t.Fatal(err)
	}
	if d := mk(t, s, "D"); d.ID == c.ID {
		t.Fatal("a deleted place's id was reused")
	}
}

func TestCyclesAreRefused(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	b := mk(t, s, "B", a.ID)
	c := mk(t, s, "C", b.ID)
	cases := map[string]func() error{
		"self parent":       func() error { _, err := s.AddParent(a.ID, a.ID); return err },
		"direct":            func() error { _, err := s.AddParent(a.ID, b.ID); return err },
		"indirect":          func() error { _, err := s.AddParent(a.ID, c.ID); return err },
		"reparent":          func() error { _, err := s.Reparent(a.ID, []string{c.ID}); return err },
		"create under self": nil,
	}
	delete(cases, "create under self")
	for name, fn := range cases {
		if err := fn(); !errors.Is(err, ErrCycle) {
			t.Errorf("%s: err = %v, want ErrCycle", name, err)
		}
	}
	rev, _ := s.Revision()
	if got, _ := snap(t, s).Place(a.ID); len(got.Parents) != 0 {
		t.Fatalf("a refused cycle changed parents: %v", got.Parents)
	}
	if after, _ := s.Revision(); after != rev {
		t.Fatal("a refused cycle bumped the revision")
	}
	// A sibling edge that is NOT a cycle still works.
	d := mk(t, s, "D")
	if _, err := s.AddParent(c.ID, d.ID); err != nil {
		t.Fatalf("legal second parent: %v", err)
	}
}

func TestDuplicateEdgesAreDeduplicated(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	b, _, err := s.CreatePlace(NewPlace{Name: "B", Parents: []string{a.ID, a.ID, RootID}})
	if err != nil || !eq(b.Parents, []string{a.ID}) {
		t.Fatalf("parents = %v (%v)", b.Parents, err)
	}
	if rc, err := s.AddParent(b.ID, a.ID); err != nil || !rc.Noop() {
		t.Fatalf("re-adding an existing parent: %v %+v", err, rc)
	}
	if rc, err := s.RemoveParent(b.ID, "pl_not_a_parent"); err != nil || !rc.Noop() {
		t.Fatalf("removing a non-parent: %v %+v", err, rc)
	}
}

// diamond builds   Top → Left, Right → Shared   with Shared's parents [Left, Right].
func diamond(t *testing.T, s *Store) (top, left, right, shared Place) {
	t.Helper()
	top = mk(t, s, "Top")
	left = mk(t, s, "Left", top.ID)
	right = mk(t, s, "Right", top.ID)
	shared = mk(t, s, "Shared", left.ID, right.ID)
	return
}

func TestMultiParentDiamond(t *testing.T) {
	s, _ := newStore(t)
	top, left, right, shared := diamond(t, s)
	if _, _, err := s.AddChat("only-shared", shared.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChat("both", left.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChat("both", right.ID, AddedByAI); err != nil {
		t.Fatal(err)
	}
	sn := snap(t, s)

	desc := ids(sn.Descendants(top.ID, false))
	if len(desc) != 3 || strings.Count(strings.Join(desc, ","), shared.ID) != 1 {
		t.Fatalf("descendants = %v (shared must appear once)", desc)
	}
	if got := sn.ChatsIn(top.ID, true); len(got) != 2 {
		t.Fatalf("chats under top = %v, each chat once", got)
	}
	if c := sn.Counts(top.ID); c.Descendants != 3 || c.Children != 2 || c.ChatsInclusive != 2 || c.Chats != 0 {
		t.Fatalf("counts = %+v", c)
	}
	// Level semantics: Top is a grandparent of Shared through two routes.
	anc := sn.ContextAncestors(shared.ID)
	want := map[string]int{left.ID: 1, right.ID: 1, top.ID: 2}
	if len(anc) != 3 {
		t.Fatalf("ancestors = %+v", anc)
	}
	for _, a := range anc {
		if want[a.PlaceID] != a.Level {
			t.Fatalf("ancestor %s at level %d, want %d", a.PlaceID, a.Level, want[a.PlaceID])
		}
	}
	if bc := ids(sn.Breadcrumb(shared.ID)); !eq(bc, []string{top.ID, left.ID}) {
		t.Fatalf("breadcrumb = %v (first-parent chain)", bc)
	}
}

func TestTintInheritsFromTheFirstParentAndNeverBlends(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	ok(t)(s.SetTint(top.ID, TintTide))
	other := mk(t, s, "Other")
	ok(t)(s.SetTint(other.ID, TintRose))
	child := mk(t, s, "Child", top.ID, other.ID)

	tint := func(id string) Tint { v, _ := snap(t, s).EffectiveTint(id); return v }
	if tint(child.ID) != TintTide {
		t.Fatalf("child = %s, want tide (first parent)", tint(child.ID))
	}
	ok(t)(s.Reparent(child.ID, []string{other.ID, top.ID}))
	if tint(child.ID) != TintRose {
		t.Fatalf("after reorder = %s, want rose", tint(child.ID))
	}
	// A child override wins, and its own children follow IT.
	ok(t)(s.SetTint(child.ID, TintSage))
	grand := mk(t, s, "Grand", child.ID)
	if tint(child.ID) != TintSage || tint(grand.ID) != TintSage {
		t.Fatalf("override chain: child %s grand %s", tint(child.ID), tint(grand.ID))
	}
	// Clearing the override goes back to inheriting.
	ok(t)(s.SetTint(child.ID, ""))
	if tint(grand.ID) != TintRose {
		t.Fatalf("after clearing = %s", tint(grand.ID))
	}
	// Changing the family's tint moves every inheriting descendant, in one place.
	ok(t)(s.SetTint(other.ID, TintIris))
	if tint(child.ID) != TintIris || tint(grand.ID) != TintIris {
		t.Fatalf("family recolour: %s %s", tint(child.ID), tint(grand.ID))
	}
	if _, err := s.SetTint(top.ID, "puce"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad tint = %v", err)
	}
}

func TestTopLevelPlacesGetTheLeastUsedTint(t *testing.T) {
	s, _ := newStore(t)
	var got []Tint
	for i := 0; i < 7; i++ {
		p := mk(t, s, fmt.Sprintf("P%d", i))
		got = append(got, p.Tint)
	}
	want := []Tint{TintTide, TintIris, TintRose, TintSand, TintSage, TintTide, TintIris}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tints = %v, want %v", got, want)
		}
	}
	// Children never auto-pick: they inherit.
	top, _ := snap(t, s).Place(snap(t, s).Places[0].ID)
	child := mk(t, s, "Kid", top.ID)
	if child.Tint != "" {
		t.Fatalf("child tint = %q, want inherited", child.Tint)
	}
	if v, _ := snap(t, s).EffectiveTint(child.ID); v != TintTide {
		t.Fatalf("inherited = %s", v)
	}
}

func TestMovingOutOfAFamilyKeepsTheColourShown(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	ok(t)(s.SetTint(top.ID, TintSand))
	kid := mk(t, s, "Kid", top.ID)
	ok(t)(s.Reparent(kid.ID, nil))
	got, _ := snap(t, s).Place(kid.ID)
	if got.Tint != TintSand {
		t.Fatalf("tint = %q, want sand pinned when the parent went away", got.Tint)
	}
}

func TestDeletePlaceMovesChildrenUpAndKeepsEveryChat(t *testing.T) {
	s, _ := newStore(t)
	root1 := mk(t, s, "Software")
	ok(t)(s.SetTint(root1.ID, TintTide))
	other := mk(t, s, "Other")
	ok(t)(s.SetTint(other.ID, TintRose))
	mid := mk(t, s, "codeaf", root1.ID, other.ID)
	ok(t)(s.SetTint(mid.ID, TintSage)) // its own colour
	inherit := mk(t, s, "Parser", mid.ID)
	multi := mk(t, s, "Release", mid.ID, other.ID)
	ok(t)(mustPin(s, mid.ID))
	for _, c := range []string{"c1", "c2"} {
		if _, _, err := s.AddChat(c, mid.ID, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.AddChat("c2", other.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}

	imp, err := snap(t, s).PreviewDelete(mid.ID)
	if err != nil || imp.Children != 2 || imp.ChatsHere != 2 || !eq(imp.WouldBeUnplaced, []string{"c1"}) {
		t.Fatalf("preview = %+v %v", imp, err)
	}
	res, _, err := s.DeletePlace(mid.ID)
	if err != nil || res.ChildrenMoved != 2 || res.Unfiled != 2 || !eq(res.NowUnplaced, []string{"c1"}) {
		t.Fatalf("result = %+v %v", res, err)
	}
	sn := snap(t, s)
	if _, found := sn.Place(mid.ID); found {
		t.Fatal("place still exists")
	}
	p, _ := sn.Place(inherit.ID)
	m, _ := sn.Place(multi.ID)
	// Children take the deleted place's parents, in position.
	if !eq(p.Parents, []string{root1.ID, other.ID}) {
		t.Fatalf("Parser parents = %v", p.Parents)
	}
	if !eq(m.Parents, []string{root1.ID, other.ID}) {
		t.Fatalf("Release parents = %v (de-duplicated, order kept)", m.Parents)
	}
	// The colour they were showing (the deleted place's sage) is pinned, not lost.
	for _, id := range []string{inherit.ID, multi.ID} {
		if v, _ := sn.EffectiveTint(id); v != TintSage {
			t.Fatalf("%s now %s, want sage kept", id, v)
		}
	}
	// Chats survive: c2 keeps Other, c1 is unplaced and still listed by the caller's ids.
	if got := sn.Unplaced([]string{"c1", "c2", "c3"}); !eq(got, []string{"c1", "c3"}) {
		t.Fatalf("unplaced = %v", got)
	}
	if len(sn.PlacesOf("c2")) != 1 || len(sn.Pinned) != 0 {
		t.Fatalf("c2 places %v pinned %v", sn.PlacesOf("c2"), sn.Pinned)
	}
}

func TestDeleteATopLevelPlaceMakesItsChildrenTopLevel(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	ok(t)(s.SetTint(top.ID, TintIris))
	kid := mk(t, s, "Kid", top.ID)
	if _, _, err := s.DeletePlace(top.ID); err != nil {
		t.Fatal(err)
	}
	sn := snap(t, s)
	got, _ := sn.Place(kid.ID)
	if len(got.Parents) != 0 || got.Tint != TintIris {
		t.Fatalf("kid = parents %v tint %q", got.Parents, got.Tint)
	}
	if !eq(ids(sn.Children(RootID, false)), []string{kid.ID}) {
		t.Fatal("kid is not top-level")
	}
}

func TestArchiveRestoreKeepsChatsAndHidesThePlace(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	kid := mk(t, s, "Kid", a.ID)
	ok(t)(mustPin(s, a.ID))
	if _, _, err := s.AddChat("c1", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChat("c2", kid.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	ok(t)(s.Archive(a.ID))
	if rc, _ := s.Archive(a.ID); !rc.Noop() {
		t.Fatal("archiving twice should be a no-op")
	}
	sn := snap(t, s)
	if len(sn.Children(RootID, false)) != 0 || len(sn.Pinned) != 0 {
		t.Fatalf("archived place still listed: %v pinned %v", ids(sn.Children(RootID, false)), sn.Pinned)
	}
	if got := sn.Unplaced([]string{"c1", "c2"}); !eq(got, []string{"c1"}) {
		t.Fatalf("unplaced = %v: c1's only place is archived, c2's is not", got)
	}
	if len(sn.PlacesOf("c1")) != 1 {
		t.Fatal("archiving dropped a membership")
	}
	if got := sn.ChatsIn(a.ID, false); !eq(got, []string{"c1"}) {
		t.Fatalf("archived Home still reads its own chats: %v", got)
	}
	if _, _, err := s.AddChat("c9", a.ID, AddedByYou); !errors.Is(err, ErrArchived) {
		t.Fatalf("filing into an archived place = %v", err)
	}
	if _, _, err := s.CreatePlace(NewPlace{Name: "New", Parents: []string{a.ID}}); !errors.Is(err, ErrArchived) {
		t.Fatalf("creating under an archived place = %v", err)
	}
	if _, err := s.Pin(a.ID, 0); !errors.Is(err, ErrArchived) {
		t.Fatalf("pinning an archived place = %v", err)
	}
	// The child is still reachable through the archived parent.
	if d := ids(sn.Descendants(RootID, false)); !eq(d, []string{kid.ID}) {
		t.Fatalf("descendants = %v", d)
	}
	ok(t)(s.Restore(a.ID))
	sn = snap(t, s)
	if got := sn.Unplaced([]string{"c1", "c2"}); len(got) != 0 {
		t.Fatalf("after restore unplaced = %v", got)
	}
	if len(sn.Pinned) != 0 {
		t.Fatal("restore must not re-pin")
	}
}

func TestSiblingNamesAreUniqueCaseInsensitively(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "Reports")
	b := mk(t, s, "Q3")
	if _, _, err := s.CreatePlace(NewPlace{Name: "reports"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("top-level duplicate = %v", err)
	}
	mk(t, s, "Q3", a.ID) // same name under a different parent is fine
	if _, _, err := s.CreatePlace(NewPlace{Name: "q3", Parents: []string{a.ID}}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("sibling duplicate = %v", err)
	}
	if _, err := s.Rename(b.ID, "REPORTS"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename into a clash = %v", err)
	}
	// Moving b under a would put two "Q3" side by side.
	if _, err := s.Reparent(b.ID, []string{a.ID}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("reparent into a clash = %v", err)
	}
	if !snap(t, s).NameAvailable("Fresh", nil, "") || snap(t, s).NameAvailable("reports", nil, "") {
		t.Fatal("NameAvailable disagrees with the store")
	}
	// An archived place stops holding its name; restoring into a clash is refused.
	ok(t)(s.Archive(a.ID))
	mk(t, s, "Reports")
	if _, err := s.Restore(a.ID); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("restore into a clash = %v", err)
	}
}

func TestContextAncestorsStopAtTwoLevelsAndNearestWins(t *testing.T) {
	s, _ := newStore(t)
	l0 := mk(t, s, "L0")
	l1 := mk(t, s, "L1", l0.ID)
	l2 := mk(t, s, "L2", l1.ID)
	l3 := mk(t, s, "L3", l2.ID)
	l4 := mk(t, s, "L4", l3.ID)
	got := snap(t, s).ContextAncestors(l4.ID)
	if len(got) != 2 || got[0] != (Ancestor{l3.ID, 1}) || got[1] != (Ancestor{l2.ID, 2}) {
		t.Fatalf("ancestors = %+v", got)
	}
	// Parent that is ALSO a grandparent counts once, at level 1.
	x := mk(t, s, "X", l3.ID, l2.ID)
	got = snap(t, s).ContextAncestors(x.ID)
	if len(got) != 3 || got[1] != (Ancestor{l2.ID, 1}) {
		t.Fatalf("ancestors = %+v", got)
	}
	// Archiving the middle place removes it but does not pull l0 into range.
	ok(t)(s.Archive(l3.ID))
	for _, a := range snap(t, s).ContextAncestors(l4.ID) {
		if a.PlaceID == l3.ID || a.PlaceID == l1.ID {
			t.Fatalf("archived or out-of-range ancestor listed: %+v", a)
		}
	}
}

func TestMergeUnionsMembershipsParentsChildrenAndContext(t *testing.T) {
	s, _ := newStore(t)
	sw := mk(t, s, "Software")
	mk2 := mk(t, s, "Marketing")
	into := mk(t, s, "Release", sw.ID)
	from := mk(t, s, "Launch", sw.ID, mk2.ID)
	kid := mk(t, s, "Notes", from.ID)
	ok(t)(s.SetTint(from.ID, TintRose))
	ok(t)(mustPin(s, from.ID))

	ok(t)(s.SetContext(into.ID, Context{Instructions: "Be brief.", Sources: []Source{{Kind: SourceURL, Ref: "https://example.test/a"}}}))
	ok(t)(s.SetContext(from.ID, Context{Instructions: "Write for customers.", Sources: []Source{
		{Kind: SourceURL, Ref: "https://example.test/a"}, {Kind: SourceFile, Ref: "/tmp/brand.md"}}}))
	ok(t)(s.SetPolicy(into.ID, Policy{Model: "pro"}))
	ok(t)(s.SetPolicy(from.ID, Policy{Model: "flash", Permissions: "ask"}))
	for _, m := range []struct{ chat, place string }{{"shared", into.ID}, {"shared", from.ID}, {"only-from", from.ID}, {"only-into", into.ID}} {
		if _, _, err := s.AddChat(m.chat, m.place, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	// "shared" was filed into `into` by the person and into `from` by the AI: the existing row wins.
	if _, _, err := s.AddChat("ai-only", from.ID, AddedByAI); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.MergePlaces(from.ID, into.ID)
	if err != nil {
		t.Fatal(err)
	}
	sn := snap(t, s)
	if _, found := sn.Place(from.ID); found {
		t.Fatal("merged place still exists")
	}
	got, _ := sn.Place(into.ID)
	if !eq(got.Parents, []string{sw.ID, mk2.ID}) {
		t.Fatalf("parents = %v, want union in order", got.Parents)
	}
	if res.ChildrenMoved != 1 || len(res.SkippedParents) != 0 {
		t.Fatalf("result = %+v", res)
	}
	k, _ := sn.Place(kid.ID)
	if !eq(k.Parents, []string{into.ID}) {
		t.Fatalf("child parents = %v", k.Parents)
	}
	if v, _ := sn.EffectiveTint(kid.ID); v != TintRose {
		t.Fatalf("child colour = %s, want the merged place's rose kept", v)
	}
	chats := sn.ChatsIn(into.ID, false)
	if len(chats) != 4 {
		t.Fatalf("chats = %v, want union deduplicated", chats)
	}
	for _, m := range sn.Memberships {
		if m.PlaceID == from.ID {
			t.Fatal("a membership still points at the merged place")
		}
	}
	count := 0
	for _, m := range sn.PlacesOf("shared") {
		count++
		if m.PlaceID != into.ID {
			t.Fatalf("shared in %s", m.PlaceID)
		}
	}
	if count != 1 {
		t.Fatalf("shared has %d rows, want 1", count)
	}
	if got.Context.Instructions != "Be brief.\n\nWrite for customers." || len(got.Context.Sources) != 2 {
		t.Fatalf("context = %+v", got.Context)
	}
	if got.Policy != (Policy{Model: "pro", Permissions: "ask"}) {
		t.Fatalf("policy = %+v (into wins, gaps filled)", got.Policy)
	}
	if !eq(sn.Pinned, []string{into.ID}) {
		t.Fatalf("pinned = %v", sn.Pinned)
	}
}

func TestMergeNeverCreatesACycle(t *testing.T) {
	t.Run("parent below the survivor is skipped", func(t *testing.T) {
		s, _ := newStore(t)
		into := mk(t, s, "Into")
		below := mk(t, s, "Below", into.ID)
		from := mk(t, s, "From", below.ID) // from's parent is below `into`
		res, _, err := s.MergePlaces(from.ID, into.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !eq(res.SkippedParents, []string{below.ID}) {
			t.Fatalf("skipped = %v", res.SkippedParents)
		}
		if _, err := validateState(&snap(t, s).State, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("survivor is a child of the merged place", func(t *testing.T) {
		s, _ := newStore(t)
		gp := mk(t, s, "GP")
		from := mk(t, s, "From", gp.ID)
		into := mk(t, s, "Into", from.ID)
		sib := mk(t, s, "Sib", from.ID)
		if _, _, err := s.MergePlaces(from.ID, into.ID); err != nil {
			t.Fatal(err)
		}
		sn := snap(t, s)
		i, _ := sn.Place(into.ID)
		sb, _ := sn.Place(sib.ID)
		if !eq(i.Parents, []string{gp.ID}) || !eq(sb.Parents, []string{into.ID}) {
			t.Fatalf("into %v sib %v", i.Parents, sb.Parents)
		}
	})
	t.Run("a child of the merged place sits above the survivor", func(t *testing.T) {
		s, _ := newStore(t)
		gp := mk(t, s, "GP")
		from := mk(t, s, "From", gp.ID)
		up := mk(t, s, "Up", from.ID)
		into := mk(t, s, "Into", up.ID) // from → up → into
		if _, _, err := s.MergePlaces(from.ID, into.ID); err != nil {
			t.Fatal(err)
		}
		sn := snap(t, s)
		u, _ := sn.Place(up.ID)
		if !eq(u.Parents, []string{gp.ID}) {
			t.Fatalf("up parents = %v, want from's parents (not `into`, which lives below it)", u.Parents)
		}
		if id := findCycle(&sn.State); id != "" {
			t.Fatalf("cycle through %s", id)
		}
	})
	t.Run("invalid merges", func(t *testing.T) {
		s, _ := newStore(t)
		a := mk(t, s, "A")
		b := mk(t, s, "B")
		if _, _, err := s.MergePlaces(a.ID, a.ID); !errors.Is(err, ErrInvalid) {
			t.Fatalf("self merge = %v", err)
		}
		if _, _, err := s.MergePlaces(a.ID, "pl_nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing target = %v", err)
		}
		ok(t)(s.Archive(b.ID))
		if _, _, err := s.MergePlaces(a.ID, b.ID); !errors.Is(err, ErrArchived) {
			t.Fatalf("archived target = %v", err)
		}
	})
}

func TestContextAndPolicyRoundTrip(t *testing.T) {
	s, path := newStore(t)
	a := mk(t, s, "A")
	ok(t)(s.SetContext(a.ID, Context{Instructions: "Plain prose.", Sources: []Source{{Kind: SourceRepo, Ref: "/work/codeaf", Label: "codeaf"}}}))
	re, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := snap(t, re).Place(a.ID)
	src := got.Context.Sources[0]
	if src.ID == "" || src.AddedBy != AddedByYou || src.At.IsZero() || src.Kind != SourceRepo {
		t.Fatalf("source not completed: %+v", src)
	}
	if rc, err := re.SetContext(a.ID, got.Context); err != nil || !rc.Noop() {
		t.Fatalf("identical context should be a no-op: %v %+v", err, rc)
	}
}

func TestPinOrderAndReorder(t *testing.T) {
	s, _ := newStore(t)
	a, b, c := mk(t, s, "A"), mk(t, s, "B"), mk(t, s, "C")
	for _, p := range []Place{a, b, c} {
		ok(t)(s.Pin(p.ID, -1))
	}
	ok(t)(s.Pin(c.ID, 0)) // reorder: drag to the top
	if got := snap(t, s).Pinned; !eq(got, []string{c.ID, a.ID, b.ID}) {
		t.Fatalf("pinned = %v", got)
	}
	if rc, _ := s.Pin(c.ID, 0); !rc.Noop() {
		t.Fatal("pinning in place should be a no-op")
	}
	ok(t)(s.Unpin(a.ID))
	if got := ids(snap(t, s).PinnedPlaces()); !eq(got, []string{c.ID, b.ID}) {
		t.Fatalf("pinned places = %v", got)
	}
	if rc, _ := s.Unpin(a.ID); !rc.Noop() {
		t.Fatal("unpinning twice should be a no-op")
	}
}

func TestEveryRefusedMutationLeavesTheFileUntouched(t *testing.T) {
	s, path := newStore(t)
	a := mk(t, s, "A")
	b := mk(t, s, "B", a.ID)
	before := readFile(t, path)
	for name, err := range map[string]error{
		"cycle":   second(s.AddParent(a.ID, b.ID)),
		"missing": second(s.Rename("pl_nope", "x")),
		"badname": second(s.Rename(a.ID, "")),
		"badtint": second(s.SetTint(a.ID, "puce")),
	} {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if string(readFile(t, path)) != string(before) {
		t.Fatal("a refused mutation rewrote the file")
	}
}

func second(rc Receipt, err error) error { return err }
