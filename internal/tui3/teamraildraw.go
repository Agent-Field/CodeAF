package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── DRAWING THE RAIL (teamrail.go says what it is) ─────────────────────────

// trafficBody is the rail's rows for team t, height tall and width wide: the
// header, and under it the threads, the newest at the top, as many as fit. It
// records each row's doors, and keeps what it drew for the next frame.
// Frame-safe: the cache only.
func (a *app) trafficBody(t team, height, width int) ([]string, [][]trafficDoor, []string, hudSpan) {
	rows := a.traffic.rows[t.ID]
	last := ""
	if len(rows) > 0 {
		last = rows[len(rows)-1].ID
	}
	hot, hotDoor := -1, -1
	if a.hot.kind == hoverTraffic {
		hot, hotDoor = a.hot.index, a.hot.entry
	}
	key := trafficCacheKey{
		team: t.ID, last: last, seen: a.traffic.seen[t.ID], rows: len(rows),
		width: width, height: height, hot: hot, hotDoor: hotDoor, open: a.traffic.opened, tasks: a.trafficTaskCount(),
		hideHot: a.hot.kind == hoverTrafficHide, tabHot: a.hot.kind == hoverTrafficTab, ascii: a.pal.ascii, minute: a.now().Unix() / 60,
	}
	if c := &a.traffic.cache; c.out != nil && c.key == key {
		return c.out, c.doors, c.hints, c.hide
	}
	out := make([]string, height)
	doors := make([][]trafficDoor, height)
	hints := make([]string, height)
	var hide, tab hudSpan
	if height <= 0 || width <= 0 {
		return out, doors, hints, hide
	}
	blank := strings.Repeat(" ", width)
	for i := range out {
		out[i] = blank
	}
	// THE HEADER SAYS WHAT THE COLUMN IS AND HOW TO PUT IT AWAY, in the shape
	// the task column's own foot says it (`ctrl+g hide`): the name in ink, the
	// way out dim at the right, and the way out is a word a hand can press.
	hideWord := "hide " + trafficKey
	head := a.pal.bold(a.pal.ink(trafficWord))
	headW := len(trafficWord)
	// AND THE MANAGER'S OWN TASKS ARE THE OTHER WORD, only while it has live
	// ones: `Traffic · Tasks 2`, the word a press or ctrl+g takes to lay them
	// in this column (teamrail.go).
	if key.tasks > 0 {
		word := trafficTasksWord + " " + itoa(key.tasks)
		if headW+ansi.StringWidth(trafficTabSep)+ansi.StringWidth(word)+2+ansi.StringWidth(hideWord) <= width {
			painted := a.pal.dim(word)
			if key.tabHot {
				painted = a.pal.cursor(a.pal.ink(word), 0)
			}
			from := headW + ansi.StringWidth(trafficTabSep)
			head += a.pal.dim(trafficTabSep) + painted
			tab = hudSpan{from: from, to: from + ansi.StringWidth(word)}
			headW = tab.to
			hints[0] = "Show the manager's tasks here" + hintSegment + railStowKey
		}
	}
	if gap := width - headW - ansi.StringWidth(hideWord); gap >= 2 {
		word := a.pal.dim(hideWord)
		if key.hideHot {
			word = a.pal.cursor(a.pal.ink(hideWord), 0)
		}
		head += strings.Repeat(" ", gap) + word
		hide = hudSpan{from: width - ansi.StringWidth(hideWord), to: width}
		if key.hideHot || hints[0] == "" {
			hints[0] = "Hide the traffic" + hintSegment + trafficKey
		}
	}
	out[0] = fit(head, width)
	// THE NEWEST THREAD IS AT THE TOP, straight under the header, and the
	// older ones run down from it; what does not fit falls off the bottom.
	// Nothing is pushed down to leave room above it.
	laid := a.trafficSheetOf(t, height-1, width, 1)
	if len(laid) == 0 {
		if height > 2 {
			quiet := fit(a.pal.dim("nothing yet"), width)
			out[1] = quiet + strings.Repeat(" ", max(width-ansi.StringWidth(quiet), 0))
		}
	}
	for i, r := range laid {
		out[1+i], doors[1+i] = r.text, r.doors
	}
	a.traffic.cache = trafficCache{key: key, out: out, doors: doors, hints: hints, hide: hide, tab: tab}
	return out, doors, hints, hide
}

// trafficRows is the rail's column beside the body, height rows exactly
// [app.trafficWidth] wide: the seam and the rows, or the edge. nil when the
// manager is not in front. It records what it drew for the pointer.
func (a *app) trafficRows(height int) []string {
	t, _ := a.teamFrontManaged()
	cols := a.trafficWidth()
	if cols == 0 || height <= 0 {
		if !a.trafficOverShowing() {
			a.traffic.drawn = trafficDrawn{}
		}
		return nil
	}
	width, _ := a.size()
	if cols == trafficGripCols {
		out := a.trafficEdgeRows(t, height)
		if !a.trafficOverShowing() {
			a.traffic.drawn = trafficDrawn{mode: trafficEdge, x0: width - cols, x1: width, y0: 0, y1: height}
		}
		return out
	}
	seamW := ansi.StringWidth(railSeam)
	body, doors, hints, hide := a.trafficBody(t, height, cols-seamW)
	a.trafficMarkSeen(t)
	left := width - cols + seamW
	a.traffic.drawn = trafficDrawn{
		mode: trafficColumn, x0: width - cols, x1: width, y0: 0, y1: height,
		doors: trafficDoorsAt(doors, left), hints: hints, hide: hudSpan{from: hide.from + left, to: hide.to + left},
	}
	if tab := a.traffic.cache.tab; tab.pressable() {
		a.traffic.drawn.tab = hudSpan{from: tab.from + left, to: tab.to + left}
	}
	if !hide.pressable() {
		a.traffic.drawn.hide = hudSpan{}
	}
	seam := a.pal.dim(railSeam)
	out := make([]string, len(body))
	for i := range body {
		out[i] = seam + body[i]
	}
	return out
}

// trafficEdgeRows is the rail put away, or a frame too narrow for it: the word
// `Traffic` down the edge at its middle, and under it how many entries came in
// since the person last had them in front of them. Every row answers a press,
// so the whole edge lights under the pointer.
func (a *app) trafficEdgeRows(t team, height int) []string {
	blank := strings.Repeat(" ", trafficGripCols)
	out := make([]string, height)
	for i := range out {
		out[i] = blank
	}
	hot := a.hot.kind == hoverTrafficGrip || a.trafficOverShowing()
	paint := func(s string) string {
		if hot {
			return a.pal.cursor(" "+a.pal.ink(s), trafficGripCols)
		}
		return " " + a.pal.ink(s)
	}
	word := []rune(trafficWord)
	if height < len(word)+2 {
		out[height/2] = paint(string(word[0]))
		return out
	}
	top := (height - len(word) - 2) / 2
	for i, r := range word {
		out[top+i] = paint(string(r))
	}
	if n := a.trafficUnseen(t); n > 0 {
		count := itoa(min(n, 99))
		if len(count) == 1 {
			count = " " + count
		}
		out[top+len(word)+1] = a.pal.dim(count)
	}
	if hot {
		for i := range out {
			if out[i] == blank {
				out[i] = a.pal.cursor(blank, trafficGripCols)
			}
		}
	}
	return out
}

// trafficBeside joins the rail's column onto the task column's rows for the
// body region, so [app.railJoin] lays both beside the conversation: the task
// column's row padded to its own width, then the traffic's. With no traffic
// column it hands the task column back as it was.
func (a *app) trafficBeside(rail []string, height int) []string {
	traffic := a.trafficRows(height)
	if traffic == nil {
		return rail
	}
	cols := a.railWidth()
	out := make([]string, height)
	for i := range out {
		task := ""
		if i < len(rail) {
			task = rail[i]
		}
		if cols > 0 {
			if w := ansi.StringWidth(task); w < cols {
				task += strings.Repeat(" ", cols-w)
			}
		}
		out[i] = task + traffic[i]
	}
	return out
}

// trafficOverBody lays the card over the lower part of the body on a narrow
// frame: a bordered, grounded card [trafficCardShare] of the region tall, set
// in from the sides, titled `Traffic` with `Close esc` in its foot. The rows
// above it are the conversation's, untouched, so the manager's last words are
// still on screen. It hands back the body at the region's height and no slack.
func (a *app) trafficOverBody(body []row, pad, view int) ([]row, int) {
	t, ok := a.teamFrontManaged()
	width := a.bodyWidth()
	h := max(view*trafficCardShare/100, min(8, view))
	inset := 1
	if width >= 60 {
		inset = 2
	}
	w := width - 2*inset
	if !ok || h < 4 || w < 20 {
		return body, pad
	}
	top := view - h
	pal, ground := a.pal.hopSurfacePalette()
	surface := func(s string, n int) string { return pal.background(s, n, ground) }
	inner := frameInner(w) - 2
	rows, doors := a.trafficCardRows(t, h-2, inner, top+1)
	a.trafficMarkSeen(t)
	painted := make([]string, len(rows))
	for i, r := range rows {
		painted[i] = surface(" "+r+" ", inner+2)
	}
	closeWord := pal.dim("Close") + " " + pal.ink("esc")
	if a.hot.kind == hoverTrafficClose {
		closeWord = pal.cursor(pal.ink("Close esc"), 0)
	}
	card, span := framed{title: pal.ink(trafficWord), keysAside: closeWord, ground: surface}.draw(pal, w, painted)
	out := make([]row, view)
	for i := range out {
		switch {
		case i >= top && i-top < len(card):
			out[i] = row{text: strings.Repeat(" ", inset) + card[i-top], entry: -1}
		case i < len(body):
			out[i] = body[i]
		default:
			out[i] = row{entry: -1}
		}
	}
	d := trafficDrawn{mode: trafficCard, x0: inset, x1: inset + w, y0: top, y1: top + h,
		doors: make([][]trafficDoor, view), hints: make([]string, view), closeY: top + h - 1}
	// The card's rows start a border and a space in from its edge.
	left := inset + (w-inner)/2
	for i := range doors {
		if top+1+i < view {
			d.doors[top+1+i] = trafficDoorsShift(doors[i], left)
		}
	}
	if span.pressable() {
		d.close = hudSpan{from: inset + span.from, to: inset + span.to}
		d.hints[d.closeY] = "Close the traffic" + hintSegment + "esc"
	}
	a.traffic.drawn = d
	return out, 0
}

// trafficCardRows is the card's rows: the threads, the newest at the top,
// with no header of its own, because the card's edge is the header. first is
// the body row the first of them lands on, which is what the pointer holds.
func (a *app) trafficCardRows(t team, height, width, first int) ([]string, [][]trafficDoor) {
	out := make([]string, height)
	doors := make([][]trafficDoor, height)
	blank := strings.Repeat(" ", width)
	for i := range out {
		out[i] = blank
	}
	laid := a.trafficSheetOf(t, height, width, first)
	if len(laid) == 0 {
		out[0] = fit(a.pal.dim("nothing yet"), width)
		return out, doors
	}
	for i, r := range laid {
		out[i], doors[i] = r.text, r.doors
	}
	return out, doors
}

// trafficDoorsAt is every row's doors moved from the rail's own columns into
// the frame's.
func trafficDoorsAt(rows [][]trafficDoor, left int) [][]trafficDoor {
	out := make([][]trafficDoor, len(rows))
	for i, r := range rows {
		out[i] = trafficDoorsShift(r, left)
	}
	return out
}

// trafficDoorsShift is one row's doors moved left columns right.
func trafficDoorsShift(r []trafficDoor, left int) []trafficDoor {
	if len(r) == 0 {
		return nil
	}
	out := make([]trafficDoor, len(r))
	for i, d := range r {
		d.span = hudSpan{from: d.span.from + left, to: d.span.to + left}
		out[i] = d
	}
	return out
}
