package placegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAddChatDeduplicatesAndRecordsWho(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	m, rc, err := s.AddChat("chat-1", a.ID, AddedByAI)
	if err != nil || rc.Noop() || m.AddedBy != AddedByAI || m.At.IsZero() {
		t.Fatalf("first add: %+v %+v %v", m, rc, err)
	}
	rev, _ := s.Revision()
	again, rc2, err := s.AddChat("chat-1", a.ID, AddedByYou)
	if err != nil || !rc2.Noop() || again != m {
		t.Fatalf("second add: %+v %+v %v (must return the existing row unchanged)", again, rc2, err)
	}
	if after, _ := s.Revision(); after != rev {
		t.Fatal("a duplicate filing bumped the revision")
	}
	if got := len(snap(t, s).Memberships); got != 1 {
		t.Fatalf("memberships = %d", got)
	}
}

func TestAddChatValidation(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	for name, err := range map[string]error{
		"now":       second2(s.AddChat("c", NowID, AddedByYou)),
		"root":      second2(s.AddChat("c", RootID, AddedByYou)),
		"missing":   second2(s.AddChat("c", "pl_nope", AddedByYou)),
		"empty id":  second2(s.AddChat("", a.ID, AddedByYou)),
		"bad actor": second2(s.AddChat("c", a.ID, "robot")),
	} {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, _, err := s.AddChat("c", NowID, AddedByYou); !errors.Is(err, ErrReservedID) {
		t.Fatalf("now = %v", err)
	}
}

func second2(m Membership, rc Receipt, err error) error { return err }

func TestAChatCanLiveInSeveralPlacesButNotWithoutBound(t *testing.T) {
	s, _ := newStore(t)
	var places []Place
	for i := 0; i <= MaxChatPlaces; i++ {
		places = append(places, mk(t, s, fmt.Sprintf("P%d", i)))
	}
	for i := 0; i < MaxChatPlaces; i++ {
		if _, _, err := s.AddChat("busy", places[i].ID, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.AddChat("busy", places[MaxChatPlaces].ID, AddedByYou); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("one place too many = %v", err)
	}
}

func TestMoveChatVariants(t *testing.T) {
	s, _ := newStore(t)
	a, b, c := mk(t, s, "A"), mk(t, s, "B"), mk(t, s, "C")
	in := func(chat string) []string {
		var out []string
		for _, m := range snap(t, s).PlacesOf(chat) {
			out = append(out, m.PlaceID)
		}
		return out
	}
	if _, _, err := s.AddChat("c1", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	// place → place
	ok(t)(s.MoveChat("c1", a.ID, b.ID, AddedByYou))
	if got := in("c1"); !eq(got, []string{b.ID}) {
		t.Fatalf("after move = %v", got)
	}
	// into a place it is already in: just leaves the source
	if _, _, err := s.AddChat("c1", c.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	ok(t)(s.MoveChat("c1", b.ID, c.ID, AddedByYou))
	if got := in("c1"); !eq(got, []string{c.ID}) {
		t.Fatalf("after move onto existing = %v", got)
	}
	// place → Now unfiles
	ok(t)(s.MoveChat("c1", c.ID, NowID, AddedByYou))
	if got := in("c1"); len(got) != 0 {
		t.Fatalf("after move to Now = %v", got)
	}
	if got := snap(t, s).Unplaced([]string{"c1"}); !eq(got, []string{"c1"}) {
		t.Fatalf("unplaced = %v", got)
	}
	// Now → place files
	ok(t)(s.MoveChat("c1", NowID, a.ID, AddedByAI))
	if got := in("c1"); !eq(got, []string{a.ID}) {
		t.Fatalf("after file from Now = %v", got)
	}
	// a source the chat is not in is an error, and nothing changes
	rev, _ := s.Revision()
	if _, err := s.MoveChat("c1", b.ID, c.ID, AddedByYou); !errors.Is(err, ErrNotFound) {
		t.Fatalf("move from a stranger place = %v", err)
	}
	// an archived destination is refused BEFORE the source is released
	ok(t)(s.Archive(c.ID))
	if _, err := s.MoveChat("c1", a.ID, c.ID, AddedByYou); !errors.Is(err, ErrArchived) {
		t.Fatalf("move into archived = %v", err)
	}
	if got := in("c1"); !eq(got, []string{a.ID}) {
		t.Fatalf("a refused move released the source: %v", got)
	}
	if after, _ := s.Revision(); after != rev+1 { // only the archive
		t.Fatalf("revision %d, want %d", after, rev+1)
	}
	if rc, _ := s.MoveChat("c1", a.ID, a.ID, AddedByYou); !rc.Noop() {
		t.Fatal("moving to the same place should be a no-op")
	}
}

func TestMovesAreUndoable(t *testing.T) {
	s, _ := newStore(t)
	a, b := mk(t, s, "A"), mk(t, s, "B")
	if _, _, err := s.AddChat("c1", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	rc := ok(t)(s.MoveChat("c1", a.ID, b.ID, AddedByYou))
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	ms := snap(t, s).PlacesOf("c1")
	if len(ms) != 1 || ms[0].PlaceID != a.ID {
		t.Fatalf("after undo = %+v", ms)
	}
}

func TestMembershipsUnionAcrossPlacesWithoutDuplicates(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	x, y := mk(t, s, "X", top.ID), mk(t, s, "Y", top.ID)
	for _, p := range []Place{top, x, y} {
		for _, c := range []string{"shared", "own-" + p.Name} {
			if _, _, err := s.AddChat(c, p.ID, AddedByYou); err != nil {
				t.Fatal(err)
			}
		}
	}
	sn := snap(t, s)
	all := sn.ChatsIn(top.ID, true)
	if len(all) != 4 {
		t.Fatalf("union = %v, want shared + 3 own", all)
	}
	seen := map[string]bool{}
	for _, c := range all {
		if seen[c] {
			t.Fatalf("%s listed twice in %v", c, all)
		}
		seen[c] = true
	}
	if got := sn.ChatsIn(top.ID, false); len(got) != 2 {
		t.Fatalf("direct = %v", got)
	}
	if got := sn.ChatsIn(RootID, true); len(got) != 4 {
		t.Fatalf("root = %v", got)
	}
	if c := sn.Counts(RootID); c.Descendants != 3 || c.Children != 1 || c.ChatsInclusive != 4 {
		t.Fatalf("root counts = %+v", c)
	}
}

func TestContextPlacesIsMembershipsPlusTwoLevelsAtTheNearestLevel(t *testing.T) {
	s, _ := newStore(t)
	sw := mk(t, s, "Software")
	mkt := mk(t, s, "Marketing")
	code := mk(t, s, "codeaf", sw.ID, mkt.ID)
	rel := mk(t, s, "Release", code.ID)
	cfg := mk(t, s, "Config", code.ID)
	for _, p := range []Place{rel, cfg, sw} {
		if _, _, err := s.AddChat("c1", p.ID, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	got := snap(t, s).ContextPlaces("c1")
	level := map[string]int{}
	for _, c := range got {
		if _, dup := level[c.PlaceID]; dup {
			t.Fatalf("%s listed twice: %+v", c.PlaceID, got)
		}
		level[c.PlaceID] = c.Level
	}
	want := map[string]int{rel.ID: 0, cfg.ID: 0, sw.ID: 0, code.ID: 1, mkt.ID: 2}
	if len(level) != len(want) {
		t.Fatalf("context places = %+v", got)
	}
	for id, l := range want {
		if level[id] != l {
			t.Fatalf("%s at level %d, want %d (%+v)", id, level[id], l, got)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Level < got[i-1].Level {
			t.Fatalf("not ordered by level: %+v", got)
		}
	}
	if got := snap(t, s).ContextPlaces("stranger"); len(got) != 0 {
		t.Fatalf("an unplaced chat has context places: %+v", got)
	}
}

func TestUnplacedIsComputedFromCallerIDs(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	if _, _, err := s.AddChat("placed", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	// "ghost" is filed but the caller no longer has it: it must not appear.
	if _, _, err := s.AddChat("ghost", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	got := snap(t, s).Unplaced([]string{"z", "placed", "y", "z", "", "x"})
	if !eq(got, []string{"z", "y", "x"}) {
		t.Fatalf("unplaced = %v (order kept, repeats and blanks dropped)", got)
	}
	if got := snap(t, s).Unplaced(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty input = %#v, want an empty non-nil list", got)
	}
}

func TestForgetChatDropsOnlyOurRows(t *testing.T) {
	s, _ := newStore(t)
	a, b := mk(t, s, "A"), mk(t, s, "B")
	for _, p := range []Place{a, b} {
		for _, c := range []string{"gone", "stays"} {
			if _, _, err := s.AddChat(c, p.ID, AddedByYou); err != nil {
				t.Fatal(err)
			}
		}
	}
	ok(t)(s.ForgetChat("gone"))
	sn := snap(t, s)
	if len(sn.PlacesOf("gone")) != 0 || len(sn.PlacesOf("stays")) != 2 {
		t.Fatalf("forget: gone %v stays %v", sn.PlacesOf("gone"), sn.PlacesOf("stays"))
	}
	if rc, _ := s.ForgetChat("gone"); !rc.Noop() {
		t.Fatal("forgetting an unknown chat should be a no-op")
	}
}

func TestRemoveChat(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	if _, _, err := s.AddChat("c", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	ok(t)(s.RemoveChat("c", a.ID))
	if rc, _ := s.RemoveChat("c", a.ID); !rc.Noop() {
		t.Fatal("removing twice should be a no-op")
	}
	if len(snap(t, s).Memberships) != 0 {
		t.Fatal("membership remains")
	}
}

// The acceptance names for chat membership. The store already implemented the
// operations; these pin the laws under the names this lane is held to.
// ChatsIn is the landed name of the brief's ChatsOf. Undo is the receipt in
// PLACES-ARCHITECTURE §3.1 (revision-checked, twenty kept), not a ten-second
// token: S-IX-10's ten seconds is how long the toast offers Undo.

func TestAChatMayHaveNoPlaceOrSeveral(t *testing.T) {
	s, _ := newStore(t)
	a, b := mk(t, s, "A"), mk(t, s, "B")
	if got := snap(t, s).PlacesOf("none"); len(got) != 0 {
		t.Fatalf("a chat with no row has places: %+v", got)
	}
	if _, _, err := s.AddChat("several", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChat("several", b.ID, AddedByAI); err != nil {
		t.Fatal(err)
	}
	// Filing somewhere it already is changes nothing: one row, same actor.
	again, rc, err := s.AddChat("several", a.ID, AddedByAI)
	if err != nil || !rc.Noop() || again.PlaceID != a.ID || again.AddedBy != AddedByYou {
		t.Fatalf("duplicate add: %+v %+v %v", again, rc, err)
	}
	ms := snap(t, s).PlacesOf("several")
	if len(ms) != 2 || ms[0].PlaceID != a.ID || ms[0].AddedBy != AddedByYou || ms[1].PlaceID != b.ID || ms[1].AddedBy != AddedByAI {
		t.Fatalf("places = %+v", ms)
	}
	// An archived place keeps the row and drops out of context.
	ok(t)(s.Archive(b.ID))
	sn := snap(t, s)
	if len(sn.PlacesOf("several")) != 2 {
		t.Fatal("archiving dropped a membership row")
	}
	ctx := sn.ContextPlaces("several")
	for _, c := range ctx {
		if c.PlaceID == b.ID {
			t.Fatalf("archived place still reaches the chat: %+v", ctx)
		}
	}
	if len(ctx) != 1 || ctx[0].PlaceID != a.ID || ctx[0].Level != 0 {
		t.Fatalf("context = %+v", ctx)
	}
}

func TestMoveIsOneWrite(t *testing.T) {
	s, path := newStore(t)
	a, b := mk(t, s, "A"), mk(t, s, "B")
	if _, _, err := s.AddChat("c1", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	before, err := s.Revision()
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	s.beforeRename = func() error {
		writes++
		return nil
	}
	rc := ok(t)(s.MoveChat("c1", a.ID, b.ID, AddedByAI))
	if writes != 1 {
		t.Fatalf("move wrote the file %d times, want 1", writes)
	}
	after, err := s.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 || rc.BeforeRevision != before || rc.AfterRevision != after || rc.Action != ActionMove {
		t.Fatalf("revision %d→%d receipt %+v", before, after, rc)
	}
	var st State
	if err := json.Unmarshal(readFile(t, path), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Memberships) != 1 || st.Memberships[0].ChatID != "c1" || st.Memberships[0].PlaceID != b.ID || st.Memberships[0].AddedBy != AddedByAI {
		t.Fatalf("on disk after the one write: %+v", st.Memberships)
	}
}

func TestChatsOfADiamondCountsEachChatOnce(t *testing.T) {
	s, _ := newStore(t)
	top, left, right, shared := diamond(t, s)
	for _, p := range []Place{shared, left, right} {
		by := AddedByYou
		if p.ID == right.ID {
			by = AddedByAI
		}
		if _, _, err := s.AddChat("everywhere", p.ID, by); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.AddChat("only-left", left.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	got := snap(t, s).ChatsIn(top.ID, true)
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	if seen["everywhere"] != 1 || seen["only-left"] != 1 || len(got) != 2 {
		t.Fatalf("chats under the diamond = %v, each chat once", got)
	}
	if direct := snap(t, s).ChatsIn(shared.ID, false); !eq(direct, []string{"everywhere"}) {
		t.Fatalf("direct = %v", direct)
	}
}

func TestUndoRestoresExactlyOnceWithinTenSeconds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places.json")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	var n int
	s, err := Open(Options{
		Path: path,
		Now:  func() time.Time { return now },
		NewID: func(prefix string) string {
			n++
			return fmt.Sprintf("%s%d", prefix, n)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a, b := mk(t, s, "A"), mk(t, s, "B")
	filed, _, err := s.AddChat("c1", a.ID, AddedByAI)
	if err != nil {
		t.Fatal(err)
	}
	rc := ok(t)(s.MoveChat("c1", a.ID, b.ID, AddedByYou))
	// Past the toast's ten seconds. The store does not expire the receipt:
	// Undo applies while the graph is still the receipt's AfterRevision.
	now = now.Add(11 * time.Second)
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	got := snap(t, s).PlacesOf("c1")
	if len(got) != 1 || got[0] != filed {
		t.Fatalf("undo restored %+v, want the exact prior row %+v", got, filed)
	}
	rev, err := s.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(rc.ID); !errors.Is(err, ErrNoReceipt) {
		t.Fatalf("second undo = %v, want ErrNoReceipt", err)
	}
	if after, _ := s.Revision(); after != rev {
		t.Fatalf("a spent receipt moved the revision %d → %d", rev, after)
	}
	if again := snap(t, s).PlacesOf("c1"); len(again) != 1 || again[0] != filed {
		t.Fatalf("second undo changed the row: %+v", again)
	}
}

func TestUnplacedListsChatsWithNoRow(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	if _, _, err := s.AddChat("filed", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if got := snap(t, s).Unplaced([]string{"loose", "filed", "also"}); !eq(got, []string{"loose", "also"}) {
		t.Fatalf("unplaced = %v", got)
	}
	// The archived row stays. The chat reads as unplaced because an archived
	// place reads as absent.
	ok(t)(s.Archive(a.ID))
	sn := snap(t, s)
	if len(sn.PlacesOf("filed")) != 1 {
		t.Fatal("archive dropped the membership row")
	}
	if len(sn.ContextPlaces("filed")) != 0 {
		t.Fatalf("archived membership still in context: %+v", sn.ContextPlaces("filed"))
	}
	if got := sn.Unplaced([]string{"filed", "loose"}); !eq(got, []string{"filed", "loose"}) {
		t.Fatalf("archived-only unplaced = %v", got)
	}
}
