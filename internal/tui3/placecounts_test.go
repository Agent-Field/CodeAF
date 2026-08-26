package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE NUMBER ON A TAB, AS A PERSON MEETS IT.
//
// Every test here asserts what the bar says and what leaving a place changes,
// rather than the shape of the cache under it (placecounts.go).

// countingBrain is a memory store that reports a fixed delta, so a test can ask
// what the tab does with two figures rather than build a database to make them.
type countingBrain struct {
	panelMemoryStore
	asked []time.Time
}

func (c *countingBrain) ChangedSince(since time.Time) (int, int, error) {
	c.asked = append(c.asked, since)
	return c.learned, c.letGo, nil
}

func countingApp(t *testing.T) (*app, *countingBrain) {
	t.Helper()
	a := placeApp(t)
	brain := &countingBrain{}
	brain.origins = map[string]memoryOrigin{}
	// THE PLACE NEEDS BOTH HALVES OF THE CAPABILITY, which is the wiring law
	// rather than a test detail: a session that is not remembering has no memory
	// place at all, whatever store the door was handed ([app.memoryReady]).
	a.agent = &rememberingAgent{}
	a.memory = brain
	return a, brain
}

// A TAB WEARS WHAT MOVED IN THAT PLACE SINCE YOU LEFT IT, and the two halves of
// memory's answer — learned, and let go of — are one number because a tab has
// room for one.
func TestATabWearsWhatChangedSinceYouLeftThatPlace(t *testing.T) {
	a, brain := countingApp(t)
	brain.learned, brain.letGo = 2, 1
	left := time.Now().Add(-time.Hour)
	session.NoteLookAt(a.placesRoot(), pageMemory.word(), left)

	a.refreshPlaceCounts(time.Now())
	if got := a.placeCount(pageMemory); got != 3 {
		t.Fatalf("the memory tab wears %d, want the 2 learned plus the 1 let go", got)
	}
	if len(brain.asked) != 1 || !brain.asked[0].Truncate(time.Second).Equal(left.UTC().Truncate(time.Second)) {
		t.Fatalf("the delta was measured from %v, want the look stamp %v", brain.asked, left)
	}
	if bar := plain(a.placeTabBar(160, false, a.pal)); !strings.Contains(bar, "memory 3") {
		t.Fatalf("the bar does not carry the count: %q", bar)
	}
}

// A PLACE NOBODY HAS LEFT HAS NO ORIGIN, SO NOTHING IN IT IS NEWS. The first
// look must greet somebody with a bare bar rather than with a number over every
// tab, which is the same refusal session's own look.go makes.
func TestAPlaceWithNoLookStampWearsNoNumber(t *testing.T) {
	a, brain := countingApp(t)
	brain.learned, brain.letGo = 40, 2
	a.refreshPlaceCounts(time.Now())
	if got := a.placeCount(pageMemory); got != 0 {
		t.Fatalf("a place with no stamp wears %d", got)
	}
	if len(brain.asked) != 0 {
		t.Fatalf("the store was asked for a delta with no origin to measure from: %v", brain.asked)
	}
	bar := plain(a.placeTabBar(160, false, a.pal))
	for _, digit := range "0123456789" {
		if strings.ContainsRune(bar, digit) {
			t.Fatalf("the bar wears a figure with no origin behind it: %q", bar)
		}
	}
}

// AND A PLACE THAT CANNOT BE COUNTED IS NEVER ASKED. Spend is a sum, search is
// something you do, settings is how this machine is set.
func TestTheUncountablePlacesAreNeverGivenANumber(t *testing.T) {
	a, brain := countingApp(t)
	brain.learned = 5
	for _, id := range []page{pageSpend, pageSearch, pageSettings} {
		session.NoteLookAt(a.placesRoot(), id.word(), time.Now().Add(-time.Hour))
	}
	a.refreshPlaceCounts(time.Now())
	for _, id := range []page{pageSpend, pageSearch, pageSettings} {
		if got := a.placeCount(id); got != 0 {
			t.Fatalf("%s wears %d, and it is not a collection", id.word(), got)
		}
	}
}

// LEAVING IS THE LOOK. The stamp is written on the way OUT — a stamp taken on
// arrival would call everything seen the instant it appeared — so walking from
// memory to another place is what clears memory's number.
func TestLeavingAPlaceWritesItsLookStampAndClearsItsNumber(t *testing.T) {
	a, brain := countingApp(t)
	brain.rows = []store.Memory{{ID: "m1", Title: "uses neovim", Scope: store.MemoryScopeUser}}
	before := session.LastLookAt(a.placesRoot(), pageMemory.word())
	if !before.IsZero() {
		t.Fatalf("a place nobody has been in already has a stamp: %v", before)
	}
	a.showPage(pageMemory)
	if !a.at(pageMemory) {
		t.Fatal("the memory place did not open")
	}
	a.showPage(pageHome)
	if session.LastLookAt(a.placesRoot(), pageMemory.word()).IsZero() {
		t.Fatal("leaving the memory place wrote no look stamp")
	}

	// AND WITH THE STAMP JUST WRITTEN, NOTHING SINCE IT IS NEWS.
	brain.learned, brain.letGo = 0, 0
	a.refreshPlaceCounts(time.Now())
	if got := a.placeCount(pageMemory); got != 0 {
		t.Fatalf("a place read a moment ago still wears %d", got)
	}
}

// HOME'S OWN TAB NEVER WEARS A NUMBER, and that is the design rather than a
// gap: home is where the "since you left" ledger is drawn in sentences, and a
// digit on its tab would be the same news said twice.
func TestHomeWearsNoNumberBecauseItDrawsTheNewsItself(t *testing.T) {
	a, _ := countingApp(t)
	session.NoteLookAt(a.placesRoot(), pageHome.word(), time.Now().Add(-time.Hour))
	a.refreshPlaceCounts(time.Now())
	if got := a.placeCount(pageHome); got != 0 {
		t.Fatalf("home wears %d", got)
	}
}

// THE SEAM IS STILL AN INTERFACE, and the cache is one thing that answers it —
// which is what keeps a test able to hand the app a tally of its own.
var _ placeCounts = placeTally(nil)

// THE BAR GOES ON COUNTING IN EVERY ROOM, AND A ROOM MAY NOT SILENCE IT.
//
// The numbers on the tab bar are recomputed on the places' three-second beat,
// and that beat used to stop the moment a room answered `tick` with false —
// which standing, tasks and settings all did. Standing on any of the three
// froze every tab's count, including the counts of the six rooms that room has
// nothing to do with, so whether the bar was alive depended on which room you
// happened to be standing in.
//
// The count watched here is memory's, which is deliberately NOT the room the
// test is standing in: what broke was the bar and not the room.
func TestTheTabsGoOnCountingInEveryRoom(t *testing.T) {
	for _, place := range everyPlaceTable() {
		if place.id == pageHome {
			// Home runs a beat of its own and refreshes the counts on it
			// ([app.refreshHome]), which is why it answers the router's beat with
			// false — a second self-re-arming chain turning behind home is the
			// thing that answer prevents.
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			brain := &countingBrain{}
			brain.origins = map[string]memoryOrigin{}
			a.agent = &rememberingAgent{}
			a.memory = brain
			session.NoteLookAt(a.placesRoot(), pageMemory.word(), time.Now().Add(-time.Hour))

			a.refreshPlaceCounts(time.Now())
			if got := a.placeCount(pageMemory); got != 0 {
				t.Fatalf("the memory tab opened wearing %d", got)
			}
			// Something happens in the next terminal.
			brain.learned, brain.letGo = 3, 1
			next := a.placeBeat(a.placeGen)
			if got := a.placeCount(pageMemory); got != 4 {
				t.Fatalf("standing on %s, the beat left the memory tab wearing %d, want 4",
					place.id.word(), got)
			}
			// AND THE BEAT COMES ROUND AGAIN. A count that moved once and then
			// stopped is the same freeze three seconds later.
			if next == nil {
				t.Fatalf("standing on %s, the beat did not re-arm itself", place.id.word())
			}
			brain.learned, brain.letGo = 5, 2
			a.placeBeat(a.placeGen)
			if got := a.placeCount(pageMemory); got != 7 {
				t.Fatalf("standing on %s, the second beat left the memory tab wearing %d, want 7",
					place.id.word(), got)
			}
		})
	}
}

// AND EVERY ROOM ARMS THE CLOCK ON THE WAY IN. A room that armed no clock had
// no beat to answer, so the two facts are one fact and this is the half a
// `tick` returning true cannot supply by itself.
func TestEveryPlaceThatIsNotHomeArmsTheClockOnTheWayIn(t *testing.T) {
	for _, place := range everyPlaceTable() {
		if place.id == pageHome {
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			a := placeApp(t)
			was := a.placeGen
			a.showPage(place.id)
			if a.placeGen == was {
				t.Fatalf("walking into %s armed no clock, so nothing there will ever beat",
					place.id.word())
			}
		})
	}
}
