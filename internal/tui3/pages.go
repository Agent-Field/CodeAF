package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE PLACES ──────────────────────────────────────────────────────────────
//
// A person reads the word PLACE, and so does this file. The word carries two
// other meanings in this repository, and both of them are one letter away:
//
//   - [session.PlacesRoot], [app.placesRoot], `placesDirName`, `SweepPlaces`,
//     `homePlacesCol` — the PLURAL, always, and always THE DIRECTORY OF
//     CONVERSATIONS AND PROJECTS ON THIS DISK;
//   - `page`, the identifier for WHICH ROOM, kept because a `page` is what the
//     surface has always called a screen that takes the frame and because
//     `place` on its own is now the interface below.
//
// So: `page` is the id, `place` is the contract, `places` is the disk, and the
// manual and every string a person sees say *place* for the room. This comment
// is the bridge between the three, and it used to say the Go identifier was
// `page` everywhere — which was true until ARCHITECTURE.md named the interface
// and the owner signed it.
//
// THE SEVEN ARE A LIST AND NOT A SWITCH. The tab bar's order, the numbers
// `alt+1`…`alt+7` jump to, and the order `tab` walks are ONE fact, held in
// [placeOrder], so a place added later is a row in that slice and a file.
//
// The rewind timeline is NOT one of them. It is still a page reached by
// `/rewind`, because it is a thing you do to this conversation rather than a
// room in the machine, and putting it in the bar would put a knife in the
// cutlery drawer.
type page uint8

const (
	// pageNone is THE CONVERSATION — no place at all. It is the zero value on
	// purpose: a window that has just opened is sitting in a chat, and every
	// question this file asks of [app.page] then answers "nothing is up" without
	// a second flag to keep in step.
	pageNone page = iota
	pageHome
	pageTasks
	pageStanding
	pageMemory
	pageSpend
	pageSearch
	pageSettings
	// pageTeams is the team-level view (place_teams.go). It is appended rather
	// than put after pageHome because the ids are only names: the order a
	// person meets the places in is [placeOrder]'s, and nothing stores an id.
	pageTeams
)

// ── THE CONTRACT EVERY PLACE ANSWERS ────────────────────────────────────────
//
// place is what a page of the switcher must be able to do, and [placeRegistry]
// is the one thing that knows all of them: NOTHING SWITCHES ON A PAGE ID
// OUTSIDE THIS FILE. Before this contract existed the seven rooms were spelled
// out as arms of eleven switches across five files — the frame, the note line,
// the hint, the composer, the alt letters, the time window, the verbs, the
// counts, and the three pointer gestures — so a place added later was eleven
// edits, and a place that answered ten of them was a room with no pointer or a
// tab that never wore its number. Both happened.
//
// A PLACE IS A STATELESS HANDLE AND THE STATE LIVES ON THE APP. The registry is
// package-level and one window's cursor is not another's, so what is registered
// is a value with no fields whose methods reach the app's own field for that
// place — `a.taskSheet`, `a.orders`, `a.mem` and the rest, each declared
// in the same `place_<word>.go` as the handle that reads it. That is what
// "per-place state owned by the place" means here: one file owns the struct, the
// handle and every method the frame can ask of it.
//
// Every method is given the app because a place is a reading of the machine and
// the machine is what the app holds. What no method may do is READ THE DISK ON A
// DRAW: `open` and `tick` are the two that may, and `body` builds rows out of
// what they left behind (ARCHITECTURE.md's three layers).
type place interface {
	// id is which place this is, and it is the key the registry files it under.
	id() page
	// word is the one lowercase word this place is called, on the tab bar and in
	// the manual. It is the whole of a place's name: a tab bar of two-word labels
	// is a menu, and this is a bar.
	word() string
	// counted answers whether a number in front of this place would mean
	// anything — see [page.counted].
	counted() bool
	// open primes this place's caches and lays it out, once, on entry. The
	// command it answers is the place's own clock, where it keeps one.
	open(a *app) tea.Cmd
	// close writes the look stamp and drops what the place was holding. Folds
	// and views that outlive the screen live on the app and survive it.
	close(a *app)
	// tick is the three-second beat: the cached reading is taken again, so a
	// memory learned in the next terminal is on this frame within three seconds.
	// It answers whether this place wants the beat AGAIN.
	//
	// A PLACE THAT DOES NOT ARM THE CLOCK MUST NOT KEEP IT TURNING. Home has a
	// beat of its own and arms it itself (home.go's [homeEvery]); the tasks place
	// re-reads on the keystroke that walks in. A clock left running on a place
	// that never asked for one is a second reader of the same disk, and walking
	// out of memory onto home used to be exactly that (placecounts.go).
	//
	// IT MAY ALSO HAND BACK A COMMAND. That is where a place asks an engine door
	// off the update loop — the spend place's run-of-seats read is the one that
	// does — because the beat is the moment a place re-reads what it draws and a
	// door asked from Update would freeze the window while the engine answered
	// (offloop.go). The bool says whether the beat goes on; the command, nil where
	// the place has none, runs beside the re-armed clock.
	tick(a *app, now time.Time) (bool, tea.Cmd)
	// body is the rows and the hit map, painted into exactly the room the frame
	// reserved. It reads caches and never a seam.
	body(a *app, width, room int) []placeRow
	// remote is THE ONE DIM LINE this place draws INSTEAD of its rows when the
	// session is on another machine and this place's reading is not, and "" when
	// there is nothing to say — which is every place on a local session, and a
	// place whose reading crosses the wire.
	//
	// IT IS THE PLACE'S OWN SENTENCE AND NOT THE FRAME'S, because the noun in it
	// belongs to the place: what this machine RAN is not what this machine has
	// LEARNED (host.go's places section holds the whole argument, and the five
	// sentences). The frame's part is that it is read on EVERY place, ahead of
	// [place.ownFrame], so a room that draws its own chrome cannot draw the wrong
	// machine's rows inside it.
	remote(a *app) string
	// bar is a foot A THUMB CAN PRESS in place of the hint line's legend, and
	// false where the place has none. At [tierPhone] a line naming four keys is a
	// line naming four keys nobody has, so the way out has to be a target rather
	// than a legend.
	bar(a *app, width int) (string, placeHit, bool)
	// ownFrame is the ONE ESCAPE from [placeFrame], and exactly two places take
	// it. Home unpacks a second hit map — the pane a row shares with an errand —
	// and below sixty columns is an inbox and a sheet rather than a list
	// (homephone.go); the tasks place draws a record CARD over its list, with a
	// head and a foot of its own (taskrecord.go). Both are modes that take the
	// terminal whole, so a body handed to the shared frame would be drawn inside
	// chrome that is not theirs. Everything else answers false and is drawn in
	// the one frame.
	ownFrame(a *app, width, height int) ([]string, []placeHit, int, int, bool)
	// stops is the cursor-legal rows of the last body, in order.
	stops(a *app) []int
	// cursorAt is WHICH LINE OF ITS OWN BODY this place's cursor is standing on,
	// in the same numbers [place.stops] answers in.
	//
	// IT HAS NO DEFAULT ON [placeBase] AND THAT IS DELIBERATE. A place that did
	// not say where its cursor was would compare equal to its own first stop from
	// every row of the list, and `↑` in the middle of it would jump to the tab bar
	// — a wrong answer that draws perfectly and that nobody would think to look
	// for. So it is required, like `id` and `word`, and a place that forgets it
	// does not build.
	//
	// The frame asks so that `↑` off the FIRST row of any body lands on the tab
	// bar ([app.barReach]). That is the whole of what it is for, and it is a fact
	// about the place rather than about the bar: where the cursor is standing.
	cursorAt(a *app) int
	// cursorRow is which of the rows JUST BUILT the cursor is standing on, and
	// -1 for a place whose cursor is on nothing this frame drew.
	//
	// THE FRAME ASKS SO THE VERB STRIP CAN BE DRAWN UNDER THE ROW IT BELONGS TO
	// (SCREEN 3c). The whole key law turns on the strip DISPLACING the list —
	// "that visible displacement is why bare letters are safe here" — and a
	// strip at the foot displaces nothing. The rows are handed in rather than
	// remembered because a map written by anything other than the draw is a map
	// that answers for a row the draw did not put there, which is the law every
	// hit map on this surface is held to.
	cursorRow(a *app, rows []placeRow) int
	// rowID NAMES THE ROW UNDER THE CURSOR, and it is what the verb strip is
	// bound to (verbstrip.go). A strip captures its verbs from one row; the name
	// captured beside them is how the frame knows, on any later frame, that the
	// row is still the one those letters were about.
	//
	// It is a NAME and not an index, for the reason every restore on this
	// surface is: a rebuild three seconds later hands the same thing a different
	// row number, and a cursor compared by number would say a row had changed
	// when only the list around it had. Two different rows must never share a
	// name; a row a place cannot name answers "", which is a row no strip
	// survives moving onto.
	rowID(a *app) string
	// owns is A LAYER INSIDE THIS PLACE THAT HAS TAKEN THE WHOLE KEYBOARD, and it
	// is read BEFORE the router's own six classes. Home's focused errand pane and
	// its phone sheet, and the settings panel's value editor, model picker and
	// key box, are all of them: each has deliberately claimed every key, `tab`
	// included, and a router that took a chord over the top of one would be
	// lifting a key out of a box a person is typing in.
	//
	// THAT IS NOT AN EXCEPTION TO ONE GRAMMAR. It is the same arbitration the
	// manual already states about `tab`: everything else that wants it gets it
	// first, and the router is the LAST claimant rather than the first
	// (placekeys.go's header).
	owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool)
	// key is this place's own reading of a key THE ROUTER DID NOT TAKE. The six
	// classes are [app.placeKey]'s and are read first on every place, so what
	// arrives here is the cursor, `enter`, `esc` and whatever the place types
	// into its box — a place has no claim on a chord, and no place may see one.
	key(a *app, msg tea.KeyPressMsg) tea.Cmd
	// enter is the row under the cursor, opened.
	enter(a *app) tea.Cmd
	// verbs is the `→` strip for the row under the cursor (verbstrip.go).
	verbs(a *app) []verb
	// alt is "show this place differently": what one place does with one letter,
	// and false where the place has nothing to change.
	alt(a *app, letter rune) bool
	// window is `shift+←→↑↓`, the stretch of time this place is showing and how
	// coarse. False is "this place has no window", and the key then does nothing
	// rather than something undrawn (SCREEN 3d).
	//
	// The bool says whether the window moved; the command, nil where the place
	// has none, carries a door the move makes necessary — the spend place re-asks
	// its seat rollup over the new window, off the update loop ([place.tick]).
	window(a *app, key string) (bool, tea.Cmd)
	// caretRow is where a place parks the terminal's caret when the box a person
	// is typing into is ONE OF THE ROWS IT JUST BUILT, rather than the composer
	// the frame draws at the foot: the row within `rows`, the column within that
	// row, and whether the place's own box owns the caret on this frame at all.
	// It is asked with the slice the frame is about to draw, so the place answers
	// from the same rows the pointer will be resolving against.
	//
	// THE SETTINGS SHEET AND THE TASKS PLACE ARE THE TWO WHO ANSWER. The sheet's
	// connections key entry is a box inside a row (connectcaps.go), and the tasks
	// filter lives in the list's control row ([placeTasks.body]) — and a
	// caret parked in the foot's resting silhouette while somebody types into
	// either is a cursor blinking in a box that is not the one the words are
	// landing in, which is the wrongness this hook exists to end.
	//
	// A TRUE ANSWER OF row < 0 SAYS THE BOX OWNS THE CARET AND HAS NO LINE ON
	// THE FRAME — a key entry showing its choice list, a control row the window
	// cut off — and the caret is HIDDEN rather than parked on some other row's
	// first cell (input.go states the same law for the /connect panel's box).
	// False hands the caret back to the frame's composer untouched.
	caretRow(a *app, width int, rows []placeRow) (row, column int, drawn bool)
	// box is the editor this place types into, and nil where it has none.
	//
	// ONLY HOME HAS A BOX THAT SENDS ANYTHING (the owner's ruling, 2026-09-17).
	// Tasks, memory and search keep an editor because typing there FILTERS or
	// SEARCHES, and each of them draws its own letters in its own body; the
	// standing and spend places take no text at all, and the foot under every
	// place but home draws no box. The shared composer that used to sit under
	// all seven — `enter talk about it · alt+enter send it off as a task` — was a
	// box on six pages where `enter` opened a row and the sentence went nowhere.
	box(a *app) *editor
	// note is the one line a place may say about what it is HOLDING, drawn under
	// the rule and above the composer.
	note(a *app, width int) []string
	// hint is the line under the composer: what the row under the cursor can be
	// asked for, and how to leave.
	hint(a *app) string
	// changed is the tab's count: how many things in here have moved since the
	// person last looked at this place.
	changed(a *app, since time.Time) int
	// summary is WHAT IS BEHIND THIS PLACE, in a clause a person reads on home's
	// typed drop-up — `6 orders, 1 fired today` (SCREEN 1g). It is not the tab
	// bar's number: that one counts what CHANGED since you last looked, and this
	// one says what is in there at all.
	//
	// A PLACE THAT CANNOT ANSWER CHEAPLY ANSWERS NOTHING, and the row then says
	// only what kind of thing it is. The clause is taken when the drop-up's
	// lines are BUILT — on home's own three-second beat — because the seams
	// behind it are the ones a draw may never touch (homeplaces.go).
	summary(a *app) string
	// press is a press on one of this place's body rows. A PRESS ON A ROW IS
	// `enter` ON IT: the cursor lands on the row and the row's door opens, which
	// is the one click grammar every list on this surface keeps — home's rows,
	// the tasks place, a picker (PLACES-AUDIT.md finding 6). The pointer resting
	// is the preview; the press is the choice. The bool is whether the place
	// took the press, which it does for every row of its body, door or not.
	//
	// A CLICK NEVER SPENDS. Where `enter` would send words to a model on a
	// person's behalf — memory's `ask me about it` — the press opens the row's
	// own card instead, because a click that starts a paid turn is a gesture
	// nobody can take back. Every verb stays a key.
	press(a *app, y int) (tea.Cmd, bool)
	// hover is the pointer resting over a body row: the row is previewed and the
	// cursor is left where it is.
	hover(a *app, y int) bool
	// wheel walks this place's cursor, by [placeWheelRows] rows a tick, and
	// answers whatever that move leaves to do — a command, exactly as [place.press]
	// does. THE TWO GESTURES THAT MOVE A CURSOR SAY SO THE SAME WAY: this used to
	// answer a bare bool, so a place with work to do about a cursor that moved could
	// hear the keyboard and the pointer and not the wheel (taskpane.go's pane
	// followed an arrow key and sat blank under a wheel).
	wheel(a *app, delta int) (tea.Cmd, bool)
}

// placeBase is the defaults, so that a place file is only what is PARTICULAR to
// that place. A room with no time window, no alt letters, no count and no note
// says none of those words at all, and the four it does say sit together on the
// screen rather than in a list of eleven overrides.
//
// It cannot default `id` or `word`: those are the two facts that make a place a
// place, and a handle that forgot them would register under the zero page and
// silently displace home.
type placeBase struct{}

func (placeBase) counted() bool                              { return false }
func (placeBase) open(a *app) tea.Cmd                        { return nil }
func (placeBase) close(a *app)                               {}
func (placeBase) tick(a *app, now time.Time) (bool, tea.Cmd) { return false, nil }
func (placeBase) body(a *app, width, room int) []placeRow    { return nil }

// remote is NOTHING TO SAY, which is the right default in both directions: a
// place on a local session has no other machine to name, and a place whose
// reading crosses the wire is drawing the right machine already. A place that
// reads THIS process's disk and has not learned to cross says its own sentence
// (host.go's places section).
func (placeBase) remote(a *app) string { return "" }
func (placeBase) bar(a *app, width int) (string, placeHit, bool) {
	return "", nil, false
}
func (placeBase) ownFrame(a *app, width, height int) ([]string, []placeHit, int, int, bool) {
	return nil, nil, 0, 0, false
}
func (placeBase) stops(a *app) []int                        { return nil }
func (placeBase) cursorRow(a *app, rows []placeRow) int     { return -1 }
func (placeBase) rowID(a *app) string                       { return "" }
func (placeBase) enter(a *app) tea.Cmd                      { return nil }
func (placeBase) verbs(a *app) []verb                       { return nil }
func (placeBase) alt(a *app, letter rune) bool              { return false }
func (placeBase) window(a *app, key string) (bool, tea.Cmd) { return false, nil }
func (placeBase) note(a *app, width int) []string           { return nil }
func (placeBase) changed(a *app, since time.Time) int       { return 0 }
func (placeBase) summary(a *app) string                     { return "" }
func (placeBase) press(a *app, y int) (tea.Cmd, bool)       { return nil, false }
func (placeBase) hover(a *app, y int) bool                  { return false }
func (placeBase) wheel(a *app, delta int) (tea.Cmd, bool)   { return nil, false }
func (placeBase) key(a *app, msg tea.KeyPressMsg) tea.Cmd   { return nil }
func (placeBase) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, false
}

// box is the SHARED composer, which is what a place types into unless it holds a
// box of its own. A sentence half typed on one place is still there after `tab`,
// which is what makes a permanent bottom line a composer rather than seven boxes
// that each forget ([app.compose]).
func (placeBase) box(a *app) *editor { return nil }

// caretRow defaults to NO: a box drawn in the composer at the foot is parked
// by the frame itself, and a place with no row-box of its own has nothing to
// say here. The two places whose live box is a row of their own body answer in
// their own file.
func (placeBase) caretRow(a *app, width int, rows []placeRow) (int, int, bool) {
	return 0, 0, false
}

// ── THERE IS NO DEFAULT hint, AND THAT IS THE WHOLE POINT ───────────────────
//
// [placeBase] used to answer `hint` with [placeHintWords], and it made a place
// that had never written a foot draw a sentence about SOMEBODY ELSE'S keys. The
// search place said `enter talk about it` six rows under its own body saying
// `enter opens the conversation at the matching turn.`, and the spend place hid
// `enter`, `→ b the limits` and its shift-arrow window behind the same line —
// two rooms lying with one borrowed sentence, and neither of them a compile
// error, a test failure or anything a reader of either file would notice.
//
// So `hint` joins `id`, `word` and `cursorAt` as a method [placeBase] does NOT
// carry: a place with no foot of its own does not build. `TestEveryPlaceSaysItsOwnKeys`
// reads this file back with go/ast and says the same thing a second time, so
// that a default quietly restored here is caught by a name rather than by
// somebody eventually reading a frame.
//
// [placeHintWords] survives as the CONVERSATION's composer foot — the line the
// design fixes for a frame with no place standing on it — and is nobody's
// fallback.

// placeRegistry is every place, by id, filled by each `place_<word>.go`'s `init`
// exactly as `registerHomeBand` fills the bands. A place added later is a file
// and a row of [placeOrder], and nothing else anywhere.
var placeRegistry = map[page]place{}

// placeOrder is the whole set, in the one order that matters: `alt+1` through
// `alt+7`, and — for the first [placeBarPlaces] of them — left to right along
// the tab bar and round the circle `tab` walks.
//
// THE BAR IS FOUR PLACES AND HOME IS THEIR SUMMARY (DESIGN.md's law 10). What
// wants you and what is running (home), the work itself (tasks), what it cost
// (spend), and how this machine is set (settings). Standing, memory and search
// come after them: still rooms, still reached by `/standing`, `/memory` and
// `/search`, by the typed box's place offers, by `alt+5`…`alt+7` and by the
// map — but not drawn on a bar a person reads a hundred times a day, until they
// are the rooms a person walks into a hundred times a day.
//
// THE THREE KEEP A DIGIT EACH so a hand that learned `alt+5` finds a room there
// rather than a key that does nothing.
//
// IT IS A LIST HERE AND NOT AN `init` ORDER. Go runs a package's `init`s in
// filename order, so a registry that took its order from them would put the tab
// bar's reading order at the mercy of what a file happens to be called — and
// `place_home.go` sorts after `place_tasks.go` would silently reorder the bar
// and every number on it.
var placeOrder = []page{pageHome, pageTeams, pageTasks, pageSpend, pageSettings, pageStanding, pageMemory, pageSearch}

// placeBarPlaces is how many of [placeOrder] the tab bar draws: the five a day
// is read through. Teams joined them right after home (DESIGN.md section 8.4,
// ruled 2026-09-24): it is where a person runs the work they handed off, so
// it is read as often as home is.
const placeBarPlaces = 5

// barPages is the places the bar draws while a person stands at `here`: the
// first [placeBarPlaces], and the room they are standing in when it is one of
// the others — a bar with no word lit is a bar that does not know where you
// are. With the map up (`every`) it is all of them, numbered, so the three that
// are off the bar are on the one surface whose job is to show every key.
func barPages(here page, every bool) []page {
	if every {
		return placeOrder
	}
	shown := placeOrder[:placeBarPlaces:placeBarPlaces]
	for _, id := range placeOrder[placeBarPlaces:] {
		if id == here {
			return append(shown, id)
		}
	}
	return shown
}

// placeDigitOf is the digit `alt+` takes to reach one place — its position in
// [placeOrder], which is not always its position on the bar.
func placeDigitOf(id page) int {
	for i, at := range placeOrder {
		if at == id {
			return i + 1
		}
	}
	return 0
}

// placeChord is the chord that reaches one place, spelled the way the command
// menu and the help write it — `alt+3`. It is READ OFF [placeOrder] so a row
// that names a place's key cannot go on naming the key it had before the bar
// was reordered (commands.go's /spend and /search rows did exactly that).
func placeChord(id page) string { return chordAltWord + itoa(placeDigitOf(id)) }

// registerPlace files one place under its own id. A second registration for one
// id is a bug this would hide, so it panics at start-up rather than letting one
// room quietly replace another.
func registerPlace(p place) {
	if _, twice := placeRegistry[p.id()]; twice {
		panic("tui3: two places registered as " + p.word())
	}
	placeRegistry[p.id()] = p
}

// pages is every place in digit order, read from the registry's order table.
// The bar draws a prefix of it ([barPages]).
func pages() []page { return placeOrder }

// placeWordList is the seven words in digit order, for the one sentence on the
// key sheet that has to say which digit is which (commands.go).
//
// IT IS READ OFF [placeOrder] AND NOT TYPED OUT, because a hand-written list on
// the help sheet is a second answer to what `alt+3` opens — and the day a place
// is added or the order changes, the sheet is the last thing anybody would think
// to edit. One source of truth (CLAUDE.md's design laws).
func placeWordList() string {
	words := make([]string, 0, len(placeOrder))
	for _, id := range placeOrder {
		if pl, ok := placeRegistry[id]; ok {
			words = append(words, pl.word())
		}
	}
	return strings.Join(words, " ")
}

// placeFor is the place one id names, and nil for the conversation or for an id
// nothing answers to. It is THE registry lookup, and every question this file
// asks about a place goes through it.
func placeFor(id page) place { return placeRegistry[id] }

// showing is the place the person is standing in, and nil when they are in the
// conversation. It is THE ONE ANSWER to "what is up": there are no `open` flags
// left anywhere to disagree with it.
func (a *app) showing() place { return placeRegistry[a.page] }

// at reports whether the person is standing in one named place. It is a
// PREDICATE and never a dispatch — a place's own file asking "am I up" is one
// fact read once, where a switch over the ids would be this file's job done
// somewhere else.
func (a *app) at(id page) bool { return a.page == id }

// word is the one lowercase word a place is called.
func (p page) word() string {
	if pl := placeFor(p); pl != nil {
		return pl.word()
	}
	return ""
}

// lookKey preserves the saved visit stamp when a tab's displayed name changes.
func (p page) lookKey() string {
	if p == pageTasks {
		return "tasks"
	}
	return p.word()
}

// counted answers whether a number in front of a place would mean anything.
//
// A COLLECTION CAN BE COUNTED AND A STATE CANNOT. Home, tasks, standing and
// memory each hold a pile of things, so "two of them changed" is a fact about
// the place. Spend is a sum, search is something you do, and settings is how
// this machine is set — a number in front of any of the three would be a number
// about nothing, and the tab bar would be teaching a lie about what is in there.
func (p page) counted() bool {
	pl := placeFor(p)
	return pl != nil && pl.counted()
}

// parsePageWord is the typed surface's half of the tab bar: a person who types
// `sta` is offered the standing place beside the chats that match (SCREEN 1g).
//
// AN EXACT WORD BEATS A PREFIX, and a prefix that fits two places is no answer
// at all — `s` is `standing`, `spend`, `search` and `settings` at once, and
// offering the first of those would be the surface guessing. So an ambiguous
// prefix offers nothing, and the person types one more letter.
func parsePageWord(s string) (page, bool) {
	word := strings.ToLower(strings.TrimSpace(s))
	if word == "" {
		return 0, false
	}
	for _, id := range pages() {
		if id.word() == word {
			return id, true
		}
	}
	found, count := page(0), 0
	for _, id := range pages() {
		if strings.HasPrefix(id.word(), word) {
			found, count = id, count+1
		}
	}
	return found, count == 1
}

// ── the counts a tab may wear ───────────────────────────────────────────────

// placeCounts is the seam the tab bar's numbers come through, and it is stated
// as an interface here because the records that answer it live in
// internal/session and are another lane's to build.
//
// A TAB WEARS A COUNT ONLY WHEN SOMETHING IN IT CHANGED. Not how many things are
// in there — a permanent `20` beside `home` is furniture, and furniture is what
// people stop seeing. The question is "how many things in this place have moved
// since you last looked AT THIS PLACE", which needs one look stamp per place
// rather than the single stamp home writes today, and until those stamps exist
// this seam is nil and every tab is bare. THAT IS THE CORRECT EMPTY STATE and
// not a gap: the emptiness law says an unknown number is drawn as nothing.
//
// It must NOT block. The tab bar is drawn on every frame of every place, so an
// implementation that walks a directory here is an implementation that walks it
// sixty times a second — the same law every home seam is held to (tui3.go).
type placeCounts interface {
	// ChangedIn is how many things in one place have moved since that place was
	// last looked at. Zero, or any negative number, draws nothing.
	ChangedIn(place string) int
}

// placeCount is the count for one place, or zero when nothing answers.
func (a *app) placeCount(id page) int {
	if a.places == nil || !id.counted() {
		return 0
	}
	if n := a.places.ChangedIn(id.word()); n > 0 {
		return n
	}
	return 0
}

// ── the tab bar ─────────────────────────────────────────────────────────────

// placeTabBar is the second row of every place: the four words ([barPages]), the
// one you are standing in wearing the band, and a number beside any place that
// has something new in it.
//
// IT IS [sheetTabBar] WITH THE TITLES PASSED IN, and it is drawn with that
// function's own geometry — [tabLead], [tabGap], [tabPad] — for the reason that
// function's comment already gives: "this panel IS a tab bar — the same object
// the task strip is, drawn the same way, so that 'which page am I on' is one
// visual question across the app rather than two". The settings panel keeps its
// own inner bar under this one, and the two are told apart by what they are
// made of rather than by a decoration: this one is the places, that one is
// settings' own sections.
//
// ── THE WIDTH LADDER ────────────────────────────────────────────────────────
//
// A bar that is cut in half is a bar that lies about how many places there are,
// so it gives up words in a stated order rather than being trimmed:
//
//  1. every word, every count, with the bar's own air between the chips —
//     while they fit;
//  2. EVERY WORD AGAIN, WITH THE AIR GIVEN UP. When the bar was seven words,
//     they and the padding each chip carries were fifty-seven cells and the air
//     between them six more, so a sixty-column terminal — a split pane, an ssh
//     session from a train, a phone — overshot by three and fell all the way
//     past the middle rung to the single word `home`, because on a quiet machine
//     no place wears a count. The words are what this row is FOR and the space
//     between them is not, so the space is what goes first.
//  3. as many words as fit, in the bar's own order, always carrying the place
//     you are standing in and any place wearing a count, and ending with a dim
//     count of the places that did not fit ([barMoreWord]).
//
// THE BAR IS THE SIGN AND THE FOOT IS THE ROUTE. A row this narrow cannot say
// `tab next place` as well as the words — at rung 3 there are not seven cells
// spare for it — so what the bar owes a person is that the other rooms EXIST,
// and the key that reaches them is on the foot of every place
// ([placeHintTail]), which [hintFit] protects to the last cell there is. A bar
// collapsed to the word `home` said neither of those things, and every other
// place was undiscoverable on exactly the tier where a person is least able to
// go looking for them.
//
// `numbered` is the map ([app.mapShowing]): every chip grows the digit that
// jumps to it, in the cells the words were already in, and the places off the
// bar are drawn after them with theirs — nothing moves that a person has to
// re-find when the map goes away, and the three digits the bar does not show
// are shown where the keys are.
func (a *app) placeTabBar(width int, numbered bool, pal palette) string {
	every := func(page) bool { return true }
	if full, spans, ok := a.tabBarAt(width, numbered, pal, every, tabGap, 0); ok {
		a.tabs = spans
		return a.placeBarMachine(full, width, pal)
	}
	if tight, spans, ok := a.tabBarAt(width, numbered, pal, every, 0, 0); ok {
		a.tabs = spans
		return a.placeBarMachine(tight, width, pal)
	}
	keep, elided := a.barWordsAt(width, numbered)
	some, spans, _ := a.tabBarAt(width, numbered, pal, func(id page) bool { return keep[id] }, 0, elided)
	a.tabs = spans
	return a.placeBarMachine(some, width, pal)
}

// barMoreWord is the count of places a narrow bar could not carry, in the
// spellings [rowfit.go]'s law 2 asks a fact to degrade through: `▸ 3 more` while
// there are cells for it, and `▸ 3` when there are not.
//
// IT IS THE SURFACE'S ONE FOLD SENTENCE ([foldSpellings]) AND NO LONGER A `+`.
// This row and the command menu's own tail mean the same thing — a navigation
// list has more items than fit — and they were two writers with two spellings:
// `+3 more` here against `▸ 3 more` there, so a person could not tell whether
// `+3` was a count, a badge or a door. The mark is the half that says which, and
// it is on every rung of the ladder: the word `more` gives way before `▸` does.
//
// IT IS A SIGN AND NOT A DOOR, and that is decided rather than unfinished: it
// opens nothing, wears no cursor and claims no span, exactly as the machine's
// name at the other end of this row does ([placeBarMachine]). A chip that
// carried a press would have to pick one of the places it stands for, and the
// key that reaches them all in order is `tab`.
func barMoreWord(n, room int) string {
	if n <= 0 {
		return ""
	}
	for _, say := range foldSpellings(n, "") {
		if tabPadCols+ansi.StringWidth(say) <= room {
			return say
		}
	}
	// AND A FRAME WITH NO ROOM EVEN FOR `+6` SAYS NOTHING, rather than running
	// past its own edge. A count that overflowed the row would be this ladder
	// committing the fault it exists to prevent.
	return ""
}

// barChipWord is the word one place's chip carries: its own word, the digit the
// map grows in front of it, and the count behind it. It is factored out of
// [app.tabBarAt] so the ladder can MEASURE a chip without painting one, and so
// the measurement and the paint can never come to disagree about how wide a
// word is.
func (a *app) barChipWord(id page, numbered bool) string {
	word := id.word()
	if numbered {
		word = itoa(placeDigitOf(id)) + " " + word
	}
	if n := a.placeCount(id); n > 0 {
		word += " " + itoa(n)
	}
	return word
}

// barWordsAt chooses the words a bar too narrow for all seven carries, and says
// how many it had to leave off.
//
// THE MANDATORY HALF FIRST: the place you are standing in and the word under the
// cursor may never go ([app.barKeeps] holds that argument), and neither may a
// place wearing a count, because a number is this row saying something moved in
// a room you are not standing in.
//
// THEN THE ROW IS FILLED IN THE BAR'S OWN ORDER AND STOPS AT THE FIRST WORD
// THAT WILL NOT FIT — [rowfit.go]'s law 3 said about words instead of facts. A
// fill that skipped `standing` because `spend` was shorter would draw a
// different four places at every width, and `alt+1` … `alt+7` name positions
// that never move; a prefix plus your own word is a reading a person can learn.
//
// The count's own cells are reserved out of the fill, measured against the
// longest spelling this row could end up drawing, because a bar that spent its
// last cells on one more word and then had no room to say two others exist
// would be the collapse this ladder is here to prevent, one word later.
func (a *app) barWordsAt(width int, numbered bool) (map[page]bool, int) {
	shown := barPages(a.page, numbered)
	cost := func(id page) int { return ansi.StringWidth(a.barChipWord(id, numbered)) + tabPadCols }
	keep := make(map[page]bool, len(shown))
	spent := tabLead
	for _, id := range shown {
		if a.barKeeps(id) || a.placeCount(id) > 0 {
			keep[id] = true
			spent += cost(id)
		}
	}
	folds := foldSpellings(len(shown), "")
	reserve := tabPadCols + ansi.StringWidth(folds[len(folds)-1])
	for _, id := range shown {
		if keep[id] {
			continue
		}
		if spent+cost(id)+reserve > width {
			break
		}
		keep[id] = true
		spent += cost(id)
	}
	elided := 0
	for _, id := range shown {
		if !keep[id] {
			elided++
		}
	}
	return keep, elided
}

// placeMachineLead is the word in front of the machine's name at the right end
// of the bar. It is there so that a bare `spark` in the row the seven places are
// drawn in cannot be read as an eighth place.
const placeMachineLead = "on "

// placeBarMachine puts the MACHINE THESE PLACES ARE ABOUT at the right end of
// the tab bar, and puts nothing there at all on a local session.
//
// THE PLACES FOLLOW THE SESSION'S MACHINE NOW, AND A ROOM THAT MOVED WITHOUT
// SAYING SO WOULD BE THE SAME FAULT WALKED BACKWARDS. Home used to draw one
// sentence saying its rows belonged to the wrong machine; it draws the right
// machine's rows instead ([app.readWorld]) — so the thing a person cannot see
// any more is WHOSE work they are reading, and the fix is a name rather than a
// sentence, because it is true on every frame of every place rather than in one
// state of one of them.
//
// IT IS [app.host] AND NOT A SECOND SPELLING OF IT. The status line's place
// segment writes `spark:app`, /status writes `spark:/srv/code/app`, and the
// legend under the input writes `spark · porting the parser` — three renderings
// of one field, which host.go's header states as the law that the connection is
// shown as the place and nowhere else. This is the fourth, and it is the machine
// alone because a place is a listing of a whole disk rather than of one
// workspace.
//
// AND IT DISAPPEARS COMPLETELY ON A LOCAL SESSION, which is the test host.go
// holds every indicator to: it is invisible when there is nothing to say. It
// also gives up its cells before the bar gives up a word — the places are what
// the row is for, and a name that pushed `search` off the end would be telling
// somebody about a machine instead of about their own rooms.
func (a *app) placeBarMachine(bar string, width int, pal palette) string {
	name := strings.TrimSpace(a.host)
	if name == "" {
		return bar
	}
	word := placeMachineLead + name
	used, room := ansi.StringWidth(bar), ansi.StringWidth(word)
	// tabLead's worth of air at each end, and tabGap between the last chip and
	// the name, so the row breathes the way every other row of this bar does.
	if used+tabGap+room+tabLead > width {
		return bar
	}
	return bar + strings.Repeat(" ", width-used-room-tabLead) + pal.dim(word)
}

// barKeeps is the word the ladder may never give up: the place you are standing
// in, and — while the cursor is on the bar — the word the cursor is on.
//
// A CURSOR ON A WORD THE LADDER DROPPED WOULD BE A CURSOR NOBODY CAN SEE, which
// is SCREEN 3a's clause said about a row rather than a key: nothing on this
// surface acts on something that is not drawn. So the narrow bar carries the
// cursor's word whether or not that place has anything new in it, and `←`/`→`
// walk cells a person is actually looking at.
func (a *app) barKeeps(id page) bool {
	return id == a.page || (a.bar.on && id == a.bar.at)
}

// placeTabSpan is where one place's CHIP sits on the bar, so the draw and the
// press agree about it. It is the settings panel's [tabSpan] with the place it
// belongs to carried on it — the bar gives up words as the frame narrows
// (the ladder above), so a span computed from the list of places rather than
// from the bar that was actually painted would open whichever room happened to
// sit at that position on a wider terminal.
//
// It covers the chip's padding as well as its word, for the reason [tabSpan]
// gives: the cell beside `tasks` is part of tasks, because a one-cell miss
// between two words is a miss people make.
type placeTabSpan struct {
	id       page
	from, to int
}

// tabBarAt draws the bar over the places `keep` admits, says where each chip
// landed, and says whether it fit.
//
// `gap` is the air between two chips, which the ladder above gives up before it
// gives up a word, and `elided` is how many places are not on this bar at all —
// drawn as [barMoreWord] at the end of the row, in the cells that are left.
func (a *app) tabBarAt(width int, numbered bool, pal palette, keep func(page) bool, gap, elided int) (string, []placeTabSpan, bool) {
	line, plain := strings.Repeat(" ", tabLead), strings.Repeat(" ", tabLead)
	shown := barPages(a.page, numbered)
	spans := make([]placeTabSpan, 0, len(shown))
	at, first := tabLead, true
	for _, id := range shown {
		if !keep(id) {
			continue
		}
		if !first {
			line += strings.Repeat(" ", gap)
			plain += strings.Repeat(" ", gap)
			at += gap
		}
		first = false
		// THE MAP GROWS THE NUMBER IN THE CELL THE WORD WAS ALREADY IN
		// (SCREEN 3b). Nothing shifts, nothing pops up, and letting go of the
		// map leaves the bar exactly where the eye left it ([app.barChipWord]).
		word := a.barChipWord(id, numbered)
		chip := tabPad + word + tabPad
		band := ansi.StringWidth(word) + tabPadCols
		switch {
		case a.bar.on && id == a.bar.at:
			// THE CURSOR'S OWN BAND, AND IT REPLACES THE SELECTED MARK RATHER THAN
			// STACKING ON IT. While the cursor is up here the bar is the row a
			// person is standing on, and the question the frame has to answer is
			// "where is my cursor" — not "which room am I in", which the body
			// underneath is already answering with every one of its rows. Two
			// grounds on one word would be the screen saying both at once and
			// neither clearly ([barCursor]).
			line += pal.cursor(pal.bold(pal.ink(chip)), band)
		case id == a.page:
			// THE WORD YOU ARE STANDING IN IS TIER 1, BOLD, AND NOT AN ACCENT.
			// SCREEN 2a's first level is spelled out: "1 · page — bright, bold,
			// one word, only in the tab bar", and the accent on a place is spent
			// on the two live states and on nothing else (styles.go's THE
			// ONE-ACCENT LAW). The band under it is what says "here".
			line += pal.selected(pal.bold(pal.ink(chip)), band)
		case id == a.tabHover:
			// AND THE POINTER LIFTS THE WORD AND DOES NOTHING ELSE: the selected
			// word's own ink and weight, with no band under it. A word that grew a
			// ground on hover would look like the room a person was standing in,
			// and a bar with two banded words on it says nothing at all; a word
			// that lifted a tier says "this one is a door", which is the whole of
			// what a pointer resting on it has learned.
			line += pal.bold(pal.ink(chip))
		default:
			line += pal.dim(chip)
		}
		plain += chip
		spans = append(spans, placeTabSpan{id: id, from: at, to: at + ansi.StringWidth(chip)})
		at += ansi.StringWidth(chip)
	}
	// AND THE COUNT OF WHAT IS NOT HERE RIDES THE END OF THE ROW, with no span
	// behind it: it is a sign, and [barMoreWord] says why it is not a door.
	if more := barMoreWord(elided, width-ansi.StringWidth(plain)); more != "" {
		chip := tabPad + more + tabPad
		line += pal.dim(chip)
		plain += chip
	}
	return line, spans, ansi.StringWidth(plain) <= width
}

// ── THE BAR IS A ROW THE CURSOR CAN STAND ON ────────────────────────────────

// barCursor is the tab bar as a ROW, and not only as a set of targets.
//
// Every place's body is a list walked with `↑` and `↓`, and the bar over it was
// the one row of the frame that only a pointer could reach. A hand on the
// keyboard had `tab`, which is a walk with NO CURSOR IN IT: every step opens the
// room it lands on, closes the one it left and throws away that room's filter,
// so looking along the seven words cost seven openings. So `↑` off the first row
// of ANY body lands here, `←`/`→` walk the words and open nothing at all, and
// `enter` or `↓` goes into the one under the cursor. Reading the bar is free
// again, which is what a cursor is for.
//
// IT IS FRAME STATE AND NO PLACE HAS A WORD TO SAY ABOUT IT. The bar belongs to
// the router — it is drawn on all seven places, in the same cells, by one
// function — so a place that kept a flag about the cursor having left it would
// be seven answers to one question, and the seventh would be the one that
// forgot. The law is pinned by [TestNoPlaceFileMentionsTheBar].
//
// AND IT IS NOT A MODE. `tab`, `shift+tab` and `alt+1`…`alt+7` mean exactly what
// they mean everywhere else while it is up, a printable character goes to the
// composer exactly as it does everywhere else — taking the cursor back down into
// the body with it — and `esc` puts the cursor back where it came from. Nothing
// is captured; one row of the frame gained a cursor.
type barCursor struct {
	// on is whether the cursor is on the bar rather than in the body.
	on bool
	// at is the place whose word wears the cursor's band, and it is NOT
	// [app.page]. Walking the bar moves this and opens nothing, which is the
	// whole difference between a cursor and `tab`.
	at page
}

// barHover records which place's word the POINTER is resting on, and repaints
// only when that is news. [pageNone] is "the pointer is not on a word of the bar
// at all", which covers the gap between two chips and every row that is not the
// bar.
//
// MOTION IS THE CHEAPEST AND COMMONEST MESSAGE THIS SURFACE GETS — a pointer
// crossing the window sends one per cell — so a hover that repainted on every
// one of them would be a screen redrawn eighty times for a highlight that did
// not move (placemouse.go's [placeHoverMoved] states the same rule for a body
// row).
func (a *app) barHover(id page) {
	if id == a.tabHover {
		return
	}
	a.tabHover = id
	a.touch()
}

// barRaise puts the cursor on the bar, on the word of the room it is standing
// in.
func (a *app) barRaise() {
	a.bar = barCursor{on: true, at: a.page}
	// AND THE ROW'S VERBS GO WITH IT. A strip's letters are about the row the
	// cursor was on (verbstrip.go), and the cursor is not on a row any more —
	// letters left bound over a bar nobody can act from would be exactly the
	// lottery that file exists to prevent.
	a.closeStrip()
	a.touch()
}

// barDrop puts the cursor back into the body, on the row it left.
//
// THE PLACE'S OWN CURSOR NEVER MOVED. `↑` onto the bar is claimed by the router
// before the place ever sees it ([app.barReach]), so the row a person walked up
// off is still the row they walk back down onto — which is what makes `↑` then
// `↓` cost nothing, and what makes "the first `↓` from the bar lands on the
// first stop of the body" true without anybody having to put it there.
func (a *app) barDrop() {
	if !a.bar.on {
		return
	}
	a.bar = barCursor{}
	a.touch()
}

// barWalk is `←` and `→` along the bar: the next word, and round again from the
// end.
//
// IT WRAPS AND DOES NOT CLAMP, which is the one place on this surface a cursor
// does. Every list here clamps because a list has a top and a bottom a person is
// reading towards; the bar is a RING — it is the circle `tab` already walks
// ([nextPage]), and stopping the cursor dead at `settings` would make the two
// keys disagree about the same words.
//
// IT WALKS THE WORDS ON THE BAR, which is the ring drawn from where the person
// is standing rather than from where the cursor is: standing in memory, the
// bar carries `memory` after the four, and a cursor that walked a ring without
// it would step over a word it can see.
func (a *app) barWalk(back bool) {
	a.bar.at = nextOn(barPages(a.page, a.mapShowing), a.bar.at, back)
	a.touch()
}

// barEnter is `enter` or `↓` on the bar: into the place under the cursor.
//
// THE CURSOR COMES DOWN EITHER WAY. Pressing the word you are already standing
// in is not a door — going there would close and reopen the room, throwing away
// the filter somebody typed and the row they were on, which is the same law the
// pointer already keeps ([app.placeTabPress]) — so it simply puts the cursor
// back in the body.
func (a *app) barEnter() tea.Cmd {
	at := a.bar.at
	a.barDrop()
	if at == a.page {
		return nil
	}
	return a.showPage(at)
}

// barReach reports that `↑` from where the cursor is standing lands on the bar.
//
// THREE THINGS HAVE TO BE TRUE, and the first of them is SCREEN 3a's clause: no
// key does anything that is not drawn on screen right now. The bar has to have
// been PAINTED — home's phone inbox and the tasks place's record card draw
// something else in those cells entirely ([app.frame] puts [app.tabRow] back to
// -1 before every frame) — the cursor must not already be up there, and the
// place's own cursor has to be on the first row of its body a cursor may stand
// on. Anywhere else `↑` is the walk it has always been, and the place keeps it.
func (a *app) barReach() bool {
	pl := a.showing()
	if pl == nil || a.bar.on || a.tabRow < 1 {
		return false
	}
	// HOME'S COLUMN HAS NO WAY UP ONTO THE BAR (owner, 2026-09-17: "don't let
	// users scroll up out of the left column onto the tabs"). `↑` at the top of
	// the field stays where it is; the bar is reached by a press on it, `tab`,
	// or a place's own chord. Every other place keeps the walk.
	if pl.id() == pageHome {
		return false
	}
	stops := pl.stops(a)
	if len(stops) == 0 {
		// A ROOM WITH NOTHING IN IT STILL HAS A BAR OVER IT. An almost-empty page
		// spends the whole screen saying what it is for (SCREEN 1a) and has no row
		// to stand on at all; `↑` from it must reach the bar rather than being the
		// one key that does nothing on the one page that most needs a way onward.
		return true
	}
	return pl.cursorAt(a) <= stops[0]
}

// barKey is every key while the cursor is on the bar. It reports whether it took
// the key; a key it does not take means on the bar exactly what it means in the
// body.
func (a *app) barKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.bar.on {
		return nil, false
	}
	switch msg.String() {
	case "left":
		a.barWalk(true)
		return nil, true
	case "right":
		// AND `→` DOES NOT OPEN A VERB STRIP UP HERE. The strip is a second
		// reading of the ROW under the cursor, and a fold is a row's too; the bar
		// is not a row of any place's reading, so both of those arrows are the
		// walk along the words and nothing else (verbstrip.go).
		a.barWalk(false)
		return nil, true
	case "down", "ctrl+n", "enter":
		return a.barEnter(), true
	case "esc":
		// esc BACKS OUT ONE LAYER, which is the settings panel's rule kept: the
		// cursor comes down off the bar, and the second esc is the place's own —
		// it leaves the room.
		a.barDrop()
		return nil, true
	case "up", "ctrl+p":
		// THERE IS NOTHING OVER THE BAR. Row zero is the pulse, which is telemetry
		// and not a control, so `↑` here is a key that has arrived at the top —
		// swallowed rather than falling through into the body it just left.
		return nil, true
	}
	// AND EVERY PRINTABLE CHARACTER GOES WHERE IT ALWAYS GOES, taking the cursor
	// back down into the body with it. "any letter goes to the composer, on every
	// page, always" is the second of the six classes (SCREEN 3a) and it has no
	// asterisk: a person who starts typing has stopped looking at the bar, and a
	// surface that swallowed the first letter of their sentence would have made
	// the bar a mode.
	if msg.Key().Text != "" {
		a.barDrop()
		return nil, false
	}
	return nil, false
}

// ── the frame every place is drawn in ───────────────────────────────────────

// placeHit is whatever ONE place resolves a pointer against, in that place's own
// vocabulary: the task page answers in `taskSheetHit`, the settings panel in
// `sheetHit`, home in a `homeMark` of a line and a pane, and the four list places
// in a line number. A router that flattened all of them into one int would be a
// router that lets a click land on a row the draw did not put there.
//
// IT IS `any` BECAUSE THE FRAME IS ONE FUNCTION AND GO METHODS TAKE NO TYPE
// PARAMETERS. The rows used to carry the hit as a type parameter, which worked
// while each place had a frame function of its own; the [place] interface below
// is the one contract every place answers, and an interface method cannot be
// generic. So the frame carries the hits opaquely and hands them straight back,
// and the ONE file that knows what a hit means for a place is that place's own —
// each casts its own map back with a `hit.(taskSheetHit)` beside the body that
// wrote it. Nothing between the two ever looks inside.
type placeHit = any

// placeRow is one row of a place's body: the text, and what that row answers to
// the pointer.
type placeRow struct {
	text string
	hit  placeHit
}

// placeDraw draws ONE place: its own frame where it has one, and the shared
// frame otherwise. It is the only door onto a place's rows — a place's own
// `<word>Frame` function is a two-line shim over this that casts the hit map
// back into that place's vocabulary.
func (a *app) placeDraw(pl place, width, height int) ([]string, []placeHit, int, int) {
	// AND THE VERB STRIP IS DROPPED HERE IF THE CURSOR HAS LEFT ITS ROW. Every
	// way a cursor can move ends in a frame — a press, a hover, a wheel tick, a
	// filter re-ranking the list, the three-second beat re-reading it — so this
	// one call, ahead of the split between a place's own frame and the shared
	// one, is what makes the strip's binding to a row true for all seven
	// (verbstrip.go's [app.holdStrip]).
	a.holdStrip()
	// AND THE MACHINE IS ASKED BEFORE THE ROWS ARE, on every place and ahead of
	// [place.ownFrame]. A place whose reading is this process's disk while the
	// session runs somewhere else has one honest thing to draw and it is not a
	// list; a room that took its own frame first would draw the wrong machine's
	// rows inside its own chrome, which is exactly how the tasks place came to
	// show a laptop's work under a server's conversation (host.go).
	if line := pl.remote(a); line != "" {
		return placeFrameWithBar(a, width, height,
			func(width, room int) []placeRow {
				return placeTeachRows(placeTeachProse(line, width, a.pal), room)
			},
			func(width int) (string, placeHit, bool) { return pl.bar(a, width) })
	}
	if lines, hits, caretX, caretY, own := pl.ownFrame(a, width, height); own {
		return lines, hits, caretX, caretY
	}
	return placeFrameWithBar(a, width, height,
		func(width, room int) []placeRow { return pl.body(a, width, room) },
		func(width int) (string, placeHit, bool) { return pl.bar(a, width) })
}

// placeFrameNow draws whatever place is standing, and nothing at all in the
// conversation. It is what view.go's frame reaches for, so THE FRAME NEVER
// KNOWS A PLACE BY NAME: which room is up is [app.page], and which rows that
// room has is the registry's answer.
func (a *app) placeFrameNow(width, height int) ([]string, []placeHit, int, int, bool) {
	pl := a.showing()
	if pl == nil {
		return nil, nil, 0, 0, false
	}
	lines, hits, caretX, caretY := a.placeDraw(pl, width, height)
	return lines, hits, caretX, caretY, true
}

// placeHitsOf reads one place's hit map back in that place's own vocabulary.
//
// IT IS CALLED FROM THE PLACE THAT WROTE THE MAP AND FROM NOWHERE ELSE. The
// frame carries the hits opaquely ([placeHit] says why); this is the one step
// back across that boundary, and it is deliberately a plain function rather than
// anything the interface exposes — a row of the frame's own chrome (the pulse,
// the tab bar, the composer) carries no hit at all, and `blank` is what that row
// means to the place asking.
func placeHitsOf[H any](hits []placeHit, blank H) []H {
	out := make([]H, len(hits))
	for i, hit := range hits {
		if got, ok := hit.(H); ok {
			out[i] = got
			continue
		}
		out[i] = blank
	}
	return out
}

// placeLineHits is [placeHitsOf] for the places whose rows answer with a LINE OF
// THEIR OWN BODY — the standing list, memory, spend and search all do — where a
// row that answers to nothing is -1.
func placeLineHits(hits []placeHit) []int { return placeHitsOf(hits, -1) }

// placeFrame is THE frame. Every place is drawn in it, and the head, the foot
// and the clamp below belong to the router rather than to any place:
//
//	row 0        the pulse — this machine's vital signs (pulse.go)
//	row 1        the tab bar — the seven places, and where you are
//	row 2        a dim rule
//	row 3        blank
//	...          the body — the place's own rows
//	...          blank, then a dim rule
//	...          the composer, with the scope chip at the right of its box row
//	...          the strip — the row's verbs, or an answer that can be given here
//	...          the hint line
//
// THE FOOT IS MEASURED BEFORE THE BODY IS GIVEN ITS ROOM. A draft that wraps to
// a second and third row takes those rows FROM the body, never from the frame —
// home learned that the hard way and the whole surface inherits the lesson here
// ([app.homeFrame]'s own note has the story).
//
// THE FRAME IS EXACTLY THE WHOLE TERMINAL, at every width, with no borders and
// no viewport. A frame too short for its own contents keeps row 0 and the last
// `height-1` rows, AND THE CARET RIDES THAT CLAMP: coordinates computed before
// the cut would leave the terminal's cursor standing a row below the box.
func placeFrame(a *app, width, height int, body func(width, room int) []placeRow) ([]string, []placeHit, int, int) {
	return placeFrameWithBar(a, width, height, body, nil)
}

// placeFrameWithBar is [placeFrame] with the hint line replaced by a BAR a thumb
// can press.
//
// At [tierPhone] a line naming four keys is a line naming four keys nobody has,
// and the way out has to be a target rather than a legend — which is the rule
// the task page and home's own phone sheet both already followed with their own
// feet. The bar carries the place's own hit so the press resolves against the
// row that was actually drawn, exactly as every other row on the frame does.
func placeFrameWithBar(a *app, width, height int,
	body func(width, room int) []placeRow, bar func(width int) (string, placeHit, bool)) ([]string, []placeHit, int, int) {
	// THE PLACE LADDER IS IN FORCE FOR THE WHOLE OF THIS FRAME, and it is put back
	// before this function returns (styles.go's [palette.onPlaces]). Every row
	// below — the pulse, the tab bar, the body the place itself builds, the
	// composer and the foot — asks `a.pal` for its colours, so re-pointing three
	// roles here is what makes THE ONE-ACCENT LAW reach two thousand call sites
	// without one of them being edited. The inks are the conversation's own; what
	// a place does not do is draw the question's violet or the tick's olive.
	was := a.pal
	a.pal = was.onPlaces()
	a.pal.placeRows = !a.at(pageHome)
	defer func() { a.pal = was }()
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]placeHit, 0, height)
	add := func(text string, hit placeHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	// THE HEAD IS THE CONVERSATION'S HEAD, drawn by the same function with the
	// bar as its middle row (head.go).
	//
	// THE BAR IS ROW ONE AND THE POINTER IS TOLD SO HERE. A press arrives as a
	// row of the terminal, and the only honest way to know which row the bar
	// ended up on is to record it where it was drawn — the clamp below can cut
	// it off a frame too short for its own contents, and a press resolved
	// against a constant would then open a place for a click on a body row.
	a.tabRow = placeTabRow
	for _, row := range a.headRows(width, a.placeTabBar(width, a.mapShowing, pal), pal) {
		add(row, nil)
	}

	// ONE BOX, ON HOME ([place.box] states the ruling). Every other place's foot
	// is the blank, the rule with the place's note on it, and the hint — three
	// rows — and a person who wants to start something presses `tab` to home,
	// whose rule already says where it will land.
	hasBox := a.at(pageHome)
	box := a.placeBox()
	var draftRows []string
	var draftCX, draftCY int
	switch {
	case a.targetPickShowing():
		// THE BOX IS THE LIST'S FILTER WHILE THE TARGET'S MODEL LIST IS UP, exactly
		// as the conversation's box is the picker's (input.go's [app.inputBlock]):
		// what a person types narrows the rows above, and the row has to show
		// them the letters they typed rather than the resting sentence. The
		// picker's own hint stands in while nothing is typed.
		draftRows, draftCX, draftCY = draftBlock(&a.target.pick.filter, pal, width-2, 1,
			a.target.pick.hintAt(width-2-ansi.StringWidth(prompt)), "")
	case hasBox && box != nil && len(box.value) > 0:
		// Blank lines still hold the caret. The send-time emptiness check
		// discards whitespace, but the editor must draw everything typed.
		draftRows, draftCX, draftCY = draftBlock(box, pal, width-2, homeDraftRows, "", "")
	}
	// THE BOX HAS A FLOOR ([boxFloor]) AND EVERY BRANCH ABOVE IS HELD TO IT, the
	// target's filter included. The rows that make it up are added BELOW what was
	// typed: padding above would move the first line a person typed off the first
	// row, and the caret's own row is derived by subtracting this block's height
	// from the rows placed below — so a pad at the bottom moves both by the same
	// amount and the caret stays on the letter it is on.
	//
	// An empty block means the box is at REST and the rows are drawn as its
	// silhouette further down, so it is left empty here rather than padded into
	// a surface a press could land in.
	for floor := boxFloor(height); len(draftRows) > 0 && len(draftRows) < floor; {
		draftRows = append(draftRows, "")
	}
	// THE BOX IS THE SAME HEIGHT TYPED IN OR NOT ([homeDraftFloor]). At rest it
	// is the place's dim sentence with the same rows under it, so the one thing
	// on this screen a person types into is a block they can see before they
	// have typed anything — which is the whole point, since somebody who cannot
	// find the box has nothing to type into it. It also means the foot does not
	// move on the first keystroke: a box that jumped from one row to three the
	// moment a letter landed would shift the list up under the hand that was
	// reaching for it.
	draftHeight := 0
	if hasBox {
		draftHeight = len(draftRows)
		if floor := boxFloor(height); draftHeight < floor {
			draftHeight = floor
		}
	}
	// THE VERB STRIP IS A ROW OF THE BODY AND THE ANSWER STRIP IS A ROW OF THE
	// FOOT, and they are asked for separately because they are two different
	// claims. The verbs belong to ONE ROW and are drawn under it, pushing the
	// list down by their own height (SCREEN 3c, verbstrip.go); home's answer
	// chips are what a conversation ANOTHER window is holding is waiting for,
	// and they belong beside the composer that could answer them.
	inline := a.verbStripRow(width)
	strip := a.placeStrip(width)
	// THE NOTE RIDES THE RULE, SO THE FOOT IS ONE HEIGHT ON EVERY PLACE. It used
	// to be a row of its own under the rule, and a place with a note (tasks,
	// settings) drew its rule one row higher than a place without one — `tab`
	// between them moved the rule and the body's bottom edge, and the first task
	// to land moved it again inside tasks (PLACES-AUDIT.md finding 1). As the
	// rule's legend it costs no row at all, which is the shape the conversation's
	// seam and home's target already have. Home keeps its notes as rows because
	// its rule is already a legend of its own ([app.targetLegend]).
	//
	// A note builder fits its words to the width it is handed less the two cells
	// its own row spent on a lead and a margin, so it is handed the rule's room
	// plus those two — and drops a whole clause, rather than the rule cutting one.
	//
	// AND WHERE THE RULE IS THE DRAFT'S SEAM (every place with a draft,
	// boxseam.go) the note rides that seam's right slot instead, through
	// [app.placeNoteLegend] inside [app.targetLegend] — still no row, still one
	// foot height on every place. What is built here is the note for the one
	// rule that is not a seam (settings).
	var legend []string
	if !hasBox {
		legend = a.placeNote(width - placeNoteRuleFrame + 2)
	}
	// AND THE TRAY IS A ROW OF THE FOOT, directly over the box, exactly where the
	// conversation draws it (attach.go, input.go's [app.inputBlock]). It is
	// measured with the foot for the composer layer's reason: a row that appeared
	// without being counted would push the box down by a cell the moment
	// somebody dropped a picture on the screen.
	tray := a.placeTray(width)
	// THE COMPOSER LAYER'S ROWS ARE PART OF THE FOOT AND ARE MEASURED WITH IT.
	// The layer's whole claim is that the box does not move (composerlayer.go),
	// which is only true if its three lines are taken from the BODY's room the
	// same way a draft's second and third rows already are — a foot measured
	// after the body would push the box down by three cells the moment the chord
	// was pressed.
	layer := a.composerRows(width, pal)
	foot := 2 + len(tray) + draftHeight + len(strip) + len(layer)
	room := height - len(lines) - foot - spacingRuleClearance
	if room < 1 {
		room = 1
	}
	// THE BODY IS BUILT INTO THE ROOM THE STRIP LEAVES IT, which is what makes
	// the displacement exact: the list gives up precisely as many rows as the
	// strip takes, and the frame is the same height it was before `→` was
	// pressed. On a frame with no room to give the strip is not drawn at all,
	// and the letters go with it — a strip nobody can see is a strip whose
	// letters are a lottery (verbstrip.go's first law).
	//
	// AND THE COMPOSER LAYER TAKES THE STRIP DOWN WITH THE REST OF THE PAGE. The
	// layer has claimed the whole keyboard (composerlayer.go), so every letter on
	// a strip drawn under it would be a letter that does nothing — which is the
	// one state this surface may never be in.
	// A wide home keeps row options beneath the middle-column description,
	// so opening them takes no space from the list or its pointer map.
	if a.homeStripInDescription(width, room) {
		inline = nil
	}
	bodyRoom := room - len(inline)
	// AND THE SWITCHER TAKES THE STRIP DOWN FOR THE LAYER'S REASON EXACTLY: it
	// has claimed the whole keyboard (hop.go), so every letter on a strip drawn
	// under it would be a letter that does nothing.
	if a.composer.open || a.hopShowing() || bodyRoom < 1 {
		bodyRoom, inline = room, nil
	}

	// AND THE PAGE BEHIND DIMS RATHER THAN BEING COVERED. While the layer is up
	// the place's own rows are repainted at the faintest stop of the depth ladder
	// — you never lose your place, and the layer is plainly a layer rather than a
	// new screen (SCREEN 2e). The rows are still the place's own: the frame asks
	// for exactly the body it would have asked for and paints it differently,
	// which is why no place has a word to say about being underneath one.
	drawn := body(width, bodyRoom)
	// AND THE SWITCHER OVER THE PLACE, on the same terms as the layer below it:
	// the place's own rows dim and the card is written over their middle
	// (hop.go). It is asked FIRST because the two can never be up together —
	// [app.hopAvailable] refuses to open over the layer — and because a card
	// drawn under a fade would be a card nobody can read.
	if a.hopShowing() {
		texts := make([]string, len(drawn))
		for i := range drawn {
			texts[i] = drawn[i].text
		}
		a.hop.originY = len(lines)
		texts = a.hopOver(texts, width, pal)
		for i := range drawn {
			drawn[i].text, drawn[i].hit = texts[i], nil
		}
	}
	// AND THE MODEL LIST OVER HOME'S TARGET IS DRAWN THE SAME WAY, for the same
	// reason and through the same body (homedraft.go's [app.targetPickRows]).
	// `/model` at home used to open [app.pick] — a bottom-anchored overlay a
	// place cannot draw and the keyboard cannot reach — which then appeared over
	// the conversation behind home the moment `esc` closed the screen.
	if a.targetPickShowing() {
		drawn = a.targetPickRows(width, bodyRoom, pal)
	}
	if a.composer.open {
		// The model list `alt+o` opens is drawn in the body's room and not over
		// the page, because a place takes the frame whole and the bottom-anchored
		// overlay has nothing under it to sit on (composerlayer.go's
		// [app.composerPickRows], which is the settings panel's own move).
		if a.composer.pick.open {
			drawn = a.composerPickRows(width, bodyRoom, pal)
		} else {
			for i := range drawn {
				drawn[i].text = composerFade(drawn[i].text, pal)
			}
		}
	}
	// AND THE PLACE'S OWN BOX MAY BE ONE OF THESE ROWS, asked before they are
	// added: the settings sheet's key entry and the tasks filter are boxes
	// drawn inside the body ([place.caretRow]), and a caret that stays parked in
	// the foot's rest silhouette while somebody types into either is a cursor
	// blinking in a box that is not the one the words are landing in. The
	// overlays that take the whole keyboard — the composer layer, the
	// switcher, home's model list — are excluded here because their boxes are
	// their own and drawn elsewhere, and the place's row-box has the keyboard's
	// backwards under every one of them.
	rows := placeStripInline(a, drawn, inline)
	ownRow, ownColumn, ownDrawn := -1, 0, false
	if !a.composer.open && !a.hopShowing() && !a.targetPickShowing() {
		if pl := a.showing(); pl != nil {
			ownRow, ownColumn, ownDrawn = pl.caretRow(a, width, rows)
		}
	}
	ownBase := len(lines)
	for _, row := range rows {
		add(row.text, row.hit)
	}
	add("", nil)
	// AND HOME'S RULE IS A LEGEND RATHER THAN A LINE. The other six places have
	// nothing to put on it — you are IN them, and the tab bar four rows up says
	// which — but home's box is a draft for a conversation that does not exist
	// yet, and this is the line a person's eye crosses on the way into it. So it
	// carries the two facts that draft is made of and the two chords that change
	// them, exactly as the conversation's own seam does one row above its box
	// (homedraft.go, foot.go's THE SEAM IS WHO AND WHERE).
	//
	// It is recorded as a local and published below the clamp for [app.boxRow]'s
	// reason: the clamp is what decides which rows this frame really kept.
	targetTop := -1
	ruleLine := placeNoteRule(legend, width, pal)
	if hasBox {
		if line, drew := a.targetLegend(width, pal); drew {
			ruleLine, targetTop = line, len(lines)
		}
	}
	add(ruleLine, nil)

	caretX, caretY := 0, 0
	// THE BOX ROW CARRIES NO CHIP. It used to wear `here ~/src/parser` at its
	// right edge on every place but home, on the argument that `alt+enter` sends
	// a task from anywhere and a verb always in reach has to say where anywhere
	// is. The rule one row up says that now on every place with a draft — `◎
	// new conversation in ~/src/parser · glm-5.3-flash …` — and says it better,
	// with the model, the rung and the gate beside it (boxseam.go); and it is the
	// same folder the layer opens a task in ([app.composerOpensAt]). A chip
	// repeating half of it a row lower was one fact spelled twice on one frame,
	// which is the defect the model's colon suffix made once (effortchip.go).
	// Settings, whose box is a value editor, has no draft and never had a chip
	// that meant anything.
	// AND THE POINTER IS TOLD WHERE THE BOX ENDED UP, on the tab bar's own
	// bargain: a press resolves against the rows that were actually drawn
	// ([app.boxRow], placemouse.go's [app.placeBoxPress]). It is recorded as a
	// local here and published below the clamp, because the clamp is what
	// decides which rows this frame really kept.
	//
	// AT REST THERE IS NOTHING TO PLACE A CARET IN. With nothing typed the row
	// carries a dim sentence about the place rather than a draft, so the span
	// stays empty and a press falls through to the place underneath.
	for _, row := range tray {
		add(row, nil)
	}
	boxTop, boxHeight := len(lines), len(draftRows)
	switch {
	case !hasBox:
		// NO BOX, NO CARET. A blinking bar with nothing under it is a cursor
		// with no box — the law the job page and the task card already keep by
		// hiding it rather than parking it on a title.
		boxTop, boxHeight = 0, 0
		a.caret = false
	case len(draftRows) == 0:
		add(" "+pal.dim(fit(a.placeRestWord(), width-2)), nil)
		// AND THE REST OF THE BLOCK IS HELD OPEN UNDER IT, so the box is the same
		// shape before the first keystroke as after it. The span stays EMPTY
		// (boxHeight is still zero above): these rows are the box's silhouette
		// and not its surface, so a press on them falls through to the place
		// underneath exactly as a press on the resting row always has.
		for row := 1; row < boxFloor(height); row++ {
			add("", nil)
		}
		// AND THE CARET STANDS IN IT, on the first cell the first character will
		// land on — which is the dim sentence's own first letter, exactly as a
		// placeholder sits behind the caret in any other text field. It is the
		// same caret the conversation's box has ([app.View] draws one
		// [tea.CursorBar], blinking, wherever this lands it).
		//
		// IT USED TO BE HIDDEN HERE, on the reading that home at rest is a
		// dashboard somebody reads rather than a thing they type at. The owner's
		// reading (2026-09-15) is the other one: the box is the screen's one
		// primary action (DESIGN §1 law 1), and a box with no caret in it does
		// not look like somewhere to type — which is the same complaint that
		// made the box three rows tall in #1000. The hazard the old comment
		// names is real and is answered by placing the caret rather than by
		// hiding it: unplaced, it blinks at the frame's origin over the `home`
		// heading.
		caretX, caretY = 1+ansi.StringWidth(prompt), boxTop
	default:
		for _, row := range draftRows {
			add(" "+row, nil)
		}
		caretX, caretY = 1+draftCX, len(lines)-len(draftRows)+draftCY
	}
	if caretX > width-1 {
		caretX = width - 1
	}
	// AND THE PLACE'S OWN ROW-BOX TAKES THE CARET FROM THE COMPOSER when its
	// hook answered: the park above is the foot's box, and the hook's is a row
	// of the body — the one box on this frame that the person's keys are
	// actually landing in. A row below zero is the hook saying the box owns the
	// caret and has no line to park it on, and the caret is hidden rather than
	// left blinking in a box that is not the one being typed into. The clamp
	// below adjusts this park by the same law it adjusts the composer's own.
	if ownDrawn {
		if ownRow >= 0 {
			caretX, caretY = min(ownColumn, width-1), ownBase+ownRow
			a.caret = true
		} else {
			a.caret = false
		}
	}
	for _, row := range layer {
		add(row, nil)
	}
	for _, row := range strip {
		add(row, nil)
	}
	switch line, hit, ok := "", placeHit(nil), false; {
	case a.composer.open:
		// AND THE THUMB BAR STANDS DOWN UNDER THE LAYER. A bar is a place's own way
		// out drawn as a target; the layer has taken the keyboard and has a way out
		// of its own, and two feet arguing about what `esc` does is worse at every
		// width than one foot naming the keys that are live.
		add(" "+paintHint(hintFit(a.placeHint(), width-2), pal, pal.dim), nil)
	case bar != nil:
		if line, hit, ok = bar(width); ok {
			add(line, hit)
			break
		}
		fallthrough
	default:
		if msg, ok := a.placeMsgLine(width); ok {
			add(msg, nil)
		} else {
			add(" "+paintHint(hintFit(a.placeHint(), width-2), pal, pal.dim), nil)
		}
	}

	if len(lines) > height {
		removed := len(lines) - height
		// A FRAME TOO SHORT FOR ITS OWN CONTENTS LOSES THE BAR, and the pointer
		// is told that too: -1 is "there is no tab bar on this frame", which is
		// the only answer that cannot turn a press on a body row into a place
		// change.
		a.tabRow = -1
		keep, keepHits := lines[:1], hits[:1]
		lines = append(keep, lines[len(lines)-(height-1):]...)
		hits = append(keepHits, hits[len(hits)-(height-1):]...)
		switch {
		case caretY >= 1+removed:
			caretY -= removed
		case caretY > 0:
			a.caret = false
		}
		// AND THE BOX'S OWN ROWS MOVE WITH THE CARET, by the same arithmetic and
		// under the same rule: a box the clamp pushed off the top is a box with
		// no rows on this frame, and a press must never be resolved against a
		// row that is no longer there.
		if boxHeight > 0 {
			if boxTop >= 1+removed {
				boxTop -= removed
			} else {
				boxHeight = 0
			}
		}
		// AND HOME'S RULE MOVES WITH THEM, by the same arithmetic and under the
		// same rule: a legend the clamp pushed off the top is a legend with no
		// row on this frame, and a press must never be resolved against one.
		switch {
		case targetTop >= 1+removed:
			targetTop -= removed
		case targetTop > 0:
			targetTop = -1
		}
	}
	a.boxRow, a.boxRows = boxTop, boxHeight
	a.targetRow = targetTop
	if targetTop < 0 {
		a.clearTargetSpans()
	}
	for len(lines) < height {
		add("", nil)
	}
	// AND NO GROUND GOES ON AT ALL. A place paints the rows it built and nothing
	// under them: the terminal's own background shows through every cell this
	// frame owns, exactly as it does behind a conversation (styles.go's THE GROUND
	// LADDER, and the reversal note under it). The only lifted cells on the whole
	// frame are the ones a person put a pointer or a cursor on.
	return lines, hits, caretX, caretY
}

// scopeWorkspace is where what is typed will land, as a REAL PATH and with
// nothing pinned: the project the cursor is standing on, then this window's
// own. It is the unpinned half of [app.targetWhere], which the rule over the
// box, `enter` and the composer layer all read.
//
// IT IS ONE ANSWER BECAUSE IT IS ON ONE FRAME TWICE. The rule says where what
// you type will land, and the composer layer's first line says where the task
// will run (composerlayer.go) — one row apart, on the same screen. Two readings
// of "where" that could disagree is exactly the drift the ONE SOURCE OF TRUTH
// law exists for, and they did: the box row's old chip drew this window's
// project on a place that is not home while the errand door fell through to
// the person's home directory, so a person read `here ~/codeaf` and started a
// task in `~`.
func (a *app) scopeWorkspace() string {
	if a.at(pageHome) {
		if line, ok := a.home.previewLine(); ok {
			if where := scopeAddress(line); where != "" {
				return where
			}
		}
	}
	return strings.TrimSpace(a.workspace)
}

// scopeAddress is THE ADDRESS A HOME LINE RECORDS, AND NEVER ITS NAME.
//
// This used to be [homeWhere], which answers a different question and answers it
// correctly: `ctrl+t` asks "which bucket does a fresh conversation in this row's
// project belong to", and a project's NAME is a perfectly good bucket key when
// nothing recorded a path. The chip is asking where a sentence will LAND, and a
// name in that slot is not an address: with the cursor on a conversation the
// chip read `here ~/codeaf` and one row down, on a standing item that
// recorded no directory, `here codeaf` — which cannot be told from a second
// checkout of the same name, and is the exact drift [app.scopeWorkspace]'s own
// header cites ("a person read `here ~/codeaf` and started a task in `~`").
//
// SO EVERY ROW ANSWERS WITH A PATH OR WITH NOTHING, and nothing falls through to
// this window's own workspace, which is what the chip already did for a row that
// records no project at all.
func scopeAddress(line homeLine) string {
	// A conversation: the project directory its journal recorded.
	if path := strings.TrimSpace(line.row.ProjectDir); path != "" {
		return path
	}
	// A standing item: the project root the order belongs to, which
	// [standing.Item.Workspace] holds as a resolved path for exactly this.
	if path := strings.TrimSpace(line.item.Workspace); path != "" {
		return path
	}
	// A project heading: the project's own path, which is an address where its
	// name is not.
	return strings.TrimSpace(line.proj.Path)
}

// The sentences the router says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// placeRestWord is what home's box row says with nothing typed into it: the
	// promise the foot used to open with. It said `say what you want done` on
	// every place until 2026-09-17, when the box came off every place but home
	// and the owner ruled that the one box left says what it is for — both of
	// its readings, the search and the start — and the foot under it keeps
	// only the draft controls ([app.targetChordWords]).
	placeRestWord = "type to search or start something new"
	// placeHintWords is the second line of the composer, AND IT IS THE DESIGN'S
	// OWN SENTENCE WORD FOR WORD (SCREEN 2b, and FIDELITY.md item 3 quotes it as
	// the composer's foot). Four clauses: what enter does, what the chord does,
	// how the map appears, and the way to the next place.
	//
	// TWO THINGS ABOUT IT WERE DRIFT AND ARE NOW FIXED. It said `alt+. map`,
	// which is a key and a noun rather than a key and what it does — every other
	// clause on this line is a verb phrase — and it carried a fifth clause,
	// `esc close`, that the design does not draw here. `esc` still closes: SCREEN
	// 3a puts it in the first of the six key classes, beside ↑↓, enter and tab,
	// as a key that is true on every screen and therefore does not have to be
	// re-advertised on each one.
	placeHintWords = "enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place"
	// placeHintTail is what every other place's hint ends with, appended rather
	// than written into each sentence so that a hint and the router can never
	// disagree about which keys exist.
	//
	// IT IS THE ONE CLAUSE THE DESIGN PUTS ON EVERY PLACE'S FOOT. Screens 1e, 1f,
	// 2c, 2d and 2f each end their own sentence with `tab next place` and with
	// nothing after it; `alt+. for the map` belongs to the composer's line above,
	// where FIDELITY.md item 3 puts it, and a tail that repeated it would put the
	// same chord on two lines of the same frame.
	placeHintTail = "tab next place"
	// placeMapVerbWords is the map's clause about the row under the cursor, named
	// so that the line can be built WITH it and drawn WITHOUT it: it is the one
	// clause on this line that is not true on every place ([placeHintSaid] drops
	// it where the place declares no verbs).
	//
	// IT SAYS WHAT THE KEY DOES. It read `→ verbs on this row`, which named a
	// CATEGORY on a line where `alt+1…7 go to a place`, `alt+enter send it off as
	// a task` and `esc close` all name an act — and `verbs` is the machinery's
	// word for the strip rather than anybody's word for what pressing `→` gets
	// them. The card's own `→ verbs: pause, stop` keeps the noun because the acts
	// are listed right after it; this line has no room to list them, so it says
	// what the key is for instead.
	placeMapVerbWords = "→ show what this row can do"
	// placeMapWords is the hint line while the map is drawn (SCREEN 3b): the
	// chord list, in the cells the hint was already in.
	placeMapWords = "alt+1…8 go to a place · " + placeMapTaskWords + " · " +
		placeMapVerbWords + " · " + mapCloseWords
	// placeMapTaskWords is the map's clause about the chord that starts a task,
	// named so the line can be drawn WITHOUT it: only home starts things, so on
	// every other place the chord does nothing and is not on the map.
	placeMapTaskWords = "alt+enter send it off as a task"
	// mapCloseWords is that line's last clause, named so the switcher's own
	// clause can be spliced IN FRONT of it rather than after it (hop.go): `esc
	// close` is the way out and the way out is always said last.
	mapCloseWords = "esc close"
)

// placeFootRows is how many rows every place spends under its body when it has
// nothing extra to say: the blank ([spacingRuleClearance]), the rule, the box
// at its floor, and the hint. A note, a tray, a verb strip or the composer
// layer each add their own rows on top of these, which is why the frame counts
// them separately — this is the floor of the foot, not its whole height, and it
// is the number every place is measured against ([placeFrameWithBar] builds it
// row by row and TestEveryPlaceSpendsTheSameHeadAndFoot reads it back).
const placeFootRows = 3 + homeDraftFloor

// placeBareFootRows is the foot on every place but home: the blank, the rule
// with the place's note on it, and the hint. There is no box to hold a floor
// open for ([place.box] states the ruling).
const placeBareFootRows = 3

// placeFootRowsFor is the foot of one place on a frame of a given height —
// home's with its box, every other place's bare — so a law reads one answer.
func placeFootRowsFor(id page, height int) int {
	if id == pageHome {
		return placeFootRowsAt(height)
	}
	return placeBareFootRows
}

// placeSmallestFrame is the shortest terminal this surface is laid out for, and
// the height nearly every law in this package is stated at. Eighty by
// twenty-four is not a guess: it is the size the design's own screens are drawn
// at and the size a frame has to survive without losing anything a person came
// for.
const placeSmallestFrame = 24

// placeFootRowsAt is [placeFootRows] on a frame of a given height: the same
// blank, the same rule and the same last line, over a box held to whatever
// floor that height can afford ([boxFloor]).
//
// IT EXISTS SO THAT NOTHING KEEPS A SECOND COPY OF THIS ARITHMETIC. The foot
// was a constant while the box was one row on every frame; it stopped being one
// the moment the floor started depending on the height, and four laws that had
// quietly written `- 3` or `- 4` into their own slicing went on reading the
// rule as though it were a row of the body.
func placeFootRowsAt(height int) int {
	return placeFootRows - homeDraftFloor + boxFloor(height)
}

// boxFloor is how many rows the composer occupies, and it is the same number
// typed in or not ([homeDraftFloor]) — in the conversation and on every place —
// on any frame with rows to spare for it.
//
// THE FLOOR IS SPENT OUT OF ROOM THE FRAME HAS OVER THE SMALLEST ONE, NEVER OUT
// OF THE SMALLEST FRAME'S OWN BODY. Held open unconditionally it cost three
// rows everywhere, and at [placeSmallestFrame] those three are not spare: home
// dropped a whole panel off the bottom of its column, an empty place drew its
// rule where its whisper had been, and the rail's standing section was squeezed
// out by a long roster. A box nobody can miss is not worth the list they came
// to read, so a frame that cannot afford the floor draws the single row the box
// has always drawn. The box is just as usable; what gives way is the space
// around it.
//
// So the test is the BODY the floor would leave, against the body an eighty by
// twenty-four frame has under a one-row box — which works out at twenty-six rows
// and taller. It is written as that comparison rather than as the number,
// because the number is a consequence of the head and the foot and would be
// wrong the next time either of them moves.
//
// THE SAME ANSWER FEEDS THE HEIGHT AND THE DRAWING, so the rows a foot reserves
// and the rows it then adds can never disagree. That is not tidiness: they are
// read far apart on both surfaces, and a frame that reserved three and drew one
// would lose a row off the top of the window, which is where the head is. It is
// why the chat applies it inside [app.inputBlock], which its height and its
// drawing both go through.
//
// It is handed the frame's own height rather than asking [app.size] for one,
// because a frame is drawn at the height it was given — the rigs draw many
// sizes through one app, and a floor decided from the window would be the wrong
// floor for every frame but the last.
func boxFloor(height int) int {
	// The body the smallest frame has under a box of one row: its height, less
	// the head, less that foot — the blank, the rule, the one box row and the
	// last line.
	const spare = placeSmallestFrame - placeHeadRows - (placeFootRows - homeDraftFloor + 1)
	if height-placeHeadRows-placeFootRows < spare {
		return 1
	}
	return homeDraftFloor
}

// placeRestWord is what home's box row says with nothing typed in it — the
// design's own sentence (SCREEN 2b). It is home's alone now: no other place
// draws a box ([place.box]).
func (a *app) placeRestWord() string { return "› " + placeRestWord }

// placeHintSaid is the line under the composer, IN THE ONE SPELLING EVERY
// CONSTANT ON THIS SURFACE IS AUTHORED IN. Home writes its own sentence for
// every row it can stand on ([app.homeHint]) without the router's tail; every
// other place says the router's own line.
//
// WHAT THIS TERMINAL ACTUALLY DRAWS IS [app.placeHint], one call above it: on a
// Mac the modifier is called `opt+` rather than `alt+`, and chords.go is the single
// door that substitutes it, so a sentence built here is the same sentence the
// manual quotes wherever a lane greps for it.
func (a *app) placeHintSaid() string {
	// THE COMPOSER LAYER'S FOOT OUTRANKS EVERY OTHER SENTENCE ON THIS LINE. While
	// it is up the only keys that do anything are its own, and SCREEN 3a's clause
	// — no key does anything that is not drawn on screen right now — cuts both
	// ways: a foot still naming `tab next place` would be naming a key the layer
	// has taken (composerlayer.go's [app.composerFoot]).
	if a.composer.open {
		return a.composerFoot()
	}
	// AND THE SWITCHER'S OWN FOOT ABOVE THE MAP'S, on the same terms: while the
	// card is up its keys are the only keys, so the line says them (hop.go).
	if a.hopShowing() {
		return hopFootWords
	}
	if a.mapShowing {
		// THE SWITCHER IS NAMED ON THE MAP AND NOWHERE ELSE ON A PLACE. The map
		// is this surface's own chord list — the one line whose job is to say
		// what the keys are — and the place's resting foot is four clauses that
		// the design fixes word for word (FIDELITY.md item 3). A key bound on
		// every place and drawn on none of them would break SCREEN 3a's clause,
		// and this is the line that keeps it, exactly as it keeps the `ctrl+1…7`
		// alias ([chordSpelling.mapLine]).
		line := a.chords.mapLine(a.placeMapSaid(), a.ctrlDigits())
		if a.hopAvailable() {
			line = strings.Replace(line, mapCloseWords, hopMapWords+" · "+mapCloseWords, 1)
		}
		return line
	}
	if a.strip.open {
		return stripHint
	}
	// AND A LAYER INSIDE A PLACE OUTRANKS THE ROUTER'S TAIL for exactly the
	// composer's reason. While the settings panel's value box or its model
	// picker is up, that layer owns every key — `tab` included, and in the
	// picker `tab` opens and closes a model's lanes ([placeSettings.owns],
	// palette.go's [picker.foldKey]) — so the two words the tail would add name
	// a key the layer has taken, which is SCREEN 3a's clause read the other way
	// round.
	if pl := a.showing(); pl != nil && a.sheetLayerOwnsKeys() {
		return pl.hint(a)
	}
	// HOME OWNS ITS WHOLE FOOT, including the model list's keys while it is
	// open. Its navigation chords still work but no longer get a router tail.
	if a.at(pageHome) {
		return a.homeHint()
	}
	// EVERY PLACE'S OWN SENTENCE, WITH THE ROUTER'S KEYS ON THE END OF IT. The
	// places that had a keys line of their own keep it — it is about the row a
	// person is standing on, which is knowledge this file does not have — and the
	// two keys that are true everywhere are appended rather than written into
	// seven sentences.
	pl := a.showing()
	if pl == nil {
		return placeHintWords
	}
	return placeTailed(pl.hint(a))
}

// placeMapSaid is the map's chord list FOR THE PLACE IT IS DRAWN OVER: the
// fixed line, minus the clause about the row under the cursor where this place
// has no verbs to open.
//
// A KEY DRAWN THAT DOES NOTHING IS SCREEN 3a'S CLAUSE READ BACKWARDS. The map
// promised `→ verbs on this row` over all seven places while [placeSearch]
// declares no verbs at all — so on search the arrow the map named opened
// nothing and fell through to the caret inside the box. The line is built from
// what the standing place actually declares rather than from a constant that
// cannot know, which is the same rule the foot above it already keeps: key
// hints true for where you stand.
//
// IT ASKS THE PLACE AND NOT A TABLE, so a place that grows a verb gains the
// clause on the day it does, and a row with nothing to open loses it — [verb]
// lists are built per row on every place that has any.
func (a *app) placeMapSaid() string {
	pl := a.showing()
	line := placeMapWords
	if pl == nil || len(pl.verbs(a)) == 0 {
		line = strings.Replace(line, placeMapVerbWords+railSep, "", 1)
	}
	// AND THE TASK CHORD IS HOME'S ALONE ([place.box]).
	if !a.at(pageHome) {
		line = strings.Replace(line, placeMapTaskWords+railSep, "", 1)
	}
	return line
}

// placeTailed puts the router's own keys on a place's sentence, and puts them
// BEFORE THE WAY OUT: every hint on this surface ends with `esc`, because the
// way out is the last thing a person needs to be told and the first thing they
// look for (homebridge.go says the same about its own clause).
func placeTailed(hint string) string {
	if strings.Contains(hint, placeHintTail) {
		return hint
	}
	// A PLACE WITH NOTHING TO SAY BUT THE WAY OUT — a bare memory page, an
	// untouched ledger — says only `esc`, and the router's clause then goes IN
	// FRONT of it rather than behind. Appending would draw `esc · tab next
	// place`, which puts the way out first: the one position this line's whole
	// law says it never takes.
	if hint == "esc" || strings.HasPrefix(hint, "esc ") {
		return placeHintTail + " · " + hint
	}
	if at := strings.LastIndex(hint, " · esc"); at >= 0 {
		return hint[:at] + " · " + placeHintTail + hint[at:]
	}
	return hint + " · " + placeHintTail
}

// ── FITTING THE FOOT: WHOLE HINTS, NEVER HALF OF ONE ────────────────────────
//
// hintFit is the foot cut to room cells BY DROPPING CLAUSES, and it is
// [rowfit.go]'s ranked-prefix law (law 3) applied to a sentence instead of to a
// row of facts: what a narrow frame shows is a subset of what a wide one shows,
// chosen by rank, and never a clause with its end sliced off.
//
// WHAT IT REPLACES. The foot used to be handed to `fit`, which is a character
// ruler with no idea what a clause is, so at eighty columns the composer's own
// line came out as `… · alt+. for the map · t…` — an ellipsis where `tab next
// place` had been, on the commonest terminal size there is. A key sheet that
// loses the way out is worse than a key sheet with one fewer key on it.
//
// THE RANK, AND WHY IT IS SPELLED THIS WAY ROUND. The last clause is the way
// out — `tab next place`, and `esc` after it where a place adds one — and it is
// kept to the last cell there is. Everything from [placeHintTail] onward is
// therefore protected, and what is dropped is taken from the clause NEAREST
// that protected tail, working backwards: on the composer's own line that is
// `alt+. for the map` first, then `alt+enter send it off as a task`, leaving
// `enter talk about it · tab next place`. The head clause — what `enter` does
// on the row you are standing on — is the last thing to go, because it is the
// only clause on the line about the thing under the cursor.
//
// It is pure and deterministic: the same sentence at the same width is the same
// string, which is what lets a test paste a foot.
func hintFit(hint string, room int) string {
	if room <= 0 {
		return ""
	}
	if ansi.StringWidth(hint) <= room {
		return hint
	}
	parts := strings.Split(hint, railSep)
	// keep is the index of the FIRST protected clause: the way out, and
	// everything after it. A foot with no `tab next place` in it — the map's
	// line, a layer's own foot — protects its last clause, which on every one of
	// them is `esc close` or `esc`.
	keep := len(parts) - 1
	for at, part := range parts {
		if strings.Contains(part, placeHintTail) {
			keep = at
			break
		}
	}
	// A CLAUSE THAT BEGINS `or` IS AN ALTERNATIVE, NOT A WAY OUT, AND IT GOES
	// FIRST. The ladder above protects the LAST clause because on a key sheet
	// that is where `esc close` lives — but a trailing `· or keep typing to steer
	// the planner` is not a way out at all: by construction it offers a SECOND
	// route to something the clause in front of it already offers a first route
	// to, which makes it the lowest-value clause on the line for exactly the
	// reason a bracketed gloss is ([hintDropClause]). Protecting it would have
	// spent a gate card's narrow row on the alternative and dropped `enter
	// answers`, which is the key the card exists to be answered with.
	//
	// It applies only where there is no `tab next place` on the line, because a
	// place's foot has a real way out and the clauses behind it are the router's,
	// not an author's sentence.
	if !strings.Contains(hint, placeHintTail) {
		for len(parts) > 1 && ansi.StringWidth(strings.Join(parts, railSep)) > room &&
			strings.HasPrefix(strings.TrimSpace(parts[len(parts)-1]), "or ") {
			parts = parts[:len(parts)-1]
			keep = len(parts) - 1
		}
	}
	for keep > 0 && ansi.StringWidth(strings.Join(parts, railSep)) > room {
		parts = append(parts[:keep-1], parts[keep:]...)
		keep--
	}
	line := strings.Join(parts, railSep)
	// THEN THE CLAUSES INSIDE A SENTENCE, for the foot that is not a key list at
	// all. Home's own foot is one of these — a refusal about a door, said as a
	// statement with an elaboration hung off a dash and a gloss in brackets
	// behind that ([takeover.go]) — and at sixty columns it used to read
	// `open in another window — enter again to move it here (it …`, which
	// promises a key and then eats it exactly as the sliced key list did. There
	// is no `·` in it for the ladder above to work with, so the ladder below
	// takes the whole trailing clause instead ([hintDropClause]).
	for ansi.StringWidth(line) > room {
		shorter, ok := hintDropClause(line)
		if !ok {
			break
		}
		// AND THE WAY OUT SURVIVES THE SENTENCE LADDER TOO. Where the line has a
		// `tab next place` on it, a clause whose going would take it with it is
		// not a clause this may drop.
		if strings.Contains(line, placeHintTail) && !strings.Contains(shorter, placeHintTail) {
			break
		}
		line = shorter
	}
	// A FRAME TOO NARROW FOR THE WAY OUT ALONE is the one case left, and there is
	// nothing to drop that would help: the tail is cut, exactly as it always was.
	return fit(line, room)
}

// hintFitBeside is [hintFit]'s ladder for the one line that carries a note
// beside the hint — the foot [app.placeMsgLine] assembles when a place has
// something to say while one of its rows is selected. THE DOOR FIRST AND THE
// NOTE BEHIND IT, and the clauses between them are the ones that go: the note
// is a fact about the row's work and the door is the row's own key, so neither
// may be dropped for a page clause — fold, verbs, filter, even the way out.
// The giving-way starts at the clause AFTER the door and works forward, so the
// way out is the last of them to leave, exactly as [hintFit] keeps it longest
// everywhere else.
//
// THE NOTE IS ONE SENTENCE AND NOT A CLAUSE LIST. A question's note arrives
// assembled (`allow rm -rf build? · waiting in this conversation · alt+y`),
// and it can arrive already opening with the separator — a question with no
// head leaves the rail's own ` · ` at the front — so the join strips a leading
// one rather than adding a second: [railSep] is the one joiner down this whole
// column, and a doubled ` · · ` is a line lying about its own shape. The note's
// own clauses are never dropped one by one, because the note is the thing the
// person is being told and a note that lost its `alt+y` would be a question
// with no way left to reach it.
//
// A FRAME TOO NARROW FOR THE DOOR AND THE NOTE ALONE is the one case left, and
// it is [hintFit]'s own last resort: the pair is cut, because there is nothing
// left to drop that would help.
func hintFitBeside(hint, note string, room int) string {
	if room <= 0 {
		return ""
	}
	// A leading run of the separator's own characters is stripped and nothing
	// else: every note that begins with its own first word keeps it.
	note = strings.TrimLeft(strings.TrimSpace(note), " ·")
	if note == "" {
		return hintFit(hint, room)
	}
	parts := strings.Split(hint, railSep)
	joined := func() string { return strings.Join(parts, railSep) + railSep + note }
	for len(parts) > 1 && ansi.StringWidth(joined()) > room {
		// THE CLAUSE AFTER THE DOOR GOES FIRST: parts[1] is removed and everything
		// behind it shifts down, so the fold, the verbs and the filter give way
		// before the way out does — and the door at parts[0] is never the clause
		// this removes, which is the whole difference from [hintFit]'s own ladder.
		parts = append(parts[:1], parts[2:]...)
	}
	return fit(joined(), room)
}

// noteFit is [hintFit]'s twin FOR A STATEMENT INSTEAD OF A KEY SHEET, and the
// one thing that differs is which end of the line is protected.
//
// WHY THERE ARE TWO. [hintFit] keeps the LAST clause because on a key sheet the
// last clause is the way out — `esc close` — and the way out is the one thing a
// narrow frame may never take. A note is the other way round: it is a sentence
// whose FIRST clause is what happened and whose later clauses elaborate on it.
// The settings foot is the worked example — `saved to your profile · a project's
// own .codeaf/config.json is a hand edit` — where the first clause answers
// the question a person asked ("where did that go?") and the second is an aside
// about a file most people will never open. A character ruler took sixty columns
// through the middle of that path; [hintFit] would have kept the aside and
// dropped the answer. So a note drops from the END, whole clause at a time, and
// then falls through to the same sentence ladder ([hintDropClause]) for the
// dash elaborations and bracketed glosses that have no middle dot to split on.
//
// It is pure and deterministic and it never cuts inside a word, which is the
// whole point of both of them.
func noteFit(note string, room int) string {
	if room <= 0 {
		return ""
	}
	if ansi.StringWidth(note) <= room {
		return note
	}
	parts := strings.Split(note, railSep)
	for len(parts) > 1 && ansi.StringWidth(strings.Join(parts, railSep)) > room {
		parts = parts[:len(parts)-1]
	}
	line := strings.Join(parts, railSep)
	for ansi.StringWidth(line) > room {
		shorter, ok := hintDropClause(line)
		if !ok {
			break
		}
		line = shorter
	}
	// A NOTE TOO LONG EVEN AS ONE CLAUSE is cut, because there is nothing left to
	// drop that would help — the same last resort [hintFit] ends on.
	return fit(line, room)
}

// hintDropClause takes the LAST WHOLE CLAUSE off a sentence and says whether
// there was one, in the two shapes the person-facing sentences on this surface
// are built out of:
//
//	a gloss in brackets   `… move it here (that window's reply stops there)`
//	a dash elaboration    `open in another window — enter again to move it here`
//
// The bracket goes first because a gloss is the lowest-value thing on the line
// by construction — it explains a clause that is still there — and the dash
// clause goes second, leaving the STATEMENT, which is the half a person needs
// to know what happened. It never cuts inside a word and never returns half a
// bracket: a clause either goes whole or the line is handed on untouched.
func hintDropClause(line string) (string, bool) {
	if strings.HasSuffix(line, ")") {
		if at := strings.LastIndex(line, " ("); at > 0 {
			return strings.TrimRight(line[:at], " "), true
		}
	}
	if at := strings.LastIndex(line, sentenceDash); at > 0 {
		return strings.TrimRight(line[:at], " "), true
	}
	return line, false
}

// sentenceDash is how this surface hangs an elaboration off a statement, and it
// is spelled here once so that [hintDropClause] and the sentences it reads are
// looking for the same three cells.
const sentenceDash = " — "

// placeMsgLine is the line a place says when it has something to say, drawn
// beside the hint rather than over it. IT REPLACED THE HINT ONCE, and that
// displacement is what #840 measured: a task that raised a question while its
// row was selected took `enter open its room` off the foot entirely, because the
// question's where-to-answer note arrived on the one line the door hint lived
// on. A question is a fact ABOUT the row's work and not about the keyboard, so
// the row's own door stays first and the note rides behind it; the clauses
// between them are the ones that go, dropped by [hintFit] rather than cut by a
// slice.
//
// DIM, AND NOT THE FAULT COLOUR. Every refusal these places have is a fact about
// a door — that conversation is open somewhere, that project is not this one —
// and none of them is anybody's mistake. AND THE PLACE IT SENDS YOU IS A DOOR:
// the sentence that names a directory opens it, applied to the FITTED text after
// the width was measured, and a directory that is not there stays plain
// (pathlink.go).
func (a *app) placeMsgLine(width int) (string, bool) {
	// THE ROUTER'S OWN LINE OUTRANKS HOME'S. Home says its refusals on a field
	// of its own ([homeView.say]) and the router says a place's on [app.pageMsg]
	// — and the one moment both can be set is a refusal that PUT YOU BACK on
	// home, where the sentence a person needs is the one about the door they
	// just tried. Home's own is read when the router has nothing to say.
	msg, path := a.pageMsg, ""
	if msg == "" && a.at(pageHome) {
		msg, path = a.home.msg, a.home.msgPath
	}
	// AND A QUESTION WITH NOWHERE ELSE TO GO IS ASKED HERE, AT DRAW TIME. The
	// grid draws a raised question as a card in its description column and the
	// foot stays quiet ([app.homeAskFitsColumn]); a frame too short or too narrow
	// for that card has to say it, and it cannot wait for the next keystroke to
	// find out — [app.sayHomeAsk] runs on a key, and a person who made the window
	// smaller has pressed none. That left the decision on no part of the screen
	// at all, which is the one state a question may never be in.
	if msg == "" && a.at(pageHome) && !a.homeAskFitsColumn() {
		msg = a.homeAskFoot()
	}
	if msg == "" {
		return "", false
	}
	// AND IT IS CUT BY DROPPING CLAUSES, NEVER BY SLICING ONE. A refusal is a
	// sentence rather than a key list, but it is the same promise: home's own
	// foot reached sixty columns as `open in another window — enter again to
	// move it here (it …`, naming a key and then eating it. [hintFitBeside] is
	// [hintFit]'s ladder carrying this line's own law — the door first, the note
	// behind it, and the clauses between them the ones that go.
	//
	// AND IT IS PAINTED AS THE HINT IT NOW IS. The note rides on the hint's own
	// line, so its keys are read by the same grammar ([paintHint]) rather than
	// dimmed flat, and the door's clause keeps the colour every other key on
	// this surface wears.
	return " " + a.pathLink(path, paintHint(hintFitBeside(a.placeHint(), msg, width-2), a.pal, a.pal.dim)), true
}

// ── opening a place ─────────────────────────────────────────────────────────

// showPage opens one place and closes whatever was standing where it is about to
// stand.
//
// IT IS A WRAPPER OVER THE EXCLUSION THAT ALREADY EXISTED, not a replacement for
// it. [app.standDownFullscreen] still closes every page that takes the frame,
// every page still carries its own `open bool`, and view.go's frame still asks
// those booleans in the same order — so `a.page` is a LABEL on state the
// booleans already carry, and the two page-stack laws in chrome_test.go hold
// without an edit to what they assert.
//
// ── EVERY PLACE OPENS, ALWAYS ───────────────────────────────────────────────
//
// This function used to have a second half: a place that refused to open put
// back whatever was standing, and three places refused — tasks with no task in
// the world, standing with nothing standing, memory with no store behind it. The
// owner ran the binary on a fresh machine and found that `alt+2`, `alt+3` and
// `alt+4` did nothing at all, because on a fresh machine all three of those are
// exactly the state a new person is in.
//
// SCREEN 1f'S PREAMBLE IS THE LAW NOW: an almost-empty place is the best teacher
// on the machine, so it always opens and spends the whole frame saying what it
// is for. There is no refusal path left here to put anything back with, and each
// place answers an empty world with its heading and its whisper
// (placeprose.go's [placeWhisper]) rather than with a bounce. The one fact a
// place cannot teach its way around — a reading that belongs to a machine this
// process cannot see — is drawn as a single dim line in the place's body
// ([place.remote]), which is still the place being open and saying why it is
// empty.
func (a *app) showPage(id page) (cmd tea.Cmd) {
	if a.startingChat() {
		back := a.parkChatStart()
		defer func() { cmd = tea.Batch(back, cmd) }()
	}
	// LEAVING A PLACE IS THE LOOK, and it is the place's own `close` that writes
	// the stamp — one call for EVERY place rather than a list of them here, which
	// would be a second answer to which places can wear a number
	// (placecounts.go's [app.leavePage] and session's look.go both hold the
	// argument).
	if was := a.showing(); was != nil {
		was.close(a)
	}
	a.page = pageNone
	// AND THE CURSOR COMES DOWN OFF THE BAR WITH THE DOOR. Every road into a room
	// ends with a person looking at that room's rows — `enter` on the bar, a press
	// on a tab word, `tab`, `alt+3`, a door on a home row — so a cursor left up
	// here would be a cursor on furniture in a room somebody has just walked into
	// ([barCursor]).
	a.bar = barCursor{}
	// AND THE PAGES THAT TAKE THE FRAME AND ARE NOT PLACES STAND DOWN WITH IT
	// ([app.standDownRest] names them and says why they are not in the bar).
	a.standDownRest()
	a.closeStrip()
	a.mapShowing = false
	// AND THE COMPOSER LAYER GOES WITH THE PLACE IT WAS OPENED ON. It names that
	// place in its own foot and dims that place's rows behind it; carried onto the
	// next room it would be a decision drawn over a page it was never about
	// (composerlayer.go).
	a.closeComposerLayer()
	// AND EVERY BOTTOM-ANCHORED MODAL GOES WITH IT. A place takes the frame
	// whole, so a picker or a panel left open behind one is drawn nowhere and
	// driven by nothing — and then appears over whatever the next `esc` lands on
	// ([app.closeModals] names the ten and says where the bug was seen).
	a.closeModals()
	a.pageMsg = ""
	// AND THE TOP LINE'S MONEY IS READ AT THE DOOR, before the room's first
	// frame: the pulse draws a memo and never a file, so a room walked into
	// between two beats would otherwise open under yesterday's figure
	// (homemachine.go's [app.readMachineMoney]).
	a.readMachineMoney(a.now())
	next := placeFor(id)
	if next == nil {
		// THE CONVERSATION IS A PAGE ID LIKE ANY OTHER, and it is the one with no
		// place behind it: `esc` out of a room lands here, and what is on the
		// frame is then whatever view.go draws under the places.
		return nil
	}
	a.page = id
	return next.open(a)
}

// closeModals puts away every bottom-anchored overlay on the way into a place.
//
// THIS IS THE FIX FOR "IT GOES TO AN OLD CHAT". A place takes the frame whole
// (view.go returns before the chrome is ever built) and every key while one is
// showing is routed to [app.placeKeyPress] above the modal checks (input.go), so
// each of these opened from home was invisible AND unreachable — and then
// [app.closeHome] handed the frame back to the conversation underneath and the
// picker appeared over THAT. Ten commands did it: /model, /resume, /folder,
// /attach, /files, /crew, /permissions, /connect, /harness and /subharness.
//
// IT IS NOT [app.standDownRest], which is the two FULLSCREEN pages that are not
// places, and it is not [app.closeForSwitch], which is about a conversation
// changing under a person's hands. This one is about the frame: these are
// overlays the place cannot draw, so a place standing up puts them away.
func (a *app) closeModals() {
	a.pick.close()
	a.roster.close()
	a.folder.close()
	a.shelf.close()
	a.crewPick.close()
	a.effPick.close()
	a.connPanel.close()
	a.harnPanel.close()
	a.permPanel.close()
	// Navigation hides an unanswered offer without resolving or losing it.
	if a.subPage.card.asked() {
		a.subPage.open = false
	} else {
		a.subPage.close()
	}
	// AND HOME'S OWN MODEL LIST, which IS drawn where it stands and is still a
	// list nobody left open on purpose: walking to another place and back to a
	// list you had not finished with is a list you have to remember opening
	// (homedraft.go). The pin it was about survives; only the list goes.
	a.target.pick.close()
}

// raisePlace says where the person is standing for a place that has ALREADY
// built its own state, and opens nothing.
//
// IT HAS EXACTLY ONE CALLER and it is meant to keep it: the launch home, made
// inside [newApp] before bubbletea exists and therefore before any command can
// be answered (home.go's [app.landHome]). Every other road in is
// [app.showPage], which closes what was standing, writes its look stamp and
// asks the next place to open itself.
func (a *app) raisePlace(id page) { a.page = id }

// leavePlace is `esc` out of the room a person is standing in: the place closes,
// its look stamp is written, and the conversation is underneath again.
//
// IT IS [app.showPage] WITH NOWHERE TO GO, said as its own verb because every
// place's `esc` arm reaches for it and a place that spelled the four statements
// itself would be a place that forgot one of them.
func (a *app) leavePlace() {
	a.showPage(pageNone)
	a.touch()
}

// pageShowing is whether a place is up at all. It is ONE FIELD now: the six
// `open bool`s this used to ask are retired, because a label on a flag and the
// flag are two answers to one question and the day they disagree is the day a
// key goes to a page nobody can see.
func (a *app) pageShowing() bool { return a.showing() != nil }

// ── the pointer, one place at a time ────────────────────────────────────────
//
// THE THREE GESTURES ARE THREE LINES EACH, because each of them asks the place
// that is standing and never a page id: a mouse router with a switch of its own
// would be a second list of the places to fall out of step with the first, which
// is what all three of these were. What each function is given is a row of the
// terminal; what it hands back is whether the place took the gesture. The shared
// arithmetic — a terminal row becoming a line of a body, a window that follows a
// cursor — is placemouse.go's, because it is the same on every place.

// placeBodyPress is a press on one place's own rows: the row under it is
// entered, exactly as `enter` on it would (the law is [place.press]'s).
func (a *app) placeBodyPress(y int) (tea.Cmd, bool) {
	pl := a.showing()
	// AND NO GESTURE REACHES A PAGE THAT IS UNDER THE COMPOSER LAYER. Its rows are
	// drawn at the faintest tier precisely to say they are not the subject any
	// more, and a cursor that moved under a layer would move a selection nobody
	// can see they are changing.
	if pl == nil || a.composer.open {
		return nil, false
	}
	return pl.press(a, y)
}

// placeBodyHover selects the row reached by the pointer. The same cursor
// drives keyboard actions, and a place repaints only when that row changes.
func (a *app) placeBodyHover(y int) bool {
	pl := a.showing()
	return pl != nil && !a.composer.open && pl.hover(a, y)
}

// placeBodyWheel is the wheel over one place: it walks that place's cursor, by
// [placeWheelRows] rows a tick, which is what every other list on this surface
// does with it (app.go's wheel ladder). A place whose window follows its cursor
// has no offset of its own to move, so a scroll and a selection are one gesture
// here — the bargain the task page and home both already struck.
func (a *app) placeBodyWheel(delta int) (tea.Cmd, bool) {
	pl := a.showing()
	if pl == nil || a.composer.open {
		return nil, false
	}
	return pl.wheel(a, delta)
}

// nextPage is `tab`: the place after this one along the bar, and round again
// from the last. `back` is `shift+tab`, the same circle walked the other way.
//
// THE CIRCLE IS THE BAR ([barPages]). `tab` is the bar walked by a key, so it
// goes where the words are: the four, and the room you are standing in when it
// is one of the three off the bar — from which `tab` goes on to home.
func nextPage(at page, back bool) page { return nextOn(barPages(at, false), at, back) }

// nextOn is one step round a ring of places from `at`.
func nextOn(all []page, at page, back bool) page {
	for i, id := range all {
		if id != at {
			continue
		}
		if back {
			return all[(i+len(all)-1)%len(all)]
		}
		return all[(i+1)%len(all)]
	}
	// `tab` FROM THE CONVERSATION IS THE FIRST PLACE ON THE BAR, which is home.
	// It is also what an id nothing answers to gets, and that is the same
	// sentence: the bar's first word is where a walk with no origin begins.
	return all[0]
}
