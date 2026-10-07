package tui3

import (
	"fmt"
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
// The left column of the factory page: the floor as a list, grouped by where
// each item stands, one compact row per item. factory_page.go owns the page's
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

// factoryFactFloor is the narrowest rail that still carries the right-hand
// fact on an item row. Under it the title gets the cells instead, because a
// title cut to three letters to make room for `review · $1.42` says nothing.
const factoryFactFloor = 30

// factoryView is every narrowing of the floor at once: the repo (its name, ""
// for all of them), the typed words, whether the words box is open, and
// whether the whole backlog is shown.
type factoryView struct {
	repo    string
	query   string
	typing  bool
	backlog bool
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
		for _, i := range in {
			rows = append(rows, factoryRailRow{kind: factoryRowItem, item: i, walk: walk})
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
	v := factoryView{query: a.fp.query, typing: a.fp.typing, backlog: a.fp.backlog}
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
	}
	k := msg.Key()
	if k.Text == "" || k.Mod&(tea.ModAlt|tea.ModCtrl|tea.ModMeta|tea.ModSuper) != 0 {
		return nil
	}
	a.factoryRefocus(func() { a.fp.query += k.Text })
	return nil
}

// ── drawing ─────────────────────────────────────────────────────────────────

// factoryRail is the rail's window of at most room lines, each exactly width
// cells, and the rail line each one shows (-1 for anything but an item). THE
// WINDOW FOLLOWS THE CURSOR ([placeTop]) and is never scrolled on its own.
func (a *app) factoryRail(width, room int) ([]string, []int) {
	rows := a.factoryRows()
	cursorLine := 0
	for line, r := range rows {
		if r.kind == factoryRowItem && r.walk == a.fp.cursor {
			cursorLine = line
			break
		}
	}
	a.fp.top = placeTop(a.fp.top, cursorLine, len(rows), room)
	end := min(a.fp.top+room, len(rows))
	out := make([]string, 0, max(end-a.fp.top, 0))
	hits := make([]int, 0, max(end-a.fp.top, 0))
	for line := a.fp.top; line < end; line++ {
		r := rows[line]
		hit := -1
		if r.kind == factoryRowItem {
			hit = line
		}
		out = append(out, factoryPad(a.factoryRailLine(r, width), width))
		hits = append(hits, hit)
	}
	a.fp.shown = len(out)
	return out, hits
}

// factoryRailLine draws one rail row, at most width cells.
func (a *app) factoryRailLine(r factoryRailRow, width int) string {
	pal := a.pal
	switch r.kind {
	case factoryRowBlank:
		return ""
	case factoryRowStrip:
		if r.heading == "" {
			return " " + pal.muted("all repos")
		}
		word := factoryRepoShort(r.heading)
		if r.count > 0 {
			word += " · " + itoa(r.count)
		}
		return " " + pal.muted(fit(word, max(width-1, 0)))
	case factoryRowQuery:
		words := fit("/ "+a.fp.query, max(width-2, 0))
		if a.fp.typing {
			return " " + pal.ink(words) + pal.cursor(" ", 1)
		}
		return " " + pal.muted(words)
	case factoryRowHeading:
		word := r.heading
		if r.count > 0 {
			word += " · " + itoa(r.count)
		}
		return " " + pal.muted(word)
	case factoryRowMore:
		word := itoa(r.count) + " older open " + plural("item", r.count) + " behind A"
		return "   " + pal.dim(fit(word, max(width-3, 0)))
	case factoryRowNone:
		return " " + pal.dim(fit("no item on the floor matches", max(width-1, 0)))
	}
	it := a.fp.snap.Items[r.item]
	return a.factoryRailItem(it, width, r.walk == a.fp.cursor)
}

// factoryRepoShort is a repo's last path segment: `codeaf` for
// `agentfield/codeaf`.
func factoryRepoShort(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// factoryRailItem is one item row, exactly width cells: a 2-cell lead, the
// ref, the title cut with an ellipsis, and the one fact its state is about at
// the right edge. THE CURSOR ROW WEARS THE CURSOR GROUND AND NOTHING ELSE
// CHANGES ON IT; a marked item's lead turns accent.
func (a *app) factoryRailItem(it factory.Item, width int, cur bool) string {
	pal := a.pal
	lead := "   "
	if g := a.factoryLead(it); g != "" {
		lead = " " + g + " "
	}
	ref := it.Ref()
	fact := ""
	if width >= factoryFactFloor {
		fact = a.factoryFact(it)
	}
	factW := ansi.StringWidth(fact)
	room := width - 3 - ansi.StringWidth(ref) - 2
	if fact != "" {
		room -= factW + 2
	}
	title := it.Title
	if room < 1 {
		title = ""
	} else if ansi.StringWidth(title) > room {
		title = ansi.Truncate(title, room, a.icon(tokens.GEllipsis))
	}
	text := lead + pal.muted(ref)
	if title != "" {
		text += " " + pal.ink(title)
	}
	if fact != "" {
		gap := width - ansi.StringWidth(text) - factW - 1
		text += strings.Repeat(" ", max(gap, 1)) + pal.muted(fact)
	}
	text = factoryPad(text, width)
	if cur {
		return pal.cursor(text, width)
	}
	return text
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

// factoryFact is the one fact on the right of an item row, plain, and "" when
// the state has nothing to say (the emptiness law: no `$0.00`, no `0✓`).
//
//	needs you  how long it has waited          7h
//	running    the phase it is in and its spend review · $1.42
//	queued     the word                        queued
//	new        its triage type and size        bug M · pr · ci
//	landed     the proof, shown and not        3✓ 1✕
//	shipped    the time it shipped             06:00
func (a *app) factoryFact(it factory.Item) string {
	now := a.fp.snap.Now
	switch it.State {
	case factory.StateNeedsYou:
		if now.IsZero() || it.Changed.IsZero() {
			return ""
		}
		return factoryAgo(now.Sub(it.Changed))
	case factory.StateRunning:
		s := it.Stream
		if s == nil {
			return ""
		}
		var parts []string
		if s.Cur >= 0 && s.Cur < len(s.Phases) && s.Phases[s.Cur].Name != "" {
			parts = append(parts, s.Phases[s.Cur].Name)
		}
		if s.Spent > 0 {
			parts = append(parts, fmt.Sprintf("$%.2f", s.Spent))
		}
		return strings.Join(parts, " · ")
	case factory.StateQueued:
		return "queued"
	case factory.StateNew:
		switch it.Kind {
		case factory.KindCI:
			return "ci"
		case factory.KindPR:
			return "pr"
		}
		return strings.TrimSpace(it.Triage.Type + " " + it.Triage.Size)
	case factory.StateLanded:
		shown, not := 0, 0
		for _, c := range it.Proof {
			if c.OK {
				shown++
			} else {
				not++
			}
		}
		var parts []string
		if shown > 0 {
			parts = append(parts, itoa(shown)+a.icon(tokens.GSettled))
		}
		if not > 0 {
			parts = append(parts, itoa(not)+a.icon(tokens.GFailed))
		}
		return strings.Join(parts, " ")
	case factory.StateShipped:
		at := it.Changed
		if it.Stream != nil && !it.Stream.Ended.IsZero() {
			at = it.Stream.Ended
		}
		if at.IsZero() {
			return ""
		}
		if !now.IsZero() {
			at = at.In(now.Location())
			if now.Sub(at) >= 24*time.Hour {
				return at.Format("2 Jan")
			}
		}
		return at.Format("15:04")
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
