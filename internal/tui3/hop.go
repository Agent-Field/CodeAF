package tui3

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE SWITCHER — alt+tab for the conversations this process already holds ──
//
// THE ASSUMPTION BEING REMOVED IS THAT THE WAY BETWEEN CONVERSATIONS IS A PAGE.
// The keeper has held up to eight of them alive since the conversations wave
// (keeper.go), and the only gesture between them was `tab`, which goes to ONE —
// the last — and says nothing about the other six. Reaching a third meant
// leaving the chat for home, reading a list and coming back, which is a screen
// transition for a gesture a person fires fifty times a day.
//
// So: a card of the open conversations, over whatever you are looking at, with
// the surface behind it DIMMED rather than covered.
//
// IT IS CALLED `hop` HERE AND `the switcher` TO A PERSON. This package already
// spends the word `switcher` on home's own reading (switcher.go), which is a
// different thing — a whole page, ranked, grouped, with standing orders and a
// ledger in it — and two `switcher`s in one package would be two things nobody
// can tell apart in a stack trace.
//
// ── WHY IT IS BUILT ONLY FROM MEMORY ────────────────────────────────────────
//
// EVERY FIELD ON EVERY ROW COMES FROM THIS PROCESS'S OWN STATE: the keeper's
// map, the sidecar each detach left, and three predicates asked of the agent
// pointers this process is already holding. Nothing here opens a file, scans a
// world, reads a presence heartbeat or crosses a wire.
//
// That is not thrift, it is the feature. Home's reading is correct and costs a
// world scan on a three-second beat; a gesture fired between two sentences must
// cost nothing at all, and over `--host` a reading that touched the disk would
// be a round trip to another machine before the card could be drawn. A switcher
// that took a quarter of a second to appear is a switcher people stop using.
//
// ── AND THE ROWS ARE FROZEN THE MOMENT IT OPENS ─────────────────────────────
//
// A conversation that finishes a turn while the card is up stirs the surface
// (keeper.go's [behindWatch.stir]), and a list that re-ranked on that stir would
// move the row under the cursor between the keystroke that aimed at it and the
// `enter` that took it. So [hopCard.rows] is a snapshot: what is drawn is what
// was true when the card opened, and the only thing that moves is the cursor.
//
// ── THE CARD WEARS THE ONE FRAME, AND THE DEPTH IS WHAT SAYS `LAYER` ────────
//
// This header used to say the card had no border, while the card drew one — a
// dim rounded box of its own, one of three copies of the same six pieces on this
// surface. It is drawn by the one frame now (frame.go, owner ruling 2026-09-11),
// dim, on the card's own raised ground. What says `layer` is still the depth:
// the body behind it is repainted at the FAINTEST stop of the depth ladder
// ([composerFade], depthfade.go) and the card's own rows are left at full ink.
// That is the same move SCREEN 2e's composer layer makes over a place, one
// mechanism rather than two, and the frame only says where the card ends.
//
// The rows themselves are home's rows — the same glyph door, the same bold
// subject, the same dim tail dropped in the same order (switcher.go's
// [switcherLine]) — because a person who has read home once should not have to
// learn a second list.
//
// The switcher is a browsing gesture: highlight first, Enter or a click to open.
// A pause must never dismiss the list while somebody is reading its titles.
// The optional quick-switch setting applies only to the terminal's distinct
// ctrl+tab chord. Modifier releases are not delivered by ordinary terminals,
// so neither gesture guesses that silence means a key was released.

// hopOpenKey is the key that opens the switcher, and it is `alt+k` for four
// reasons stated in the order they were weighed:
//
//  1. IT LEAVES THE LETTER IN THE DRAFT. This key was `ctrl+k` and that is
//     readline's kill-to-the-end-of-the-line, which the composer now does with
//     it (input.go's [editor.killToEnd]) beside the `ctrl+u` it has always had.
//     EVERY `ctrl+<letter>` ON THIS SURFACE IS SPENT — a through z, with only
//     `h`, `i` and `m` left, and each of those three is a byte the terminal
//     already spends on backspace, tab and enter. So a door that wanted a
//     letter back had to leave the modifier the box edits under, and `alt+` is
//     the class the places are already built on (chords.go).
//  2. IT ARRIVES WITH NO PROTOCOL NEGOTIATION. `alt+k` is escape-then-`k`, which
//     every terminal that sends Alt as Meta emits unasked — no kitty keyboard
//     flag, no cooperation from a multiplexer in between. That is the first
//     question asked of any chord on this surface, because a capability that
//     cannot work is absent rather than broken (bargein.go states the law).
//  3. THE REVERSE COMES FREE, AND EVERYWHERE. See [hopBackKey]: `alt+shift+k` is
//     escape-then-`K`, a different byte from escape-then-`k`, so the backwards
//     gesture stops being a thing only a kitty terminal could spell. Under
//     `ctrl+` it was the same byte as the forward one and half the world lost it.
//  4. NOTHING TAKES IT FIRST, OUTSIDE OR IN. No window manager claims it and no
//     common emulator binds it — unlike `ctrl+tab`, which WezTerm and Windows
//     Terminal both spend on their own tabs by default, and unlike `alt+tab`,
//     which the window manager takes on Windows and on most Linux desktops.
//     Inside this surface the `alt+` letters already spent are `b`, `f`, `g`,
//     `i`, `o`, `q`, `s`, `t` and `w`; `k` is free.
//
// AND THE ONE COST, SAID PLAINLY RATHER THAN LEFT TO BE DISCOVERED. On a Mac,
// Option composes accents unless the terminal profile says otherwise, and `opt+k`
// is then the character `˚` rather than a chord — which is the tax every `alt+`
// chord on this surface already pays. chords.go is the whole of the answer: it
// spells the chord `opt+k` on a Mac ([chordSpelling.say]), it watches for `˚`
// arriving where the chord was aimed ([chordDeadKeys]), and it draws one dim
// line naming that terminal's own setting until a real `alt+` chord retires it.
// `ctrl+k` needed none of that, and this is what was traded for the letter.
const hopOpenKey = chordAltWord + "k"

// hopBackKey is the same gesture the other way, and it is `alt+shift+k` on EVERY
// terminal rather than on the few that negotiated for it.
//
// THIS IS THE ONE THING THE MOVE OFF `ctrl+` BOUGHT OUTRIGHT. `ctrl+shift+k` and
// `ctrl+k` are the same byte in an ordinary terminal, so nothing could tell them
// apart until the kitty keyboard protocol's disambiguation flag had been taken
// and the reverse was simply absent everywhere else ([app.ctrlDigits]). The
// escape-prefixed spelling has no such collision: escape-then-`K` is a different
// byte from escape-then-`k`, and ultraviolet's decoder reads the uppercase rune
// back as shift+alt over the lowered letter. So the chord is bound flat, with no
// capability question in front of it.
//
// `shift+tab` REMAINS THE REVERSE THE CARD ITSELF TAKES, on every terminal, as
// CSI Z. It is the one people's hands reach for once the card is up; this chord
// is the one that opens the ring at its far end without the card being up first.
const hopBackKey = chordAltWord + "shift+k"

// hopFoldKey and hopShutKey open and shut the fold at the foot of the card —
// `→` and `←`, the two keys this surface already folds with everywhere
// (task.go's roster, place_tasks.go's families).
const (
	hopFoldKey = "right"
	hopShutKey = "left"
)

// hopAwayKey DISMISSES a conversation from the card. It is `ctrl+w`, which is
// the key the grooming drew and the key every browser and editor closes a tab
// with — and it now does what that key does everywhere else: it takes the view
// away and leaves the work alone.
//
// IT USED TO END THE CONVERSATION, agent and all, with a two-press arm in front
// of it when something was running. That was the wrong act under this spelling.
// A person pressing the tab-close key is putting a row away, not ending an hour
// of work, and a key that quietly did the second while looking like the first is
// exactly the shape the arm existed to apologise for. Ending work is `Stop` on a
// task's own page, and ending the program is `/quit`.
//
// Outside the switcher, the same chord closes the current tab. Modal search
// fields retain their word-delete edit while they own the keyboard.
const hopAwayKey = chordCtrlWord + "w"

// hopAlias and hopBackAlias are the muscle memory, bound ONLY where the terminal
// says it can spell them ([app.ctrlDigits], the same reply `ctrl+1`…`ctrl+7`
// hang off in chords.go).
//
// `ctrl+tab` has no legacy encoding: on a terminal that has not taken the kitty
// keyboard protocol's disambiguation flag it arrives as a bare `tab` and means
// whatever `tab` means there. That is why it cannot be the way in and can only
// ever be a second name for one — and why it is never advertised where it would
// not be delivered.
const (
	hopAlias     = chordCtrlWord + "tab"
	hopBackAlias = chordCtrlWord + "shift+tab"
)

// hopHereWord marks the conversation you are standing in, which is drawn LAST so
// the ring has a visible seam: the list reads "these are the others, and here is
// where you are", rather than being a circle with no beginning.
const hopHereWord = "you are here"

// hopShown is how many rows the card holds. TWELVE, because the card is a card
// and not a page: it is a glance at the handful of conversations somebody is
// moving between, and a person who wants the whole list wants home, which is a
// page and has the room to be one. The keeper's soft ceiling is this same
// number (keeper.go's [keptCeiling]): a person who can see every conversation they
// have open on one card has not lost track of any of them. A window whose held
// conversations are all busy still sails past it, and those extra rows this
// card does not draw are still on home.
const hopShown = 12

// hopDigits is how many rows wear a number: nine, because `1`…`9` is every digit
// a single keystroke can be.
const hopDigits = 9

// hopHeldWord is a conversation another window is holding. It is the one thing a
// closed row says about itself unprompted, because it is the one that changes
// what `enter` will do: the door refuses a journal somebody else has locked.
const hopHeldWord = "open in another window"

// The four things a row can say about what changed since you last looked. They
// are sentences rather than figures because the question a person is asking when
// they open this card is "does anything want me", and `0` is not an answer to it.
const (
	hopAskingWord  = "asking you something"
	hopLandedWord  = "it finished while you were away"
	hopNothingWord = "nothing new"
)

// hopRow is one open conversation as the card draws it. Every field is a string
// the card prints, decided once here, so the paint never re-derives a fact.
type hopRow struct {
	// file is the transcript, and it is the ADDRESS: committing a row hands it
	// to [app.bringForward], which is the same door home's `enter` uses.
	file    string
	title   string
	project string
	note    string
	age     string
	here    bool
	needs   bool
	moving  bool
	// open says THIS PROCESS is already holding this conversation, which is what
	// decides whether taking the row is an attach or an open ([app.hopTake]) and
	// which side of the card's one rule it is drawn on.
	open bool
	// where is the folder the conversation works in, and it is only ever read for
	// a row that is not open yet — the door that opens one needs a workspace, and
	// a row that is already open has an agent that has had one since it was built.
	where string
	// held is another window holding this journal, and gone is a project folder
	// that is not there any more. Both refuse when they are pressed, and home's
	// own rows carry the same two facts for the same reason (switcher.go).
	held bool
	gone bool
}

// hopCard is the whole of the switcher's state. The zero value is closed.
type hopCard struct {
	open bool
	// Pointer targets are recorded by the same layout that draws the card.
	spots                             []hopSpot
	originY, left, right, top, bottom int
	// at is the cursor, an index into rows. It opens on ZERO, which is the most
	// recently open conversation behind this one — the same place `tab` goes —
	// so the commonest journey is `alt+k enter` and the second commonest is one
	// more `alt+k` before the `enter`.
	at int
	// rows are frozen at open. See the header: a stir must never renumber a list
	// somebody is aiming at.
	rows []hopRow
	// all is the fold at the foot standing open: the conversations this terminal
	// is NOT holding, drawn under the ones it is.
	//
	// THE CARD IS ABOUT WHAT IS OPEN, and the rest is behind a door. A list that
	// mixed the two was the thing that could not be read — every row looked the
	// same and nothing said which of them were alive — so the ring is the open
	// ones, and `→` is how you reach anything else. It is a door and not a
	// setting (home's own folds hold the same law, [homeQuietWord]): a
	// line that says rows are being hidden and cannot be asked to stop hiding
	// them is a dead end somebody hits and gives up at.
	all bool
	// rest is how many conversations the fold is standing for, and it is zero
	// once the fold is open, because nothing is behind it any more.
	rest int
	// tabs is how many of the rows are tabs on the row above — the leading run of
	// them, since [app.hopReading] draws them first. It is the head's `3 of 12`,
	// and it is kept rather than counted off the rows because opening the fold
	// puts conversations with no tab on the list beside them.
	tabs int
	// total is how many conversations this machine has, counted once when the
	// card opened and kept through the fold. It is what the head's `1 of 12`
	// reads, and it may NOT be derived from rest: opening the fold empties rest,
	// and a count that fell to `1 of 1` at that moment would be the head saying
	// the machine shrank because somebody looked at it.
	total int
	// armed is the row `ctrl+w` has warned about — a conversation with work
	// running in it, which takes a second press to close ([app.hopAway]). It is
	// -1 when nothing is armed, and a single walk of the cursor disarms it.
	armed int
	// say is the one line the card's foot carries about what just happened: a
	// refusal, or the warning the arm above raised. It is cleared by the next key.
	say string
	// from is the conversation the card opened over — the place `esc` goes back
	// to. It matters under quick switching, where the surface has already moved
	// by the time anyone presses it; with the setting off it is simply where you
	// already are, and `esc` going there is `esc` doing nothing, which is right.
	from string
	// live is quick switching in progress: every press of the chord has switched
	// the surface, and the card is a receipt that will fade on its own. It ends
	// the moment any key that is not the chord arrives — an arrow, a fold, a
	// close — because that person has stopped switching and started looking, and
	// a card must never fade out from under somebody who is reading it.
	live bool
	// pulse numbers the fade timers, so a tick scheduled by an early press is
	// stale by construction once a later press has scheduled its own.
	pulse int
}

// hopSettle is how long the card lingers after the last press of the chord
// before fading. Long enough to read the row you landed on and the two around
// it; short enough that the card is gone before the next sentence is typed. The
// switch itself happened ON the keypress, so nothing at all is waiting on this.
const hopSettle = 900 * time.Millisecond

// hopSettleMsg is one fade timer coming due.
type hopSettleMsg struct{ pulse int }

// hopShowing is the one predicate the frame asks.
func (a *app) hopShowing() bool { return a.hop.open && len(a.hop.rows) > 0 }

// hopAvailable reports whether the key would DO anything if it were pressed
// right now, which is both the guard and the advertisement's condition — the two
// may not come apart (render.go's [app.hintWord] states the law).
func (a *app) hopAvailable() bool {
	if !a.hopMayOpen() {
		return false
	}
	if len(a.behind) == 0 && a.hopKnown < 2 {
		// NOWHERE TO GO. One conversation open, and nothing else on the machine
		// that the last reading saw. The key is neither bound nor named, which is
		// the emptiness law said about a keystroke.
		//
		// IT IS THE REMEMBERED COUNT AND NEVER A FRESH READ, because this
		// predicate is asked on every frame — it gates the legend's own clause —
		// and a walk of the disk on the paint path would be a world scan thirty
		// times a second, or a call to another machine over `--host`.
		// [app.countConversations] takes that reading off the loop instead.
		return false
	}
	return true
}

// hopMayOpen is the KEY's own guard, and it is deliberately looser than the
// advertisement's: the two layers that have already claimed the keyboard, and
// nothing else.
//
// THE COUNT IS NOT ASKED HERE. [app.hopKnown] is a remembered number that lands
// a moment after boot, and a key gated on it would do nothing for the first
// frames of a session in which the card would plainly have had rows. The reading
// is the real answer — [app.hopOpen] refuses a card with nowhere to go — and it
// can afford to be, because it only runs on the keystroke.
//
// It errs in the safe direction the capability law cares about: a key that works
// slightly before it is named, never a name for a key that does nothing.
//
// The composer layer is a decision with four answers on screen
// (composerlayer.go) and copy mode is a frozen viewport (copymode.go); a card
// that opened over either would be drawn over a gesture somebody is in the
// middle of. They are asked HERE because this claim is read above the place
// router and so does not pass through either of their own arbitration.
func (a *app) hopMayOpen() bool { return !a.composer.open && !a.copy.on }

// THERE USED TO BE A SECOND DOOR HERE, `hopOpenAll`: the card raised with its
// fold already open, which is what the `Chats ▾` control at the right end of the
// tab row pressed. That control is deleted (chattabs.go) and this went with it,
// because the state is not gone — `→` on the card opens the fold
// ([app.hopSpread], [hopFoldKey]) and always has. A second constructor for a
// state one keystroke away, with no caller left, is a second thing to keep in
// step with the first for nothing.

// hopOpen builds the reading and raises the card.
func (a *app) hopOpen() {
	// A shared engine handle can only have one open conversation. Show its
	// other saved chats immediately; an open-only list would offer no choice.
	rows, tabs, rest := a.hopReading(a.shared)
	if len(rows) < 2 && rest == 0 {
		// Nowhere to go. The guard above has already refused this, and this is
		// the same refusal said where the rows are actually counted.
		return
	}
	a.dropHover()
	a.hop = hopCard{open: true, all: a.shared, rows: rows, rest: rest, tabs: tabs, total: len(rows) + rest, at: a.hopFirstStop(rows), armed: -1, from: a.file}
	a.touch()
}

// hopSpread opens or shuts the fold and re-reads, keeping the cursor on the row
// it was on.
//
// RE-READING IS RIGHT HERE AND WRONG EVERYWHERE ELSE. The rows are frozen against
// a STIR — news arriving on its own must not move the row under somebody's finger
// — and this is not news: it is the person asking for more of the list, which is
// the one moment a list is allowed to grow.
func (a *app) hopSpread(all bool) {
	if a.hop.all == all {
		return
	}
	if all && a.hop.rest == 0 {
		return
	}
	a.dropHover()
	at := a.hop.at
	rows, tabs, rest := a.hopReading(all)
	a.hop.rows, a.hop.rest, a.hop.tabs, a.hop.all, a.hop.armed, a.hop.say = rows, rest, tabs, all, -1, ""
	a.hop.at = min(at, max(0, len(rows)-1))
	// AND THE CURSOR LEAVES `you are here` THE MOMENT THERE IS SOMEWHERE ELSE TO
	// BE. Opening the fold on a session holding one conversation is a person
	// asking for the others; leaving the cursor on the row they are already in
	// would make `enter` do nothing at the end of that gesture.
	if a.hop.at < len(rows) && rows[a.hop.at].here {
		a.hop.at = a.hopFirstStop(rows)
	}
	a.touch()
}

// hopFirstStop is where the cursor opens: THE ROW `tab` WOULD HAVE GONE TO —
// the conversation this window was in before this one — and the first row that
// is not `you are here` when there is no such conversation.
//
// IT IS NOT SIMPLY ZERO, AND IT STOPPED BEING ROW ZERO WHEN THE ROWS TOOK THE
// STRIP'S ORDER ([app.hopStripOrder]). The two keystrokes this card exists for
// are `ctrl+k` `enter`, and what they have always meant is "the last one" — so
// the cursor follows the recency stack even though the LIST no longer does.
// Losing that would have made the commonest journey through the card a walk.
//
// On a fresh session the only open conversation IS the front one, so a cursor
// left at zero would open the card on `you are here` and make `enter` do
// nothing; the walk below is what answers that.
func (a *app) hopFirstStop(rows []hopRow) int {
	for at := len(a.prev) - 1; at >= 0; at-- {
		key := a.prev[at]
		if key == a.convKey(a.file) {
			continue
		}
		for i, row := range rows {
			if !row.here && a.convKey(row.file) == key {
				return i
			}
		}
	}
	for at, row := range rows {
		if !row.here {
			return at
		}
	}
	return 0
}

// hopClose puts it away and leaves the person exactly where they were.
func (a *app) hopClose() {
	if !a.hop.open {
		return
	}
	if a.hop.live {
		a.hopSeal()
	}
	a.dropHover()
	a.hop = hopCard{}
	a.touch()
}

// hopSeal restacks the previous-stack at the end of a quick burst so that `tab`
// goes back to where the burst STARTED rather than to its last stepping stone.
//
// Cycling A → B → C attached B on the way through, which put B where `tab`
// looks; but the person's own history is "I was in A, now I am in C", and B was
// three hundred milliseconds of passing scenery. Windows restacks its window
// order at exactly this moment for exactly this reason.
func (a *app) hopSeal() {
	from, front := a.convKey(a.hop.from), a.convKey(a.file)
	if from == "" || from == front || a.behind[from] == nil {
		return
	}
	a.rememberOpen(from)
	a.rememberOpen(front)
}

// hopSettled is the fade timer coming due: the card goes, and nothing else
// happens, because the switch it was a receipt for happened on the keypress.
// A stale pulse is a timer some earlier press scheduled, outrun by a later one.
func (a *app) hopSettled(msg hopSettleMsg) {
	if !a.hop.open || !a.hop.live || msg.pulse != a.hop.pulse {
		return
	}
	a.hopClose()
}

// hopTick schedules the fade and outdates every timer before it.
func (a *app) hopTick() tea.Cmd {
	a.hop.pulse++
	pulse := a.hop.pulse
	return surfaceTick(hopSettle, func(time.Time) tea.Msg { return hopSettleMsg{pulse: pulse} })
}

// hopReading reads open tabs in the strip's exact order, then the remaining
// held and saved conversations behind the fold. Remembered tabs whose agents
// are elsewhere remain open rows because membership belongs to the strip.
func (a *app) hopReading(all bool) (rows []hopRow, tabs, rest int) {
	now := a.now()
	open := a.openTabRows(now)
	var loose []hopRow
	for _, key := range a.prev {
		if held := a.behind[key]; held != nil && a.tabShut[key] {
			loose = append(loose, a.hopKept(held, now))
		}
	}
	if a.tabShut[a.frontTabKey()] {
		loose = append(loose, a.hopFront(now))
	}
	// The held set hopRest skips is BOTH halves: a conversation this window holds
	// must not be drawn a second time off the machine's own reading, where it
	// would come back wearing the lock this very process is holding.
	behind := append(loose, a.hopRest(append(append([]hopRow(nil), open...), loose...), now)...)
	if !all {
		// THE COUNT IS STILL TAKEN. The fold has to say what is behind it, and a
		// door that could not name what it holds is a door nobody opens.
		return open, len(open), len(behind)
	}
	return append(open, behind...), len(open), 0
}

// openTabRows reads exactly the tab strip, including remembered connections
// whose agents are no longer held locally. Home reads the same tab list.
func (a *app) openTabRows(now time.Time) []hopRow {
	var rows []hopRow
	for _, tab := range a.tabList() {
		// A run tab is a view of its conversation, not another conversation.
		if tab.work {
			continue
		}
		row := hopRow{file: tab.file, title: tab.word, where: tab.where, open: true}
		if tab.here {
			row = a.hopFront(now)
		} else if held := a.behind[tab.key]; held != nil {
			row = a.hopKept(held, now)
		}
		row.title, row.where = tab.word, tab.where
		rows = append(rows, row)
	}
	return rows
}

// hopTabbed is whether a row has a tab on the row above the card — the one
// question that decides which side of the fold it is drawn on.
//
// IT ASKS [app.tabShut] AND NEVER THE DRAWN STRIP. `tabShut` is set the instant a
// tab is dismissed, by the `✕`, by `ctrl+w` in the conversation and by `ctrl+w`
// on this card alike, and it is cleared in [app.rememberOpen] — the one door
// every road back to the front goes through. The drawn strip is a frame behind
// that: the card rebuilt from it after its own `ctrl+w` would have kept the row
// it had just closed until something else redrew the row.
//
// THE CONVERSATION IN FRONT IS ALWAYS TABBED. It is the one tab the strip cannot
// be without ([app.tabList] appends it whatever else it found), and a `you are
// here` row below the fold would be the card saying the person is standing
// somewhere it is not showing.
//
// AND A ROW THIS WINDOW IS NOT HOLDING IS NOT TABBED EITHER, WHICH IS THE HALF
// `tabShut` CANNOT ANSWER. Nothing ever dismissed a conversation this terminal
// never opened, so the map says nothing about it and said so as `false` — which
// read as "it has a tab". [app.hopReading] never noticed, because every row it
// splits is one the keeper is holding; [app.hopAway] did, and closed a tab that
// was not there: it swore `tab closed · <title>` at a machine row, marked a
// conversation it had never held as dismissed, and changed nothing on the row.
// `dev` refused that correctly before this card learned to fold. Found in review.
func (a *app) hopTabbed(row hopRow) bool {
	if row.here {
		return !a.tabShut[a.frontTabKey()]
	}
	return row.open && !a.tabShut[a.convKey(row.file)]
}

// hopStripOrder lays the open rows out in the order the tab row above them is
// drawn in: the leftmost tab is the first row on the card.
//
// ── WHY THE CARD FOLLOWS THE STRIP AND NOT THE RING ─────────────────────────
//
// These are two readings of one set of conversations shown one line apart, and a
// person uses them together: they see `Chats` on the row, press `ctrl+k`, and
// look for the conversation they were just looking at. Ordered by recency the
// card put it somewhere else — the third tab could be the first row — so the two
// lines disagreed about the same five conversations and neither position meant
// anything. Asked for by the owner.
//
// THE STRIP'S ORDER IS FIRST-ENTERED AND IT NEVER MOVES (chattabs.go says why: a
// person reaches for the position, not for the word). Taking that order here
// buys the card the same stability, and it is what makes the digits worth
// drawing — `3` on the card is the third tab on the row.
//
// A CONVERSATION WITH NO TAB KEEPS ITS PLACE AFTER THE ONES THAT HAVE ONE. A tab
// dismissed with `ctrl+w` leaves the conversation held, running and on this card
// (hop.go's [app.hopAway]), and a row the strip never drew has no position to
// borrow — so those sort after the tabbed rows, most recently in front first,
// which is the order this function was handed. The sort is STABLE for exactly
// that reason.
//
// AND IT READS THE STRIP THAT WAS DRAWN, never [app.tabList], which rebuilds the
// row in place and would have the card writing to the thing it is reading. On a
// frame too short to draw the strip at all there is no visible order to follow
// and the rows keep the one they came in with.
func (a *app) hopStripOrder(rows []hopRow) []hopRow {
	if len(a.chatTabs) == 0 || len(rows) < 2 {
		return rows
	}
	at := make(map[string]int, len(a.chatTabs))
	for i, tab := range a.chatTabs {
		if tab.key != "" && !tab.start {
			if _, seen := at[tab.key]; !seen {
				at[tab.key] = i
			}
		}
	}
	place := func(row hopRow) int {
		if i, ok := at[a.convKey(row.file)]; ok {
			return i
		}
		return len(a.chatTabs)
	}
	sort.SliceStable(rows, func(i, j int) bool { return place(rows[i]) < place(rows[j]) })
	return rows
}

// hopRest is every OTHER conversation on this machine, ranked the way home ranks
// them, and it is the half of the card that made the key worth binding.
//
// THE CARD USED TO HOLD ONLY WHAT WAS ALREADY OPEN, AND THAT WAS THE DEFECT. A
// person who has just started codeaf holds exactly one conversation, so the key
// did nothing, was advertised nowhere, and could only be discovered by somebody
// who already knew that `enter` on home opens a second one beside the first.
// The feature was invisible until you had learned the thing it exists for.
//
// THE WORLD IS READ ON THE KEYSTROKE, ONCE, and that is affordable for one
// reason: this gesture REPLACES pressing `space space`, which takes the same
// reading and then draws a whole page with it. It can be no slower than what a
// person does today to answer the same question.
func (a *app) hopRest(open []hopRow, now time.Time) []hopRow {
	world, known := a.readWorldKnown()
	if !known {
		return nil
	}
	a.hopKnown = 0
	// THE SAME LOOK STAMP HOME MEASURES `since you left` FROM (home.go), so a
	// row's note reads identically in both places.
	seen := session.LastLook(a.looksRoot())
	held := make(map[string]bool, len(open))
	for _, row := range open {
		if key := a.convKey(row.file); key != "" {
			held[key] = true
		}
	}
	var all []switcherRow
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			if row.Archived {
				continue
			}
			a.hopKnown++
			if held[a.convKey(row.Transcript)] {
				continue
			}
			needs := row.NeedsPerson()
			all = append(all, switcherRow{
				kind: switcherConversation, session: row, project: project.Name,
				title: homeName(row), note: switcherConversationNote(row, seen),
				age: sinceAt(row.At, now), at: switcherSortAt(row), needs: needs,
				moving: !needs && (row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking),
				// THE LOCK AND NOT THE HEARTBEAT, on switcher.go's own reasoning:
				// `Open` is another window holding this journal, which is what the
				// door refuses on.
				held:  row.Open && strings.TrimSpace(row.Transcript) != "",
				place: switcherWhere(row, project),
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return switcherLess(all[i], all[j]) })
	rest := make([]hopRow, 0, len(all))
	for _, row := range all {
		if len(open)+len(rest) >= hopShown {
			break
		}
		// THE FOLDER IS ASKED ABOUT ONCE AND BOTH READINGS TAKE THAT ANSWER. It
		// used to be asked here for the GLYPH and left false on the row
		// [hopRestNote] reads, so a conversation whose project had gone was drawn
		// with the refusing `✕` and no clause saying why — a mark a person cannot
		// account for, which is the whole of what the owner's report was about.
		row.gone = !homeFolderThere(row.place)
		rest = append(rest, hopRow{
			file: row.session.Transcript, title: row.title, project: row.project,
			note: hopRestNote(row), age: row.age, needs: row.needs, moving: row.moving,
			where: row.place, held: row.held, gone: row.gone,
		})
	}
	return rest
}

// hopRestNote is what a conversation that is NOT open says about itself. It is
// home's own note where there is one — a question, work turning — and the plain
// statement of its state where there is not.
//
// `not open yet` IS SAID OUT LOUD rather than left blank, because it is the one
// fact that changes what `enter` will do on that row: everything above the rule
// is one keystroke away and everything below it is a conversation being started
// up again.
func hopRestNote(row switcherRow) string {
	switch {
	case row.gone:
		return homeGoneWord
	case row.held:
		return hopHeldWord
	}
	// AND A QUIET ONE SAYS NOTHING AT ALL. `not open yet` on nine rows in a row is
	// the layout's own fact said nine times — the rule above them already draws
	// the line between what is running and what would be started up — and the
	// emptiness law is exactly this: a column repeats news, never state.
	return strings.TrimSpace(row.note)
}

// hopKept is one row for a conversation this process holds but is not drawing.
func (a *app) hopKept(held *kept, now time.Time) hopRow {
	agent := held.conv.Agent
	running := runningTasks(agent)
	// THE CARD READS THE WATCHER'S CACHED ANSWER rather than asking the agent a
	// second live question. The watcher recomputes it after every event this
	// conversation produces, and a second question here is exactly how the tab
	// and card came to disagree about one conversation on one frame.
	sig := held.watch.signal()
	row := hopRow{
		file:    held.conv.SessionFile,
		title:   hopTitle(agent, held.side),
		project: hopProject(held.conv.Place, held.conv.Workspace),
		needs:   sig == tabNeedsPerson,
		moving:  sig == tabWorking,
		open:    true,
	}
	if held.side != nil {
		row.age = sinceAt(held.side.since, now)
	}
	row.note = hopNote(sig, running, held.watch.landedSince())
	return row
}

// hopFront is the conversation on screen. It is a row like any other — same
// title, same project, same age — because a ring with a hole in it is a ring a
// person has to count their way around.
func (a *app) hopFront(now time.Time) hopRow {
	// THE ROW A PERSON IS STANDING ON IS A ROW LIKE ANY OTHER. The mark it wears
	// is therefore the mark its own tab is wearing this instant, while its note
	// remains the ring's statement that this is where the person is.
	sig := a.frontSignal()
	return hopRow{
		file: a.file,
		// THE SURFACE'S OWN SPELLING FOR THE ONE ON SCREEN. It is what the status
		// line is showing this instant ([app.sessionName]), and a card that named
		// the conversation you are sitting in differently from the line at the
		// foot of the frame would be two names for one thing on one screen.
		title:   hopFrontName(a.sessionName()),
		project: hopProject(a.place, a.workspace),
		note:    hopHereWord,
		age:     sinceAt(a.frontAt, now),
		here:    true,
		needs:   sig == tabNeedsPerson,
		moving:  sig == tabWorking,
		open:    true,
	}
}

// hopTitle is the conversation's name: the agent's own, then the one the surface
// was using when it was left, then the word for a conversation that has not been
// called anything yet.
//
// THE AGENT IS ASKED FIRST because a title goes on being groomed while nobody is
// watching, and a name copied into the sidecar would be the one it had at the
// moment somebody walked away from it.
//
// AND THE FILE NAME IS NEVER REACHED. The resume picker's ladder ends at the
// transcript's own name, which is right for a page somebody went to in order to
// choose between sessions; names.go states the rule this obeys instead — a
// session file is called `20260816-150405_a1b2c3`, and a card that offered that
// to a person as the name of their conversation would be worse than saying
// plainly that it has no name yet.
func hopTitle(agent Agent, side *aside) string {
	if title := hopRawTitle(agent, side); title != "" {
		return readableName(title)
	}
	return hopNewWord
}

// Tabs and the wider switcher share title lookup, with their own empty labels.
func hopRawTitle(agent Agent, side *aside) string {
	title := ""
	if agent != nil {
		title = strings.TrimSpace(agent.Title())
	}
	if title == "" && side != nil {
		title = strings.TrimSpace(side.title)
		if title == "" || title == unnamedConversationWord {
			title = promptName(side.openingPrompt)
			if title == "" {
				title = promptName(side.draft)
			}
		}
	}
	return title
}

// hopNewWord is the switcher's name for a conversation nothing has been said
// in yet. It is the same source as every other surface's spelling, because one
// thing has one name.
const hopNewWord = unnamedConversationWord

// hopFrontName is that spelling with the empty case answered.
func hopFrontName(name string) string {
	if strings.TrimSpace(name) == "" {
		return hopNewWord
	}
	return name
}

// hopProject is the word at the right margin: the name the door gave this
// conversation's workspace, or the folder's own name where it gave none.
func hopProject(place, workspace string) string {
	if place = strings.TrimSpace(place); place != "" {
		return place
	}
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		return filepath.Base(workspace)
	}
	return ""
}

// hopNote is what changed since you last looked, and the order is worst news
// first — which is the order home's own rows are ranked in (switcher.go's
// [switcherRank]) and the order a person needs them in.
//
// IT IS THE SAME READING THE ROW'S MARK CAME FROM, which is why the state
// arrives here as a [tabSignal] rather than as a second yes-or-no this function
// works out for itself. A row that drew `◐` and said `nothing new` beside it was
// exactly that second reading (#708), and the only way it comes back is somebody
// deciding the words apart from the glyph. The count refines the working word
// where there is one — `3 tasks running` is the same sentence with a figure in
// it — and never contradicts it: work the project index cannot count, a queued
// node or a background job, still says `working`.
//
// IT NEVER GUESSES AT A QUESTION'S WORDS. Home can say "asks: …" because it
// reads the presence file the conversation wrote, which carries the question's
// text; this card is asking a live agent pointer a yes-or-no question and has
// nothing but the yes. Naming the tool it wants would mean a door onto the
// engine that does not exist, and inventing a sentence for it would be worse
// than the short true one.
func hopNote(sig tabSignal, running, landed int) string {
	switch {
	case sig == tabNeedsPerson:
		return hopAskingWord
	case sig == tabWorking && running > 0:
		return itoa(running) + " " + plural("task", running) + " running"
	case sig == tabWorking:
		return tabSignalWord(tabWorking)
	case landed > 0:
		return hopLandedWord
	}
	return hopNothingWord
}

// runningTasks is how many nodes are turning in one conversation, asked of
// whatever agent this is.
//
// IT IS [app.behindTasks]'S OWN QUESTION ASKED OF ONE AGENT, and that function
// now asks it through this one — the assertion, the status string and the walk
// were about to exist twice, which is how the two answers drift.
func runningTasks(agent Agent) int {
	door, ok := agent.(interface {
		TaskIndex() []session.TaskIndexEntry
	})
	if !ok {
		return 0
	}
	running := 0
	for _, entry := range door.TaskIndex() {
		if entry.Status == string(session.TaskRunning) {
			running++
		}
	}
	return running
}

// ── the keyboard, while the card is up ──────────────────────────────────────

// hopKey is the switcher's whole claim on the keyboard: the key that OPENS it,
// and — while it is open — every key there is.
//
// IT TAKES EVERYTHING BUT `ctrl+c`, for the composer layer's reason
// (composerlayer.go's [app.composerLayerKey]): the card is a choice with the
// answers on screen, and a key that fell through it would be a key acting on a
// conversation the person is looking past. `ctrl+c` is excepted as it is at every
// modal on this surface — leaving is never modal — and it puts the card away on
// its way through.
func (a *app) hopKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.hopShowing() {
		a.keyboardPlaceSelection()
	}
	key := msg.String()
	if !a.hop.open {
		forward, backward := a.hopOpens(key), a.hopBacks(key)
		if (!forward && !backward) || !a.hopMayOpen() {
			return nil, false
		}
		a.hopOpen()
		if !a.hop.open {
			// Nowhere to go; the open refused. The key is still spent — a chord
			// that fell through to mean something else would be a keystroke with
			// two meanings on one screen.
			return nil, true
		}
		if backward {
			// THE REVERSE CHORD ENTERS AT THE OTHER END OF THE RING — the open
			// conversation you have not looked at for longest — which is the row
			// alt+shift+tab has selected first on every desktop since Windows 3.
			a.hop.at = hopLastStop(a.hop.rows)
		}
		if at := a.hop.at; a.hopQuick && (key == hopAlias || key == hopBackAlias) && at < len(a.hop.rows) && a.hop.rows[at].open && !a.hop.rows[at].here {
			// QUICK SWITCHING: the press IS the switch. The card stays up as a
			// receipt and fades on its own; there is nothing to commit, because
			// it already happened. A card with no open row to slide to — one
			// conversation, everything else behind the fold — opens as the
			// browsing card instead: a receipt for a switch that did not happen
			// would fade before its fold line could be read.
			a.hop.live = true
			return a.hopSlide(), true
		}
		return nil, true
	}
	// A delayed sweep must not repaint the old pointer after keyboard navigation.
	a.ptr.have = false
	a.dropHover()
	if key == "ctrl+c" {
		// Put away, and the key goes on to mean what it always means.
		a.hopClose()
		return nil, false
	}
	// EVERY KEY CLEARS THE LINE THE LAST ONE LEFT. A refusal that outlived the
	// keystroke after it would be the card answering a question nobody asked.
	say := a.hop.say
	a.hop.say = ""
	_ = say
	// THE CHORD KEEPS SWITCHING, EVERYTHING ELSE STOPS IT. While the card is
	// live, another press of the chord is one more step of the same gesture; any
	// other key is the person changing what they are doing — looking, folding,
	// closing — and the card converts to the browsing one, which moves without
	// switching and never fades out from under a reader.
	if key == hopOpenKey || key == hopBackKey {
		a.hop.live = false
	}
	switch {
	case a.hopOpens(key):
		a.hopWalk(1)
		if a.hop.live {
			return a.hopSlide(), true
		}
		return nil, true
	case a.hopBacks(key):
		a.hopWalk(-1)
		if a.hop.live {
			return a.hopSlide(), true
		}
		return nil, true
	case key == "down", key == "tab", key == "ctrl+n":
		a.hop.live = false
		a.hopWalk(1)
		return nil, true
	case key == "up", key == "shift+tab", key == "ctrl+p":
		a.hop.live = false
		a.hopWalk(-1)
		return nil, true
	case key == hopFoldKey:
		a.hop.live = false
		a.hopSpread(true)
		return nil, true
	case key == hopShutKey:
		a.hop.live = false
		a.hopSpread(false)
		return nil, true
	case key == hopAwayKey:
		a.hop.live = false
		return a.hopAway(), true
	case key == "enter":
		return a.hopTake(), true
	case key == "esc":
		return a.hopBack(), true
	}
	if a.hop.live {
		// TYPING RIDES STRAIGHT THROUGH A LIVE CARD. Under quick switching the
		// switch already happened and the card is only lingering; a person who
		// lands in a conversation and starts a sentence must not lose its first
		// letter to a receipt. The card goes, and the key means what it means.
		a.hopClose()
		return nil, false
	}
	// A DIGIT TAKES ITS ROW OUTRIGHT, for the first nine rows — every digit a
	// single keystroke can be ([hopDigits]) — and the digit is drawn on the rows
	// that have one. The rows past it are reached with the cursor.
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		if at := int(key[0] - '1'); at < len(a.hop.rows) && at < hopDigits {
			a.hop.at = at
			return a.hopTake(), true
		}
		return nil, true
	}
	// ANYTHING ELSE PUTS IT AWAY AND IS SWALLOWED. A person who reached for a
	// key that means nothing on the BROWSING card has stopped switching; the
	// card goes, and the keystroke is not also delivered to the conversation
	// underneath, because a letter that arrived in a draft on the way out of an
	// overlay it was aimed at is a letter nobody typed on purpose.
	a.hopClose()
	return nil, true
}

// hopLastStop is where the reverse chord enters the ring: the last row that is
// not the one you are standing in — which is the last tab on the row, now that
// the rows are laid out the way the strip is ([app.hopStripOrder]).
func hopLastStop(rows []hopRow) int {
	for at := len(rows) - 1; at >= 0; at-- {
		if !rows[at].here {
			return at
		}
	}
	return 0
}

// hopSlide is one step of quick switching: the surface actually moves to the
// row under the cursor, and the card stays up over it as a receipt.
//
// ONLY A ROW THIS PROCESS HOLDS IS SLID TO. Opening a closed conversation
// replays a journal and takes a lock, which is far too much to do to three rows
// in passing on the way to a fourth; those rows keep their `enter`, and the
// fold that reveals them already converts the card to browsing.
func (a *app) hopSlide() tea.Cmd {
	if a.hop.at < 0 || a.hop.at >= len(a.hop.rows) {
		return a.hopTick()
	}
	row := a.hop.rows[a.hop.at]
	front := a.convKey(a.file)
	if !row.open || a.convKey(row.file) == front {
		return a.hopTick()
	}
	// The row being left gets the note it would have been built with had the
	// card opened here — asked NOW, while it is still on this side of the attach
	// and there is still an agent on the loop to ask.
	//
	// AND IT IS ASKED OF THE SAME READING ITS MARK CAME FROM ([app.frontSignal],
	// which is what [app.hopFront] built this row with). The row keeps the glyph
	// it was drawn with and gains a sentence, so the two halves of one row cannot
	// be about two different moments.
	wasNote := hopNote(a.frontSignal(), runningTasks(a.agent), 0)
	cmd, ok := a.bringForward(row.file)
	if !ok {
		if _, remembered := chatTabAt(a.tabList(), a.convKey(row.file)); remembered {
			return a.hopStart(row)
		}
		// The conversation went away mid-burst. The card stops fading and says
		// so where the person is looking; they are mid-gesture, and a receipt
		// that vanished while carrying a refusal would be a refusal nobody saw.
		a.hop.say, a.hop.live = hopGoneWord, false
		a.touch()
		return nil
	}
	// THE `you are here` MARK MOVES WITH THE SURFACE. The rows stay frozen —
	// nothing is re-read, nothing renumbers — but a card whose mark stayed on
	// the conversation three steps back would be lying about the one fact the
	// person is mid-gesture about.
	for i := range a.hop.rows {
		held := &a.hop.rows[i]
		switch {
		case a.convKey(held.file) == front:
			held.here, held.note = false, wasNote
		case held.here:
			held.here, held.note = false, ""
		}
	}
	a.hop.rows[a.hop.at].here = true
	a.hop.rows[a.hop.at].note = hopHereWord
	// AND THE PLACE COMES DOWN ON THE BURST'S FIRST SWITCH, exactly as `enter`
	// does: quick switching is the same act on a different chord, and a card
	// fading over home while the conversation changed underneath it is the bug
	// [app.hopLand] is about.
	cmd = a.hopLand(cmd)
	a.touch()
	return tea.Batch(cmd, a.hopTick())
}

// hopBack is `esc`: back to the conversation the card opened over, card down.
// Under quick switching the surface has already moved, so this is the undo; on
// the browsing card nothing moved, and going where you already are is staying.
func (a *app) hopBack() tea.Cmd {
	from := a.hop.from
	// NOT A SEAL. Going back is the burst being taken back, and a previous-stack
	// restacked for it would put the abandoned stepping stones where `tab` looks.
	a.hop.live = false
	a.hopClose()
	if from == "" || a.convKey(from) == a.convKey(a.file) {
		return nil
	}
	cmd, _ := a.bringForward(from)
	return cmd
}

// hopOpens reports whether this key is a way in — the binding, or the alias on a
// terminal that answered the keyboard query.
func (a *app) hopOpens(key string) bool {
	return key == hopOpenKey || (key == hopAlias && a.ctrlDigits())
}

// hopBacks is the same question for the reverse, and the two chords no longer
// answer it on the same terms. `alt+shift+k` is escape-then-`K` and arrives
// everywhere, so it is bound flat; `ctrl+shift+tab` is still `ctrl+tab`'s own
// reverse and still only real where the terminal answered the keyboard query
// ([hopBackKey] states why the asymmetry is the point of the spelling).
func (a *app) hopBacks(key string) bool {
	return key == hopBackKey || (key == hopBackAlias && a.ctrlDigits())
}

// hopWalk moves the cursor, wrapping at both ends. It wraps because this is a
// ring: a person who overshoots the row they wanted must not have to walk back
// through seven conversations to reach it.
func (a *app) hopWalk(by int) {
	n := len(a.hop.rows)
	if n == 0 {
		return
	}
	a.hop.at = ((a.hop.at+by)%n + n) % n
	// A WALK DISARMS THE CLOSE. `ctrl+w` warned about ONE row, and a warning that
	// survived the cursor leaving it would close a conversation the person was
	// no longer looking at.
	a.hop.armed = -1
	a.touch()
}

// hopTake goes to the row under the cursor.
//
// THE ROW YOU ARE ALREADY ON IS NOT A SWITCH. Committing `you are here` brings
// nothing forward — [app.bringForward] answers the same way for the same reason,
// and doing it here as well means the card never depends on that agreement
// holding — but it still LANDS, because a place standing over the conversation
// is a place `enter` has to come down off ([app.hopLand]).
func (a *app) hopTake() (cmd tea.Cmd) {
	if a.startingChat() {
		back := a.parkChatStart()
		defer func() { cmd = tea.Batch(back, cmd) }()
	}
	if a.hop.at < 0 || a.hop.at >= len(a.hop.rows) {
		a.hopClose()
		return nil
	}
	row := a.hop.rows[a.hop.at]
	a.hopClose()
	if row.here {
		a.rememberOpen(a.frontTabKey())
		return a.hopLand(nil)
	}
	if !row.open {
		return a.hopStart(row)
	}
	cmd, ok := a.bringForward(row.file)
	if !ok {
		if _, remembered := chatTabAt(a.tabList(), a.convKey(row.file)); remembered {
			return a.hopStart(row)
		}
		// The conversation went away between the card opening and this key —
		// another window took it over (takeover.go), or it was closed. The card
		// is already down; saying so is better than a keystroke that did nothing.
		a.hopSay(hopGoneWord)
		return nil
	}
	return a.hopLand(cmd)
}

// hopLand is the last step of every take that succeeded: the place a person was
// standing on comes down, so `enter` on the card leaves them looking at the
// conversation it named.
//
// ── WHY THE CARD HAS TO DO THIS AT ALL ──────────────────────────────────────
//
// The switcher is drawn over the screen rather than being a screen of its own,
// which is what lets it open on home, tasks, standing, memory, spend, search and
// settings alike. The cost of that is that taking a row moved the conversation
// UNDERNEATH a place and left the place in front: from home, `enter` looked like
// a key that did nothing, while it had in fact quietly swapped the conversation
// behind the screen the person was reading. Reported by the owner, who pressed
// `ctrl+k` on home, chose a conversation, and stayed on home.
//
// EVERY OTHER DOOR BETWEEN CONVERSATIONS ALREADY DOES IT, and each spells it for
// itself: home's own `enter` ends in [app.closeHome] (home.go's
// [app.homeWalkIn]), search's row door in [app.standDownFullscreen]
// (place_search.go's [app.openConversationRow]), and `ctrl+shift+t` in
// [app.closeHome] again (tabreopen.go). This is that same statement, made once
// for the one door that can be opened from ANY place — which is why it asks
// [app.pageShowing] rather than naming home.
//
// IT RUNS ONLY WHERE THE TAKE SUCCEEDED. A refusal leaves the place standing,
// with its sentence on that place's own line ([app.hopSay]), because a person
// who has just been told no must still be able to read it.
func (a *app) hopLand(cmd tea.Cmd) tea.Cmd {
	if a.pageShowing() {
		a.leavePlace()
	}
	return cmd
}

// hopSay puts one of the card's refusals on whichever line the person can
// actually see it on: home's own sentence, a place's one line, or the entry line
// of the conversation underneath when no place is standing.
//
// IT IS [app.sayWhereQuestionWent] WITH THE THIRD CASE, and it exists for the
// same reason that one does — there is no line invented for this. A refusal said
// with [app.note] while home was up went into a transcript nobody was looking at,
// which is the quietest way to answer a keystroke.
func (a *app) hopSay(note string) {
	switch {
	case a.at(pageHome):
		a.home.say(note, "")
	case a.pageShowing():
		a.pageMsg = note
	default:
		a.note(note)
	}
}

// hopGoneWord is what the switcher says about a row that stopped existing while
// it was on screen. It names no path: the person pressed a row, and the row is
// what they are being told about.
const hopGoneWord = "that conversation is no longer open"

// hopStart opens a conversation this process was NOT holding, and it makes
// exactly the two checks home's `enter` makes, in the same order (home.go's
// [app.homeOpenDoor]): is the folder still there, and does the door itself
// refuse. Nothing asks how many are already open — a window holds as many
// conversations as somebody opens (keeper.go).
//
// IT SAYS THE REFUSAL WHERE THE PERSON IS ([app.hopSay]). The card is already
// down by the time this runs, so the sentence goes on the line the screen they
// are looking at already has for saying things — home's own, a place's one line,
// or the entry line of the conversation when nothing is standing over it.
func (a *app) hopStart(row hopRow) tea.Cmd {
	if !homeFolderThere(row.where) {
		a.hopSay(WorkspaceGoneWord + " · " + row.where)
		return nil
	}
	cmd, refusal := a.openBeside(row.where, row.file)
	if refusal != "" {
		a.hopSay(refusal)
		return nil
	}
	return a.hopLand(cmd)
}

// hopOpenRows is how many conversations are OPEN IN THIS WINDOW — how many tabs
// are on the row above — and it is the figure the head says `3 of 12` with.
//
// IT IS WHAT THE CARD WAS READ WITH and not a walk of the rows, because the rows
// gain the rest of the machine when the fold opens and a count taken off them
// then would say the window had twelve conversations open the moment somebody
// looked at what else there was.
func (a *app) hopOpenRows() int { return a.hop.tabs }

// ── what the card looks like ────────────────────────────────────────────────
//
// THE CARD IS A BOX, AND IT IS THE ONE ON THIS SURFACE. The house rule is that
// nothing is outlined — emphasis is a raised ground and an accent, never a ring
// drawn round a thing (harnesscard.go states it) — and this is the deliberate
// exception, ruled by the owner off SCREEN 3b's own drawing. The reason it earns
// the exception is that it is the only thing here that FLOATS: every other panel
// on this surface takes the frame or hangs off an edge, so its bounds are the
// screen's. A list dropped into the middle of a dimmed transcript has no edge of
// its own, and without one the eye reads it as text that happens to be brighter.
//
// THE COLUMNS ARE FIXED AND THAT IS THE WHOLE OF WHY IT SCANS. Glyph, subject,
// one clause, project, clock — the subjects form a straight edge you read down
// and the clauses are short enough to skip. A fluid subject column would put
// every note at a different indent and turn eight rows into eight sentences.
const (
	// hopSideInset is how far the box stands in from the body on each side. SIX,
	// which is SCREEN 3b's own figure: enough that the dimmed transcript is
	// visible past both edges, which is what says the page is still there.
	hopSideInset = 6
	// hopPad is the air inside the box, between its border and its rows.
	hopPad = 3
	// The columns, in cells. THE SUBJECT IS THE ONE THAT GROWS: every other
	// column holds a phrase of known length, and the names are what a person is
	// actually reading down the card, so the room a wide terminal has to spare
	// belongs to them. It used to be the clause that took it, which left
	// `Investigate the parsing regression…` cut at thirty-four cells beside forty
	// cells of `nothing new`.
	hopGlyphCol   = 2
	hopNoteCol    = 32
	hopProjectCol = 12
	hopAgeCol     = 5
	// hopSubjectMax is where the subject stops growing and hands the rest back to
	// the clause. A name is read in one glance; past about this many cells the
	// card is a page of sentences and the straight edge stops doing its work.
	hopSubjectMax = 64
	// hopTightSubject is the least the subject is cut to while any other column
	// still has cells to give: a name cut to twenty cells is still a name, and one
	// cut to eight is a shrug.
	hopTightSubject = 20
	// hopTightNote is what the clause narrows to on the way down, which is
	// `3 tasks running` whole.
	hopTightNote = 16
	// hopGutter is the air kept at the right-hand end of the subject column, so a
	// name that fills its column does not touch the clause beside it. Without it
	// the two run together as one word — `…parsing regres…nothing new` — which is
	// the reading the owner reported, and no amount of extra column width fixes
	// it, because a long enough name fills whatever it is given.
	hopGutter = 2
)

// hopMinBody is the shortest body the card will draw itself into: two border
// rows, the head, its blank, one conversation, and a row of air above and below.
const hopMinBody = 8

// hopCardLines is the card itself — the top border, the head row, a blank, one
// line per conversation, the fold at the foot, and the bottom border — laid out
// to `width` and capped to `height` rows.
//
// A CARD THAT DOES NOT FIT DROPS CONVERSATIONS AND NEVER ITS FRAME. The head
// says what the keys are and the borders say where the card ends; a box that ate
// either to show one more row would be a box a person cannot get out of.
func (a *app) hopCardLines(width, height int, pal palette) []string {
	if !a.hopShowing() || width < 12 || height < 5 {
		return nil
	}
	pal, ground := pal.hopSurfacePalette()
	surface := func(s string, width int) string { return pal.background(s, width, ground) }
	inner := width - 2
	room := inner - 2*hopPad
	pad := strings.Repeat(" ", hopPad)
	// THE BORDER IS THE ONE FRAME (frame.go), and it is dim — the tier this
	// surface says its own furniture in (a rule, a seam, a connector). An accent
	// border would be the box announcing itself, and what has to be read here is
	// the list inside it. The card's own raised ground is the frame's ground, so
	// the edge stands on the surface the rows stand on.
	//
	// topEdge is the one row the frame draws above the rows laid out below,
	// which every row index recorded for the pointer counts.
	const topEdge = 1
	// The rows a person reads are laid out at `room` and then set inside the
	// borders whole, so the band on the cursor's row covers the padding too —
	// which is what makes it read as a row of the card rather than a highlight
	// floating inside one.
	inside := func(line string, band, hovered bool) string {
		line = fit(line, room)
		if w := room - ansi.StringWidth(line); w > 0 {
			line += strings.Repeat(" ", w)
		}
		left := pad
		if band {
			// Selection remains a position even when the terminal has no color.
			left = pal.ink(">") + pad[1:]
		}
		if hovered {
			// Hover and selection keep separate cells, so reading another row does
			// not pretend that Enter has changed its destination. The last padding
			// cell stays blank so neither marker touches the row number.
			mark := "·"
			if pal.ascii {
				mark = "."
			}
			left = ansi.Cut(left, 0, 1) + pal.accent(mark) + pad[2:]
		}
		body := left + line + pad
		switch {
		case band:
			body = pal.selected(body, inner)
		case hovered:
			body = pal.cursor(body, inner)
		default:
			body = surface(body, inner)
		}
		return body
	}

	a.hop.spots = nil
	// Roomy cards breathe inside the outline. On a short terminal this air
	// yields first, preserving a visible selected row and the ways out.
	verticalPad := 0
	if height >= 9 {
		verticalPad = 1
	}
	lines := []string{}
	if verticalPad > 0 {
		lines = append(lines, inside("", false, false))
	}
	lines = append(lines, inside(a.hopHead(room, pal), false, false), inside("", false, false))
	footWord := a.hopFoot()
	foot := 1 + verticalPad
	if footWord != "" {
		foot++
	}
	// THERE IS NO SECOND READING OF THE SELECTED TITLE. A block under the rows
	// used to re-wrap the highlighted row's name whenever the subject column had
	// abbreviated it — but it only ever repeated the row the cursor was already
	// on, it appeared on some rows and not others depending on how long that one
	// name was, and it cost the rows underneath it their places on a short card.
	// The name is worth more room on the row itself, which is what the subject
	// column now takes ([hopLine]). Asked for by the owner.
	// AND THE WORD THAT SAYS WHERE THE OPEN ONES STOP, which is the fold's own
	// half of the head's `open` ([hopClosedLabel]).
	//
	// IT IS SPACED THE WAY THE HEAD IS SPACED, which is the whole of why it reads
	// as a heading and not as a row: one blank above it and one below, exactly as
	// `open` has the card's own air above it and a blank under it before the first
	// row. Drawn tight against the rows it sat between two lists and belonged to
	// neither. The air goes on a short card, on the same rule as the card's own
	// (`verticalPad` above): air yields first, and the heading itself does not.
	label, seamAir := -1, verticalPad
	if a.hop.all && a.hop.tabs > 0 && a.hop.tabs < len(a.hop.rows) {
		label = a.hop.tabs
	}
	available := max(1, height-len(lines)-topEdge-foot)
	// THE SEAM'S LINES ARE ONLY SPENT WHERE THE SEAM IS DRAWN, and it is drawn
	// only when the scroll window actually reaches the first closed row.
	//
	// Charged unconditionally they were charged on every short card: the window
	// lost three rows, the rows it lost were the closed ones, and pressing
	// `→ show closed` on a small terminal made the list SHORTER and showed
	// nothing — with the foot cheerfully offering `← hide closed`. Found in
	// review; measured at card heights 8 through 13, where the fold drew no
	// closed row at all.
	//
	// SO THE THREE LINES ARE GIVEN UP IN THE ORDER THEY CAN BE AFFORDED: the air
	// first, on the card's own rule that air yields before anything else
	// (`verticalPad` above), then the word — and a card too short even for the
	// word draws the closed rows without it rather than drawing none of them,
	// because a person who pressed `show closed` asked for the rows.
	window := func(rows int) (int, int) {
		start := max(0, a.hop.at-rows+1)
		return start, min(len(a.hop.rows), start+rows)
	}
	start, end := window(available)
	if label >= start && label < end {
		for _, cost := range []int{1 + 2*seamAir, 1} {
			rows := max(1, available-cost)
			if from, to := window(rows); label >= from && label < to {
				available, start, end = rows, from, to
				seamAir = (cost - 1) / 2
				break
			}
			// The word alone did not fit either: the seam is not drawn, and the
			// window keeps every row it already had.
			if cost == 1 {
				label = -1
			}
		}
	} else {
		// The window does not reach the closed rows at all, so there is no seam on
		// this card and nothing to pay for it with.
		label = -1
	}
	for at := start; at < end; at++ {
		if at == label {
			// THE BLANK ABOVE IS NOT DRAWN AT THE TOP OF THE LIST, where the head's
			// own blank is already the air above this word and a second one would be
			// a gap nobody put there.
			if seamAir > 0 && at > start {
				lines = append(lines, inside("", false, false))
			}
			lines = append(lines, inside(pal.dim(hopClosedLabel), false, false))
			if seamAir > 0 {
				lines = append(lines, inside("", false, false))
			}
		}
		a.hop.spots = append(a.hop.spots, hopSpot{row: len(lines) + topEdge, at: at})
		hovered := a.hot.kind == hoverHop && a.hot.index == at && a.hop.at == at
		lines = append(lines, inside(hopLine(a.hop.rows[at], at, at == a.hop.at, hovered, room, pal), at == a.hop.at, hovered))
	}
	if footWord != "" && len(lines)+topEdge+1+verticalPad < height {
		hovered := a.hop.say == "" && a.hot.kind == hoverHop && a.hot.index == -1
		if a.hop.say == "" {
			a.hop.spots = append(a.hop.spots, hopSpot{row: len(lines) + topEdge, at: -1})
		}
		ink := pal.dim
		if hovered {
			ink = pal.ink
		}
		lines = append(lines, inside(ink(fit(footWord, room)), false, hovered))
	}
	if verticalPad > 0 {
		lines = append(lines, inside("", false, false))
	}
	out, _ := framed{ground: surface}.draw(pal, width, lines)
	return out
}

// hopFoot is the one line under the rows: what the card just said, or the fold
// standing for the conversations this terminal is not holding.
//
// THE SENTENCE OUTRANKS THE FOLD, and only while there is one. A refusal or a
// receipt is about the key just pressed; the fold is always true and will still
// be there on the next frame.
func (a *app) hopFoot() string {
	if a.hop.say != "" {
		return a.hop.say
	}
	switch {
	case a.hop.all:
		return hopShutKeyWord
	case a.hop.rest > 0:
		return hopFoldKeyWord
	}
	return ""
}

// The two halves of the fold's own sentence, spelled once and quoted in the
// manual exactly as they are here.
//
// THEY NAME THE KEY AND WHAT IT DOES, AND NOTHING ELSE. This line used to read
// `▸ 9 more on this machine · → reach them`, which spent a whole row on a figure
// the head already carries — `3 of 12` says both halves of it — and then said
// the same thing twice, once in a triangle nobody reads as a verb and once in
// words. A foot is the one row a person looks at to find out what else they can
// press; it is worth exactly one instruction. Asked for by the owner.
//
// `closed` IS THE WORD FOR WHAT IS DOWN THERE. Every row behind the fold is a
// conversation with no tab on the row above — one this terminal never opened,
// or one whose tab was closed ([app.hopTabbed]) — and that is the one thing they
// have in common. The arrow is the key, so the line names it without a glyph
// in front of it.
const (
	hopFoldKeyWord = "→ show closed"
	hopShutKeyWord = "← hide closed"
)

// hopHead is the line above the list: what this card is on the left, and what
// the keys do on the right.
//
// THE KEYS ARE DROPPED FROM THE RIGHT AS THE FRAME TIGHTENS, in the order a
// person can most afford to lose them — the close, then the way out, then the
// walk — and the count alone survives, because a card with no head is the one
// shape this refuses to draw.
func (a *app) hopHead(width int, pal palette) string {
	left := hopOpenWord
	clauses := append([]string(nil), hopClauses...)
	// ONE COUNT AND NOT TWO. The word on the left already says what this list
	// is; the figure says how much of the machine it is showing, in the
	// grooming's own phrasing — `8 of 11`.
	right := itoa(a.hopOpenRows()) + " of " + itoa(a.hop.total)
	for {
		tail := right
		if len(clauses) > 0 {
			tail += " · " + strings.Join(clauses, " · ")
		}
		if ansi.StringWidth(left)+2+ansi.StringWidth(tail) <= width {
			pad := width - ansi.StringWidth(left) - ansi.StringWidth(tail)
			return pal.dim(left) + strings.Repeat(" ", max(1, pad)) + pal.dim(tail)
		}
		if len(clauses) == 0 {
			return pal.dim(fit(right, width))
		}
		clauses = clauses[:len(clauses)-1]
	}
}

// hopOpenWord is the card's own name for itself, and it is the word the keeper
// already uses for a conversation this terminal is holding (keeper.go's header
// states the law: `open`, never `behind`).
const hopOpenWord = "open"

// hopClosedLabel is the one dim word drawn between the tabs and everything else,
// and it exists because WITHOUT IT THE FOLD OPENS INTO ONE UNDIFFERENTIATED
// LIST.
//
// The head says `open` over the rows that are tabs. `→ show closed` then adds
// rows that are not, and a conversation whose tab was closed a minute ago comes
// back wearing the same `○` and the same live clause it had when it was open —
// because it IS still open in every sense but the tab row's. Reported by the
// owner, who read the `✕` on the rows below as "this tab is closed" and asked
// why the ones they had closed did not have it. They do not: `✕` is
// [tokens.GlyphFailed] and on this card it means the row REFUSES TO OPEN —
// another window is holding that conversation, or its project folder has gone.
// Two different facts, and only one of them had a mark.
//
// SO THE SEAM IS DRAWN AS A WORD AND NOT AS A RULE. The house rule is that
// nothing is outlined (the card's own header states it), the ladder this surface
// separates things with is ink, and one dim word at the seam answers the whole
// question — everything under it has no tab.
const hopClosedLabel = "closed"

// hopClauses are the keys the card owns, in the order a person meets them.
//
// THE CLOSE CLAUSE SAYS `close tab`, because home's `close` archives a
// conversation and hides it from the list until its name is typed. This key
// only removes the tab from this window and leaves the conversation on home's
// list, as [hopAwayWord] explains after the press.
var hopClauses = []string{"enter open", "esc cancel", "↑↓ choose", hopAwayKey + " close tab"}

// hopFootWords is the foot of the FRAME while the card is up — the same clauses
// from the same list, so a person reading the bottom of the screen and a person
// reading the top of the card are told the same things.
var hopFootWords = strings.Join(hopClauses, " · ")

// hopLine is one conversation, in five fixed columns.
func hopLine(row hopRow, at int, sel, hovered bool, width int, pal palette) string {
	glyph, glyphInk := tokens.GlyphQueued, pal.dim
	switch {
	case row.needs:
		glyph, glyphInk = tokens.GlyphNeedsHuman, pal.warn
	case row.moving:
		glyph, glyphInk = tokens.GlyphWorking, pal.accent
	case row.gone, row.held:
		glyph, glyphInk = tokens.GlyphFailed, pal.bad
	}
	// THE DIGIT IS DRAWN EXACTLY WHERE IT IS BOUND, AND THE COLUMN IS HELD OPEN
	// WHERE IT IS NOT. A row a person can take with `3` and is never told about
	// is a key that does nothing until somebody guesses; a tenth row wearing a
	// `10` nothing answers is the same defect the other way round.
	mark := "  "
	if at < hopDigits {
		mark = itoa(at+1) + " "
	}
	// THE SUBJECT TAKES WHAT IS LEFT AND THE TAIL GIVES WAY BEFORE IT DOES. The
	// straight edge the layout is for is the LEFT edge of the names, and that is
	// fixed by the two columns in front of them; what the name needs is room to
	// finish, so every cell the frame has to spare is its. On the way down the
	// columns are given up in the order a person can most afford to lose them —
	// the project, then the clause's own tail, then the clock, then the clause.
	room := max(0, width-len(mark)-hopGlyphCol)
	project, age, note := hopProjectCol, hopAgeCol, hopNoteCol
	if room-project-age-note < hopTightSubject {
		project = 0
	}
	if room-project-age-note < hopTightSubject {
		note = hopTightNote
	}
	if room-project-age-note < hopTightSubject {
		age = 0
	}
	if room-project-age-note < hopTightSubject {
		note = 0
	}
	subject := max(4, room-project-age-note)
	if subject > hopSubjectMax {
		// AND THE CELLS PAST A NAME'S WORTH GO BACK TO THE CLAUSE, so the project
		// and the clock stay on the frame's own right edge rather than floating in
		// the middle of a very wide card.
		note, subject = note+subject-hopSubjectMax, hopSubjectMax
	}
	// A ROW WITH NEWS IS AT FULL INK AND A QUIET ONE IS A STEP BACK, which is
	// what lets the two or three that want you separate from the eight that do
	// not with no heading saying so (SCREEN 2b's own clause).
	// THE NAME IS CUT SHORT OF ITS OWN COLUMN and then padded out to it, which is
	// what keeps the clause a column and not a suffix ([hopGutter]).
	title := fit(row.title, max(1, subject-hopGutter))
	name := pal.narr(fitPad(title, subject))
	clause := pal.dim(fitPad(row.note, note))
	if row.needs || row.moving {
		name, clause = pal.ink(fitPad(title, subject)), pal.narr(fitPad(row.note, note))
	}
	if hovered {
		name, clause = pal.ink(fitPad(title, subject)), pal.narr(fitPad(row.note, note))
	}
	if sel {
		// THE ROW THE KEYBOARD IS ON TAKES THE GROUND AND THE WEIGHT. The band is
		// applied around this line by the card; the subject going bold is the
		// other half, and the tail steps up with it because dim grey on a raised
		// ground is grey on grey (switcher.go holds the same rule for home).
		name, clause = pal.bold(pal.ink(fitPad(title, subject))), pal.narr(fitPad(row.note, note))
	}
	line := pal.dim(mark) + glyphInk(fitPad(glyph, hopGlyphCol)) + name + clause
	if project > 0 {
		line += pal.dim(rightPad(row.project, project))
	}
	if age > 0 {
		line += pal.dim(rightPad(row.age, age))
	}
	return line
}

// fitPad is one column: cut to fit, then padded out to its full width so the
// column after it starts in the same cell on every row.
func fitPad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = fit(s, width)
	if w := width - ansi.StringWidth(s); w > 0 {
		s += strings.Repeat(" ", w)
	}
	return s
}

// rightPad is the same for a column that reads from the right — the project and
// the clock, whose right edges are the card's own margin.
func rightPad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = fit(s, width)
	if w := width - ansi.StringWidth(s); w > 0 {
		s = strings.Repeat(" ", w) + s
	}
	return s
}

// hopOver is the whole of how the card meets the surface underneath: the body's
// own rows are repainted at the faintest stop of the depth ladder, and the card
// is written over the middle of them.
//
// THE ROWS ARE REPLACED RATHER THAN THE FRAME BEING REBUILT, which is what makes
// the card cost nothing: the body was going to be laid out anyway, the frame is
// the same height it was before the key was pressed, and no place, room or
// transcript has a single line about being underneath one.
func (a *app) hopOver(body []string, width int, pal palette) []string {
	a.hop.spots = nil
	a.hop.left, a.hop.right, a.hop.top, a.hop.bottom = 0, 0, 0, 0
	room := width - 2*hopSideInset
	if room < 24 {
		// Too narrow for the inset. The box takes the width it can have rather
		// than not being drawn: a person on a sixty-column frame needs the
		// switcher more than they need the margin.
		room = width
	}
	card := a.hopCardLines(room, len(body)-2, pal)
	if len(card) == 0 || len(body) < hopMinBody {
		// Too short to lay a card into. The body is still faded — the card is up,
		// and a surface that dimmed nothing would be a surface where the keys the
		// card owns are being pressed at a page that looks live.
		return hopFadeAll(body, pal)
	}
	out := hopFadeAll(body, pal)
	// CENTRED, AND NUDGED UP BY A THIRD. Dead centre puts the head row below the
	// middle of the frame on a tall window, which reads as low; a third of the
	// way down is where a person's eye already is on a page of prose.
	top := (len(out) - len(card)) / 3
	if top < 1 {
		top = 1
	}
	if top+len(card) > len(out) {
		top = len(out) - len(card)
	}
	a.hop.left, a.hop.right, a.hop.top = (width-room)/2, (width+room)/2, top+a.hop.originY
	a.hop.bottom = a.hop.top + len(card)
	pad := strings.Repeat(" ", (width-room)/2)
	for i, line := range card {
		out[top+i] = pad + line
	}
	return out
}

// hopFadeAll repaints every row of a body at the faintest stop of the depth
// ladder. It is [composerFade] over a whole body, and it goes through that same
// function rather than reaching for [palette.fade] itself, so the two layers on
// this surface can never end up at two different depths.
func hopFadeAll(body []string, pal palette) []string {
	out := make([]string, len(body))
	for i, line := range body {
		out[i] = composerFade(line, pal)
	}
	return out
}

// hopMaybe lays the card over a body that is already a list of lines, and gives
// the lines straight back when the card is down. It is the shape the roster's
// full-width branch needs (view.go) and the shape a place's body needs
// (pages.go), which is why it is one function rather than two `if`s.
func (a *app) hopMaybe(body []string, width int) []string {
	if !a.hopShowing() {
		return body
	}
	return a.hopOver(body, width, a.pal)
}

// hopFadeRail dims the roster's column while the card is up.
//
// IT IS DONE ROW BY ROW WHERE THE COLUMN IS JOINED, because the rail is not part
// of the body: it is a second column drawn beside it, and a card that dimmed the
// conversation and left the roster at full ink would say the roster was still
// live — which is exactly what it is not while the switcher holds the keyboard.
func (a *app) hopFadeRail(line string) string {
	if !a.hopShowing() {
		return line
	}
	return composerFade(line, a.pal)
}

// hopMapWords names the switcher on a place's map — the one line on a place
// whose job is to say what the keys are (pages.go's [app.placeHintSaid] states
// why it is that line and not the resting foot).
const hopMapWords = hopOpenKey + " chats"

// ── how many there are, asked off the loop ──────────────────────────────────

// hopCountMsg is the answer: how many conversations this machine has.
type hopCountMsg struct{ n int }

// countConversations counts them, off the frame.
//
// IT IS A COMMAND AND NOT A METHOD FOR ONE REASON, and it is the same reason
// home's own beat is a command: the walk opens every project's index and every
// session's meta.json ([app.readWorldKnown] says so outright), and over `--host`
// it is a call to another machine. The legend asks whether to name the switcher
// on every single frame, so what it reads has to be a number that is already in
// memory ([app.hopKnown]).
//
// A door that cannot answer leaves the count where it was rather than zeroing
// it: "nobody could be asked just now" is not "there is nothing there".
func (a *app) countConversations() tea.Cmd {
	// THE SEAM'S THREE INPUTS ARE TAKEN HERE, ON THE LOOP, and the reading is
	// taken there, off it (home.go's [worldSeam]). A command that reached back
	// into the app for them would be reading fields the update loop is writing.
	door, root, hosted := a.world, a.placesRoot(), a.hosted()
	return func() tea.Msg {
		seen, known := worldSeam(door, root, hosted)
		if !known {
			return nil
		}
		n := 0
		for _, project := range seen.Projects {
			for _, row := range project.Sessions {
				if !row.Archived {
					n++
				}
			}
		}
		return hopCountMsg{n: n}
	}
}

// hopAway is `ctrl+w`: put the conversation under the cursor away — off the tab
// row of this window, and nowhere else.
//
// NOTHING IS CLOSED AND NOTHING IS INTERRUPTED. The agent goes on running, the
// unsent sentence in its box is kept, the transcript is untouched, and the row
// stays on this very card — which is what makes the gesture safe to repeat and
// safe to undo: `enter` on the same row brings the conversation, its tab and its
// draft straight back (chattabs.go's [app.tabDismiss] holds the whole of the
// law, including where the person lands when the row they dismissed is the one
// on screen).
//
// SO THERE IS NO ARM AND NO WARNING, and their absence is the point. The two
// presses existed because the first one used to end work; a gesture that ends
// nothing is a gesture nobody needs protecting from.
//
// A ROW THAT IS NOT OPEN HERE HAS NO TAB TO PUT AWAY, and says so rather than
// doing nothing: it is below the fold precisely because this terminal is not
// holding it.
func (a *app) hopAway() tea.Cmd {
	if a.hop.at < 0 || a.hop.at >= len(a.hop.rows) {
		return nil
	}
	row := a.hop.rows[a.hop.at]
	// A ROW WITH NO TAB HAS NOTHING FOR THIS KEY TO CLOSE, AND SAYS NOTHING ABOUT
	// IT. That is both halves of the fold: a conversation this terminal never
	// opened, and one whose tab was closed a moment ago and which is drawn below
	// the fold for exactly that reason ([app.hopTabbed]).
	//
	// THE REFUSAL USED TO BE A SENTENCE and the owner took it out: `ctrl+w` on a
	// tab that is already closed is a key doing what the person asked for — there
	// is no tab on the row — and a line explaining that is the surface answering
	// a question nobody asked. The state the key is for is already the state it
	// found. Nothing is said, nothing moves, and the card stays exactly as it is.
	if !a.hopTabbed(row) {
		return nil
	}
	if row.here {
		// THE ONE ON SCREEN GOES THROUGH THE TAB ROW'S OWN DOOR, which either
		// switches this window to another conversation it is holding or takes it
		// home with this one still in front. The card comes down with it, because
		// the screen underneath is about to be a different page.
		a.hopClose()
		return a.tabDismiss(chatTab{key: a.frontTabKey(), file: a.file, where: a.workspace, word: row.title})
	}
	a.tabShutKey(a.convKey(row.file))
	// THE CARD STAYS UP AND RE-READS ITSELF, and the row it just closed LEAVES
	// THE LIST — down behind the fold, or off the card entirely while the fold is
	// shut. That is the whole of what the person asked for by pressing it, and
	// the re-read sees it because [app.hopTabbed] asks [app.tabShut], which the
	// line above has already set. Closing tabs is something a person does two or
	// three of in a row, so the card stays up rather than making tidying up cost
	// three openings.
	rows, tabs, rest := a.hopReading(a.hop.all)
	a.hop.rows, a.hop.rest, a.hop.tabs, a.hop.armed = rows, rest, tabs, -1
	a.hop.at = min(a.hop.at, max(0, len(rows)-1))
	a.hop.say = hopAwayWord + " · " + row.title
	a.touch()
	return nil
}

// hopRunning is how much work is turning in one row's conversation, asked only of
// a conversation this process is holding — the others have no agent here to ask.
func (a *app) hopRunning(row hopRow) int {
	if row.here {
		// THE ONE ON SCREEN IS ASKED THE SAME WAY THE OTHERS ARE — through the
		// agent's own index rather than through the rail, so a conversation's
		// count does not change meaning when it comes forward.
		return runningTasks(a.agent)
	}
	if held := a.behind[a.convKey(row.file)]; held != nil {
		return runningTasks(held.conv.Agent)
	}
	return 0
}

// The two sentences the card says about closing. There used to be a third,
// refusing a row with no tab; it is gone, because a `ctrl+w` that finds no tab
// has already got what it was pressed for ([app.hopAway]).
const (
	hopClosedWord = "closed"
	// hopAwayWord is what the card says after a tab has been put away, and it
	// says what actually happened rather than "closed": the conversation is still
	// running and still on this list, and a word claiming otherwise would be the
	// surface reporting an act it did not perform.
	hopAwayWord    = "tab closed"
	hopLastOneWord = "that is the only conversation open — /quit closes codeaf"
)

// The floating switcher owns the pointer as well as the keyboard. A miss on
// its dimmed backdrop must not activate an invisible task or stop control.
type hopSpot struct{ row, at int }

// hopTarget shares the painted interior between clicks and hover. Borders,
// headings, preview text and refusal messages offer no navigation target.
func (a *app) hopTarget(x, y int) (int, bool) {
	if x <= a.hop.left || x >= a.hop.right-1 {
		return 0, false
	}
	for _, spot := range a.hop.spots {
		if y == a.hop.top+spot.row {
			return spot.at, true
		}
	}
	return 0, false
}

func (a *app) hopPress(x, y int) tea.Cmd {
	// The backdrop dismisses without delivering the press to the chat below.
	if x < a.hop.left || x >= a.hop.right || y < a.hop.top || y >= a.hop.bottom {
		return a.hopBack()
	}
	at, ok := a.hopTarget(x, y)
	if !ok {
		return nil
	}
	a.hop.live = false
	if at < 0 {
		if a.hop.say == "" {
			a.hopSpread(!a.hop.all)
		}
		return nil
	}
	a.hop.at = at
	return a.hopTake()
}
