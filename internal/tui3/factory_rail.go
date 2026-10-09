package tui3

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FACTORY RAIL ────────────────────────────────────────────────────────
//
// The rows of the factory floor: the floor as a list, grouped by where each
// item stands, one row per item on a fixed grid of columns, with the factory's
// read under it in the comfortable density (`z`). factory_page.go owns the page's
// read, its body and its pane; this file owns what the rail lays out, how a
// row is drawn, and the four ways a person narrows it (a repo, typed words,
// the backlog, and the marks they put on new work).
//
// THE RAIL READS a.fp AND NOTHING ELSE. Every narrowing is a field on the page
// state, folded into one [factoryView] that the row builder takes, so the walk
// the cursor counts in and the rows the frame draws can never disagree about
// what is on the floor.
//
// THE DELTA RULE. `new` shows what arrived in the last [factoryFresh] and keeps
// the rest of the backlog behind `A`, with one dim line saying how many it
// kept back. A floor that drew every open item ever filed would bury the ten
// that arrived this morning under the four hundred nobody will run.

// factoryFresh is how recent a new item has to be to draw without the backlog.
const factoryFresh = 72 * time.Hour

// THE ROW'S GRID is factory_grid.go's, and every width a row is laid out in
// is named there. A row carries at most [factoryFactsMost] facts.
const factoryFactsMost = 4

// factoryAnswerWord is the needs-you row's reminder that the question takes a
// yes or a no, drawn right before the age.
const factoryAnswerWord = "[y/n]"

// factoryView is every narrowing of the floor at once: the repo (its name, ""
// for all of them), the typed words, whether the words box is open, and
// whether the whole backlog is shown.
type factoryView struct {
	repo    string
	query   string
	typing  bool
	backlog bool
	// comfy is `z`: each item row carries the factory's read under it, and
	// a blank row stands between two items.
	comfy bool
	// order is `O`: how rows sit within each section (factory_order.go).
	order factoryOrder
	// pos is the places the page handed out (factory_order.go's
	// [app.factoryPlace]), which keep a row where it stood across a re-read;
	// nil sorts by the order alone.
	pos map[int]int
	// titleW is the title column a comfortable row wraps at, 0 for none.
	titleW int
}

// factoryRowKind is what one rail line is.
type factoryRowKind int

const (
	factoryRowItem factoryRowKind = iota
	factoryRowBlank
	factoryRowHeading
	factoryRowStrip // the repo the rail is showing
	factoryRowQuery // the words it is narrowed by
	factoryRowMore  // how many older new items the delta rule kept back
	factoryRowNone  // the words matched nothing
	factoryRowRead  // the factory's read under an item, in the comfortable density
	// factoryRowTitle2 is the rest of a long title, under its row, in the
	// comfortable density: one more line, cut with an ellipsis.
	factoryRowTitle2
)

// factoryGroup is one heading of the rail and the states filed under it.
type factoryGroup struct {
	word   string
	states []factory.State
}

// factoryGroups is the rail's order: what waits on the person first, a
// question and then a landed item waiting for its sign-off, then what is
// running, then what is new, then what has shipped. LANDED SITS BESIDE NEEDS
// YOU (owner ruling, 2026-10-08): an item waiting for a sign-off is the second
// thing a person is there for, and under the backlog it was the last thing on
// the floor. Dismissed items are not on the floor at all.
var factoryGroups = []factoryGroup{
	{word: "needs you", states: []factory.State{factory.StateNeedsYou}},
	{word: "landed", states: []factory.State{factory.StateLanded}},
	{word: "streams", states: []factory.State{factory.StateRunning, factory.StateQueued}},
	{word: "new", states: []factory.State{factory.StateNew}},
	{word: "shipped", states: []factory.State{factory.StateShipped}},
}

// factoryRailRow is one line of the rail. An item row names its index in
// snap.Items and its place in the walk; every other row carries -1 in both.
// count is a heading's number, the strip's number, or the kept-back number.
type factoryRailRow struct {
	kind    factoryRowKind
	heading string
	item    int // index into snap.Items, -1 for anything but an item
	walk    int // position in the walk order, -1 for anything but an item
	count   int
}

// factoryOnFloor says whether an item draws at all. A dismissed item never
// does.
func factoryOnFloor(it factory.Item) bool { return it.State != factory.StateDismissed }

// factoryInRepo says whether an item belongs to the repo name: arrived on it,
// or touches it.
func factoryInRepo(it factory.Item, repo string) bool {
	if repo == "" || it.Repo == repo {
		return true
	}
	for _, p := range it.Places {
		if p == repo {
			return true
		}
	}
	return false
}

// factoryFreshAt says whether a new item arrived inside the delta rule's
// window. A floor with no clock, or an item with no birth, is fresh: unknown
// hides nothing.
func factoryFreshAt(it factory.Item, now time.Time) bool {
	if now.IsZero() || it.Created.IsZero() {
		return true
	}
	return now.Sub(it.Created) <= factoryFresh
}

// factoryRailRows lays the snapshot out as rail lines under a view; with no
// view it is the whole floor, every repo, no words, the backlog kept back. A
// group with nothing in it draws nothing, heading included (the emptiness
// law), and a floor with nothing on it draws no rows at all.
//
// THE STRIP AND THE WORDS ARE ROWS TOO, at the top, so they scroll with the
// window the cursor drags rather than standing over it, and a press is
// resolved against one list.
func factoryRailRows(snap factory.Snapshot, views ...factoryView) []factoryRailRow {
	var v factoryView
	if len(views) > 0 {
		v = views[0]
	}
	floor := 0
	inRepo := 0
	for _, it := range snap.Items {
		if !factoryOnFloor(it) {
			continue
		}
		floor++
		if v.repo != "" && factoryInRepo(it, v.repo) {
			inRepo++
		}
	}
	if floor == 0 {
		return nil
	}
	rows := []factoryRailRow{{kind: factoryRowStrip, heading: v.repo, count: inRepo, item: -1, walk: -1}}
	if v.typing || v.query != "" {
		rows = append(rows, factoryRailRow{kind: factoryRowQuery, item: -1, walk: -1})
	}
	match := factory.Match(v.query)
	walk := 0
	for _, g := range factoryGroups {
		var in []int
		kept := 0
		for i, it := range snap.Items {
			if !factoryIn(it.State, g.states) || !factoryInRepo(it, v.repo) || !match(it) {
				continue
			}
			if it.State == factory.StateNew && !v.backlog && !factoryFreshAt(it, snap.Now) {
				kept++
				continue
			}
			in = append(in, i)
		}
		if len(in) == 0 && kept == 0 {
			continue
		}
		in = factoryPlaced(snap, factoryOrdered(snap, in, v.order), v.pos)
		rows = append(rows,
			factoryRailRow{kind: factoryRowBlank, item: -1, walk: -1},
			factoryRailRow{kind: factoryRowHeading, heading: g.word, count: len(in), item: -1, walk: -1})
		for n, i := range in {
			// IN THE COMFORTABLE DENSITY a blank stands between two items and
			// the read is a row of its own, which a press counts as its item's.
			if v.comfy && n > 0 {
				rows = append(rows, factoryRailRow{kind: factoryRowBlank, item: -1, walk: -1})
			}
			rows = append(rows, factoryRailRow{kind: factoryRowItem, item: i, walk: walk})
			if v.comfy && v.titleW > 0 && len(wrap(snap.Items[i].Title, v.titleW)) > 1 {
				rows = append(rows, factoryRailRow{kind: factoryRowTitle2, item: i, walk: walk})
			}
			if v.comfy && strings.TrimSpace(snap.Items[i].Triage.Read) != "" {
				rows = append(rows, factoryRailRow{kind: factoryRowRead, item: i, walk: walk})
			}
			walk++
		}
		if kept > 0 {
			rows = append(rows, factoryRailRow{kind: factoryRowMore, count: kept, item: -1, walk: -1})
		}
	}
	if walk == 0 && len(rows) <= 2 && v.query != "" {
		rows = append(rows,
			factoryRailRow{kind: factoryRowBlank, item: -1, walk: -1},
			factoryRailRow{kind: factoryRowNone, item: -1, walk: -1})
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

// factoryWalk is the items the cursor walks under a view, as indexes into
// snap.Items. With no view it is the whole floor's walk.
func factoryWalk(snap factory.Snapshot, views ...factoryView) []int {
	var out []int
	for _, r := range factoryRailRows(snap, views...) {
		if r.kind == factoryRowItem {
			out = append(out, r.item)
		}
	}
	return out
}

// ── the page's view of its own rail ─────────────────────────────────────────

// factoryViewNow is the view a.fp holds. A repo index the snapshot no longer
// has reads as every repo, so a floor whose repos changed under the filter
// shows everything rather than nothing.
func (a *app) factoryViewNow() factoryView {
	v := factoryView{query: a.fp.query, typing: a.fp.typing, backlog: a.fp.backlog, comfy: a.fp.comfy, order: a.fp.order, pos: a.fp.pos}
	if v.comfy {
		v.titleW = a.fp.titleCols
	}
	if r := a.fp.repo; r > 0 && r <= len(a.fp.snap.Repos) {
		v.repo = a.fp.snap.Repos[r-1].Name
	}
	return v
}

// factoryRows is the rail's rows as the page stands now. EVERY CURSOR, LINE
// AND PRESS ON THE PAGE READS THESE, never the bare [factoryRailRows], so a
// narrowed rail and the walk the cursor counts in are the same list.
//
// THEY ARE LAID OUT ONCE PER FLOOR AND VIEW, NOT ONCE PER QUESTION (owner's
// floor, 2026-10-09). The cursor's item is asked for dozens of times a
// message — every layout of the hosted chat asks the top bar's height, which
// asks the cursor's item — and each ask laid three hundred items out again,
// filtered, grouped and sorted: on the item page that was 40% of the loop's
// time under a moving pointer. The memo is keyed on everything the layout
// reads (the snapshot's items and clock, the view, the places), so a fold, a
// narrowing or a new place lays them out again and nothing else does. THE
// SLICES ARE SHARED, so no caller may write into them.
func (a *app) factoryRows() []factoryRailRow {
	return a.factoryRowsNow().rows
}

// factoryWalkNow is [factoryWalk] under the page's own view.
func (a *app) factoryWalkNow() []int {
	return a.factoryRowsNow().walk
}

// factoryRowsMemo is the rail laid out once ([app.factoryRowsNow]): the key
// it was laid out under, and what it said.
type factoryRowsMemo struct {
	ok      bool
	snapGen int
	posGen  int
	items   *factory.Item
	n       int
	now     time.Time
	view    factoryViewKey
	rows    []factoryRailRow
	walk    []int
}

// factoryViewKey is a [factoryView] without its places, which are a map and
// are compared by [factoryPage.posGen] instead.
type factoryViewKey struct {
	repo, query            string
	typing, backlog, comfy bool
	order                  factoryOrder
	titleW                 int
}

// factoryRowsNow is the rail's rows and walk under the page's view, from the
// memo when nothing they read has moved. The view's places are compared by
// [factoryPage.posGen], which every change to them bumps, and the items by
// their backing array as well as by [factoryPage.snapGen], so a snapshot
// put in place without a fold is laid out again too.
func (a *app) factoryRowsNow() *factoryRowsMemo {
	v := a.factoryViewNow()
	snap := &a.fp.snap
	var first *factory.Item
	if len(snap.Items) > 0 {
		first = &snap.Items[0]
	}
	m := &a.fp.rowsMemo
	key := factoryViewKey{repo: v.repo, query: v.query, typing: v.typing, backlog: v.backlog, comfy: v.comfy, order: v.order, titleW: v.titleW}
	if m.ok && m.snapGen == a.fp.snapGen && m.posGen == a.fp.posGen && m.items == first && m.n == len(snap.Items) &&
		m.now.Equal(snap.Now) && m.view == key {
		return m
	}
	rows := factoryRailRows(*snap, v)
	var walk []int
	for _, r := range rows {
		if r.kind == factoryRowItem {
			walk = append(walk, r.item)
		}
	}
	*m = factoryRowsMemo{ok: true, snapGen: a.fp.snapGen, posGen: a.fp.posGen, items: first, n: len(snap.Items), now: snap.Now, view: key, rows: rows, walk: walk}
	return m
}

// factoryNarrowed says whether anything narrows the rail that `esc` would
// clear before it leaves: words, or one repo.
func (a *app) factoryNarrowed() bool {
	return a.fp.query != "" || a.fp.typing || a.fp.repo != 0
}

// factoryFloorHas says whether the floor holds any item at all, whatever the
// view narrows it to.
func (a *app) factoryFloorHas() bool {
	for _, it := range a.fp.snap.Items {
		if factoryOnFloor(it) {
			return true
		}
	}
	return false
}

// factoryRefocus changes the view and keeps the cursor on the item it was on,
// by id, when that item is still drawn — the same promise [app.factoryFold]
// makes across a re-read.
func (a *app) factoryRefocus(change func()) {
	was, had := a.factoryCursorItem()
	change()
	a.factoryKeep(was, had)
}

// factoryKeep puts the cursor back on was, by id, and clamps it otherwise.
func (a *app) factoryKeep(was factory.Item, had bool) {
	walk := a.factoryWalkNow()
	if had {
		for at, i := range walk {
			if a.fp.snap.Items[i].ID == was.ID {
				a.fp.cursor = at
				break
			}
		}
	}
	a.fp.cursor = moveCursor(a.fp.cursor, 0, len(walk))
	a.touch()
}

// factoryClear is `esc` over a narrowed rail: the words and the repo go, and
// the cursor stays on its item.
func (a *app) factoryClear() {
	a.factoryRefocus(func() {
		a.fp.query, a.fp.typing, a.fp.repo = "", false, 0
	})
}

// factoryCycleRepo is `[` and `]`: every repo, then each repo in the order the
// floor lists them, then every repo again.
func (a *app) factoryCycleRepo(delta int) {
	n := len(a.fp.snap.Repos) + 1
	if n <= 1 {
		return
	}
	a.factoryRefocus(func() {
		a.fp.repo = ((a.fp.repo+delta)%n + n) % n
	})
}

// factoryToggleBacklog is `A`: the whole backlog under `new`, or only what is
// fresh.
func (a *app) factoryToggleBacklog() {
	a.factoryRefocus(func() { a.fp.backlog = !a.fp.backlog })
}

// factoryMarked says whether an item wears a mark: the person's own toggle
// when they made one, and the floor's own mark otherwise.
func (a *app) factoryMarked(it factory.Item) bool {
	if m, ok := a.fp.marked[it.ID]; ok {
		return m
	}
	return it.Marked
}

// factoryMark is `space`: the new item under the cursor is marked, or
// unmarked. It answers false when the cursor is not on a new item, so the
// key falls through to what a space means everywhere else.
//
// THE MARK IS THE PERSON'S, HELD ON THE PAGE, BY ID. No door on the seam
// takes a mark, and a mark written into the snapshot would be wiped by the
// next three-second read.
func (a *app) factoryMark() bool {
	it, ok := a.factoryCursorItem()
	if !ok || it.State != factory.StateNew {
		return false
	}
	if a.fp.marked == nil {
		a.fp.marked = map[int]bool{}
	}
	a.fp.marked[it.ID] = !a.factoryMarked(it)
	a.touch()
	return true
}

// factoryOpenFilter is `/`: the words box opens at the top of the rail with
// whatever was typed before still in it.
func (a *app) factoryOpenFilter() {
	a.fp.typing = true
	a.touch()
}

// factoryFilterKey is a key while the words box is open, which has the whole
// keyboard ([place.owns]): letters type, `backspace` takes one back, `enter`
// keeps the words and closes the box, `esc` throws them away, and the arrows
// still walk the narrowed list.
func (a *app) factoryFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.factoryRefocus(func() { a.fp.query, a.fp.typing = "", false })
		return nil
	case "enter":
		a.fp.typing = false
		if strings.TrimSpace(a.fp.query) == "" {
			a.fp.query = ""
		}
		a.touch()
		return nil
	case "up", "ctrl+p":
		a.factoryMove(-1)
		return nil
	case "down", "ctrl+n":
		a.factoryMove(1)
		return nil
	case "backspace":
		if a.fp.query != "" {
			_, size := utf8.DecodeLastRuneInString(a.fp.query)
			a.factoryRefocus(func() { a.fp.query = a.fp.query[:len(a.fp.query)-size] })
		}
		return nil
	case "ctrl+u":
		a.factoryRefocus(func() { a.fp.query = "" })
		return nil
	case "ctrl+k":
		// THE CARET STANDS AT THE END OF THE WORDS, so the kill to the end has
		// nothing after it to take; the key is answered, and keeps the words.
		return nil
	}
	k := msg.Key()
	if k.Text == "" || k.Mod&(tea.ModAlt|tea.ModCtrl|tea.ModMeta|tea.ModSuper) != 0 {
		return nil
	}
	a.factoryRefocus(func() { a.fp.query += k.Text })
	return nil
}

// ── drawing ─────────────────────────────────────────────────────────────────

// factoryRail is the rows' window of at most room lines, each exactly width
// cells, and the rail line each one shows (-1 for a line that holds no item).
// THE WINDOW FOLLOWS THE CURSOR ([placeTop]) and is never scrolled on its own.
func (a *app) factoryRail(width, room int) ([]string, []int) {
	a.fp.refW = factoryRefWidth(a.fp.snap)
	// THE GRID IS MEASURED BEFORE THE ROWS ARE LAID OUT, because a
	// comfortable row's second title line is a row of its own.
	a.fp.titleCols = 0
	if a.fp.comfy {
		a.fp.titleCols = a.factoryGridAt(width).titleW
	}
	rows := a.factoryRows()
	cursorLine := 0
	for line, r := range rows {
		if r.kind == factoryRowItem && r.walk == a.fp.cursor {
			cursorLine = line
			break
		}
	}
	// THE REPO LINE AND THE WORDS LINE STAY PUT when the rows do not fit: they
	// say what the list below them is narrowed to, and a window that scrolled
	// them away left a narrowed list that looked like the whole floor. They
	// are drawn first, and the window follows the cursor through the rest.
	pinned := 0
	if len(rows) > room {
		for pinned < len(rows) && (rows[pinned].kind == factoryRowStrip || rows[pinned].kind == factoryRowQuery) {
			pinned++
		}
		if pinned >= room {
			pinned = 0
		}
	}
	a.fp.pinned = pinned
	out := make([]string, 0, room)
	hits := make([]int, 0, room)
	for line := 0; line < pinned; line++ {
		out = append(out, factoryPad(a.factoryRailLine(rows[line], width), width))
		hits = append(hits, -1)
	}
	// THE WINDOW KEEPS A SECTION'S HEADING WITH ITS FIRST ROW, and the list's
	// own top with its first item (factory_page.go's [app.factoryWindowTop]).
	a.fp.top = a.factoryWindowTop(rows, cursorLine, pinned, room)
	end := min(a.fp.top+room-pinned, len(rows))
	for line := a.fp.top; line < end; line++ {
		r := rows[line]
		hit := -1
		if r.walk >= 0 {
			hit = line
		}
		text := a.factoryRailLine(r, width)
		if factoryOrphanHeading(rows, line, end) {
			text = ""
		}
		out = append(out, factoryPad(text, width))
		hits = append(hits, hit)
	}
	a.fp.shown = len(out)
	return out, hits
}

// factoryRefWidth is the ref column's cells: [factoryRefW], or the widest
// ref on the floor when one is wider, so no row's title starts a cell late.
func factoryRefWidth(snap factory.Snapshot) int {
	w := factoryRefW
	for _, it := range snap.Items {
		w = max(w, ansi.StringWidth(it.Ref()))
	}
	return w
}

// factoryTitleCol is the cell the title column starts on: after the lead, the
// ref and the one space.
func (a *app) factoryTitleCol() int {
	return factoryLeadW + factoryPriorityW + max(a.fp.refW, factoryRefW) + 1
}

// factoryRailLine draws one line of the rows, at most width cells.
func (a *app) factoryRailLine(r factoryRailRow, width int) string {
	pal := a.pal
	switch r.kind {
	case factoryRowBlank:
		return ""
	case factoryRowStrip:
		if r.heading == "" {
			return pal.dim("all repos")
		}
		word := factoryRepoShort(r.heading)
		if r.count > 0 {
			word += " · " + itoa(r.count)
		}
		return pal.muted(fit(word, width))
	case factoryRowQuery:
		words := fit("/ "+a.fp.query, max(width-1, 0))
		if a.fp.typing {
			return pal.ink(words) + pal.cursor(" ", 1)
		}
		return pal.muted(words)
	case factoryRowHeading:
		return a.factoryHeading(r, width)
	case factoryRowMore:
		word := itoa(r.count) + " older open " + plural("item", r.count) + " behind A"
		lead := a.factoryTitleCol()
		return factorySpaces(lead) + pal.dim(fit(word, max(width-lead, 0)))
	case factoryRowNone:
		return pal.dim(fit("no item on the floor matches", width))
	case factoryRowRead:
		lead := a.factoryTitleCol()
		read := strings.TrimSpace(a.fp.snap.Items[r.item].Triage.Read)
		return factorySpaces(lead) + pal.dim(fit(read, max(width-lead, 0)))
	case factoryRowTitle2:
		lead := a.factoryTitleCol()
		w := a.factoryGridAt(width).titleW
		rest := strings.Join(wrap(a.fp.snap.Items[r.item].Title, w)[1:], " ")
		if ansi.StringWidth(rest) > w {
			rest = ansi.Truncate(rest, w, a.icon(tokens.GEllipsis))
		}
		text := factoryPad(factorySpaces(lead)+a.pal.ink(rest), width)
		if r.walk == a.fp.cursor {
			return pal.cursorRow(text, width)
		}
		return text
	}
	it := a.fp.snap.Items[r.item]
	return a.factoryRailItem(it, width, r.walk == a.fp.cursor)
}

// factoryHeading is a section's heading: its name in muted capitals with its
// count, and a dim hairline out to the row's right edge. HEADINGS ARE
// FURNITURE, so they never light (THE ACCENT BUDGET).
func (a *app) factoryHeading(r factoryRailRow, width int) string {
	word := strings.ToUpper(r.heading)
	if r.count > 0 {
		word += " · " + itoa(r.count)
	}
	word = fit(word, width)
	line := a.pal.muted(word)
	if rest := width - ansi.StringWidth(word) - 1; rest > 0 {
		line += " " + a.pal.dim(strings.Repeat(a.linearMark("─", "-"), rest))
	}
	return line
}

// factoryRepoShort is a repo's last path segment: `codeaf` for
// `agentfield/codeaf`.
func factoryRepoShort(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// THE ROW'S GRID, [factoryGrid] and [app.factoryGridAt], is factory_grid.go's.

// factoryRailItem is one item row, exactly width cells, on the grid of
// [factoryGridAt]. THE CURSOR ROW WEARS THE CURSOR GROUND AND NOTHING ELSE
// CHANGES ON IT; a marked item's lead turns accent ([app.factoryLead]).
func (a *app) factoryRailItem(it factory.Item, width int, cur bool) string {
	pal := a.pal
	g := a.factoryGridAt(width)
	lead := factorySpaces(factoryLeadW)
	if m := a.factoryLead(it); m != "" {
		lead = m + " "
	}
	lead += a.factoryPrioCell(it) + " "
	ref := it.Ref()
	text := lead + factorySpaces(max(g.refW-ansi.StringWidth(ref), 0)) + a.factoryRefLink(it, pal.muted(ref)) + " "
	title := it.Title
	if a.fp.comfy {
		// A COMFORTABLE ROW CARRIES ITS TITLE'S FIRST LINE, and the rest is
		// the row under it ([factoryRowTitle2]).
		if lines := wrap(title, g.titleW); len(lines) > 0 {
			title = lines[0]
		}
	}
	if ansi.StringWidth(title) > g.titleW {
		title = ansi.Truncate(title, g.titleW, a.icon(tokens.GEllipsis))
	}
	text += pal.ink(title)
	if g.columns {
		text += factorySpaces(g.titleW-ansi.StringWidth(title)) + factorySpaces(factoryGutter)
		text += pal.muted(factoryPad(fit(factoryRepoShort(it.Repo), factoryRepoW), factoryRepoW)) + factorySpaces(factoryGutter)
		// THE RIGHT EDGE: the age in its own cells, and on a question the
		// reminder that it takes a yes or a no right before it.
		age := ""
		if now, at := a.fp.snap.Now, factoryChanged(it); !now.IsZero() && !at.IsZero() {
			age = factoryAgo(now.Sub(at))
		}
		right := factorySpaces(max(factoryAgeW-ansi.StringWidth(age), 0)) + pal.dim(age)
		rightW := factoryAgeW
		factsW := g.factsW
		if it.State == factory.StateNeedsYou {
			right = pal.dim(factoryAnswerWord) + factorySpaces(factoryGutter) + right
			rightW += ansi.StringWidth(factoryAnswerWord) + factoryGutter
			factsW -= ansi.StringWidth(factoryAnswerWord) + factoryGutter
		}
		// The `first` order's reason stands before the age, only in the cells
		// the facts leave free AT THIS WIDTH: the facts are drawn first, at
		// the room the row has, and the reason takes what they did not use
		// (factory_order.go). Measured against the facts at their widest
		// there was never room, at any width.
		facts := a.factoryFactsLine(it, factsW, g.factsN)
		reason, reasonW := a.factoryOrderReason(it, factsW-ansi.StringWidth(facts))
		right, rightW = reason+right, rightW+reasonW
		text += facts
		gap := width - ansi.StringWidth(text) - rightW
		text += factorySpaces(max(gap, 0)) + right
	}
	text = factoryPad(text, width)
	// THE CURSOR ROW WEARS THE CURSOR STEP, the one the Teams page and home's
	// lists put under the row a person is on, so the floor's cursor reads as
	// the same thing it is everywhere else (owner ruling, 2026-10-08). The
	// SELECTED step is one rung louder and means "the chosen one", which the
	// Teams page gives only to the row that is chosen, never to the cursor.
	if cur {
		return pal.cursorRow(text, width)
	}
	return text
}

// factoryPrioCell is the priority column's one cell: the four marks first to
// fourth, the first in the accent and the rest dim, and a blank for an item
// nobody ranked. AN ITEM BEING READ RIGHT NOW draws the spinner there instead
// (factory_busy.go), which is the row's one claim that it is moving; AN ITEM
// WAITING ITS TURN draws a still dim dot, because a row that is only queued
// is not moving and must not say it is (factory_marks.go).
func (a *app) factoryPrioCell(it factory.Item) string {
	if a.factoryRowSpins(it.ID) {
		return a.pal.accent(a.factorySpin())
	}
	if a.factoryRowWaits(it.ID) {
		return a.pal.dim(a.icon(tokens.GSeparator))
	}
	switch it.Triage.Priority {
	case 1:
		return a.pal.accent(a.icon(tokens.GPriorityFirst))
	case 2:
		return a.pal.dim(a.icon(tokens.GPrioritySecond))
	case 3:
		return a.pal.dim(a.icon(tokens.GPriorityThird))
	case 4, 5:
		return a.pal.dim(a.icon(tokens.GPriorityFourth))
	}
	return " "
}

// factoryRefLink is an item's painted ref as a hyperlink to its page on the
// forge, where it has one and the terminal takes links (pathlink.go's
// [terminalTakesLinks]); the label untouched otherwise. A hyperlink takes no
// cells, so the grid does not move.
func (a *app) factoryRefLink(it factory.Item, painted string) string {
	if it.URL == "" || !a.pathLinks {
		return painted
	}
	return linkify(painted, it.URL)
}

// factoryChanged is when an item last moved, and when it arrived when it
// never has: the moment a row's age counts from.
func factoryChanged(it factory.Item) time.Time {
	if !it.Changed.IsZero() {
		return it.Changed
	}
	return it.Created
}

// factoryLead is an item's one-cell mark, painted, and "" for a new item at
// rest. THE MAPPING IS factory_marks.go's ([app.itemLeadMark]), which names
// the paused and the stopped apart from the running and the new.
func (a *app) factoryLead(it factory.Item) string { return a.itemLeadMark(it) }

// ── the facts ───────────────────────────────────────────────────────────────

// factoryFactPart is one fact on a row: what it says plain, which is what it
// measures, and what it says painted.
type factoryFactPart struct {
	plain, painted string
}

// factoryFactsLine is a row's facts in at most width cells, joined by a dim
// middle dot. FACTS ARE DROPPED FROM THE RIGHT, WHOLE, AS THE WIDTH SHRINKS,
// never cut mid-fact: the state fact first, then the money, then the author,
// then the tags. The one exception is a state fact wider than the whole
// column on its own, which is cut with an ellipsis, because it is the one fact
// a row exists to carry.
func (a *app) factoryFactsLine(it factory.Item, width, most int) string {
	if width <= 0 {
		return ""
	}
	parts := a.factoryRowFacts(it)
	// THE FACTS DROP AS A COLUMN: the grid says how many fact columns the
	// floor carries at this width ([factoryGrid].factsN), and no row draws
	// one more, so one column never says a fact on one row and nothing on
	// the next.
	if most < 0 {
		return ""
	}
	if most > 0 && len(parts) > most {
		parts = parts[:most]
	}
	sep := a.pal.dim(rowSep)
	sepW := ansi.StringWidth(rowSep)
	out, used := "", 0
	for i, p := range parts {
		w := ansi.StringWidth(p.plain)
		if i == 0 {
			if w > width {
				return fit(p.painted, width)
			}
			out, used = p.painted, w
			continue
		}
		if used+sepW+w > width {
			break
		}
		out += sep + p.painted
		used += sepW + w
	}
	return out
}

// factoryRowFacts is an item's facts in rank order, at most [factoryFactsMost]
// of them, and none that would say nothing (the emptiness law: no `$0.00`, no
// `0✓`).
//
//	1  the state fact   the question · ●●◐○○ review 26m · queued · 3✓ 1✕ · bug · S · ci red
//	2  the money        $1.42/$5 spent against the cap, or ~$3 estimated
//	3  the author       priya, or olu (stranger)
//	4  the tags         factory · thin · dup #950? · from chat ▸ · terminal only
func (a *app) factoryRowFacts(it factory.Item) []factoryFactPart {
	pal := a.pal
	var out []factoryFactPart
	add := func(plain string, paint func(string) string) {
		if strings.TrimSpace(plain) != "" {
			out = append(out, factoryFactPart{plain: plain, painted: paint(plain)})
		}
	}
	if st, ok := a.factoryStateFact(it); ok {
		out = append(out, st)
	}
	// AN ESTIMATE IS A GUESS AND WEARS THE FACTS' MUTED TONE; money's green
	// is kept for what a stream actually spent, so the colour is not spent
	// on a guess on every row.
	money := placeMoneyInk(pal)
	if m := factoryMoneyFact(it); strings.HasPrefix(m, "~") {
		money = pal.muted
	}
	add(factoryMoneyFact(it), money)
	if it.Author != "" {
		who := it.Author
		if it.Tier == factory.TierStranger {
			who += " (" + string(factory.TierStranger) + ")"
		}
		add(who, pal.dim)
	}
	for _, l := range it.Labels {
		if strings.EqualFold(l, "factory") {
			add("factory", pal.accent)
			break
		}
	}
	if r := it.Triage.Readiness; r > 0 && r < factory.ThinReadiness {
		add("thin", pal.ask)
	}
	if it.Triage.DupOf > 0 {
		add("dup #"+itoa(it.Triage.DupOf)+"?", pal.dim)
	}
	switch {
	case it.Origin == factory.OriginChat:
		add("from chat "+a.linearMark("▸", ">"), pal.accent)
	case it.Origin == factory.OriginTerminal && !it.Synced:
		add("terminal only", pal.dim)
	}
	if len(out) > factoryFactsMost {
		out = out[:factoryFactsMost]
	}
	// WORK IN FLIGHT IS THE LAST FACT, so it is the first a narrow row drops:
	// the spinner in the priority cell still says it. AND WHILE IT IS IN
	// FLIGHT IT STANDS RIGHT AFTER THE STATE FACT, in place of the money, the
	// author and the tags, which the read in flight is about to say again: at
	// the floor's widest the facts column is already full with them, and a
	// flight fact queued behind three others never fit at any width.
	if word := a.factoryFlightFact(it.ID); word != "" {
		if len(out) > 1 {
			out = out[:1]
		}
		out = append(out, factoryFactPart{plain: word, painted: pal.dim(word)})
	}
	return out
}

// factoryStateFact is the one fact a row's state is about, and false when the
// state has nothing to say.
func (a *app) factoryStateFact(it factory.Item) (factoryFactPart, bool) {
	pal := a.pal
	one := func(plain string, paint func(string) string) (factoryFactPart, bool) {
		if strings.TrimSpace(plain) == "" {
			return factoryFactPart{}, false
		}
		return factoryFactPart{plain: plain, painted: paint(plain)}, true
	}
	now := a.fp.snap.Now
	switch it.State {
	case factory.StateNeedsYou:
		return one(strings.TrimSpace(it.Question), pal.ask)
	case factory.StateRunning:
		s := it.Stream
		if s == nil {
			return factoryFactPart{}, false
		}
		plain, painted := a.factoryStripCells(it)
		word := ""
		if s.Cur >= 0 && s.Cur < len(s.Phases) {
			word = s.Phases[s.Cur].Name
		}
		if !s.Started.IsZero() && !now.IsZero() && now.After(s.Started) {
			word = strings.TrimSpace(word + " " + factoryAgo(now.Sub(s.Started)))
		}
		// A HELD ITEM SAYS HOW LONG IT HAS BEEN HELD, never how long it ran:
		// `paused 24m` beside a stream that started 24 minutes ago read as a
		// hold of 24 minutes (factory_marks.go).
		if factoryPaused(it) {
			word = a.factoryPausedFor(it)
		}
		if word != "" {
			if plain != "" {
				plain, painted = plain+" ", painted+" "
			}
			plain, painted = plain+word, painted+pal.muted(word)
		}
		return factoryFactPart{plain: plain, painted: painted}, plain != ""
	case factory.StateQueued:
		return one("queued", pal.muted)
	case factory.StateNew:
		// A STOPPED ITEM SAYS IT WAS STOPPED, never what its last phase said
		// as it was stopped (factory_marks.go).
		if factoryStopped(it) {
			return one(factoryStoppedWords, pal.muted)
		}
		switch it.Kind {
		case factory.KindCI:
			return one("ci red", pal.bad)
		case factory.KindPR:
			return one(strings.Join(nonEmpty([]string{"pr", it.Checks}), rowSep), pal.muted)
		}
		return one(strings.Join(nonEmpty([]string{it.Triage.Type, it.Triage.Size}), rowSep), pal.muted)
	case factory.StateLanded:
		shown, not := 0, 0
		for _, c := range it.Proof {
			if c.OK {
				shown++
			} else {
				not++
			}
		}
		var plain, painted []string
		if shown > 0 {
			w := itoa(shown) + a.icon(tokens.GSettled)
			plain, painted = append(plain, w), append(painted, pal.muted(w))
		}
		if not > 0 {
			w := itoa(not) + a.icon(tokens.GFailed)
			plain, painted = append(plain, w), append(painted, pal.bad(w))
		}
		return factoryFactPart{plain: strings.Join(plain, " "), painted: strings.Join(painted, " ")}, len(plain) > 0
	case factory.StateShipped:
		at := it.Changed
		if it.Stream != nil && !it.Stream.Ended.IsZero() {
			at = it.Stream.Ended
		}
		if at.IsZero() {
			return factoryFactPart{}, false
		}
		word := at.Format("15:04")
		if !now.IsZero() {
			at = at.In(now.Location())
			word = at.Format("15:04")
			if now.Sub(at) >= 24*time.Hour {
				word = at.Format("2 Jan")
			}
		}
		return one("shipped "+word, pal.muted)
	}
	return factoryFactPart{}, false
}

// factoryStripCells is a stream's phases as one cell each, plain and painted,
// each the mark factory_marks.go's [app.phaseMark] gives it: done muted, the
// running one in accent, a stage waiting on the person in the asking colour,
// a failed one in red, a held one the pause mark, a stopped one the stop
// square, one the run went past the skip stroke, the rest the pending ring.
// An item with no stream has no strip.
func (a *app) factoryStripCells(it factory.Item) (string, string) {
	if it.Stream == nil {
		return "", ""
	}
	var plain, painted strings.Builder
	for i := range it.Stream.Phases {
		mark, paint := a.phaseMark(it, i)
		plain.WriteString(mark)
		painted.WriteString(paint(mark))
	}
	return plain.String(), painted.String()
}

// factoryPhaseMark is one phase STATE's mark and its paint, for a drawing
// that has a state and no item: a stage not yet run on the recipe page, the
// pending ring an off stage used to wear. A PHASE OF AN ITEM'S STREAM IS DRAWN
// THROUGH [app.phaseMark], which also knows held, stopped and passed over.
func (a *app) factoryPhaseMark(st factory.PhaseState) (string, func(string) string) {
	switch st {
	case factory.PhaseDone:
		return a.factoryKindMark(factoryMarkDone)
	case factory.PhaseRunning:
		return a.factoryKindMark(factoryMarkRunning)
	case factory.PhaseWaiting:
		return a.factoryKindMark(factoryMarkWaiting)
	case factory.PhaseFailed:
		return a.factoryKindMark(factoryMarkFailed)
	}
	return a.factoryKindMark(factoryMarkPending)
}

// factoryMoneyFact is the row's money: what a stream spent over its cap when
// it has spent anything, and otherwise the triage's estimate with a tilde.
func factoryMoneyFact(it factory.Item) string {
	if s := it.Stream; s != nil && s.Spent > 0 {
		spent := dollars(s.Spent)
		if c := factoryMoney(it.Cap); c != "" {
			return spent + "/" + c
		}
		return spent
	}
	if est := factoryMoney(it.Triage.Est); est != "" {
		return "~" + est
	}
	return ""
}

// factoryAgo is a wait in the rail's one-unit spelling: `now`, `12m`, `7h`,
// `3d`.
func factoryAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	}
	return itoa(int(d/(24*time.Hour))) + "d"
}
