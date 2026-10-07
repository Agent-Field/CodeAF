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
// by where it stands, on a rail at the left, and the item under the cursor in a
// pane at the right. place_factory.go is the handle the registry files; this
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

// factoryRailFloor is the narrowest terminal that still splits a rail and a
// pane; under it the rail takes the whole width.
const factoryRailFloor = 72

// factoryRailMin and factoryRailMax bound the rail's columns, its separator
// included.
const (
	factoryRailMin = 24
	factoryRailMax = 40
)

// factoryUnconnectedWords is the page with no seam behind it. It names what
// arrives here, never that the page is empty (the emptiness law's panel rule).
const factoryUnconnectedWords = "nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected"

// factoryPage is the page's own state, held on the app as `fp`.
//
// THE NEXT PIECES OF THIS PAGE BUILD ON THESE FIELDS AND NO OTHERS: snap is the
// only reading a draw may use, cursor names a position in the rail's WALK
// ORDER ([app.factoryRows]'s item rows, top to bottom) rather than an index
// into snap.Items, and railW is what the last body measured, so a press can be
// told rail from pane without drawing again.
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
	// railW is the rail's columns at the last body, its separator included,
	// and 0 when the rail took the whole width.
	railW int

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

	// act is what the verbs leave behind (factory_keys.go): an open typing
	// row, a habit offer, and the mock clock's beat.
	act factoryActs
}

// factoryRailCols is the rail's columns at width, its separator included, and
// 0 under [factoryRailFloor].
func factoryRailCols(width int) int {
	if width < factoryRailFloor {
		return 0
	}
	return min(max(width*3/10, factoryRailMin), factoryRailMax)
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
	a.fp.cursor = moveCursor(a.fp.cursor, delta, len(a.factoryWalkNow()))
	a.touch()
}

// ── the body ────────────────────────────────────────────────────────────────

// factoryBody is the page, exactly room rows of exactly width cells: the one
// dim line when nothing is connected, and otherwise the rail and the pane. Every
// rail row that holds an item carries its rail line as its hit; every other row
// carries -1.
func (a *app) factoryBody(width, room int) []placeRow {
	if room <= 0 {
		return nil
	}
	if !a.factoryConnected() {
		a.fp.railW, a.fp.shown = 0, 0
		return placeTeachRows(placeTeachProse(factoryUnconnectedWords, width, a.pal), room)
	}
	railW := factoryRailCols(width)
	a.fp.railW = railW
	listW := width
	if railW > 0 {
		listW = railW - 1
	}
	paneW := width - railW
	// THE VERBS' ROWS STAND AT THE BOTTOM OF THE PANE COLUMN, inside the body
	// and above the hint line: the habit offer, then the typing row
	// (factory_keys.go's [app.factoryFootRows]). The pane is asked for the
	// room above them, so they never cover its last lines. Under the rail
	// floor there is no pane, and they stand under the rail instead.
	footW := paneW
	if railW == 0 {
		footW = width
	}
	var foot []string
	for _, row := range a.factoryFootRows(footW - factoryPaneLead) {
		foot = append(foot, factoryPad(strings.Repeat(" ", factoryPaneLead)+row, footW))
	}
	if len(foot) > room {
		foot = foot[len(foot)-room:]
	}
	above := room - len(foot)
	railRoom := room
	if railW == 0 {
		railRoom = above
	}
	rail, hits := a.factoryRail(listW, railRoom)
	var pane []string
	if railW > 0 {
		// THE PANE COLUMN IS THE HANDOVER, ONE BLANK ROW, THEN THE PANE. The
		// handover ([app.factoryHead]) carries its own blank as its last row,
		// so the pane starts on the row after it and gets the room that is
		// left; the rail beside both keeps its own window.
		head := a.factoryHead(paneW)
		if len(head) > above {
			head = head[:above]
		}
		pane = append(head, a.factoryPane(paneW, max(0, above-len(head)))...)
		pane = append(pane, foot...)
	} else {
		for len(rail) < above {
			rail = append(rail, strings.Repeat(" ", listW))
			hits = append(hits, -1)
		}
		rail = append(rail, foot...)
		for range foot {
			hits = append(hits, -1)
		}
	}
	sep := a.pal.dim(a.linearMark("│", "|"))
	rows := make([]placeRow, 0, room)
	for i := 0; i < room; i++ {
		left := strings.Repeat(" ", listW)
		hit := -1
		if i < len(rail) {
			left, hit = rail[i], hits[i]
		}
		if railW == 0 {
			rows = append(rows, placeRow{text: left, hit: hit})
			continue
		}
		right := ""
		if i < len(pane) {
			right = pane[i]
		}
		rows = append(rows, placeRow{text: left + sep + factoryPad(right, paneW), hit: hit})
	}
	return rows
}

// factoryPress puts the cursor on the item drawn on screen row y, and answers
// false for a row that holds no item. It reads the window the last body drew,
// so a press lands on the row a person saw.
func (a *app) factoryPress(y int) bool {
	line, ok := placeBodyLine(y, a.fp.top, a.fp.shown)
	if !ok {
		return false
	}
	rows := a.factoryRows()
	if line < 0 || line >= len(rows) || rows[line].kind != factoryRowItem {
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
