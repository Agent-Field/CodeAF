package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE NUMBER A TAB WEARS, AND THE CLOCK THAT KEEPS IT TRUE ────────────────
//
// A TAB WEARS A COUNT ONLY WHEN SOMETHING IN IT CHANGED SINCE YOU LOOKED AT
// THAT PLACE (pages.go's [placeCounts] holds the law). Two things had to exist
// before that sentence could be true of anything: a look stamp PER PLACE, which
// internal/session's look.go now writes ([session.LastLookAt],
// [session.NoteLookAt]), and somewhere for the answers to be cached — because
// the tab bar is drawn on every frame of every place, and a bar that asked
// SQLite how many memories are new would ask it sixty times a second.
//
// So: the counts are computed on a clock, held in a map, and the map is what
// the bar reads. The map IS the seam — [placeTally] answers [placeCounts] — so
// the interface pages.go states goes on being the only thing the drawing knows
// about, and a test still hands the app its own tally.
//
// THE STAMP IS WRITTEN ON THE WAY OUT. A stamp taken on arrival would declare
// everything seen the instant it appeared, before a person's eye had crossed a
// row; leaving is the first moment "they looked" is actually true, and it is
// the same argument [session.NoteLook] makes about home's own stamp. A window
// that dies with a place open writes nothing, and the same things are news
// again next time — which is the harmless direction to fail in.

// placeTally is the cached count per place, and it is the whole of what the tab
// bar reads. Zero and missing are one answer: nothing in there has moved.
type placeTally map[string]int

// ChangedIn is [placeCounts]. It cannot block, which is the property the cache
// exists for: every answer here was computed on the clock below.
func (t placeTally) ChangedIn(place string) int { return t[place] }

// refreshPlaceCounts recomputes every tab's number. It is the ONE function that
// reads the records behind the counts, and it runs on a clock — home's while
// home is up, this file's while any other place is ([app.placeBeat]).
//
// A PLACE WITH NO STAMP YET COUNTS NOTHING. [session.LastLookAt] answers the
// zero time for a place nobody has left, and every reader below treats that as
// "no origin, therefore no news" rather than as "everything is news" — the
// first look must not greet somebody with a number over every tab.
func (a *app) refreshPlaceCounts(now time.Time) {
	root := a.placesRoot()
	tally := placeTally{}
	for _, id := range pages() {
		pl := placeFor(id)
		if pl == nil || !pl.counted() {
			// Spend is a sum, search is something you do, and settings is how this
			// machine is set. A number in front of any of them would be a number
			// about nothing (pages.go's [page.counted]).
			continue
		}
		seen := session.LastLookAt(root, id.word())
		if seen.IsZero() {
			continue
		}
		// EACH ARM LIVES BESIDE THE PLACE IT COUNTS. This was one switch that knew
		// all the places, with a TODO on it naming the day the interface would
		// land; the registry walks itself now, and a place added later is counted
		// by having been registered.
		tally[id.word()] = pl.changed(a, seen)
	}
	a.places = tally
	_ = now
}

// memoryChangedSince is how many memories have moved since a place was left:
// the ones learned after it plus the ones let go of after it.
//
// THE TWO ARE ADDED BECAUSE THE TAB HAS ROOM FOR ONE NUMBER, and both are the
// same kind of event to somebody glancing at a bar — something in there is not
// what it was. The place itself draws the difference, in words, on the rows.
func (a *app) memoryChangedSince(seen time.Time) int {
	if a.memory == nil || seen.IsZero() {
		return 0
	}
	learned, letGo, err := a.memory.ChangedSince(seen)
	if err != nil {
		// A COUNT THAT COULD NOT BE READ IS NO COUNT. Drawing a zero would be a
		// claim about the store; drawing nothing is the emptiness law.
		return 0
	}
	return learned + letGo
}

// The tasks and standing counts are not here: each is a reading of the records
// its own place already holds, so a count is never a second walk of the same
// cached world (place_tasks.go's [app.tasksChangedSince], place_standing.go's
// [app.standingChangedSince]).

// ── the clock the places that are not home run on ───────────────────────────
//
// Home has its own three-second beat and it re-arms only while home is open
// (home.go's [homeEvery]). Every other place needs the same beat for the same
// reason — a memory learned in the next terminal, a model call charged by a
// task running behind this window — so this is that beat, armed when one of
// those places opens and re-armed only while one is standing.
//
// IT IS THE SAME PERIOD AS HOME'S AND IT IS HOME'S CONSTANT. Two clocks that
// drifted apart would be two answers to "how often does this surface look at
// the disk", and the number is already written down once.

// placeTickMsg is that clock's beat, carrying the generation of the place that
// armed it. The generation is home's own device ([homeTickMsg] tells the story):
// walking from memory to spend and back before an old tick landed would
// otherwise leave two self-re-arming chains reading the disk, then four.
type placeTickMsg struct{ gen int }

func placeTick(gen int) tea.Cmd {
	return tea.Tick(homeEvery, func(time.Time) tea.Msg { return placeTickMsg{gen: gen} })
}

// armPlaceClock starts a clock for the place that just opened, and retires
// whatever was ticking for the place it replaced.
func (a *app) armPlaceClock() tea.Cmd {
	a.placeGen++
	return placeTick(a.placeGen)
}

// placeBeat is the beat, arriving: every place that is standing re-reads what
// it draws, the tab bar's numbers are recomputed, and the clock re-arms itself
// only while there is still a place to keep current.
func (a *app) placeBeat(gen int) tea.Cmd {
	if gen != a.placeGen {
		return nil
	}
	// THE PLACE ANSWERS THE BEAT AND SAYS WHETHER IT WANTS ANOTHER — the registry
	// walking itself, so a room added later is refreshed by having a `tick` and
	// nothing else. Home runs a beat of its own and therefore answers false, which
	// is what stops a clock armed by the memory place turning forever behind home
	// ([place.tick] holds the whole argument).
	pl := a.showing()
	if pl == nil {
		return nil
	}
	now := a.now()
	// THE COUNTS ARE THE BAR'S AND NOT THE ROOM'S, so they are recomputed before
	// the room is asked anything. They used to sit under the `tick` gate, which
	// meant a place answering false stopped every OTHER tab's number from being
	// recomputed as well — standing on the tasks place froze the memory tab's
	// count, and which room you happened to be in decided whether the bar was
	// alive. A room may decline to re-read its own rows; it may not silence the
	// bar.
	a.refreshPlaceCounts(now)
	// AND THE ROOM ANSWERS ONLY WHETHER THE BEAT GOES ON. Home runs a beat of
	// its own and answers false, which is what stops a clock armed by another
	// place turning forever behind it ([place.tick] holds the whole argument).
	if !pl.tick(a, now) {
		return nil
	}
	a.touch()
	return placeTick(a.placeGen)
}

// leavePage writes the look stamp for one place: the record that says nothing
// in there is news any more.
//
// It is called where a place is CLOSED — by `esc`, and by the router on its way
// to another place ([app.showPage]) — and never where one is opened. The count
// this stamp feeds is recomputed on the next beat, so a tab loses its number
// within three seconds of being read rather than the instant it is entered.
func (a *app) leavePage(id page) {
	if !id.counted() {
		return
	}
	session.NoteLookAt(a.placesRoot(), id.word(), a.now())
}
