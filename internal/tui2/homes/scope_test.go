package homes

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// Every home's scope id round-trips, and nothing else in the product answers to
// one. The prefix is the whole guarantee: an unprefixed "self" would be one
// badly-named task away from opening the wrong room.
func TestScopeIdsRoundTripAndClaimNothingElse(t *testing.T) {
	for _, h := range All() {
		id := h.ScopeID()
		if !strings.HasPrefix(id, scopePrefix) {
			t.Fatalf("%v: unprefixed id %q", h, id)
		}
		got, ok := ParseScopeID(id)
		if !ok || got != h {
			t.Fatalf("%v: round trip gave %v/%v", h, got, ok)
		}
		if !Owns(id) {
			t.Fatalf("%v: Owns says no", h)
		}
	}
	for _, foreign := range []string{
		"", rail.HomeScopeID, "task:abc", "room:abc", "self", "notebook",
		"home:", "home:nope", GroupRowID, BeliefRowPrefix + "1",
	} {
		if Owns(foreign) {
			t.Fatalf("%q claimed by this package", foreign)
		}
	}
	if HomeNone.ScopeID() != "" || Home(200).ScopeID() != "" {
		t.Fatal("an invalid home minted a scope id")
	}
}

// The Source is a rail.ScopeSource and refuses what is not its own — the rail
// reads a false as "this row has no room to descend into", which is exactly
// right for a task id that reached the wrong source.
func TestSourceAnswersOnlyItsOwnRooms(t *testing.T) {
	var src *Source = NewSource(rich())
	var _ rail.ScopeSource = src
	for _, h := range All() {
		sc, ok := src.Scope(h.ScopeID())
		if !ok {
			t.Fatalf("%v: source refused its own room", h)
		}
		if sc.ID != h.ScopeID() || sc.Title != h.Word() {
			t.Fatalf("%v: scope %+v", h, sc)
		}
	}
	if _, ok := src.Scope("task:1"); ok {
		t.Fatal("source claimed a task scope")
	}
	// The zero Source is usable and honest: four rooms with nothing collected
	// in them. Self is the one exception and it is the designed one — its eight
	// routes are a fixed vocabulary, not a collection, so an unwired self room
	// still shows eight places to go rather than an empty list.
	var zero Source
	for _, h := range All() {
		sc, ok := zero.Scope(h.ScopeID())
		want := 1
		if h == HomeSelf {
			want = 1 + len(Routes())
		}
		if !ok || len(sc.Rows) != want {
			t.Fatalf("%v: zero source gave %d rows (want %d) ok=%v", h, len(sc.Rows), want, ok)
		}
	}
}

// Every scope this package builds must survive the rail's own normalisation
// with its row 0 intact and its member ids unique — the two properties the rail
// model, the fold and the composer binding all lean on.
func TestEveryScopeHasASurfaceRowAndUniqueMemberIds(t *testing.T) {
	for name, state := range map[string]State{"rich": rich(), "hostile": hostile(), "zero": {}} {
		for _, h := range All() {
			sc := Scope(state, h)
			if len(sc.Rows) == 0 {
				t.Fatalf("%s/%v: no rows", name, h)
			}
			if sc.Rows[0].Kind != rail.RowSurface {
				t.Fatalf("%s/%v: row 0 is %v", name, h, sc.Rows[0].Kind)
			}
			if sc.Seed != "" {
				t.Fatalf("%s/%v: a home claimed an identity pastel", name, h)
			}
			seen := map[string]bool{}
			for _, row := range sc.Rows[1:] {
				if row.ID == "" {
					continue // an id-less row keys on its name; the rail allows it
				}
				if seen[row.ID] {
					t.Fatalf("%s/%v: duplicate member id %q", name, h, row.ID)
				}
				seen[row.ID] = true
			}
			// The rail hands a row id straight back on enter; no member may
			// answer as a scope of its own, or enter would re-scope sideways.
			for _, row := range sc.Rows[1:] {
				if Owns(row.ID) {
					t.Fatalf("%s/%v: member %q answers as a scope", name, h, row.ID)
				}
			}
		}
	}
}

// 5.24's one silencing. A service is not a conversation, so every service row
// and the services surface itself bind rail.ComposerNone — and nothing else
// here does, because a person may always ask aforge about a belief or a
// charter.
func TestOnlyTheServicesRoomSilencesItsComposer(t *testing.T) {
	state := rich()
	for _, h := range All() {
		sc := Scope(state, h)
		want := rail.ComposerChat
		if h == HomeServices {
			want = rail.ComposerNone
		}
		for i, row := range sc.Rows {
			if row.Composer != want {
				t.Fatalf("%v row %d (%s): composer %v, want %v", h, i, row.Name, row.Composer, want)
			}
		}
	}
}

// The group is 5.24's collapsed dim group: one row shut, a lid plus four rooms
// open, and the four in their stated order.
func TestTheHomeGroupIsCollapsedByDefaultAndOpensToFour(t *testing.T) {
	state := rich()
	state.Expanded = false
	shut := Rows(state)
	if len(shut) != 1 || shut[0].ID != GroupRowID {
		t.Fatalf("collapsed group is %d rows: %+v", len(shut), shut)
	}
	state.Expanded = true
	open := Rows(state)
	if len(open) != 1+len(All()) {
		t.Fatalf("open group is %d rows", len(open))
	}
	for i, h := range All() {
		row := open[i+1]
		if row.ID != h.ScopeID() {
			t.Fatalf("row %d is %q, want %q", i+1, row.ID, h.ScopeID())
		}
		if row.Depth != 1 {
			t.Fatalf("%v: depth %d — the group must read as a group", h, row.Depth)
		}
	}
	// The lid never answers as a scope: entering it expands, it does not
	// re-scope, and a wiring that forwarded it must get a clean refusal.
	if Owns(GroupRowID) {
		t.Fatal("the lid answers as a scope")
	}
}

// A shut group still carries its attention. Background work with no panel goes
// invisible (10.3.15), and a charter waiting to be stood up is not allowed to
// become invisible because the room it lives in is closed.
func TestAShutGroupStillCarriesWhatIsAskingForAHuman(t *testing.T) {
	state := rich()
	state.Expanded = false
	lid := Rows(state)[0]
	if lid.Questions == 0 {
		t.Fatalf("a proposed charter did not reach the shut lid: %+v", lid)
	}
	if !strings.Contains(lid.Status, "need") {
		t.Fatalf("lid says %q rather than naming the ask", lid.Status)
	}

	// With nothing asking, the lid names the rooms instead — a summary that
	// read the same in both states would only ever be decoration.
	calm := State{}
	quiet := Rows(calm)[0]
	if quiet.Questions != 0 {
		t.Fatalf("a calm group claims %d questions", quiet.Questions)
	}
	for _, h := range All() {
		if !strings.Contains(quiet.Status, h.Word()) {
			t.Fatalf("lid does not name %v: %q", h, quiet.Status)
		}
	}

	// A dead service is broken, not waiting: it takes the failed lifecycle and
	// not the amber question, because the two are different asks.
	broken := State{Services: Services{Services: []Service{{ID: "a", Life: LifeFailed}}}}
	row := Rows(broken)[0]
	if row.Questions != 0 || row.Life != LifeFailed {
		t.Fatalf("a dead service read as %v/%d questions", row.Life, row.Questions)
	}
}

// The notebook is deliberately not counted as an ask, and the reason is that
// the same table is reachable from two rooms: counting it in both would report
// one unsettled belief as two people-blocking facts.
func TestOneCollectionIsNeverCountedTwice(t *testing.T) {
	state := State{
		Notebook: Notebook{Beliefs: []Belief{{ID: "1", Body: "unsure"}}},
		Self: Self{Routes: []Route{
			{ID: RouteBeliefs, Items: []Item{{ID: "1", Name: "unsure", Needs: true}}},
		}},
	}
	if got := state.NeedsIn(HomeNotebook); got != 0 {
		t.Fatalf("the notebook counted %d asks", got)
	}
	if got := state.NeedsIn(HomeSelf); got != 1 {
		t.Fatalf("self counted %d asks, want 1", got)
	}
	if got := state.needs(); got != 1 {
		t.Fatalf("the group rolled up %d asks, want 1", got)
	}
}

// Every self route survives a state that never mentioned it: eight rows with a
// name, an explainer and an empty line, rather than a short list that looks
// complete.
func TestAnUnwiredSelfRoomStillShowsEightRooms(t *testing.T) {
	rows := Scope(State{}, HomeSelf).Rows[1:]
	if len(rows) != len(Routes()) {
		t.Fatalf("%d route rows, want %d", len(rows), len(Routes()))
	}
	for i, id := range Routes() {
		if rows[i].ID != id.RowID() {
			t.Fatalf("row %d is %q, want %q", i, rows[i].ID, id.RowID())
		}
		if id.Explain() == "" || id.Empty() == "" {
			t.Fatalf("%v has no sentence to teach with", id)
		}
		got, ok := ParseRowID(id.RowID())
		if !ok || got != id {
			t.Fatalf("%v: row id does not round trip", id)
		}
	}
	if _, ok := ParseRowID("self/nope"); ok {
		t.Fatal("an unknown route id parsed")
	}
}

// Practice and Dials carry no count: a number in front of a state rather than a
// collection is a number about nothing.
func TestOnlyCollectionsAreCounted(t *testing.T) {
	for _, id := range Routes() {
		want := id != RoutePractice && id != RouteDials
		if id.Counted() != want {
			t.Fatalf("%v: Counted()=%v", id, id.Counted())
		}
	}
}

// A scan ceiling reported as a count is a floor wearing a count's clothes
// (10.2.8). The `+` is the whole difference and it must reach the row.
func TestACeilingRendersAsAFloorAndNotAsACount(t *testing.T) {
	if got := count(500, true); got != "500+" {
		t.Fatalf("ceiling rendered %q", got)
	}
	if got := count(500, false); got != "500" {
		t.Fatalf("count rendered %q", got)
	}
	rows := Scope(rich(), HomeSelf).Rows[1:]
	if !strings.Contains(rows[int(RouteBeliefs)].Name, "500+") {
		t.Fatalf("the beliefs row lost its ceiling: %q", rows[int(RouteBeliefs)].Name)
	}
}
