package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// ── THE HOME PLACE ──────────────────────────────────────────────────────────
//
// THIS FILE IS HOME AS A PLACE (docs/design/home-rethink/ARCHITECTURE.md): the
// switcher it reads, the verbs its rows offer, and the card beside a typed
// search. The reading is switcher.go's and is PURE; the frame
// around it is the router's and knows no place by name; nothing here switches on
// a page id, and the shared router files carry a call into this file rather than
// a home-shaped body.
//
// ── WHERE HOME'S `place` METHODS ALREADY LIVE ───────────────────────────────
//
// The interface itself is the refactor lane's, and these are the functions it
// binds to, written down here so that lane has one list rather than a search:
//
//	id       pageHome
//	open     [app.openHome]        home.go — reads the world, the bands and the
//	                               ledger once, then builds
//	close    [app.closeHome]       home.go — writes the look stamp
//	tick     [app.refreshHome]     home.go — the three-second beat
//	body     [app.homeBody]        home.go, the grid in homegrid.go
//	stops    [homeLine.stop]       home.go
//	enter    [app.homeEnter]       home.go — the doors and their refusals
//	verbs    [app.homeRowVerbs]    HERE
//	alt      —                     the grid is home's one shape
//	window   —                     home has no time window
//	box      [homeView.box]        the one foot box: filter and message at once
//	note     —                     each panel says its own count
//	hint     [app.homeHint]        home.go
//	changed  —                     the per-place look stamps are another lane's
//
// The state struct is [homeView] (home.go), which the refactor lane renames; it
// is not moved here in this wave because four hundred lines of doors, clock and
// typed surface still hold it.
//
// ── HOME IS A SWITCHER, NOT A DIRECTORY ─────────────────────────────────────
//
// Home used to be a tree: every project a heading, its conversations under it,
// three open and the rest folded away under a rule, with two attention strips
// standing over the whole thing. That shape answers "where is my work", and the
// person opening this screen twenty times a day is not asking that. Seconds
// between opens, ten to twenty live chats, three to five projects — so the tree
// bought five rows of scaffolding to reach twenty leaves, and the two strips
// said a second time what the list was already saying once.
//
// SO THE LIST IS ONE FLAT RANKED LIST (SCREEN 1a): what needs you, then what is
// moving, then what is quiet, with the project demoted to a tag on the row and
// the right-hand note carrying the one fact the card was really for. NEEDS-YOU
// AND MOVING ARE THE SORT ORDER NOW, which is why the strips are gone rather
// than moved: a summary standing over a list sorted the same way is the same
// reading twice.
//
// THE READING IS switcher.go's AND THIS FILE OWNS ONLY THE WIRING. [readSwitcher]
// is pure — a world, the standing bands, a look stamp and a clock in, a list of
// lines out — and everything this file does is turn those lines into lines of
// home's own column so that every door, card, digit and key that already worked
// on a conversation or a standing item goes on working on it untouched. A
// switcher row for a conversation IS a [homeSession] row; a switcher row for a
// watch IS a [homeItem] row, painted from the panel cell it wears (homegrid.go).
//
// AND TYPING IS UNTOUCHED. The moment there is something in the box the column
// is [homeView.buildWorld]'s drop-up again, ranked by [homeRank], with `ask
// here` and the action row against the foot — "type to search or start" is the
// promise the foot has always made and this wave does not touch it.

// The row kinds the switcher adds, declared HERE and given values far above the
// iota block in home.go for [homePlace]'s reason: that block is edited by other
// lanes in the same wave, and a constant appended to it would be a conflict over
// a line that says nothing.
const (
	// homeLedger is one line of `since you left` — a watch that fired, work that
	// landed, something memory learned. IT IS A DOOR AND THAT IS THE WHOLE POINT
	// (SCREEN 1a): discoverability on this machine is solved by events rather
	// than by inventories, so you learn the memory place exists on the day it
	// tells you it learned something, and enter on the line goes there.
	homeLedger homeRowKind = 241
	// homeSwitchHead is a line that names rather than opens: a panel's heading
	// and its whisper on the grid (homegrid.go). It is not a cursor stop, for
	// [homeHeading]'s reason.
	homeSwitchHead homeRowKind = 243
)

// switchExchanges is every errand this screen is holding, in the order the
// project blocks used to draw them in: what wants you, then what is moving, then
// what is done, older first inside each.
//
// It is [homeView.exchangeLines] with the project taken out of it, because there
// are no project blocks at rest any more — an errand belongs to the machine's
// one list, not to a heading it happened to be asked under.
func (h *homeView) switchExchanges() []homeLine {
	if len(h.exchanges) == 0 {
		return nil
	}
	mine := append([]*homeExchange(nil), h.exchanges...)
	sort.SliceStable(mine, func(i, j int) bool {
		if a, b := exchangeRank(mine[i]), exchangeRank(mine[j]); a != b {
			return a < b
		}
		return mine[i].began.Before(mine[j].began)
	})
	lines := make([]homeLine, 0, len(mine))
	for _, ex := range mine {
		lines = append(lines, homeLine{kind: homeExchangeRow, dir: ex.bucket, ex: ex})
	}
	return lines
}

// ── what the memory place has to say for itself ─────────────────────────────

// readSwitchLedger takes the two figures the ledger's memory line is made of —
// how many things this machine learned since the look stamp, and how many it let
// go of — once per reading of the world, where every other disk-backed fact on
// this screen is taken.
//
// ON HOME'S OWN BEAT AND NEVER ON A DRAW. There is SQLite behind that seam
// (place_memory.go's [memoryStore]), and the law every home reader is held to is
// that a draw never touches a disk (tui3.go). A window with no store, or a store
// that could not be read, answers nothing — and the ledger then draws no memory
// line at all, which is the emptiness law rather than a gap.
func (a *app) readSwitchLedger() {
	a.home.ledger = switcherLedgerInput{made: a.madeSince(a.home.seen)}
	if a.memory == nil || a.home.seen.IsZero() {
		return
	}
	learned, letGo, err := a.memory.ChangedSince(a.home.seen)
	if err != nil {
		return
	}
	a.home.ledger.learned, a.home.ledger.letGo = learned, letGo
}

// ── the doors ───────────────────────────────────────────────────────────────

// homeLedgerEnter is enter on a `since you left` line: GO TO THE PLACE THAT OWNS
// IT.
//
// A line that says a watch fired and cannot be asked about that watch is a
// notification, and this surface does not have notifications. The word in the
// right margin IS the door, which is why the two are one field.
func (a *app) homeLedgerEnter(line homeLine) tea.Cmd {
	if cmd, took := a.leftEnter(line); took {
		return cmd
	}
	id, ok := parsePageWord(line.project)
	if !ok {
		return nil
	}
	return a.showPage(id)
}

// ── the strip ───────────────────────────────────────────────────────────────

// homeRowVerbs is `→` on a row of home: the verbs the READING itself says this
// row has, wired to the doors home already had for them.
//
// IT IS HOME'S HALF OF THE STRIP AND IT LIVES HERE. verbstrip.go knows the
// mechanism — a letter is a verb only while the strip naming it is drawn — and
// carries one call into this file for the place it is standing in.
//
// THE READING NAMES THEM AND THIS FUNCTION ONLY WIRES THEM. A conversation that
// is not asking anything has no `y`; a row with no address has no `open folder`;
// a paused watch offers `resume it` where a running one offers `pause it`. That
// is [switcherVerbsFor]'s law, and a strip that invented a verb here could offer
// one the row has no way to perform.
func (a *app) homeRowVerbs() []verb {
	line, ok := a.home.previewLine()
	if !ok {
		return nil
	}
	// A task's options address that task, including its own archive mark.
	// The containing conversation and any sibling work keep their state.
	if line.cell != nil && line.cell.row != nil && line.cell.row.task != nil {
		return a.runningVerbs(line)
	}
	// A ROW OF THE GRID CARRIES THE SWITCHER'S OWN ROW ON ITS CELL, so its verbs
	// are the reading's exactly as they were on the list (homegrid.go).
	if line.cell != nil && line.cell.row != nil {
		return a.homeReadingVerbs(line, *line.cell.row)
	}
	// A ROW THE TYPED SURFACE BUILT, WHICH THE READING NEVER SAW. Under a query
	// the column is [homeRank]'s drop-up and a standing item's row is the one
	// thing on it with verbs — the two actions home has been ADVERTISING on such
	// a row without binding (`homeItemActions`, homestanding.go), bound to ctrl+e
	// and ctrl+x, which the line never named, and whose bare `p` and `s` typed.
	if !line.standsForItem() {
		return nil
	}
	return []verb{
		{key: 'p', word: homeItemPauseWord, do: func() tea.Cmd { return a.homeItemWrite(line, standing.StatusPaused) }},
		{key: 's', word: homeItemStopWord, do: func() tea.Cmd { return a.homeItemWrite(line, standing.StatusRetired) }},
	}
}

// homeReadingVerbs is [switcherVerbsFor]'s verbs for one row, less the ones
// this window cannot perform, wired to the doors home already had for them.
func (a *app) homeReadingVerbs(line homeLine, row switcherRow) []verb {
	var verbs []verb
	for _, v := range switcherVerbsFor(row) {
		if a.hosted() && row.kind == switcherConversation && v.answer == "" && (v.key == 'o' || v.key == 'p' || v.key == 'n') {
			continue
		}
		// A VERB THAT CANNOT WORK IS ABSENT, NOT BROKEN. Two of the doors want a
		// folder — a fresh conversation rooted in it, and handing it to the
		// machine's file manager — and a row whose folder is not there any more
		// would offer two keystrokes it has already decided against. It is the
		// place that drops them and not the reading: the reading is pure and has
		// no disk, and this is what the cached stat map is for.
		if row.gone && v.answer == "" && (v.key == 'n' || v.key == 'o') {
			continue
		}
		if v.key == 'x' && row.kind == switcherConversation && (row.session.Archived || line.cell != nil && line.cell.closed) {
			v.word = "reopen"
		}
		verbs = append(verbs, a.homeSwitchVerb(line, row, v))
	}
	return verbs
}

// homeSwitchVerb is one of them, given the door it names.
func (a *app) homeSwitchVerb(line homeLine, row switcherRow, v switcherVerb) verb {
	do := func() tea.Cmd { return nil }
	switch {
	case v.answer != "":
		// 1b's ANSWER IN PLACE: the question's own option words, sent down the
		// road the digits already ride (homeband_answer.go's [app.sendAnswer]),
		// so a question answered from the strip and the same question answered
		// with `1` are one act with one record.
		do = func() tea.Cmd {
			live := a.homeTrue(row.session)
			question, ok := answerable(live, a.now())
			if !ok {
				return nil
			}
			cmd, _ := a.sendAnswer(live, question, v.answer)
			return cmd
		}
	case v.key == 'x':
		do = func() tea.Cmd { return a.homeArchiveRow(row.session) }
	case v.key == 'n':
		do = func() tea.Cmd { return a.homeStartInProject(homeWhere(line)) }
	case v.key == 'o':
		do = func() tea.Cmd { return a.homeOpenFolder(row.session) }
	case v.key == 'p' && row.kind == switcherConversation:
		do = func() tea.Cmd { return a.homeCopyPath(row.session) }
	case v.key == 'p':
		do = func() tea.Cmd { return a.homeItemWrite(line, standing.StatusPaused) }
	case v.key == 'r':
		do = func() tea.Cmd { return a.homeItemWrite(line, standing.StatusActive) }
	}
	return verb{key: v.key, word: v.word, do: do}
}

// homeArchiveRow closes the same tab the row names, keeping its work and draft.
// Reopening returns through the normal conversation door, restoring the tab too.
func (a *app) homeArchiveRow(row session.SessionRow) tea.Cmd {
	if a.homeConversationClosed(row) {
		return a.homeOpenLine(homeLine{kind: homeSession, row: row})
	}
	if err := a.writeHomeArchived(row, true); err != nil {
		a.home.say("could not close conversation", "")
		return nil
	}
	key := a.convKey(row.Transcript)
	if row.Transcript == a.file {
		key = a.frontTabKey()
	}
	a.tabShutKey(key)
	a.home.say(homeClosedWord, "")
	a.refreshHome()
	return nil
}

func (a *app) homeConversationClosed(row session.SessionRow) bool {
	key := a.convKey(row.Transcript)
	if row.Transcript == a.file {
		key = a.frontTabKey()
	}
	return row.Archived || a.tabShut[key]
}

// A new tab without a saved session has no archive record yet.
func (a *app) writeHomeArchived(row session.SessionRow, closed bool) error {
	if row.Dir == "" {
		return nil
	}
	if a.archive != nil {
		return a.archive(row.Dir, closed)
	}
	return session.SetArchived(row.Dir, closed)
}

// homeOpenFolder is `o open folder`, and homeCopyPath is `p copy project` — the
// same two doors ctrl+o and ctrl+y are.
func (a *app) homeOpenFolder(row session.SessionRow) tea.Cmd {
	return a.homeOpenPath(row.Workspace)
}

// homeOpenPath hands one path to this machine's opener and says what came of
// it — a folder for `o`, a file made while you were away (homepanel_left.go).
func (a *app) homeOpenPath(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	if path == "" || processOpener(path) != nil {
		a.home.say("could not open "+path, "")
		return nil
	}
	a.home.say("opened "+path, path)
	return nil
}

func (a *app) homeCopyPath(row session.SessionRow) tea.Cmd {
	path := strings.TrimSpace(row.Workspace)
	if path == "" {
		a.home.say("could not copy path", "")
		return nil
	}
	a.home.say("copied "+path, path)
	return tea.Raw(osc52(path, a.tmux))
}

// homeClosedWord is what closing a conversation says, in one place because
// the key and the strip both say it.
const homeClosedWord = "closed · type its name to find it again"

// ── the place ───────────────────────────────────────────────────────────────

// placeHome is this place's handle on the registry (pages.go's [place] states
// the contract and why the handle holds no state of its own). Home's state is
// [homeView], which is home.go's, because home is the oldest surface here and
// the one every other place borrowed its laws from.
type placeHome struct{ placeBase }

func init() { registerPlace(placeHome{}) }

func (placeHome) id() page      { return pageHome }
func (placeHome) word() string  { return "home" }
func (placeHome) counted() bool { return true }

func (placeHome) open(a *app) tea.Cmd { return a.raiseHome() }
func (placeHome) close(a *app)        { a.dropHome() }

// tick is home's own three-second beat, and home arms and reads it itself
// ([app.refreshHome], home.go's [homeEvery]) — the clock every other place
// borrowed. There is nothing for the router's beat to do here.

// body is home's own column, and the pane map beside it: two facts per row, so
// the hit is a [homeMark] rather than a line number.
func (placeHome) body(a *app, width, room int) []placeRow {
	// AT REST THE BODY IS THE GRID (homegrid.go), and its shape is settled
	// before it is drawn: the column count, the width and the room all decide
	// which rows exist — a whisper wraps at its column's width — so any of them
	// moving is a rebuild, and the lines are made again only when one moved.
	if cols := homeGridCols(width); !a.home.searching() && !a.home.phone &&
		(room != a.home.room || cols != a.home.cols || width != a.home.gridWidth) {
		a.home.room, a.home.cols, a.home.gridWidth = room, cols, width
		a.home.build()
	}
	// AN ERRAND HOLDING THE KEYBOARD STACKS OVER THE GRID, which has no pane
	// column to draw it in ([app.homeStacked]); [app.homeBody] draws that shape.
	if _, stacked := a.homeStacked(); a.home.gridOn() && !stacked {
		return a.homeGridRows(width, room, a.pal)
	}
	left, right := homeColumns(width)
	// THE HEIGHT REACHES THE READING HERE AND NOWHERE ELSE. It is the same law
	// the width is settled under one layer up (home.go's [app.homeFrame]: the
	// shape of the column is decided before the column is drawn), and the same
	// shape — one number, compared, and the lines made again only when it moved,
	// because a rebuild on every frame would re-rank the world sixty times a
	// second for a list that had not changed.
	if room != a.home.room {
		a.home.room = room
		a.home.rebuild()
	}
	a.homeWindow(room)
	body := a.homeBody(left, right, room, a.pal)
	rows := make([]placeRow, 0, len(body))
	for _, drawn := range body {
		rows = append(rows, placeRow{
			text: drawn.text,
			hit:  homeMark{line: drawn.hit, pane: drawn.pane},
		})
	}
	return rows
}

// ownFrame is home's own, for two reasons and not one. Below sixty columns this
// screen is an inbox and a sheet rather than a list (homephone.go), and at every
// width home unpacks a SECOND hit map — the pane each row shares with an errand
// drawn beside it — which has to be taken off the frame AFTER the clamp that
// cuts rows, so that both maps are cut the same way.
func (placeHome) ownFrame(a *app, width, height int) ([]string, []placeHit, int, int, bool) {
	lines, hits, caretX, caretY := a.homeFrame(width, height)
	marks := make([]placeHit, len(hits))
	for i, at := range hits {
		marks[i] = at
	}
	return lines, marks, caretX, caretY, true
}

// stops is every line of home's column the cursor may rest on: the walk
// [homeView.move] takes, said as a list ([homeLine.stop] is the one rule).
func (placeHome) stops(a *app) []int {
	// ON THE GRID THE STOPS ARE THE CURSOR'S OWN COLUMN, which is what makes `↑`
	// off the top of ANY column reach the tab bar (pages.go's [app.barReach]
	// asks whether the cursor is on the first of these).
	if a.home.gridOn() {
		return a.home.columnStops(a.home.columnOf(a.home.cursor))
	}
	out := make([]int, 0, len(a.home.lines))
	for i, line := range a.home.lines {
		if line.stop() {
			out = append(out, i)
		}
	}
	return out
}

// cursorRow is which drawn row home's cursor landed on, found through the hit
// map the body just wrote. Home's rows carry two facts each — the list line and
// the errand pane sharing it — so the line is unpacked from [homeMark] rather
// than read as a bare index (pages.go's [place.cursorRow] says why the rows are
// handed in).
func (placeHome) cursorRow(a *app, rows []placeRow) int {
	// A GRID ROW HOLDS A LINE OF EVERY COLUMN, and the strip goes under the LAST
	// screen row of the cursor's line, so a row with a line under it keeps the
	// two together.
	if a.home.gridOn() {
		found := -1
		for i, row := range rows {
			col := a.home.columnOf(a.home.cursor)
			if mark, ok := row.hit.(homeMark); ok && mark.grid && col >= 0 && mark.cells[col] == a.home.cursor {
				found = i
			}
		}
		return found
	}
	for i, row := range rows {
		if mark, ok := row.hit.(homeMark); ok && mark.line == a.home.cursor {
			return i
		}
	}
	return -1
}

// cursorAt is the line of home's column the cursor is on (pages.go's
// [place.cursorAt]). It is the same number [homeView.move] walks and the same
// number [placeHome.stops] answers in.
func (placeHome) cursorAt(a *app) int { return a.home.cursor }

func (placeHome) enter(a *app) tea.Cmd { return a.homeEnter() }

func (placeHome) verbs(a *app) []verb { return a.homeRowVerbs() }

// rowID names the row home's cursor is standing on (pages.go's [place.rowID]).
//
// IT IS THE ROW'S OWN IDENTITY AND NEVER ITS POSITION. Home rebuilds its lines
// on every three-second beat and re-ranks them under every letter typed into
// the query, so the conversation at line nine is a different conversation a
// moment later — which is the whole reason [homeView.pointAt] finds a row by
// what it IS rather than by where it was. The name is the kind and whichever
// handle that kind of row carries, so two rows of one list cannot answer alike.
func (placeHome) rowID(a *app) string {
	line, ok := a.home.previewLine()
	if !ok {
		return ""
	}
	parts := []string{strconv.Itoa(int(line.kind)), line.project, line.dir, line.row.Transcript, line.item.ID}
	if line.ex != nil {
		parts = append(parts, line.ex.id)
	}
	if line.task != nil {
		parts = append(parts, line.task.SessionID, line.task.ID)
	}
	return strings.Join(parts, "\x00")
}

// box is home's own one foot box — new message AND live query at once, no mode —
// or a focused errand's line, because THE FOOT BELONGS TO WHOEVER HOLDS THE
// KEYBOARD (homeexchange.go).
func (placeHome) box(a *app) *editor {
	if ex := a.paneExchange(); ex != nil && ex.focused {
		return &ex.box
	}
	return &a.home.box
}

// hint is HOME'S WHOLE LINE, the router's own keys included. At rest that line is
// the design's sentence word for word and names four keys exactly (SCREEN 1a,
// home.go's [app.homeHint]); the router's tail appended here would make it five.
func (placeHome) about() string { return "what wants you and what is running" }

func (placeHome) hint(a *app) string { return a.homeHint() }

// changed is ZERO AND THAT IS THE DESIGN. Home is where the "since you left"
// ledger is DRAWN, in sentences that say what happened and open the place it
// happened in — so a digit on its tab would be the same news said twice, once
// uselessly.
func (placeHome) changed(a *app, since time.Time) int { return 0 }

// press, hover and wheel are home's own, because home resolves the pointer
// against two maps and a column boundary rather than against a body line
// (homemouse.go). The router hands the gesture straight over.
func (placeHome) press(a *app, y int) (tea.Cmd, bool)     { return nil, false }
func (placeHome) hover(a *app, y int) bool                { return false }
func (placeHome) wheel(a *app, delta int) (tea.Cmd, bool) { return nil, false }

// key is home's whole grammar, which is the oldest on this surface and the one
// every other place borrowed from (home.go's [app.homeKey]). The router is read
// before it, exactly as it is before every other place's.
// key is the place's own keyboard, and EVERY key on it is an arrival.
//
// The card follows the cursor and the cursor is moved by more than the arrows —
// a filter typed into the box rebuilds the list under it, `alt+g` regroups it,
// a fold opens — so the readings the card needs are asked for after whatever the
// key did rather than beside the four keys that most obviously move it
// (homecardread.go). Asking costs a map lookup on every key that changed
// nothing, which is what it costs to never be stale.
func (placeHome) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	answered := a.homeKey(msg)
	return tea.Batch(answered, a.refreshHomeCard(a.now()))
}

// owns is the two layers of home that take the WHOLE keyboard, `tab` included,
// and it is read before the router claims a single chord (pages.go's
// [place.owns] holds the argument).
//
// The phone tier's sheet over the inbox is the first (homesheet.go). The second
// is a FOCUSED ERRAND: while it holds the keyboard, `tab` hands it back to the
// list; `esc` also returns there while preserving a half-typed follow-up. This
// is the zone law homeexchange.go states in full — a `tab` the router took first
// would walk the person out of home mid-sentence.
//
// THE KEYBOARD IS SETTLED BEFORE THE KEY IS READ. An exchange holds it only
// while the cursor is on that exchange's row, so walking away can never leave
// the arrows moving a pane nobody is looking at ([app.settleExchangeFocus]).
func (placeHome) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, took := a.homeSheetKeyFirst(msg); took {
		return cmd, true
	}
	// THE TARGET IS READ NEXT, before the router swallows unclaimed chords.
	// Home owns the draft's project, thinking and approval controls, and the
	// model list opened by `/model` owns the keyboard while it is up.
	//
	// IT LOSES TO THE PHONE SHEET AND WINS OVER EVERYTHING ELSE. The sheet is a
	// full-frame card a thumb is in the middle of, and the phone's rule names no
	// chord at all — there is no `alt` on a phone to press.
	if cmd, took := a.placeTargetKey(msg); took {
		return cmd, true
	}
	a.settleExchangeFocus()
	ex := a.paneExchange()
	if ex == nil || !ex.focused {
		// THE ARROWS ARE THE ROUTER'S: `→` opens the row's verbs and `←` closes
		// them, on every column (homegrid.go, "the arrows stay in their column").
		return nil, false
	}
	a.home.say("", "")
	cmd := a.exchangeKey(ex, msg)
	// AND THE SWEEP RUNS AFTER THE KEY, for [app.homeKey]'s reason: what a key
	// does is move the cursor, and "have they moved off it" is a question only
	// answerable once they have.
	a.sweepExchanges()
	a.touch()
	return cmd, true
}
