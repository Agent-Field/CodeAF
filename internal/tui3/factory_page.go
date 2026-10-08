package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FACTORY PAGE ────────────────────────────────────────────────────────
//
// The factory floor: every item a chat split off or a repository sent, grouped
// by where it stands, as rows under the handover, and on a wide terminal the
// item under the cursor in a peek at the right. place_factory.go is the handle the registry files; this
// file is the page's state, its one read and its drawing.
//
// THE PAGE READS ONE SEAM AND ONLY OFF THE LOOP. [factory.Seam.Load] may touch
// disk, the network and the clock, so it is asked beside the door line
// ([app.besideLine]) on the opening and on the place's three-second beat, and
// what it hands back is a [factory.Snapshot] of plain values folded in on the
// loop. THE BODY READS THAT SNAPSHOT AND NOTHING ELSE (the framedisk law).
//
// A SEAM WITH NO LOAD IS A FLOOR THAT IS NOT CONNECTED, and the page says so
// in one dim line rather than drawing an empty rail. That is every launch until
// an engine stands behind the page, and every launch over --host.

// ── THE FLOOR IS ONE OBJECT IN THREE GEOMETRIES ─────────────────────────────
//
// At [factoryPaneFloor] columns and wider the floor is two columns: the rows at
// the left, [factoryRowsShare] percent of the width until the person moves the
// divider (factory_split.go), never under [factoryRowsMin] and never leaving
// the peek under [factoryPeekMin], a dim rule, and the peek at the right. From
// [factoryFactsFloor] up to that it is the rows alone, at the full width, with
// every column a row carries. Under [factoryFactsFloor] a row is its lead, its
// ref and its title and nothing else. THE HANDOVER SPANS THE WHOLE WIDTH ABOVE
// ALL OF IT ([app.factoryHead]) wherever the rows carry their columns, so it is
// one strip over the floor rather than the head of one column.
//
// The item page (factory_item.go) is how a person sees an item whole at any
// width; the peek is a glance, drawn only where there is room for one beside
// rows that still carry their facts.

// factoryHeadLeaves is the fewest rows the floor keeps for its own rows under
// the handover. A frame shorter than the handover and these draws no handover,
// because a floor whose strip ate the rows answers "what is happening" and
// hides "what is it happening to".
const factoryHeadLeaves = 6

// factoryUnconnectedWords is the page with no seam behind it. It names what
// arrives here, never that the page is empty (the emptiness law's panel rule).
const factoryUnconnectedWords = "nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected"

// factoryBareWords is a floor that is connected, read and holds nothing yet:
// one dim line under the handover naming what arrives there, never a sentence
// saying it is empty (the emptiness law's panel rule). `n` is the page's own
// key for new work, and the foot rows it opens still stand under this line.
const factoryBareWords = "work arrives here from chat, from n, and from the repositories you connect"

// factoryPage is the page's own state, held on the app as `fp`.
//
// THE NEXT PIECES OF THIS PAGE BUILD ON THESE FIELDS AND NO OTHERS: snap is the
// only reading a draw may use, cursor names a position in the rail's WALK
// ORDER ([app.factoryRows]'s item rows, top to bottom) rather than an index
// into snap.Items, and rowsW is what the last body measured, so a press can be
// told rows from peek without drawing again.
type factoryPage struct {
	// snap is the last floor the seam handed back, and loaded whether one has
	// arrived at all: a page that has not heard yet draws nothing rather than
	// an empty floor that might be false.
	snap   factory.Snapshot
	loaded bool
	// reading is true while a Load is out, so a beat that lands during a slow
	// read does not stack a second one behind it.
	reading bool
	// err is what the last Load said when it failed; the snapshot before it is
	// kept and drawn, and the note line says the read failed.
	err error
	// cursor is the item the keyboard is on, as a position in the walk order.
	cursor int
	// top is the first rail line drawn, so the window follows the cursor, and
	// shown is how many rail lines the last body drew from it: the pair a press
	// is resolved against ([placeBodyLine]).
	top   int
	shown int
	// pinned is how many of the rail's first lines (the repo line and the
	// words line) the last body drew above the window, fixed, because the
	// rows did not fit; 0 when the rows fit whole ([app.factoryRail]).
	pinned int
	// headRows is how many rows the handover took above the rows at the last
	// body, so a press is counted from the first row of the floor itself.
	headRows int
	// rowsW is the rows' columns at the last body, the rule beside the peek
	// not included, and columns whether those rows carried the repo, the
	// facts and the age ([factoryFactsFloor]); refW is the ref column the
	// last draw measured, so every row's title starts in the same cell.
	rowsW   int
	columns bool
	refW    int

	// THE RAIL'S NARROWINGS (factory_rail.go), each additive to the fields
	// above. repo is 0 for every repo and i for snap.Repos[i-1]; query is what
	// was typed into the `/` box and typing whether that box is open; backlog
	// is `A`, the whole backlog under `new` rather than what is fresh; marked
	// is the person's own marks on new items, by id, which a re-read keeps.
	repo    int
	query   string
	typing  bool
	backlog bool
	marked  map[int]bool
	// order is `O`, how rows sit within each section (factory_order.go).
	order factoryOrder

	// THE LAYOUT'S OWN STATE (factory_item.go), additive like the narrowings.
	// comfy is `z`, a second line under each row and air between rows; open is
	// the item page standing over the floor, stage the stage its rail's cursor
	// is on, and said whether `enter` on a stage with no conversation has put
	// why on the pane's action line (factory_run.go's [factoryNoRoomWords]). The floor's cursor is left exactly where it was, so `esc` lands on the row
	// the page was opened from. Nothing here is persisted.
	comfy bool
	open  bool
	stage int
	said  bool

	// act is what the verbs leave behind (factory_keys.go): an open typing
	// row, a habit offer, and the mock clock's beat.
	act factoryActs

	// THE FLOOR'S OWN SETTINGS (factory_settings.go), additive like the rest:
	// the repo picker and the recipe page, each standing over the floor while
	// it is not nil, and the login gh answered while it is offered.
	pick    *factoryPicker
	recipe  *factoryRecipePage
	ghOffer string

	// THE DOCUMENT'S OWN STATE (factory_pane.go, factory_item.go), additive
	// like the rest. scroll is how far `J` and `K` have moved the item's body
	// down, and scrollID the item it is about, so a cursor that moves to
	// another item starts that one at its top; scrollMax and scrollPage are
	// what the last draw measured, the farthest the body can go and the rows
	// one page is. railTop, railFirst and railShown are the item page's rail
	// as the last draw placed it: the body row it starts on, the first entry
	// drawn, and how many, and pageRows the page's whole room, so a press
	// lands on the row a person saw.
	scroll     int
	scrollID   int
	scrollMax  int
	scrollPage int
	railTop    int
	railFirst  int
	railShown  int
	pageRows   int

	// THE SPLIT (factory_split.go). split is the rows' share of the width in
	// percent, 0 for [factoryRowsShare]; splitRead says the remembered one has
	// been read, or a key has already chosen one this launch; dragging is a
	// press on the divider not yet let go; bodyW is the width the last body
	// was drawn at, which the keys and the pointer move the divider within.
	split     float64
	splitRead bool
	dragging  bool
	bodyW     int

	// THE STABLE ORDER (factory_order.go). pos is each item's place inside
	// its section, by id, lower first; posState the state it had when it was
	// placed, so a move to another section is seen; posTop the lowest place
	// handed out, which the next arrival goes above. A re-read keeps every
	// place it finds; only `O`, an arrival and a change of state hand out a
	// new one. Nothing here is persisted.
	pos      map[int]int
	posState map[int]factory.State
	posTop   int
	// gridMemo is the row grid the frame measured once for all its rows
	// (factory_grid.go's [app.factoryGridAt]).
	gridMemo factoryGridMemo
	// titleCols is the title column the last rail draw measured, which the
	// comfortable density wraps a long title at ([factoryRowTitle2]).
	titleCols int

	// headFull is `h`: the handover's four rows rather than its one line,
	// remembered with the split in `factory.json` (factory_split.go).
	headFull bool

	// THE FLOOR'S WORK IN FLIGHT (factory_busy.go). busySince is when each
	// busy item was first seen busy, so its row can count up; rereads are the
	// items `u` asked to read again, by id, with what the door said the read
	// costs, kept until the floor says the read is over.
	busySince map[int]time.Time
	rereads   map[int]float64
	// phaseSince is when each running phase was first seen running, by
	// [factoryPhaseKey], on the floor's own clock (factory_run.go), so its
	// rail row can say how long it has run.
	phaseSince map[string]time.Time
}

// factoryRowsCols is the rows' columns at width with the divider where it
// starts: the whole width under [factoryPaneFloor], and otherwise
// [factoryRowsShare] percent of it, never under [factoryRowsMin]. Where the
// person has moved it is [app.factoryRowsAt] (factory_split.go).
func factoryRowsCols(width int) int { return factoryRowsColsAt(width, 0) }

// factoryConnected says whether a floor stands behind the page at all.
func (a *app) factoryConnected() bool { return a.factory.Load != nil }

// factoryRead asks the seam for the floor beside the door line and folds the
// snapshot in on the loop. It answers nil when there is no door or a read is
// already out.
func (a *app) factoryRead() tea.Cmd {
	load := a.factory.Load
	if load == nil || a.fp.reading {
		return nil
	}
	a.fp.reading = true
	// THE REMEMBERED SPLIT IS READ ON THE SAME TRIP, once a launch, off the
	// loop like the floor (factory_split.go's [readFactoryPrefs]).
	prefsPath := ""
	if !a.fp.splitRead {
		prefsPath = factoryPrefsPath(a.profileDir)
	}
	return a.besideLine(func() func(bool) tea.Cmd {
		snap, err := load()
		prefs, read := factoryPrefs{}, prefsPath != ""
		if read {
			prefs = readFactoryPrefs(prefsPath)
		}
		return func(bool) tea.Cmd {
			a.fp.reading = false
			if read && !a.fp.splitRead {
				a.fp.split, a.fp.headFull, a.fp.splitRead = prefs.Split, prefs.Handover, true
			}
			if err != nil {
				a.fp.err = err
				return nil
			}
			a.factoryFold(snap)
			return nil
		}
	})
}

// factoryLaunchRead is the one read of the floor a window takes as it opens,
// so the tab bar can count what waits on the person before anybody has been to
// the page ([app.barAsk]). It is [app.factoryRead] and nothing more, asked
// only of a seam that can load: a window with no floor behind it reads nothing
// and draws no chip, which is the emptiness law on the bar.
func (a *app) factoryLaunchRead() tea.Cmd {
	if !a.factory.Has("load") {
		return nil
	}
	return a.factoryRead()
}

// factoryFold takes one snapshot in. THE CURSOR STAYS ON THE ITEM IT WAS ON, by
// id, because a re-read three seconds later may have moved that item to another
// group and a cursor kept by position would land on a stranger.
//
// AND THE ROWS STAY WHERE THEY STOOD (factory_order.go's [app.factoryPlace]):
// a re-read that changed an item's priority or its age moves nothing, and only
// an arrival or a change of state hands a row a new place. What the floor says
// about work in flight is folded in on the same pass ([app.factoryFoldBusy]).
func (a *app) factoryFold(snap factory.Snapshot) {
	was, had := a.factoryCursorItem()
	a.factoryFoldStoreMarks(a.fp.snap, &snap)
	a.fp.snap, a.fp.loaded, a.fp.err = snap, true, nil
	a.factoryGridForget()
	a.factoryPlace(false)
	a.factoryFoldBusy()
	a.factoryFoldPhases()
	a.factoryKeep(was, had)
}

// ── the rail's rows ─────────────────────────────────────────────────────────

// The rail's rows — its groups, its view, the delta rule and the filter — are
// factory_rail.go's. Everything below reads them through [app.factoryRows] and
// [app.factoryWalkNow], so the cursor counts in the same list the rail draws.

// factoryWaiting is how many items on the last floor read wait on the
// person, and 0 before the first read: the tab bar's `? 5` ([app.barAsk]).
func (a *app) factoryWaiting() int {
	if !a.fp.loaded {
		return 0
	}
	return a.fp.snap.Count(factory.StateNeedsYou)
}

// factoryCursorItem is the item under the cursor, and false when the floor
// holds none.
func (a *app) factoryCursorItem() (factory.Item, bool) {
	walk := a.factoryWalkNow()
	if a.fp.cursor < 0 || a.fp.cursor >= len(walk) {
		return factory.Item{}, false
	}
	return a.fp.snap.Items[walk[a.fp.cursor]], true
}

// factoryLines is the rail line each walk position stands on, top first: the
// place's stops.
func (a *app) factoryLines() []int {
	var out []int
	for line, r := range a.factoryRows() {
		if r.kind == factoryRowItem {
			out = append(out, line)
		}
	}
	return out
}

// factoryMove walks the cursor by delta items.
func (a *app) factoryMove(delta int) {
	switch {
	case a.fp.pick != nil:
		a.fp.pick.cursor = moveCursor(a.fp.pick.cursor, delta, len(a.fp.pick.visible()))
		a.touch()
		return
	case a.fp.recipe != nil:
		a.fp.stage = moveCursor(a.fp.stage, delta, len(a.fp.recipe.stages()))
		a.touch()
		return
	}
	if a.fp.open {
		a.factoryStageMove(delta)
		return
	}
	a.fp.cursor = moveCursor(a.fp.cursor, delta, len(a.factoryWalkNow()))
	// THE NEXT ITEM IS READ FROM ITS TOP: the body's scroll belonged to the
	// item the cursor left.
	a.fp.scroll = 0
	a.touch()
}

// ── the body ────────────────────────────────────────────────────────────────

// factoryBody is the page, exactly room rows of exactly width cells: the one
// dim line when nothing is connected, the item page while one is open, and
// otherwise the handover over the floor in whichever of its three geometries
// the width allows. Every row that holds an item carries its rail line as its
// hit; every other row carries -1.
func (a *app) factoryBody(width, room int) []placeRow {
	if room <= 0 {
		return nil
	}
	if !a.factoryConnected() {
		a.fp.rowsW, a.fp.shown = 0, 0
		return placeTeachRows(placeTeachProse(factoryUnconnectedWords, width, a.pal), room)
	}
	// THE PICKER AND THE RECIPE PAGE STAND OVER EVERYTHING, at the full
	// width at every width, as the item page does.
	if a.fp.pick != nil {
		a.fp.shown = 0
		return a.factoryPickerBody(width, room)
	}
	if a.fp.recipe != nil {
		a.fp.shown = 0
		return a.factoryRecipeBody(width, room)
	}
	if a.fp.open {
		if it, ok := a.factoryCursorItem(); ok {
			a.fp.shown = 0
			return a.factoryItemBody(it, width, room)
		}
		a.fp.open = false
	}
	a.fp.columns = width >= factoryFactsFloor
	a.factoryGridForget()
	// THE DIVIDER STANDS WHERE THE PERSON PUT IT ([app.factoryRowsAt]): `{`,
	// `}`, `|` and a drag move it, inside the limits that keep both columns
	// readable.
	rowsW := a.factoryRowsAt(width)
	// AND IT STANDS ONLY BESIDE A PEEK THAT HAS AN ITEM TO SHOW (owner
	// ruling, 2026-10-08): a floor with nothing under the cursor draws its
	// rows at the whole width, as Teams and Home draw nothing where they
	// have nothing, rather than a rule beside forty empty rows.
	if _, ok := a.factoryCursorItem(); !ok {
		rowsW = width
	}
	a.fp.rowsW, a.fp.bodyW = rowsW, width
	paneW := 0
	if rowsW < width {
		paneW = width - rowsW - factoryRuleW
	}
	rows := make([]placeRow, 0, room)
	// THE HANDOVER SPANS THE WHOLE WIDTH, ABOVE BOTH COLUMNS. It carries its
	// own blank as its last row, so the columns start on the row after it.
	var head []string
	if a.fp.columns {
		if h := a.factoryHead(width); len(h)+factoryHeadLeaves <= room {
			head = h
		}
	}
	for _, line := range head {
		rows = append(rows, placeRow{text: line, hit: -1})
	}
	a.fp.headRows = len(head)
	left := room - len(head)
	// THE VERBS' ROWS STAND AT THE BOTTOM OF THE PANE COLUMN, inside the body
	// and above the hint line: the habit offer, then the typing row
	// (factory_keys.go's [app.factoryFootRows]). The peek is asked for the
	// room above them, so they never cover its last lines. With no peek they
	// stand under the rows at the full width, and the rows get the room above.
	footW := paneW
	if paneW == 0 {
		footW = width
	}
	foot := a.factoryFoot(footW, left)
	// THE FLOOR'S OWN FOOT ROWS (`n`, and `U`'s question) stand under the
	// rows instead, at the rows' whole width, when there is a peek to leave
	// alone ([app.factoryFootOnRows]).
	var rowsFoot []string
	if paneW > 0 && a.factoryFootOnRows() {
		foot = a.factoryFootSide(paneW, left, false)
		rowsFoot = a.factoryFootSide(rowsW, left, true)
	}
	above := left - len(foot)
	railRoom := left - len(rowsFoot)
	if paneW == 0 {
		railRoom = above
	}
	// THE ROWS STAND [factoryMargin] IN FROM THE FRAME'S EDGE, like the
	// handover above them, so a group's heading and the handover's mark share
	// a column; beside the peek the divider's air cell keeps an age off the
	// rule, and at the whole width they stop [factoryMargin] before the
	// frame's right edge (factory_grid.go's THE RIGHT MARGIN).
	railW := rowsW - factoryMargins
	if paneW > 0 {
		railW = rowsW - factoryMargin - (factoryDividerW - factoryRuleW)
	}
	rail, hits := a.factoryRail(max(railW, 0), railRoom)
	if a.factoryBare() {
		rail, hits = a.factoryBareRail(max(railW, 0), railRoom)
	}
	for i := range rail {
		rail[i] = factoryPad(factoryMarginPad()+rail[i], rowsW)
	}
	// THE PEEK'S TITLE STANDS ON ONE FIXED ROW: the row the rows' first
	// section heading stands on when the list is at its top (owner ruling,
	// 2026-10-08), so the two columns start their content on one row, and the
	// list scrolls under it without moving it. Pinned to the first heading
	// still on screen, the peek shrank to a title and a key line whenever the
	// cursor walked low.
	var pane []string
	if paneW > 0 {
		off := min(a.factoryPeekTop(), max(left, 0))
		pane = append(make([]string, off), a.factoryPaneWithFoot(paneW, left-off, foot)...)
	}
	sep := a.pal.dim(a.linearMark("│", "|"))
	for i := 0; i < left; i++ {
		line := factorySpaces(rowsW)
		hit := -1
		if i < len(rail) {
			line, hit = rail[i], hits[i]
		}
		if paneW == 0 {
			if i >= above {
				line, hit = foot[i-above], -1
			}
			rows = append(rows, placeRow{text: factoryPad(line, width), hit: hit})
			continue
		}
		right := ""
		if i < len(pane) {
			right = pane[i]
		}
		if at := i - (left - len(rowsFoot)); at >= 0 && at < len(rowsFoot) {
			line, hit = rowsFoot[at], -1
		}
		rows = append(rows, placeRow{text: line + sep + factoryPad(right, paneW), hit: hit})
	}
	return rows
}

// factoryPaneWithFoot is the peek over room rows with the verbs' foot rows
// (a typing box, a habit offer) set in above its key line: THE KEY LINE STAYS
// THE COLUMN'S LAST ROW whatever opens (owner ruling, 2026-10-08, after a box
// opened under the keys and left them stale above it), with one blank between
// the box and the keys when the room allows it.
func (a *app) factoryPaneWithFoot(width, room int, foot []string) []string {
	if len(foot) == 0 {
		return a.factoryPane(width, room)
	}
	peekRoom := room - len(foot)
	gap := 0
	if peekRoom-factoryBlockGap >= factoryActionRows+1 {
		gap = factoryBlockGap
	}
	peek := a.factoryPane(width, peekRoom-gap)
	if len(peek) < factoryActionRows {
		return append(peek, foot...)
	}
	last := len(peek) - 1
	out := append(append([]string{}, peek[:last]...), foot...)
	for i := 0; i < gap; i++ {
		out = append(out, factorySpaces(width))
	}
	return append(out, peek[last])
}

// factoryPeekTop is the body row the peek's title stands on: the row of the
// rows' first section heading when the list is scrolled to its top, which is
// that heading's place in the rail's lines, and 0 for a rail with none.
func (a *app) factoryPeekTop() int {
	for line, r := range a.factoryRows() {
		if r.kind == factoryRowHeading {
			return line
		}
	}
	return 0
}

// factoryWindowTop is the rail line the rows' window starts on, given the
// cursor's line and the pinned lines above the window. THE WINDOW FOLLOWS THE
// CURSOR ([placeTop]) WITH TWO LAWS ON TOP (owner ruling, 2026-10-08):
//
//   - THE LIST'S TOP COMES BACK WITH ITS FIRST ITEM. With the cursor on the
//     first item of the walk the window is at its top, the first heading and
//     the air above it on screen; a window that only kept the cursor's row in
//     view scrolled back up to the row and left the heading above it.
//   - A SECTION'S HEADING MOVES WITH ITS FIRST ROW: the cursor on a section's
//     first item shows the heading over it, and a heading that would stand on
//     the window's last row with its rows under the edge is not drawn there
//     ([factoryOrphanHeading]).
//
// The rows an item carries under it (a second title line, the read) come into
// view with it.
func (a *app) factoryWindowTop(rows []factoryRailRow, cursor, pinned, room int) int {
	span, n := room-pinned, len(rows)-pinned
	if span < 1 || n <= span || cursor < 0 || cursor >= len(rows) {
		return pinned
	}
	lo, hi := cursor, cursor
	for hi+1 < len(rows) && rows[hi+1].kind != factoryRowItem && rows[hi+1].walk >= 0 && rows[hi+1].walk == rows[cursor].walk {
		hi++
	}
	switch {
	case rows[cursor].walk == 0:
		lo = pinned
	case cursor-1 >= pinned && rows[cursor-1].kind == factoryRowHeading:
		lo = cursor - 1
	}
	top := min(max(a.fp.top-pinned, 0), n-span)
	lo, hi = max(lo-pinned, 0), hi-pinned
	if lo < top {
		top = lo
	}
	if hi >= top+span {
		top = hi - span + 1
	}
	return pinned + top
}

// factoryOrphanHeading says whether rail line sits on the window's last row
// (end is one past it) as a section heading whose rows are all under the
// edge. It is drawn as air instead, and comes back with its first row.
func factoryOrphanHeading(rows []factoryRailRow, line, end int) bool {
	return rows[line].kind == factoryRowHeading && line == end-1 && line+1 < len(rows)
}

// factoryBare says whether the floor has been read and holds no item at all.
// A floor that has not been read yet draws nothing rather than a line that may
// be false, and a read that failed keeps the note line's sentence instead.
func (a *app) factoryBare() bool {
	return a.fp.loaded && a.fp.err == nil && len(a.fp.snap.Items) == 0
}

// factoryBareRail is the rail of a bare floor: [factoryBareWords], dim, wrapped
// to the rows' column and never more than room rows, with no row a press can
// land on. The cursor's window is emptied with it, so a click finds no item.
func (a *app) factoryBareRail(width, room int) ([]string, []int) {
	a.fp.top, a.fp.shown, a.fp.pinned = 0, 0, 0
	var out []string
	var hits []int
	for _, line := range placeTeachProse(factoryBareWords, width, a.pal) {
		if len(out) >= room {
			break
		}
		out = append(out, factoryPad(line, width))
		hits = append(hits, -1)
	}
	return out, hits
}

// factoryFoot is the verbs' rows ([app.factoryFootRows]) as whole rows of
// exactly width cells, each on the pane's lead, and never more than room of
// them: the newest, the typing row, are the ones kept.
func (a *app) factoryFoot(width, room int) []string {
	return a.factoryFootFrom(a.factoryFootRows(width-factoryMargins), width, room)
}

// factoryFootSide is one side's foot rows ([app.factoryFootRowsWhere]) laid
// out as [app.factoryFoot] lays them.
func (a *app) factoryFootSide(width, room int, rows bool) []string {
	return a.factoryFootFrom(a.factoryFootRowsWhere(width-factoryMargins, rows), width, room)
}

// factoryFootFrom pads foot rows to whole rows on the pane's lead, newest kept.
func (a *app) factoryFootFrom(rows []string, width, room int) []string {
	var foot []string
	for _, row := range rows {
		foot = append(foot, factoryPad(factorySpaces(factoryMargin)+row, width))
	}
	if len(foot) > room {
		foot = foot[len(foot)-max(room, 0):]
	}
	return foot
}

// factoryPress puts the cursor on the item drawn on screen row y, and answers
// false for a row that holds no item. It reads the window the last body drew,
// so a press lands on the row a person saw.
func (a *app) factoryPress(y int) bool {
	if a.fp.open || a.fp.pick != nil || a.fp.recipe != nil {
		return false
	}
	at, ok := placeBodyLine(y-a.fp.headRows, 0, a.fp.shown)
	if !ok || at < a.fp.pinned {
		return false
	}
	line := a.fp.top + at - a.fp.pinned
	rows := a.factoryRows()
	if line < 0 || line >= len(rows) || rows[line].walk < 0 {
		return false
	}
	if rows[line].walk != a.fp.cursor {
		a.fp.scroll = 0
	}
	a.fp.cursor = rows[line].walk
	a.touch()
	return true
}

// factoryPad is s cut or padded to exactly width cells.
func factoryPad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = fit(s, width)
	return s + factorySpaces(max(width-ansi.StringWidth(s), 0))
}
