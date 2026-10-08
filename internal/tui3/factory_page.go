package tui3

import (
	"strings"

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
// the left, [factoryRowsShare] percent of the width and never under
// [factoryRowsMin], a dim rule, and the peek at the right. From
// [factoryFactsFloor] up to that it is the rows alone, at the full width, with
// every column a row carries. Under [factoryFactsFloor] a row is its lead, its
// ref and its title and nothing else. THE HANDOVER SPANS THE WHOLE WIDTH ABOVE
// ALL OF IT ([app.factoryHead]) wherever the rows carry their columns, so it is
// one strip over the floor rather than the head of one column.
//
// The item page (factory_item.go) is how a person sees an item whole at any
// width; the peek is a glance, drawn only where there is room for one beside
// rows that still carry their facts.

// factoryPaneFloor is the narrowest terminal that draws the peek beside the
// rows.
const factoryPaneFloor = 120

// factoryFactsFloor is the narrowest terminal whose rows carry the repo, the
// facts and the age. Under it a row is its lead, its ref and its title.
const factoryFactsFloor = 90

// factoryRowsShare and factoryRowsMin size the rows' column beside the peek:
// a share of the width in percent, and the fewest columns it may have.
const (
	factoryRowsShare = 58
	factoryRowsMin   = 70
)

// factoryHeadLeaves is the fewest rows the floor keeps for its own rows under
// the handover. A frame shorter than the handover and these draws no handover,
// because a floor whose strip ate the rows answers "what is happening" and
// hides "what is it happening to".
const factoryHeadLeaves = 6

// factoryUnconnectedWords is the page with no seam behind it. It names what
// arrives here, never that the page is empty (the emptiness law's panel rule).
const factoryUnconnectedWords = "nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected"

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

	// THE LAYOUT'S OWN STATE (factory_item.go), additive like the narrowings.
	// comfy is `z`, a second line under each row and air between rows; open is
	// the item page standing over the floor, and stage the stage its rail's
	// cursor is on. The floor's cursor is left exactly where it was, so `esc` lands on the row
	// the page was opened from. Nothing here is persisted.
	comfy bool
	open  bool
	stage int

	// act is what the verbs leave behind (factory_keys.go): an open typing
	// row, a habit offer, and the mock clock's beat.
	act factoryActs
}

// factoryRowsCols is the rows' columns at width: the whole width under
// [factoryPaneFloor], and otherwise [factoryRowsShare] percent of it, never
// under [factoryRowsMin].
func factoryRowsCols(width int) int {
	if width < factoryPaneFloor {
		return width
	}
	return max(width*factoryRowsShare/100, factoryRowsMin)
}

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
	return a.besideLine(func() func(bool) tea.Cmd {
		snap, err := load()
		return func(bool) tea.Cmd {
			a.fp.reading = false
			if err != nil {
				a.fp.err = err
				return nil
			}
			a.factoryFold(snap)
			return nil
		}
	})
}

// factoryFold takes one snapshot in. THE CURSOR STAYS ON THE ITEM IT WAS ON, by
// id, because a re-read three seconds later may have moved that item to another
// group and a cursor kept by position would land on a stranger.
func (a *app) factoryFold(snap factory.Snapshot) {
	was, had := a.factoryCursorItem()
	a.fp.snap, a.fp.loaded, a.fp.err = snap, true, nil
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
	if a.fp.open {
		a.factoryStageMove(delta)
		return
	}
	a.fp.cursor = moveCursor(a.fp.cursor, delta, len(a.factoryWalkNow()))
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
	if a.fp.open {
		if it, ok := a.factoryCursorItem(); ok {
			a.fp.shown = 0
			return a.factoryItemBody(it, width, room)
		}
		a.fp.open = false
	}
	a.fp.columns = width >= factoryFactsFloor
	rowsW := factoryRowsCols(width)
	a.fp.rowsW = rowsW
	paneW := 0
	if rowsW < width {
		paneW = width - rowsW - 1
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
	above := left - len(foot)
	railRoom := left
	if paneW == 0 {
		railRoom = above
	}
	rail, hits := a.factoryRail(rowsW, railRoom)
	var pane []string
	if paneW > 0 {
		pane = append(a.factoryPane(paneW, above), foot...)
	}
	sep := a.pal.dim(a.linearMark("│", "|"))
	for i := 0; i < left; i++ {
		line := strings.Repeat(" ", rowsW)
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
		rows = append(rows, placeRow{text: line + sep + factoryPad(right, paneW), hit: hit})
	}
	return rows
}

// factoryFoot is the verbs' rows ([app.factoryFootRows]) as whole rows of
// exactly width cells, each on the pane's lead, and never more than room of
// them: the newest, the typing row, are the ones kept.
func (a *app) factoryFoot(width, room int) []string {
	var foot []string
	for _, row := range a.factoryFootRows(width - factoryPaneLead) {
		foot = append(foot, factoryPad(strings.Repeat(" ", factoryPaneLead)+row, width))
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
	if a.fp.open {
		return false
	}
	line, ok := placeBodyLine(y-a.fp.headRows, a.fp.top, a.fp.shown)
	if !ok {
		return false
	}
	rows := a.factoryRows()
	if line < 0 || line >= len(rows) || rows[line].walk < 0 {
		return false
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
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}
