package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── DRAWING THE RAIL (teamrail.go says what it is) ─────────────────────────

// trafficBody is the rail's rows for team t, height tall and width wide: the
// header, and the newest entries at the bottom, as many as fit. It records
// where each row goes on a press, and keeps what it drew for the next frame.
// Frame-safe: the cache only.
func (a *app) trafficBody(t team, height, width int) ([]string, []string, []string, hudSpan) {
	rows := a.traffic.rows[t.ID]
	last := ""
	if len(rows) > 0 {
		last = rows[len(rows)-1].ID
	}
	hot := -1
	if a.hot.kind == hoverTraffic {
		hot = a.hot.index
	}
	key := trafficCacheKey{
		team: t.ID, last: last, seen: a.traffic.seen[t.ID], rows: len(rows),
		width: width, height: height, hot: hot, hideHot: a.hot.kind == hoverTrafficHide,
		ascii: a.pal.ascii, minute: a.now().Unix() / 60,
	}
	if c := &a.traffic.cache; c.out != nil && c.key == key {
		return c.out, c.lines, c.hints, c.hide
	}
	out := make([]string, height)
	lines := make([]string, height)
	hints := make([]string, height)
	var hide hudSpan
	if height <= 0 || width <= 0 {
		return out, lines, hints, hide
	}
	blank := strings.Repeat(" ", width)
	for i := range out {
		out[i] = blank
	}
	// THE HEADER SAYS WHAT THE COLUMN IS AND HOW TO PUT IT AWAY, in the shape
	// the task column's own foot says it (`ctrl+g hide`): the name in ink, the
	// way out dim at the right, and the way out is a word a hand can press.
	hideWord := "hide " + trafficKey
	head := a.pal.ink(trafficWord)
	if gap := width - len(trafficWord) - ansi.StringWidth(hideWord); gap >= 2 {
		word := a.pal.dim(hideWord)
		if key.hideHot {
			word = a.pal.cursor(a.pal.ink(hideWord), 0)
		}
		head += strings.Repeat(" ", gap) + word
		hide = hudSpan{from: width - ansi.StringWidth(hideWord), to: width}
		hints[0] = "Hide the traffic" + hintSegment + trafficKey
	}
	out[0] = fit(head, width)
	room := height - 1
	shown := a.trafficVisible(t, room)
	if len(shown) == 0 {
		if height > 2 {
			quiet := fit(a.pal.dim("nothing yet"), width)
			out[height-1] = quiet + strings.Repeat(" ", max(width-ansi.StringWidth(quiet), 0))
		}
	} else {
		at := height - len(shown)
		for i, e := range shown {
			line, target, hint := a.trafficLine(t, e, width)
			if target != "" && hot == at+i {
				line = a.pal.cursor(line, width)
			}
			out[at+i], lines[at+i], hints[at+i] = line, target, hint
		}
	}
	a.traffic.cache = trafficCache{key: key, out: out, lines: lines, hints: hints, hide: hide}
	return out, lines, hints, hide
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
	body, lines, hints, hide := a.trafficBody(t, height, cols-seamW)
	a.trafficMarkSeen(t)
	left := width - cols + seamW
	a.traffic.drawn = trafficDrawn{
		mode: trafficColumn, x0: width - cols, x1: width, y0: 0, y1: height,
		lines: lines, hints: hints, hide: hudSpan{from: hide.from + left, to: hide.to + left},
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
	rows, lines, hints := a.trafficCardRows(t, h-2, inner, top+1)
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
		lines: make([]string, view), hints: make([]string, view), closeY: top + h - 1}
	for i := range lines {
		d.lines[top+1+i], d.hints[top+1+i] = lines[i], hints[i]
	}
	if span.pressable() {
		d.close = hudSpan{from: inset + span.from, to: inset + span.to}
		d.hints[d.closeY] = "Close the traffic" + hintSegment + "esc"
	}
	a.traffic.drawn = d
	return out, 0
}

// trafficCardRows is the card's rows: the entries, newest at the bottom, with
// no header of its own, because the card's edge is the header. first is the
// body row the first of them lands on, which is what the pointer holds.
func (a *app) trafficCardRows(t team, height, width, first int) ([]string, []string, []string) {
	out := make([]string, height)
	lines := make([]string, height)
	hints := make([]string, height)
	blank := strings.Repeat(" ", width)
	for i := range out {
		out[i] = blank
	}
	shown := a.trafficVisible(t, height)
	if len(shown) == 0 {
		out[height-1] = fit(a.pal.dim("nothing yet"), width)
		return out, lines, hints
	}
	at := height - len(shown)
	for i, e := range shown {
		line, target, hint := a.trafficLine(t, e, width)
		if target != "" && a.hot.kind == hoverTraffic && a.hot.index == first+at+i {
			line = a.pal.cursor(line, width)
		}
		out[at+i], lines[at+i], hints[at+i] = line, target, hint
	}
	return out, lines, hints
}
