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

// THE ROW'S GRID. A row is a fixed set of columns so the eye scans down them:
// a 2-cell lead, the ref right-aligned in [factoryRefCols] (wider when the
// floor holds a longer ref), one space, the title in a column whose width is
// what the others leave and never under [factoryTitleMin], two spaces, the
// repo's short name in [factoryRepoCols], two spaces, the facts, and the age
// right-aligned in the last [factoryAgeCols]. Under [factoryFactsFloor] the
// row is the lead, the ref and the title alone.
const (
	factoryLeadCols  = 2
	factoryRefCols   = 5
	factoryTitleMin  = 24
	factoryRepoCols  = 11
	factoryAgeCols   = 4
	factoryFactsMost = 4
)

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
)

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
	v := factoryView{query: a.fp.query, typing: a.fp.typing, backlog: a.fp.backlog, comfy: a.fp.comfy}
	if r := a.fp.repo; r > 0 && r <= len(a.fp.snap.Repos) {
		v.repo = a.fp.snap.Repos[r-1].Name
	}
	return v
}

// factoryRows is the rail's rows as the page stands now. EVERY CURSOR, LINE
// AND PRESS ON THE PAGE READS THESE, never the bare [factoryRailRows], so a
// narrowed rail and the walk the cursor counts in are the same list.
func (a *app) factoryRows() []factoryRailRow {
	return factoryRailRows(a.fp.snap, a.factoryViewNow())
}

// factoryWalkNow is [factoryWalk] under the page's own view.
func (a *app) factoryWalkNow() []int {
	return factoryWalk(a.fp.snap, a.factoryViewNow())
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
	rows := a.factoryRows()
	a.fp.refW = factoryRefWidth(a.fp.snap)
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
	a.fp.top = pinned + placeTop(a.fp.top-pinned, max(cursorLine-pinned, 0), len(rows)-pinned, room-pinned)
	end := min(a.fp.top+room-pinned, len(rows))
	for line := a.fp.top; line < end; line++ {
		r := rows[line]
		hit := -1
		if r.walk >= 0 {
			hit = line
		}
		out = append(out, factoryPad(a.factoryRailLine(r, width), width))
		hits = append(hits, hit)
	}
	a.fp.shown = len(out)
	return out, hits
}

// factoryRefWidth is the ref column's cells: [factoryRefCols], or the widest
// ref on the floor when one is wider, so no row's title starts a cell late.
func factoryRefWidth(snap factory.Snapshot) int {
	w := factoryRefCols
	for _, it := range snap.Items {
		w = max(w, ansi.StringWidth(it.Ref()))
	}
	return w
}

// factoryTitleCol is the cell the title column starts on: after the lead, the
// ref and the one space.
func (a *app) factoryTitleCol() int {
	return factoryLeadCols + max(a.fp.refW, factoryRefCols) + 1
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
		return strings.Repeat(" ", lead) + pal.dim(fit(word, max(width-lead, 0)))
	case factoryRowNone:
		return pal.dim(fit("no item on the floor matches", width))
	case factoryRowRead:
		lead := a.factoryTitleCol()
		read := strings.TrimSpace(a.fp.snap.Items[r.item].Triage.Read)
		return strings.Repeat(" ", lead) + pal.dim(fit(read, max(width-lead, 0)))
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

// factoryGrid is one row's columns at a width: the ref's cells, the title's,
// and the facts' (0 when the row carries none).
type factoryGrid struct {
	refW, titleW, factsW int
	columns              bool
}

// factoryGridAt lays the grid out for a row of width cells. With columns the
// title and the facts split what the fixed columns leave, the title taking the
// larger half and never under [factoryTitleMin]; without, the title takes
// everything after the ref.
func (a *app) factoryGridAt(width int) factoryGrid {
	g := factoryGrid{refW: max(a.fp.refW, factoryRefCols), columns: a.fp.columns}
	head := factoryLeadCols + g.refW + 1
	if !g.columns {
		g.titleW = max(width-head, 0)
		return g
	}
	rest := width - head - 2 - factoryRepoCols - 2 - 2 - factoryAgeCols
	g.titleW = max(factoryTitleMin, (rest+1)/2)
	g.factsW = max(rest-g.titleW, 0)
	return g
}

// factoryRailItem is one item row, exactly width cells, on the grid of
// [factoryGridAt]. THE CURSOR ROW WEARS THE CURSOR GROUND AND NOTHING ELSE
// CHANGES ON IT; a marked item's lead turns accent ([app.factoryLead]).
func (a *app) factoryRailItem(it factory.Item, width int, cur bool) string {
	pal := a.pal
	g := a.factoryGridAt(width)
	lead := strings.Repeat(" ", factoryLeadCols)
	if m := a.factoryLead(it); m != "" {
		lead = m + " "
	}
	ref := it.Ref()
	text := lead + strings.Repeat(" ", max(g.refW-ansi.StringWidth(ref), 0)) + pal.muted(ref) + " "
	title := it.Title
	if ansi.StringWidth(title) > g.titleW {
		title = ansi.Truncate(title, g.titleW, a.icon(tokens.GEllipsis))
	}
	text += pal.ink(title)
	if g.columns {
		text += strings.Repeat(" ", max(g.titleW-ansi.StringWidth(title), 0)) + "  "
		text += pal.muted(factoryPad(fit(factoryRepoShort(it.Repo), factoryRepoCols), factoryRepoCols)) + "  "
		// THE RIGHT EDGE: the age in its own cells, and on a question the
		// reminder that it takes a yes or a no right before it.
		age := ""
		if now, at := a.fp.snap.Now, factoryChanged(it); !now.IsZero() && !at.IsZero() {
			age = factoryAgo(now.Sub(at))
		}
		right := strings.Repeat(" ", max(factoryAgeCols-ansi.StringWidth(age), 0)) + pal.dim(age)
		rightW := factoryAgeCols
		factsW := g.factsW
		if it.State == factory.StateNeedsYou {
			right = pal.dim(factoryAnswerWord) + "  " + right
			rightW += ansi.StringWidth(factoryAnswerWord) + 2
			factsW -= ansi.StringWidth(factoryAnswerWord) + 2
		}
		facts := a.factoryFactsLine(it, factsW)
		text += facts
		gap := width - ansi.StringWidth(text) - rightW
		text += strings.Repeat(" ", max(gap, 0)) + right
	}
	text = factoryPad(text, width)
	if cur {
		return pal.cursor(text, width)
	}
	return text
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
// rest, which draws nothing in its lead.
//
//	needs you  ? in the asking colour
//	running    the working mark in accent
//	queued     the queued ring, dim
//	new        nothing; a red CI run wears the failed cross
//	landed     the settled check
//	shipped    a dim dot
//	marked     the dot, or the state's own mark, in accent
func (a *app) factoryLead(it factory.Item) string {
	pal := a.pal
	glyph, paint := "", pal.muted
	switch it.State {
	case factory.StateNeedsYou:
		glyph, paint = a.icon(tokens.GNeedsHuman), pal.ask
	case factory.StateRunning:
		glyph, paint = a.icon(tokens.GWorking), pal.accent
	case factory.StateQueued:
		glyph, paint = a.icon(tokens.GQueued), pal.dim
	case factory.StateNew:
		if it.Kind == factory.KindCI {
			glyph, paint = a.icon(tokens.GFailed), pal.bad
		}
	case factory.StateLanded:
		glyph = a.icon(tokens.GSettled)
	case factory.StateShipped:
		glyph, paint = a.icon(tokens.GDoneCell), pal.dim
	}
	if a.factoryMarked(it) {
		if glyph == "" {
			glyph = a.icon(tokens.GDoneCell)
		}
		paint = pal.accent
	}
	if glyph == "" {
		return ""
	}
	return paint(glyph)
}

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
func (a *app) factoryFactsLine(it factory.Item, width int) string {
	if width <= 0 {
		return ""
	}
	parts := a.factoryRowFacts(it)
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
	add(factoryMoneyFact(it), placeMoneyInk(pal))
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
		if s.Paused {
			word = "paused"
		} else if s.Cur >= 0 && s.Cur < len(s.Phases) {
			word = s.Phases[s.Cur].Name
		}
		if !s.Started.IsZero() && !now.IsZero() && now.After(s.Started) {
			word = strings.TrimSpace(word + " " + factoryAgo(now.Sub(s.Started)))
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

// factoryStripCells is a stream's phases as one cell each, plain and painted:
// done `●` muted, the running one `◐` in accent, a stage waiting on the person
// `?` in the asking colour, a failed one `✕` in red, the rest `○` dim. An item
// with no stream has no strip.
func (a *app) factoryStripCells(it factory.Item) (string, string) {
	if it.Stream == nil {
		return "", ""
	}
	var plain, painted strings.Builder
	for _, ph := range it.Stream.Phases {
		mark, paint := a.factoryPhaseMark(ph.State)
		plain.WriteString(mark)
		painted.WriteString(paint(mark))
	}
	return plain.String(), painted.String()
}

// factoryPhaseMark is one phase state's mark and its paint, the one mapping the
// row's strip, the peek's strip and the item page's stage rail all draw with.
func (a *app) factoryPhaseMark(st factory.PhaseState) (string, func(string) string) {
	pal := a.pal
	switch st {
	case factory.PhaseDone:
		return a.icon(tokens.GStepDone), pal.muted
	case factory.PhaseRunning:
		return a.icon(tokens.GStepRunning), pal.accent
	case factory.PhaseWaiting:
		return a.icon(tokens.GNeedsHuman), pal.ask
	case factory.PhaseFailed:
		return a.icon(tokens.GFailed), pal.bad
	}
	return a.icon(tokens.GStepPending), pal.dim
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
