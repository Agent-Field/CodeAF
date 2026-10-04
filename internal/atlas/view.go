package atlas

import "strings"

// drawFrame lays out, paints and composes the whole screen: the header, the
// arrows under the boxes, the boxes, and then the panels that sit on top —
// the flow story, the detail pane, the help — and the status line last.
// The hit rectangles a click is matched against are recorded while drawing,
// in draw order, so the topmost layer wins.
func (m *Model) drawFrame() string {
	g := m.geometry()
	m.layout(g)
	c := newCanvas(g.W, g.H)
	m.hits = m.hits[:0]
	m.drawHeader(c, g)
	m.drawEdges(c, g)
	m.drawNodes(c, g)
	if m.flow >= 0 {
		m.drawFlowPanel(c, g)
	}
	if m.detail != "" {
		m.drawDetail(c, g)
	}
	if m.help {
		m.drawHelp(c, g)
	}
	m.drawStatus(c, g)
	return c.frame()
}

// drawHeader draws the title, the subtitle when the terminal is wide enough,
// and one clickable tab per flow.
func (m *Model) drawHeader(c *canvas, g geom) {
	head := "◆ " + m.data.Title
	c.text(0, 0, head, colAccent, true)
	x := len(head) + 2
	if g.W >= 110 && m.data.Description != "" {
		c.text(x, 0, m.data.Description, colDim, false)
		x += len(m.data.Description) + 2
	}
	for i, f := range m.data.Flows {
		name := f.Title
		if g.compact {
			if word, _, ok := strings.Cut(f.Title, " "); ok {
				name = word
			}
		}
		label := itoa(i+1) + " " + name
		if m.flow == i {
			c.text(x, 0, label, colAccent, true)
		} else {
			c.text(x, 0, label, colFg, false)
		}
		m.hits = append(m.hits, hit{rect: Rect{X: x, Y: 0, W: len(label), H: 1}, kind: hitTab, arg: i})
		x += len(label) + 2
		if x >= g.W {
			return
		}
	}
}

// drawEdges paints every arrow once, dimmed or lit by what is selected, and
// then the active flow step's arrow again in the accent colour with its
// message as the label.
func (m *Model) drawEdges(c *canvas, g geom) {
	step, inFlow := m.activeStep()
	stepEdge := Edge{}
	if inFlow {
		stepEdge, _ = m.edgeForStep(step)
	}
	focus := m.detail
	if focus == "" {
		focus = m.selected
	}
	offs := portOffsets(m.data, m.rects)
	for i, e := range m.data.Edges {
		if inFlow && e.ID == stepEdge.ID {
			continue
		}
		touches := focus != "" && (e.From == focus || e.To == focus)
		color := colMuted
		if focus != "" {
			color = colEdge
			if touches {
				color = colEdgeHi
			}
		}
		showLabel := !inFlow && (touches || !g.compact)
		drawEdge(c, m.rects[e.From], m.rects[e.To], offs[i][0], offs[i][1], e.Label, color, showLabel)
	}
	if !inFlow {
		return
	}
	// The active arrow travels the overview edge's ports when it follows one,
	// from the end that matches the step's own direction.
	i := -1
	for j, e := range m.data.Edges {
		if e.ID == stepEdge.ID {
			i = j
			break
		}
	}
	offA, offB := 0, 0
	if i >= 0 {
		offA, offB = offs[i][0], offs[i][1]
		if stepEdge.From != step.From {
			offA, offB = offB, offA
		}
	}
	drawEdge(c, m.rects[step.From], m.rects[step.To], offA, offB,
		itoa(m.step+1)+". "+step.Message, colAccent, true)
}

// drawNodes paints the boxes over the arrows: a rounded border in the kind's
// colour, the label inside it, and the short subtitle when there is room.
// The box an active step touches, and the one selected or open, draw louder.
func (m *Model) drawNodes(c *canvas, g geom) {
	step, inFlow := m.activeStep()
	for _, n := range m.data.Nodes {
		r := m.rects[n.ID]
		isActive := inFlow && (step.From == n.ID || step.To == n.ID)
		isSel := m.selected == n.ID || m.detail == n.ID
		color := kindColor[n.Kind]
		if isActive {
			color = colAccent
		}
		m.drawBox(c, r, color, isSel)
		if !isActive && isSel {
			color = colEdgeHi
		}
		label := fitRune(n.Label, r.W-4)
		c.text(r.X+2, r.Y+1, label, color, isActive || isSel)
		if !g.compact && n.Short != "" && r.H >= 4 {
			c.text(r.X+2, r.Y+2, fitRune(n.Short, r.W-4), colDim, false)
		}
	}
}

// drawBox draws one border: single and quiet, or double when the box is the
// one the person is reading.
func (m *Model) drawBox(c *canvas, r Rect, color string, selected bool) {
	corners := [4]rune{'┌', '┐', '└', '┘'}
	horiz, vert := '─', '│'
	if selected {
		corners = [4]rune{'╔', '╗', '╚', '╝'}
		horiz, vert = '═', '║'
	}
	if r.W < 2 || r.H < 2 {
		return
	}
	c.put(r.X, r.Y, corners[0], color, false)
	c.put(r.X+r.W-1, r.Y, corners[1], color, false)
	c.put(r.X, r.Y+r.H-1, corners[2], color, false)
	c.put(r.X+r.W-1, r.Y+r.H-1, corners[3], color, false)
	c.hline(r.Y, r.X+1, r.X+r.W-2, horiz, color, false)
	c.hline(r.Y+r.H-1, r.X+1, r.X+r.W-2, horiz, color, false)
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		c.put(r.X, y, vert, color, false)
		c.put(r.X+r.W-1, y, vert, color, false)
	}
}

// fitRune cuts s to at most n characters.
func fitRune(s string, n int) string {
	if n < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// drawFlowPanel draws the step-through story at the bottom of the screen: a
// rule, then the prev and next click targets, then what is happening now.
func (m *Model) drawFlowPanel(c *canvas, g geom) {
	f := m.data.Flows[m.flow]
	s := f.Steps[m.step]
	top := g.H - 1 - g.flowH
	c.hline(top, 0, g.W-1, '─', colAccent, false)
	row := top + 1
	prev := "◀ prev"
	c.text(1, row, prev, colAccent, false)
	m.hits = append(m.hits, hit{rect: Rect{X: 1, Y: row, W: len(prev), H: 1}, kind: hitPrev})
	next := "next ▶"
	c.text(g.W-1-len(next), row, next, colAccent, false)
	m.hits = append(m.hits, hit{rect: Rect{X: g.W - 1 - len(next), Y: row, W: len(next), H: 1}, kind: hitNext})

	// The story's first line names where the flow stands: title, step number,
	// one dot per step, and a marker while it plays itself.
	x := 1
	put := func(s, fg string, bold bool) {
		if x >= g.W-1 {
			return
		}
		s = fitRune(s, g.W-1-x)
		c.text(x, row+1, s, fg, bold)
		x += len(s)
	}
	put(f.Title, colAccent, true)
	put(" step "+itoa(m.step+1)+"/"+itoa(len(f.Steps)), colDim, false)
	dots := make([]string, len(f.Steps))
	for i := range f.Steps {
		switch {
		case i == m.step:
			dots[i] = "●"
		case i < m.step:
			dots[i] = "•"
		default:
			dots[i] = "·"
		}
	}
	put(strings.Join(dots, ""), colAccent, false)
	if m.playing {
		put("  ▶ playing", kindColor[KindGo], false)
	}

	// Then what this step sends, wrapped, and why — when there is room.
	width := g.W - 2
	body := m.nodeLabel(s.From) + " → " + m.nodeLabel(s.To) + ": " + s.Message
	lines := wrapText(body, width)
	line := row + 2
	for i, l := range lines {
		if line+i > g.H-2 {
			break
		}
		if i == 0 {
			prefix := m.nodeLabel(s.From) + " → " + m.nodeLabel(s.To) + ": "
			c.text(1, line+i, prefix, colFg, true)
			c.text(1+len(prefix), line+i, strings.TrimPrefix(l, prefix), colFg, false)
			continue
		}
		c.text(1, line+i, l, colFg, false)
	}
	if s.Note != "" && !g.compact {
		if y := line + len(lines); y <= g.H-2 {
			c.text(1, y, s.Note, colMuted, false)
		}
	}
}

// drawDetail draws the pane about one part: what it does, its files, and what
// it talks to. Wide terminals open it beside the map; narrow ones cover it.
func (m *Model) drawDetail(c *canvas, g geom) {
	n := m.node(m.detail)
	if n == nil {
		return
	}
	w, x := g.W, 0
	if g.side {
		w, x = sidePaneW, g.W-sidePaneW
	}
	r := Rect{X: x, Y: 1, W: w, H: g.mapH}
	m.drawBox(c, r, kindColor[n.Kind], false)
	title := " " + n.Label + " "
	c.text(r.X+1, r.Y, fitRune(title, r.W-2), kindColor[n.Kind], false)
	foot := " esc close "
	c.text(r.X+r.W-1-len(foot), r.Y+r.H-1, foot, colMuted, false)

	inner := r.W - 4
	y := r.Y + 1
	// line writes one row of the pane and answers whether rows remain.
	line := func(s, fg string, bold bool) bool {
		if y > r.Y+r.H-2 {
			return false
		}
		c.text(r.X+2, y, fitRune(s, inner), fg, bold)
		y++
		return true
	}
	lines := func(s, fg string, bold bool) bool {
		for _, l := range wrapText(s, inner) {
			if !line(l, fg, bold) {
				return false
			}
		}
		return true
	}
	if !line(kindName[n.Kind], kindColor[n.Kind], false) {
		return
	}
	if !line("", "", false) || !lines(n.Summary, colFg, false) {
		return
	}
	for _, d := range n.Details {
		if !lines("• "+d, colFg, false) {
			return
		}
	}
	if len(n.Files) > 0 {
		if !line("", "", false) || !line("Files", colAccent, true) {
			return
		}
		for _, f := range n.Files {
			if !line(f.Path, kindColor[KindSpec], false) {
				return
			}
			if len(f.Symbols) > 0 && !line("  "+strings.Join(f.Symbols, ", "), colMuted, false) {
				return
			}
			if f.Note != "" && !line("  "+f.Note, colDim, false) {
				return
			}
		}
	}
	out, in := m.edgesOf(n.ID)
	if len(out) == 0 && len(in) == 0 {
		return
	}
	if !line("", "", false) || !line("Talks to", colAccent, true) {
		return
	}
	talk := func(arrow, other string, e Edge) bool {
		text := arrow + " " + m.nodeLabel(other) + " " + e.Label
		if e.Detail != "" {
			text += " — " + e.Detail
		}
		return lines(text, colFg, false)
	}
	for _, e := range out {
		if !talk("→", e.To, e) {
			return
		}
	}
	for _, e := range in {
		if !talk("←", e.From, e) {
			return
		}
	}
}

// drawHelp draws the keys and the colour legend over everything else.
func (m *Model) drawHelp(c *canvas, g geom) {
	w, h := 64, 24
	if g.W-4 < w {
		w = g.W - 4
	}
	if g.H-2 < h {
		h = g.H - 2
	}
	r := Rect{X: max(0, (g.W-w)/2), Y: max(0, (g.H-h)/2), W: w, H: h}
	m.drawBox(c, r, colAccent, false)
	title := " help · legend "
	c.text(r.X+(r.W-len(title))/2, r.Y, title, colAccent, false)
	foot := " ? or esc closes "
	c.text(r.X+(r.W-len(foot))/2, r.Y+r.H-1, foot, colMuted, false)

	inner := r.W - 4
	y := r.Y + 1
	line := func(s, fg string, bold bool) bool {
		if y > r.Y+r.H-2 {
			return false
		}
		c.text(r.X+2, y, fitRune(s, inner), fg, bold)
		y++
		return true
	}
	if !line("Keys & mouse", colFg, true) {
		return
	}
	for _, k := range []struct{ key, what string }{
		{"drag a box", "move it; arrows follow"},
		{"click a box", "open its detail pane"},
		{"tab / S-tab", "select next / previous box"},
		{"enter", "open detail for selection"},
		{"1-" + itoa(len(m.data.Flows)) + " / f", "open a flow / cycle flows"},
		{"← →  h l", "step the flow (or click prev/next)"},
		{"p / space", "play / pause the flow"},
		{"0 / esc", "back to overview / close pane"},
		{"?", "toggle this help"},
		{"q / ctrl+c", "quit and restore the terminal"},
	} {
		pad := k.key + strings.Repeat(" ", max(0, 14-len([]rune(k.key))))
		if !line(pad, colAccent, true) {
			return
		}
		c.text(r.X+2+len([]rune(pad)), y-1, k.what, colFg, false)
	}
	if !line("", "", false) || !line("Legend", colFg, true) {
		return
	}
	for _, k := range []Kind{KindMachine, KindGo, KindService, KindEngine, KindSpec} {
		if !line("■ "+kindName[k], kindColor[k], false) {
			return
		}
	}
	line("─▶ active flow step", colAccent, false)
	line("─▶ edges of the selected box", colEdgeHi, false)
}

// drawStatus draws the one hint line at the bottom.
func (m *Model) drawStatus(c *canvas, g geom) {
	hints := "drag boxes · click/enter details · tab select · 1-" + itoa(len(m.data.Flows)) + " flows · ? help · q quit"
	if m.flow >= 0 {
		hints = "←/→ step · p play · 0 overview · tab select · ? help · q quit"
	}
	if g.W < len(hints)+2 {
		hints = hints[:max(0, g.W-3)] + "…"
	}
	c.text(0, g.H-1, hints, colMuted, false)
}

// node is the node with an id, or nil.
func (m *Model) node(id string) *Node {
	for i := range m.data.Nodes {
		if m.data.Nodes[i].ID == id {
			return &m.data.Nodes[i]
		}
	}
	return nil
}

// nodeLabel is a node's title, or its id when there is no node — a flow step
// always names real nodes, and data_test.go holds that line.
func (m *Model) nodeLabel(id string) string {
	if n := m.node(id); n != nil {
		return n.Label
	}
	return id
}

// edgesOf answers the edges leaving and entering one node, in data order.
func (m *Model) edgesOf(id string) (out, in []Edge) {
	for _, e := range m.data.Edges {
		switch {
		case e.From == id:
			out = append(out, e)
		case e.To == id:
			in = append(in, e)
		}
	}
	return out, in
}

// itoa is strconv.Itoa spelled locally so the drawing files need no import.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
