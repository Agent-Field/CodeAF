package tui3

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
// ── THE CARD HAS NO BORDER, AND THE DEPTH IS THE WHOLE OF WHAT SAYS `LAYER` ──
//
// This surface draws no boxes (home.go's preview card states the same law), so
// the card is not framed: the body behind it is repainted at the FAINTEST stop
// of the depth ladder ([composerFade], depthfade.go) and the card's own rows are
// left at full ink. The contrast between the two is the layer. That is the same
// move SCREEN 2e's composer layer makes over a place, one mechanism rather than
// two, and it is why nothing underneath has a word to say about being under one.
//
// The rows themselves are home's rows — the same glyph door, the same bold
// subject, the same dim tail dropped in the same order (switcher.go's
// [switcherLine]) — because a person who has read home once should not have to
// learn a second list.
//
// ── QUICK SWITCHING — the press is the switch ───────────────────────────────
//
// By default ([config.DefaultQuickSwitch]) the chord does not open a menu: it
// SWITCHES, on the spot, the way a browser's ctrl+tab does — and the card is a
// receipt over the conversation just landed in, fading on its own after
// [hopSettle]. Pressing again keeps going round the ring; `esc` takes the whole
// burst back; touching any other key converts the receipt into the browsing
// card, which holds still and waits for `enter`, because that person has stopped
// switching and started reading. The setting turns the chord back into a menu.
//
// IT COMMITS ON THE PRESS AND NEVER ON A RELEASE, and that is a law rather than
// a shortcut. Windows commits alt+tab when the modifier comes up, but a key
// RELEASE only exists on terminals speaking the kitty keyboard protocol with
// flags this surface deliberately does not request (app.go's enhancements arm
// states the ruling) — a gesture built on one would work at the desk and die
// inside tmux. Chrome commits ctrl+tab eagerly on every press and nobody can
// feel the difference, because there is no difference to feel: by the time a
// release could have been heard, you are already there.

// hopOpenKey is the key that opens the switcher, and it is `ctrl+k` for four
// reasons stated in the order they were weighed:
//
//  1. IT ARRIVES IN EVERY TERMINAL. `ctrl+k` has a legacy encoding (0x0B), so it
//     needs no protocol negotiation, no kitty keyboard flag and no cooperation
//     from a multiplexer in between. That is the first question asked of any
//     chord on this surface, because a capability that cannot work is absent
//     rather than broken (bargein.go states the law).
//  2. NOTHING TAKES IT FIRST. No window manager claims it, and no common
//     emulator binds it — unlike `ctrl+tab`, which WezTerm and Windows Terminal
//     both spend on their own tabs by default, and unlike `alt+tab`, which the
//     window manager takes on Windows and on most Linux desktops.
//  3. IT ALREADY MEANS THIS. `ctrl+k` / `cmd+k` is "jump to a conversation" in
//     Slack, and the switcher in VS Code, Linear and Notion. A person who has
//     used any of them has already learned this key.
//  4. IT IS FREE HERE. The one other place this surface binds it is the harness
//     design's approval row (roomapproval.go), which is a modal that owns the
//     whole keyboard while it is up and is read before this claim — so the two
//     never contend for one keystroke on one screen, exactly as `ctrl+.` means
//     two things on two screens (chords.go's [chordMapAlias]).
const hopOpenKey = "ctrl+k"

// hopBackKey is the same gesture the other way. It is `ctrl+shift+k` and it is
// bound ONLY where the terminal can spell it, for `ctrl+tab`'s reason exactly: an
// ordinary terminal sends `ctrl+shift+k` and `ctrl+k` as the same byte, so
// nothing can tell them apart until the kitty keyboard protocol's disambiguation
// flag has been taken ([app.ctrlDigits]).
//
// THE ALWAYS-AVAILABLE REVERSE IS `shift+tab`, which every terminal sends as
// CSI Z and which the card takes while it is up. That is why this chord being
// absent on half the terminals in the world costs nothing: the gesture has a
// spelling that always works, and this is the one people's hands reach for.
const hopBackKey = chordCtrlWord + "shift+k"

// hopFoldKey and hopShutKey open and shut the fold at the foot of the card —
// `→` and `←`, the two keys this surface already folds with everywhere
// (task.go's roster, place_tasks.go's families).
const (
	hopFoldKey = "right"
	hopShutKey = "left"
)

// hopAwayKey closes a conversation from the card. It is `ctrl+w`, which is the
// key the grooming drew and the key every browser and editor closes a tab with.
//
// IT MEANS SOMETHING ELSE IN THE MESSAGE BOX — delete the word behind the caret
// (input.go) — and that is not a collision: the card has taken the whole
// keyboard while it is up, and there is no caret on it to delete a word behind.
// The manual's page about chords that mean more than one thing carries the pair.
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

// hopShown is how many rows the card holds. TWELVE, because the card is a card:
// eight is the cap on what can be open at once ([convCap]) and four more is a
// glance at what else is on the machine — a person who wants the whole list
// wants home, which is a page and has the room to be one.
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
	// at is the cursor, an index into rows. It opens on ZERO, which is the most
	// recently open conversation behind this one — the same place `tab` goes —
	// so the commonest journey is `ctrl+k enter` and the second commonest is one
	// more `ctrl+k` before the `enter`.
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
	// setting ([switcherView.all] holds the same law for home's own fold): a
	// line that says rows are being hidden and cannot be asked to stop hiding
	// them is a dead end somebody hits and gives up at.
	all bool
	// rest is how many conversations the fold is standing for, and it is zero
	// once the fold is open, because nothing is behind it any more.
	rest int
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

// hopOpen builds the reading and raises the card.
func (a *app) hopOpen() {
	rows, rest := a.hopReading(false)
	if len(rows) < 2 && rest == 0 {
		// Nowhere to go. The guard above has already refused this, and this is
		// the same refusal said where the rows are actually counted.
		return
	}
	a.hop = hopCard{open: true, rows: rows, rest: rest, total: len(rows) + rest, at: hopFirstStop(rows), armed: -1, from: a.file}
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
	at := a.hop.at
	rows, rest := a.hopReading(all)
	a.hop.rows, a.hop.rest, a.hop.all, a.hop.armed, a.hop.say = rows, rest, all, -1, ""
	a.hop.at = min(at, max(0, len(rows)-1))
	// AND THE CURSOR LEAVES `you are here` THE MOMENT THERE IS SOMEWHERE ELSE TO
	// BE. Opening the fold on a session holding one conversation is a person
	// asking for the others; leaving the cursor on the row they are already in
	// would make `enter` do nothing at the end of that gesture.
	if a.hop.at < len(rows) && rows[a.hop.at].here {
		a.hop.at = hopFirstStop(rows)
	}
	a.touch()
}

// hopFirstStop is where the cursor opens: the first row that is not the one you
// are already standing in.
//
// IT IS NOT SIMPLY ZERO. With conversations in the keeper, row zero is the one
// `tab` would go to and the cursor belongs there; on a fresh session the only
// open conversation IS the front one, so zero would open the card with the
// cursor on `you are here` and make `enter` do nothing.
func hopFirstStop(rows []hopRow) int {
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
	return tea.Tick(hopSettle, func(time.Time) tea.Msg { return hopSettleMsg{pulse: pulse} })
}

// hopReading is the card's whole reading: the conversations in the keeper,
// most recently in front first, and then the one on screen.
//
// IT WALKS THE PREVIOUS-STACK AND NEVER THE MAP. Go's map order is random, and a
// switcher whose rows moved between two presses of the same key would be
// unusable; [app.prev] is the order the keeper already keeps and the order `tab`
// already walks (keeper.go's [app.rememberOpen]), so the card and the key agree
// about what "the last one" means by construction.
func (a *app) hopReading(all bool) ([]hopRow, int) {
	now := a.now()
	rows := make([]hopRow, 0, hopShown)
	for at := len(a.prev) - 1; at >= 0; at-- {
		held := a.behind[a.prev[at]]
		if held == nil {
			// A key the keeper no longer has. Stepped over rather than cleaned
			// up, exactly as [app.lastBehind] steps over it.
			continue
		}
		rows = append(rows, a.hopKept(held, now))
	}
	rows = append(rows, a.hopFront(now))
	rest := a.hopRest(rows, now)
	if !all {
		// THE COUNT IS STILL TAKEN. The fold has to say what is behind it, and a
		// door that could not name what it holds is a door nobody opens.
		return rows, len(rest)
	}
	return append(rows, rest...), 0
}

// hopRest is every OTHER conversation on this machine, ranked the way home ranks
// them, and it is the half of the card that made the key worth binding.
//
// THE CARD USED TO HOLD ONLY WHAT WAS ALREADY OPEN, AND THAT WAS THE DEFECT. A
// person who has just started aforge holds exactly one conversation, so the key
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
		rest = append(rest, hopRow{
			file: row.session.Transcript, title: row.title, project: row.project,
			note: hopRestNote(row), age: row.age, needs: row.needs, moving: row.moving,
			where: row.place, held: row.held, gone: !homeFolderThere(row.place),
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
	row := hopRow{
		file:    held.conv.SessionFile,
		title:   hopTitle(agent, held.side),
		project: hopProject(held.conv.Place, held.conv.Workspace),
		needs:   needsPerson(agent),
		moving:  running > 0,
		open:    true,
	}
	if held.side != nil {
		row.age = sinceAt(held.side.since, now)
	}
	row.note = hopNote(row.needs, running, held.watch.landedSince())
	return row
}

// hopFront is the conversation on screen. It is a row like any other — same
// title, same project, same age — because a ring with a hole in it is a ring a
// person has to count their way around.
func (a *app) hopFront(now time.Time) hopRow {
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
	title := ""
	if agent != nil {
		title = strings.TrimSpace(agent.Title())
	}
	if title == "" && side != nil {
		title = strings.TrimSpace(side.title)
	}
	if title == "" {
		return hopNewWord
	}
	return readableName(title)
}

// hopNewWord is a conversation nothing has been said in yet. It is the phrase
// the entry line already uses for the same fact (app.go's `new conversation ·
// <place>`), because one thing has one name.
const hopNewWord = "new conversation"

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
// IT NEVER GUESSES AT A QUESTION'S WORDS. Home can say "asks: …" because it
// reads the presence file the conversation wrote, which carries the question's
// text; this card is asking a live agent pointer a yes-or-no question and has
// nothing but the yes. Naming the tool it wants would mean a door onto the
// engine that does not exist, and inventing a sentence for it would be worse
// than the short true one.
func hopNote(needs bool, running, landed int) string {
	switch {
	case needs:
		return hopAskingWord
	case running > 0:
		return itoa(running) + " " + plural("task", running) + " running"
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
		if at := a.hop.at; a.hopQuick && at < len(a.hop.rows) && a.hop.rows[at].open && !a.hop.rows[at].here {
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
	// A DIGIT TAKES ITS ROW OUTRIGHT. Eight is the cap (keeper.go's [convCap]),
	// so every row this card can hold has a digit, and the digit is drawn on it.
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
// not the one you are standing in — the open conversation longest unlooked-at.
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
	// card opened here — asked of the agent NOW, while it is still on this side
	// of the attach and there is still an agent on the loop to ask.
	wasNote := hopNote(needsPerson(a.agent), runningTasks(a.agent), 0)
	cmd, ok := a.bringForward(row.file)
	if !ok {
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

// hopBacks is the same question for the reverse: `ctrl+shift+k` and `ctrl+tab`'s
// own reverse, both of them only where the terminal can spell them.
func (a *app) hopBacks(key string) bool {
	return (key == hopBackKey || key == hopBackAlias) && a.ctrlDigits()
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
// THE ROW YOU ARE ALREADY ON IS NOT A SWITCH. Committing `you are here` closes
// the card and does nothing else — [app.bringForward] answers the same way for
// the same reason, and doing it here as well means the card never depends on
// that agreement holding.
func (a *app) hopTake() tea.Cmd {
	if a.hop.at < 0 || a.hop.at >= len(a.hop.rows) {
		a.hopClose()
		return nil
	}
	row := a.hop.rows[a.hop.at]
	a.hopClose()
	if row.here {
		return nil
	}
	if !row.open {
		return a.hopStart(row)
	}
	cmd, ok := a.bringForward(row.file)
	if !ok {
		// The conversation went away between the card opening and this key —
		// another window took it over (takeover.go), or it was closed. The card
		// is already down; saying so is better than a keystroke that did nothing.
		a.note(hopGoneWord)
		return nil
	}
	return cmd
}

// hopGoneWord is what the switcher says about a row that stopped existing while
// it was on screen. It names no path: the person pressed a row, and the row is
// what they are being told about.
const hopGoneWord = "that conversation is no longer open"

// hopStart opens a conversation this process was NOT holding, and it makes
// exactly the three checks home's `enter` makes, in the same order (home.go's
// [app.homeOpenDoor]): is the folder still there, is there room for another, and
// does the door itself refuse.
//
// IT SAYS THE REFUSAL WHERE THE PERSON IS. The card is already down by the time
// this runs, so the sentence goes on the entry line of the conversation they are
// standing in — which is where every other refusal made on a keystroke is said.
func (a *app) hopStart(row hopRow) tea.Cmd {
	if !homeFolderThere(row.where) {
		a.note(WorkspaceGoneWord + " · " + row.where)
		return nil
	}
	if word, room := a.roomForAnother(); !room {
		a.note(word)
		return nil
	}
	cmd, refusal := a.openBeside(row.where, row.file)
	if refusal != "" {
		a.note(refusal)
		return nil
	}
	return cmd
}

// hopOpenRows is how many of the card's rows this process is already holding: the
// count the head row says, and the seam the rule is drawn on.
func (a *app) hopOpenRows() int {
	n := 0
	for _, row := range a.hop.rows {
		if row.open {
			n++
		}
	}
	return n
}

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
	hopPad = 2
	// The fixed columns, in cells.
	hopGlyphCol   = 2
	hopSubjectCol = 34
	hopProjectCol = 12
	hopAgeCol     = 5
	// hopTightSubject is what the subject column narrows to before the tail
	// columns start being dropped: a name cut to twenty cells is still a name,
	// and one cut to eight is a shrug.
	hopTightSubject = 20
)

// hopBox is the six pieces of the border, and the ascii floor under them. The
// flag is the palette's own ([palette.ascii]), whose one job in this codebase is
// exactly this question.
type hopBox struct{ tl, tr, bl, br, h, v string }

func hopBoxOf(pal palette) hopBox {
	if pal.ascii {
		return hopBox{tl: "+", tr: "+", bl: "+", br: "+", h: "-", v: "|"}
	}
	return hopBox{tl: "╭", tr: "╮", bl: "╰", br: "╯", h: "─", v: "│"}
}

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
	box := hopBoxOf(pal)
	if !a.hopShowing() || width < 12 || height < 5 {
		return nil
	}
	inner := width - 2
	room := inner - 2*hopPad
	pad := strings.Repeat(" ", hopPad)
	// THE BORDER IS DIM — the tier this surface says its own furniture in (a
	// rule, a seam, a connector). An accent border would be the box announcing
	// itself, and what has to be read here is the list inside it.
	edge := func(left, fill, right string) string {
		return pal.dim(left + strings.Repeat(fill, inner) + right)
	}
	// The rows a person reads are laid out at `room` and then set inside the
	// borders whole, so the band on the cursor's row covers the padding too —
	// which is what makes it read as a row of the card rather than a highlight
	// floating inside one.
	inside := func(line string, band bool) string {
		line = fit(line, room)
		if w := room - ansi.StringWidth(line); w > 0 {
			line += strings.Repeat(" ", w)
		}
		body := pad + line + pad
		if band {
			body = pal.cursor(body, inner)
		}
		return pal.dim(box.v) + body + pal.dim(box.v)
	}

	lines := []string{edge(box.tl, box.h, box.tr), inside(a.hopHead(room, pal), false), inside("", false)}
	// The frame's own rows — two borders and whatever foot the card owes — are
	// taken off the top before a single conversation is drawn, so the last thing
	// dropped is never the way out.
	foot := 1
	if a.hop.rest > 0 || a.hop.say != "" {
		foot++
	}
	for at, row := range a.hop.rows {
		if len(lines)+foot >= height {
			break
		}
		lines = append(lines, inside(hopLine(row, at, at == a.hop.at, room, pal), at == a.hop.at))
	}
	if word := a.hopFoot(); word != "" && len(lines)+1 < height {
		lines = append(lines, inside(pal.dim(fit(word, room)), false))
	}
	return append(lines, edge(box.bl, box.h, box.br))
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
		return tokens.GlyphExpanded + " " + hopShutKeyWord
	case a.hop.rest > 0:
		return tokens.GlyphCollapsed + " " + itoa(a.hop.rest) + " more on this machine · " + hopFoldKeyWord
	}
	return ""
}

// The two halves of the fold's own sentence, spelled once and quoted in the
// manual exactly as they are here.
const (
	hopFoldKeyWord = "→ reach them"
	hopShutKeyWord = "← just the open ones"
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

// hopClauses are the keys the card owns, in the order a person meets them.
var hopClauses = []string{"tab down", "shift+tab up", "enter go", hopAwayKey + " put away", "esc back"}

// hopFootWords is the foot of the FRAME while the card is up — the same clauses
// from the same list, so a person reading the bottom of the screen and a person
// reading the top of the card are told the same things.
var hopFootWords = strings.Join(hopClauses, " · ")

// hopLine is one conversation, in five fixed columns.
func hopLine(row hopRow, at int, sel bool, width int, pal palette) string {
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
	// THE SUBJECT KEEPS ITS COLUMN AND THE TAIL GIVES WAY. Narrowing the subject
	// first would break the straight edge the whole layout is for, so the
	// clauses go before it does: the clock last, because it is two cells and is
	// the one thing every row has.
	subject, project, age := hopSubjectCol, hopProjectCol, hopAgeCol
	fixed := func() int { return len(mark) + hopGlyphCol + subject + project + age }
	for fixed()+2 > width {
		switch {
		case project > 0:
			project = 0
		case subject > hopTightSubject:
			subject = max(hopTightSubject, width-len(mark)-hopGlyphCol-age-2)
		case age > 0:
			age = 0
		default:
			subject = max(4, width-len(mark)-hopGlyphCol)
		}
		if project == 0 && age == 0 && subject <= hopTightSubject {
			break
		}
	}
	note := max(0, width-fixed())
	// A ROW WITH NEWS IS AT FULL INK AND A QUIET ONE IS A STEP BACK, which is
	// what lets the two or three that want you separate from the eight that do
	// not with no heading saying so (SCREEN 2b's own clause).
	name := pal.narr(fitPad(row.title, subject))
	clause := pal.dim(fitPad(row.note, note))
	if row.needs || row.moving {
		name, clause = pal.ink(fitPad(row.title, subject)), pal.narr(fitPad(row.note, note))
	}
	if sel {
		// THE ROW THE KEYBOARD IS ON TAKES THE GROUND AND THE WEIGHT. The band is
		// applied around this line by the card; the subject going bold is the
		// other half, and the tail steps up with it because dim grey on a raised
		// ground is grey on grey (switcher.go holds the same rule for home).
		name, clause = pal.bold(pal.ink(fitPad(row.title, subject))), pal.narr(fitPad(row.note, note))
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
const hopMapWords = hopOpenKey + " switch conversation"

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

// hopAway is `ctrl+w`: close the conversation under the cursor, in this terminal.
//
// CLOSING IS NOT SWITCHING AND THE CARD SAYS SO PLAINLY. The grooming called
// this "put away without stopping it", and that sentence is not true of this
// engine: closing a conversation closes its agent, and work it has running stops
// with it — which is exactly what the quit door already warns about
// (quitarm.go). So a conversation with nothing running closes on one press, and
// one with work in it takes two, with the work named in between.
//
// A ROW THAT IS NOT OPEN HAS NOTHING TO CLOSE, and says so rather than doing
// nothing: it is below the fold precisely because this terminal is not holding
// it.
func (a *app) hopAway() tea.Cmd {
	if a.hop.at < 0 || a.hop.at >= len(a.hop.rows) {
		return nil
	}
	row := a.hop.rows[a.hop.at]
	if !row.open {
		a.hop.say = hopNotOpenWord
		a.touch()
		return nil
	}
	if running := a.hopRunning(row); running > 0 && a.hop.armed != a.hop.at {
		// THE ARM, WITH THE WORK NAMED. One keystroke that ends an hour of work
		// is the shape stop.go's confirmation card exists to refuse.
		a.hop.armed = a.hop.at
		a.hop.say = itoa(running) + " " + plural("task", running) + " running · " + hopAwayKey + " again to close it anyway"
		a.touch()
		return nil
	}
	if row.here {
		// THE ONE ON SCREEN IS THE KEEPER'S OWN DOOR, and it brings the next
		// conversation forward as it goes (keeper.go's [app.closeFront]). The
		// card comes down with it, because the screen underneath is about to be
		// a different conversation.
		a.hopClose()
		cmd, ok := a.closeFront()
		if !ok {
			a.note(hopLastOneWord)
		}
		return cmd
	}
	a.closeKept(row.file)
	// THE CARD STAYS UP AND RE-READS ITSELF. Closing is something a person does
	// two or three of in a row, and a card that dropped after each one would make
	// tidying up cost three openings.
	rows, rest := a.hopReading(a.hop.all)
	a.hop.rows, a.hop.rest, a.hop.armed = rows, rest, -1
	a.hop.at = min(a.hop.at, max(0, len(rows)-1))
	a.hop.say = hopClosedWord + " · " + row.title
	if len(rows) < 2 && rest == 0 {
		a.hopClose()
	}
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

// The three sentences the card says about closing.
const (
	hopNotOpenWord = "that one is not open here — enter opens it"
	hopClosedWord  = "closed"
	hopLastOneWord = "that is the only conversation open — /quit closes aforge"
)
