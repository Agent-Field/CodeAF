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

// factoryPaneNextWords stands in for the rest of the pane, which is the next
// piece of this page to be built. It says what will be there, so a person
// reading the fixture is not left wondering whether something failed to load.
const factoryPaneNextWords = "stages, the running stream and the proof sheet arrive in this pane next"

// factoryPage is the page's own state, held on the app as `fp`.
//
// THE NEXT PIECES OF THIS PAGE BUILD ON THESE FIELDS AND NO OTHERS: snap is the
// only reading a draw may use, cursor names a position in the rail's WALK
// ORDER ([factoryRailRows]'s item rows, top to bottom) rather than an index
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
	walk := factoryWalk(snap)
	if had {
		for at, i := range walk {
			if snap.Items[i].ID == was.ID {
				a.fp.cursor = at
				break
			}
		}
	}
	a.fp.cursor = moveCursor(a.fp.cursor, 0, len(walk))
	a.touch()
}

// ── the rail's rows ─────────────────────────────────────────────────────────

// factoryGroup is one heading of the rail and the states filed under it.
type factoryGroup struct {
	word   string
	states []factory.State
}

// factoryGroups is the rail's order: what waits on the person first, then what
// is running, then what is new, then what came back. Dismissed items are not on
// the floor at all.
var factoryGroups = []factoryGroup{
	{word: "needs you", states: []factory.State{factory.StateNeedsYou}},
	{word: "streams", states: []factory.State{factory.StateRunning, factory.StateQueued}},
	{word: "new", states: []factory.State{factory.StateNew}},
	{word: "landed", states: []factory.State{factory.StateLanded}},
	{word: "shipped", states: []factory.State{factory.StateShipped}},
}

// factoryRailRow is one line of the rail: a heading, a blank between groups,
// or an item, which names its index in snap.Items and its place in the walk.
type factoryRailRow struct {
	heading string
	item    int // index into snap.Items, -1 for a heading or a blank
	walk    int // position in the walk order, -1 for a heading or a blank
}

// factoryRailRows lays the snapshot out as rail lines. A group with nothing in
// it draws nothing, heading included.
func factoryRailRows(snap factory.Snapshot) []factoryRailRow {
	var rows []factoryRailRow
	walk := 0
	for _, g := range factoryGroups {
		first := true
		for i, it := range snap.Items {
			if !factoryIn(it.State, g.states) {
				continue
			}
			if first {
				if len(rows) > 0 {
					rows = append(rows, factoryRailRow{item: -1, walk: -1})
				}
				rows = append(rows, factoryRailRow{heading: g.word, item: -1, walk: -1})
				first = false
			}
			rows = append(rows, factoryRailRow{item: i, walk: walk})
			walk++
		}
	}
	return rows
}

func factoryIn(st factory.State, states []factory.State) bool {
	for _, s := range states {
		if s == st {
			return true
		}
	}
	return false
}

// factoryWalk is the items the cursor walks, as indexes into snap.Items.
func factoryWalk(snap factory.Snapshot) []int {
	var out []int
	for _, r := range factoryRailRows(snap) {
		if r.item >= 0 {
			out = append(out, r.item)
		}
	}
	return out
}

// factoryCursorItem is the item under the cursor, and false when the floor
// holds none.
func (a *app) factoryCursorItem() (factory.Item, bool) {
	walk := factoryWalk(a.fp.snap)
	if a.fp.cursor < 0 || a.fp.cursor >= len(walk) {
		return factory.Item{}, false
	}
	return a.fp.snap.Items[walk[a.fp.cursor]], true
}

// factoryLines is the rail line each walk position stands on, top first: the
// place's stops.
func (a *app) factoryLines() []int {
	var out []int
	for line, r := range factoryRailRows(a.fp.snap) {
		if r.item >= 0 {
			out = append(out, line)
		}
	}
	return out
}

// factoryMove walks the cursor by delta items.
func (a *app) factoryMove(delta int) {
	a.fp.cursor = moveCursor(a.fp.cursor, delta, len(factoryWalk(a.fp.snap)))
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
	rail, hits := a.factoryRail(listW, room)
	var pane []string
	paneW := width - railW
	if railW > 0 {
		// THE PANE COLUMN IS THE HANDOVER, ONE BLANK ROW, THEN THE PANE. The
		// handover ([app.factoryHead]) carries its own blank as its last row,
		// so the pane starts on the row after it and the rail beside both
		// keeps its own window.
		pane = append(a.factoryHead(paneW), a.factoryPane(paneW)...)
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

// factoryRail is the rail's window of at most room lines, each exactly width
// cells, and the rail line each one shows (-1 for a heading or a blank). THE
// WINDOW FOLLOWS THE CURSOR ([placeTop]) and is never scrolled on its own.
func (a *app) factoryRail(width, room int) ([]string, []int) {
	pal := a.pal
	rows := factoryRailRows(a.fp.snap)
	cursorLine := 0
	for line, r := range rows {
		if r.walk == a.fp.cursor && r.item >= 0 {
			cursorLine = line
			break
		}
	}
	a.fp.top = placeTop(a.fp.top, cursorLine, len(rows), room)
	end := min(a.fp.top+room, len(rows))
	out := make([]string, 0, end-a.fp.top)
	hits := make([]int, 0, end-a.fp.top)
	for line := a.fp.top; line < end; line++ {
		r := rows[line]
		switch {
		case r.heading != "":
			out = append(out, factoryPad(pal.dim(" "+r.heading), width))
			hits = append(hits, -1)
		case r.item < 0:
			out = append(out, strings.Repeat(" ", width))
			hits = append(hits, -1)
		default:
			it := a.fp.snap.Items[r.item]
			if r.walk == a.fp.cursor {
				text := factoryPad(" "+it.Ref()+" "+it.Title, width)
				out = append(out, pal.cursor(pal.ink(text), width))
			} else {
				out = append(out, factoryPad(" "+pal.muted(it.Ref())+" "+pal.ink(it.Title), width))
			}
			hits = append(hits, line)
		}
	}
	a.fp.shown = len(out)
	return out, hits
}

// factoryPress puts the cursor on the item drawn on screen row y, and answers
// false for a row that holds no item. It reads the window the last body drew,
// so a press lands on the row a person saw.
func (a *app) factoryPress(y int) bool {
	line, ok := placeBodyLine(y, a.fp.top, a.fp.shown)
	if !ok {
		return false
	}
	rows := factoryRailRows(a.fp.snap)
	if line < 0 || line >= len(rows) || rows[line].item < 0 {
		return false
	}
	a.fp.cursor = rows[line].walk
	a.touch()
	return true
}

// factoryPane is the item under the cursor: its ref and repository, its title,
// and the factory's one-sentence read of it, then the line saying what arrives
// here next. Each line is at most width cells.
func (a *app) factoryPane(width int) []string {
	pal := a.pal
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	measure := max(width-2, 1)
	out := []string{
		" " + pal.muted(fit(it.Ref()+"  "+it.Repo, measure)),
	}
	for _, line := range wrap(it.Title, measure) {
		out = append(out, " "+pal.bold(pal.ink(line)))
	}
	if read := strings.TrimSpace(it.Triage.Read); read != "" {
		out = append(out, "")
		for _, line := range wrap(read, measure) {
			out = append(out, " "+pal.ink(line))
		}
	}
	out = append(out, "")
	for _, line := range wrap(factoryPaneNextWords, measure) {
		out = append(out, " "+pal.dim(line))
	}
	return out
}

// factoryPad is s cut or padded to exactly width cells.
func factoryPad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = fit(s, width)
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}
