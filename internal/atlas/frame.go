package atlas

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The palette, one colour per node kind and one hot accent, kept to the same
// colours the map has always read on screen. Colours ride on lipgloss, which codeaf already
// carries: it strips them itself on a terminal with no colour, so nothing here
// has to detect the profile.
const (
	colFg     = "#c0caf5"
	colDim    = "#565f89"
	colMuted  = "#737aa2"
	colEdge   = "#3b4261"
	colEdgeHi = "#7aa2f7"
	colAccent = "#ff9e64"
)

// kindColor is the one colour per node kind that the box borders and the
// legend draw.
var kindColor = map[Kind]string{
	KindMachine: "#7aa2f7",
	KindGo:      "#9ece6a",
	KindService: "#e0af68",
	KindEngine:  "#bb9af7",
	KindSpec:    "#7dcfff",
}

// kindName is the plain words a kind reads as, in the legend and detail panes.
var kindName = map[Kind]string{
	KindMachine: "machine",
	KindGo:      "codeaf (Go)",
	KindService: "relay / hosted",
	KindEngine:  "furrow (Rust)",
	KindSpec:    "spec / doc",
}

// playMillis is how long one step of a playing flow stays on screen.
const playMillis = 1400

// sidePaneW is the width of the detail pane when the terminal is wide enough
// to open it beside the map instead of over it.
const sidePaneW = 46

// paint colours a run of text.
func paint(s, hex string, bold bool) string {
	if s == "" {
		return ""
	}
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	if bold {
		st = st.Bold(true)
	}
	return st.Render(s)
}

// cell is one character of the frame and the colour it draws in.
type cell struct {
	ch   rune
	fg   string
	bold bool
}

// canvas is the whole frame: a grid every layer paints into, in the order the
// layers stack — edges first, then boxes, then the panels on top.
type canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c := &canvas{w: w, h: h, cells: make([]cell, w*h)}
	// Every cell starts as a blank: a cell never drawn is a space on screen,
	// not a NUL.
	for i := range c.cells {
		c.cells[i].ch = ' '
	}
	return c
}

func (c *canvas) put(x, y int, ch rune, fg string, bold bool) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.cells[y*c.w+x] = cell{ch: ch, fg: fg, bold: bold}
}

// text writes s from (x, y), clipping at the right edge.
func (c *canvas) text(x, y int, s, fg string, bold bool) {
	for i, r := range s {
		if x+i >= c.w {
			return
		}
		c.put(x+i, y, r, fg, bold)
	}
}

// hline fills a row with one character between x0 and x1.
func (c *canvas) hline(y, x0, x1 int, ch rune, fg string, bold bool) {
	for x := x0; x <= x1; x++ {
		c.put(x, y, ch, fg, bold)
	}
}

// row renders one row as a styled string: runs of same-styled cells become one
// escape sequence each, and an unstyled run is left plain.
func (c *canvas) row(y int) string {
	if y < 0 || y >= c.h {
		return ""
	}
	base := y * c.w
	var b strings.Builder
	runStart := 0
	cur := c.cells[base]
	open := false
	emit := func(upto int) {
		if !open {
			return
		}
		runes := make([]rune, 0, upto-runStart)
		for i := runStart; i < upto; i++ {
			runes = append(runes, c.cells[base+i].ch)
		}
		b.WriteString(paint(string(runes), cur.fg, cur.bold))
		open = false
	}
	for x := 0; x < c.w; x++ {
		ce := c.cells[base+x]
		if !open || ce != cur {
			emit(x)
			cur, runStart, open = ce, x, true
		}
	}
	emit(c.w)
	return b.String()
}

// frame turns the canvas into the lines the terminal draws, already styled.
func (c *canvas) frame() string {
	lines := make([]string, c.h)
	for y := range lines {
		lines[y] = c.row(y)
	}
	return strings.Join(lines, "\n")
}

// frameWidths answers the display width of every row, which is the number of
// cells the row was drawn into: styling must never change it.
func (c *canvas) frameWidths() []int {
	w := make([]int, c.h)
	for y := range w {
		w[y] = lipgloss.Width(c.row(y))
	}
	return w
}

// Rect is a box on screen, in cells.
type Rect struct{ X, Y, W, H int }

// Contains answers whether a cell sits inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r Rect) centerX() int { return r.X + r.W/2 }
func (r Rect) centerY() int { return r.Y + r.H/2 }

// geom is the one layout of a frame: where the map starts and ends, whether
// the terminal is too small for the roomy drawing, and how tall the flow
// panel is when one is open.
type geom struct {
	W, H    int
	compact bool
	flowH   int
	mapTop  int
	mapW    int
	mapH    int
	side    bool // the detail pane opens beside the map, not over it
}

func (m *Model) geometry() geom {
	W, H := m.W, m.H
	compact := W < 100 || H < 30
	flowH := 0
	if m.flow >= 0 {
		flowH = 5
		if compact {
			flowH = 4
		}
	}
	side := m.detail != "" && W >= 100
	mapW := W
	if side {
		mapW = W - sidePaneW
	}
	if mapW < 10 {
		mapW = 10
	}
	mapH := H - 2 - flowH
	if mapH < 4 {
		mapH = 4
	}
	return geom{W: W, H: H, compact: compact, flowH: flowH, mapTop: 1, mapW: mapW, mapH: mapH, side: side}
}

// nodeSize is how wide and tall a box draws: the label and, when there is
// room, the short subtitle, each inside a one-cell border.
func nodeSize(n Node, compact bool) (int, int) {
	if compact {
		w := len(n.Label) + 4
		if w > 18 {
			w = 18
		}
		return w, 3
	}
	w := len(n.Label)
	if len(n.Short) > w {
		w = len(n.Short)
	}
	w += 4
	if w < 14 {
		w = 14
	}
	return w, 4
}

// layout computes every node's on-screen rectangle from its fractional centre,
// clamped into the map so a drag can never push a box off screen.
func (m *Model) layout(g geom) {
	if m.rects == nil {
		m.rects = map[string]Rect{}
	}
	for _, n := range m.data.Nodes {
		w, h := nodeSize(n, g.compact)
		p := m.pos[n.ID]
		x := clamp(int(0.5+p[0]*float64(g.mapW))-w/2, 0, max(0, g.mapW-w))
		y := clamp(g.mapTop+int(0.5+p[1]*float64(g.mapH))-h/2, g.mapTop, max(g.mapTop, g.mapTop+g.mapH-h))
		m.rects[n.ID] = Rect{X: x, Y: y, W: w, H: h}
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ── edge routing ─────────────────────────────────────────────────────────────
// An arrow leaves the side of one box that faces the other, runs as an
// orthogonal Z, and lands with an arrowhead just outside the target box.
// Edges sharing a side spread their ports so two arrows never overlap.

type dir byte

const (
	dirR dir = 'r'
	dirL dir = 'l'
	dirU dir = 'u'
	dirD dir = 'd'
)

type pt struct{ X, Y int }

func isHorizontal(a, b Rect) bool {
	dx := b.centerX() - a.centerX()
	dy := b.centerY() - a.centerY()
	return abs(dx) > abs(dy)*5/2 && (b.X >= a.X+a.W+2 || a.X >= b.X+b.W+2)
}

// sideOf is which side of a a route towards b leaves from.
func sideOf(a, b Rect) dir {
	if isHorizontal(a, b) {
		if b.centerX() > a.centerX() {
			return dirR
		}
		return dirL
	}
	if b.centerY() >= a.centerY() {
		return dirD
	}
	return dirU
}

// route is the orthogonal path from the edge of a to just outside b; the
// offsets spread ports along a side.
func route(a, b Rect, offA, offB int) []pt {
	dx := b.centerX() - a.centerX()
	dy := b.centerY() - a.centerY()
	if isHorizontal(a, b) {
		sx, ex := a.X+a.W, b.X-1
		if dx <= 0 {
			sx, ex = a.X-1, b.X+b.W
		}
		sy := clamp(a.centerY()+offA, a.Y+1, a.Y+a.H-2)
		ey := clamp(b.centerY()+offB, b.Y+1, b.Y+b.H-2)
		mx := (sx + ex + 1) / 2
		return []pt{{sx, sy}, {mx, sy}, {mx, ey}, {ex, ey}}
	}
	sy, ey := a.Y+a.H, b.Y-1
	if dy < 0 {
		sy, ey = a.Y-1, b.Y+b.H
	}
	sx := clamp(a.centerX()+offA, a.X+1, a.X+a.W-2)
	ex := clamp(b.centerX()+offB, b.X+1, b.X+b.W-2)
	my := (sy + ey + 1) / 2
	return []pt{{sx, sy}, {sx, my}, {ex, my}, {ex, ey}}
}

// rasterize turns the corner points into one cell per step of the path.
func rasterize(pts []pt) []pt {
	if len(pts) == 0 {
		return nil
	}
	cells := []pt{pts[0]}
	for _, to := range pts[1:] {
		cur := cells[len(cells)-1]
		for cur.X != to.X || cur.Y != to.Y {
			next := pt{cur.X + sign(to.X-cur.X), cur.Y}
			if next.X == cur.X {
				next.Y = cur.Y + sign(to.Y-cur.Y)
			}
			cur = next
			cells = append(cells, cur)
		}
	}
	return cells
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// portOffsets spreads the ports of the edges that share one side of one box,
// ordered by where on that side each edge's far end sits.
func portOffsets(data *Map, rects map[string]Rect) [][2]int {
	type item struct {
		i   int
		to  bool // the offset lands on the edge's target side
		key int
	}
	groups := map[string][]item{}
	along := func(r Rect, side dir) int {
		if side == dirL || side == dirR {
			return r.centerY()
		}
		return r.centerX()
	}
	for i, e := range data.Edges {
		a, okA := rects[e.From]
		b, okB := rects[e.To]
		if !okA || !okB {
			continue
		}
		sa := sideOf(a, b)
		sb := dirD
		if isHorizontal(a, b) {
			if sa == dirR {
				sb = dirL
			} else {
				sb = dirR
			}
		} else if sa == dirD {
			sb = dirU
		}
		fromKey := e.From + ":" + string(rune(sa))
		toKey := e.To + ":" + string(rune(sb))
		groups[fromKey] = append(groups[fromKey], item{i, false, along(b, sa)})
		groups[toKey] = append(groups[toKey], item{i, true, along(a, sb)})
	}
	out := make([][2]int, len(data.Edges))
	for key, items := range groups {
		vertSide := strings.HasSuffix(key, string(rune(dirL))) || strings.HasSuffix(key, string(rune(dirR)))
		spacing := 3
		if vertSide {
			spacing = 1
		}
		// A stable order by key, then spread them round the middle of the side.
		for i := 1; i < len(items); i++ {
			for j := i; j > 0 && items[j].key < items[j-1].key; j-- {
				items[j], items[j-1] = items[j-1], items[j]
			}
		}
		for j, it := range items {
			off := (j - (len(items)-1)/2) * spacing
			if it.to {
				out[it.i][1] = off
			} else {
				out[it.i][0] = off
			}
		}
	}
	return out
}

// The line characters an edge draws with: one glyph per direction of travel,
// the four arrowheads, and the eight corners a Z-route can turn through.
var (
	lineCh = map[dir]rune{dirR: '─', dirL: '─', dirU: '│', dirD: '│'}
	headCh = map[dir]rune{dirR: '▶', dirL: '◀', dirU: '▲', dirD: '▼'}
)

// cornerCh names a turn by the direction it came from and the one it leaves in.
var cornerCh = map[string]rune{
	"rd": '┐', "ru": '┘', "ld": '┌', "lu": '└',
	"dr": '└', "dl": '┘', "ur": '┌', "ul": '┐',
}

// edgeCells is the path of one arrow, drawn or not — the tests read it to
// assert that a drag re-routes the arrows with the box.
func edgeCells(a, b Rect, offA, offB int) []pt {
	return rasterize(route(a, b, offA, offB))
}

// drawEdge paints one arrow and, when showLabel, its label at the middle of
// the path, truncated to the screen.
func drawEdge(c *canvas, a, b Rect, offA, offB int, label, color string, showLabel bool) {
	cells := edgeCells(a, b, offA, offB)
	for i, cell := range cells {
		var ch rune
		din, dout := dir(0), dir(0)
		if i > 0 {
			din = dirOf(cells[i-1], cell)
		}
		if i < len(cells)-1 {
			dout = dirOf(cell, cells[i+1])
		}
		switch {
		case dout == 0:
			ch = headCh[din]
		case din == 0 || din == dout:
			ch = lineCh[dout]
		default:
			ch = cornerCh[string(rune(din))+string(rune(dout))]
			if ch == 0 {
				ch = '┼'
			}
		}
		c.put(cell.X, cell.Y, ch, color, false)
	}
	if showLabel && label != "" && len(cells) > 2 {
		mid := cells[len(cells)/2]
		maxLen := 30
		if c.w-2 < maxLen {
			maxLen = c.w - 2
		}
		if maxLen < 4 {
			maxLen = 4
		}
		text := " " + label + " "
		if len([]rune(text)) > maxLen {
			text = string([]rune(text)[:maxLen-1]) + "…"
		}
		x := clamp(mid.X-len(text)/2, 0, max(0, c.w-len(text)))
		c.text(x, mid.Y, text, color, false)
	}
}

func dirOf(a, b pt) dir {
	switch {
	case b.X > a.X:
		return dirR
	case b.X < a.X:
		return dirL
	case b.Y > a.Y:
		return dirD
	default:
		return dirU
	}
}

// wrapText folds s into lines of at most width cells, breaking on spaces and
// never mid-word when a single word fits.
func wrapText(s string, width int) []string {
	width = max(width, 1)
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if len(line)+1+len(w) <= width {
				line += " " + w
				continue
			}
			out = append(out, line)
			line = w
		}
		out = append(out, line)
	}
	return out
}
