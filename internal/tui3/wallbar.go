package tui3

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── THE WALL'S CONTROLS: BUTTONS, SPACES, CARDS AND POPOVERS ────────────────
//
// Every act the wall answers a key for has a button, and every button names
// its key: ` Filter / `, ` New space s `. A person who clicks learns the key on
// the way, and a person who never clicks loses nothing, because the keys do
// what they did.
//
// This is still the painting half (wallview.go): pure functions from a
// [wallView] to rows and [wallHit]s. The hover arrives in v.hover and is only
// ever drawn here, never resolved.
//
// A CONTROL UNDER THE POINTER WEARS THE CURSOR GROUND (styles.go), the step the
// tab strip's own doors wear. A control inside something already on a ground
// takes the ladder's next step up, so the two never read as one.
//
// COLOUR BELONGS TO SPACES AND TO STATE, AND TO NOTHING ELSE. A space's colour
// (spacehue.go) is drawn only as its dot: on its segment, on the tiles it
// holds, in its popovers. Borders and grounds stay with state.

// wallButton is one pressable label and the key that does the same thing.
type wallButton struct {
	act   wallAct
	label string
	key   string // "" for an act no single key does
}

// wallButtonW is the cells a button takes: a cell of padding either side, so
// the hover ground reads as a button and not as a highlighted word.
func wallButtonW(b wallButton) int {
	w := 2 + ansi.StringWidth(b.label)
	if b.key != "" {
		w += 1 + ansi.StringWidth(b.key)
	}
	return w
}

// wallButtonPaint draws one button: the label in ink, the key dim.
func wallButtonPaint(pal palette, b wallButton, hot bool) string {
	s := " " + pal.ink(b.label)
	if b.key != "" {
		s += " " + pal.dim(b.key)
	}
	s += " "
	if hot {
		return pal.cursor(s, 0)
	}
	return s
}

// wallLay draws buttons left to right from column x on row y, gap blank cells
// apart, and says where each landed.
func wallLay(pal palette, bs []wallButton, hover wallHitRef, x, y, gap int) (string, int, []wallHit) {
	var b strings.Builder
	hits := make([]wallHit, 0, len(bs))
	w := 0
	for i, btn := range bs {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", gap))
			w += gap
		}
		bw := wallButtonW(btn)
		hot := hover.kind == wallHitAction && hover.arg == int(btn.act)
		b.WriteString(wallButtonPaint(pal, btn, hot))
		hits = append(hits, wallHit{x0: x + w, y0: y, x1: x + w + bw, y1: y + 1, kind: wallHitAction, arg: int(btn.act)})
		w += bw
	}
	return b.String(), w, hits
}

// wallBarWidth is the cells a row of buttons takes, gap blank cells between.
func wallBarWidth(bs []wallButton, gap int) int {
	w := 0
	for i, b := range bs {
		if i > 0 {
			w += gap
		}
		w += wallButtonW(b)
	}
	return w
}

// wallKeys is the spelling of the marks the controls use, in the palette's
// tier.
type wallKeys struct {
	back, enter, shuffle, minus, more, menu, caret, spaces, rule string
	boxOff, boxOn, boxSome                                       string
}

func wallKeysFor(ascii bool) wallKeys {
	if ascii {
		return wallKeys{back: "<", enter: "enter", shuffle: "~", minus: "-", more: "...", menu: "~", caret: "v",
			spaces: "*+", rule: "-", boxOff: "[ ]", boxOn: "[x]", boxSome: "[-]"}
	}
	return wallKeys{back: "‹", enter: "↵", shuffle: "↻", minus: "−", more: "…", menu: "⋯", caret: "▾",
		spaces: "●+", rule: "─", boxOff: "☐", boxOn: "☑", boxSome: "▣"}
}

// wallPart is one piece of a row drawn on a ground: hot pieces are a control
// under the pointer and take the ladder's next step over it.
type wallPart struct {
	s   string
	hot bool
}

// wallCompose draws parts on ground, the hot ones on the mark step.
func wallCompose(pal palette, parts []wallPart, ground func(string) string) string {
	var b, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(ground(run.String()))
			run.Reset()
		}
	}
	for _, p := range parts {
		if p.hot {
			flush()
			b.WriteString(pal.background(p.s, 0, pal.ramp.mark))
			continue
		}
		run.WriteString(p.s)
	}
	flush()
	return b.String()
}

// wallSpaceMark is space i's dot in its colour, or, where there is no colour
// to draw, its initial, dim. Either way it is one cell.
func wallSpaceMark(pal palette, v wallView, i int, glyph string) string {
	if i >= 0 && i < len(v.hues) {
		if ink := pal.spaceInk(v.hues[i]); ink != nil {
			return ink(glyph)
		}
	}
	initial := "?"
	if i >= 0 && i < len(v.spaces) {
		if r := []rune(v.spaces[i]); len(r) > 0 {
			initial = strings.ToLower(string(r[0]))
			if ansi.StringWidth(initial) != 1 {
				initial = "?"
			}
		}
	}
	return pal.dim(initial)
}

func wallPlural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// ── THE TOOLBAR ─────────────────────────────────────────────────────────────

// wallBar is the toolbar along the bottom: the way back on the left, the acts
// on the wall as a whole on the right. It never wraps: on a narrow frame the
// least needed buttons leave first, and a button is drawn whole or not at all.
func wallBar(pal palette, v wallView, width, y int) (string, []wallHit) {
	k := wallKeysFor(pal.ascii)
	if v.naming {
		n := wallMarked(v)
		word := " Naming a space for " + strconv.Itoa(n) + " " + wallPlural(n, "conversation")
		return pal.dim(ansi.Truncate(word, width, "")), nil
	}
	const gap = 2
	left := []wallButton{{act: wallActBack, label: k.back + " Back", key: "esc"}}
	acts := []wallButton{
		{act: wallActFilter, label: "Filter", key: "/"},
		{act: wallActNewSpace, label: "New space", key: "s"},
	}
	cols := []wallButton{{act: wallActColsLess, label: k.minus}, {act: wallActColsMore, label: "+"}}
	const colsWord = "Columns"
	colsW := len(colsWord) + wallBarWidth(cols, 0)

	showCols, showActs := true, len(acts)
	fits := func() bool {
		w := 1 + wallBarWidth(left, gap) + gap
		if showActs > 0 {
			w += wallBarWidth(acts[:showActs], gap)
		}
		if showCols {
			w += gap + colsW
		}
		return w+1 <= width
	}
	for !fits() {
		switch {
		case showCols:
			showCols = false
		case showActs > 0:
			showActs--
		default:
			return "", nil
		}
	}
	s, lw, hits := wallLay(pal, left, v.hover, 1, y, gap)
	row := " " + s
	right := acts[:showActs]
	rw := wallBarWidth(right, gap)
	if showCols {
		if rw > 0 {
			rw += gap
		}
		rw += colsW
	}
	at := width - 1 - rw
	row += strings.Repeat(" ", max(at-1-lw, 0))
	if len(right) > 0 {
		rs, w, rh := wallLay(pal, right, v.hover, at, y, gap)
		row += rs
		hits = append(hits, rh...)
		at += w
		if showCols {
			row += strings.Repeat(" ", gap)
			at += gap
		}
	}
	if showCols {
		row += pal.dim(colsWord)
		cs, _, ch := wallLay(pal, cols, v.hover, at+len(colsWord), y, 0)
		row += cs
		hits = append(hits, ch...)
	}
	return row, hits
}

// wallEmptyRow is the whisper an empty wall draws, with the way back beside it
// as a button, centred.
func wallEmptyRow(pal palette, v wallView, width, y int) (string, []wallHit) {
	btn := wallButton{act: wallActBack, label: wallKeysFor(pal.ascii).back + " Back", key: "esc"}
	ww, bw := ansi.StringWidth(wallEmptyWord), wallButtonW(btn)
	if ww+3+bw > width {
		word := ansi.Truncate(wallEmptyWord, width, "")
		return strings.Repeat(" ", (width-ansi.StringWidth(word))/2) + pal.dim(word), nil
	}
	left := (width - ww - 3 - bw) / 2
	s, _, hits := wallLay(pal, []wallButton{btn}, v.hover, left+ww+3, y, 1)
	return strings.Repeat(" ", left) + pal.dim(wallEmptyWord) + "   " + s, hits
}

// ── THE SPACES ROW ──────────────────────────────────────────────────────────

// wallChipCap is the widest a space's name is drawn on its segment.
const wallChipCap = spaceNameCells

// wallSpacesRow is the second row of the head: the Spaces control, a row of
// segments, one per space and one for All, the one shown on the selected
// ground; then + New space; and the minimap at the right end when there are
// more conversations than the screen shows. While a filter narrows the grid
// the row is the filter instead, so what narrowed it and the way to undo it
// are both on screen.
//
// A SEGMENT IS A DOOR TO ITS SPACE. Its dot opens the space's settings, and so
// does the ⋯ that appears at its end under the pointer, drawn into two cells
// the segment kept for it.
func wallSpacesRow(pal palette, g wallGlyphs, v wallView, width, height, c, first, last, y int) (string, []wallHit) {
	var b strings.Builder
	var hits []wallHit
	x := 1
	b.WriteString(" ")
	put := func(s string, w int) {
		b.WriteString(s)
		x += w
	}
	fitsAt := func(w int) bool { return x+w <= width }
	k := wallKeysFor(pal.ascii)
	button := func(btn wallButton) {
		if !fitsAt(1 + wallButtonW(btn)) {
			return
		}
		put(" ", 1)
		s, w, h := wallLay(pal, []wallButton{btn}, v.hover, x, y, 1)
		put(s, w)
		hits = append(hits, h...)
	}

	switch {
	case v.naming && !wallNameCardFits(width, height):
		// The card has no room on this frame; the prompt is drawn here instead,
		// with the same two buttons.
		lead := pal.muted("New space "+g.gt+" ") + pal.ink(v.name) + pal.ink(g.cursor)
		put(lead, ansi.StringWidth(lead))
		count := "   " + pal.dim(strconv.Itoa(wallMarked(v))+" picked") + "  "
		put(count, ansi.StringWidth(count))
		button(wallButton{act: wallActSave, label: "Create", key: k.enter})
		button(wallButton{act: wallActCancel, label: "Cancel", key: "esc"})
		return ansi.Truncate(b.String(), width, ""), wallHitsWithin(hits, width)
	case v.filtering || v.filter != "":
		put(pal.dim("Filter  "), 8)
		w := 2 + ansi.StringWidth(v.filter)
		paint := pal.dim("/ ") + pal.ink(v.filter)
		if v.filtering {
			paint += pal.ink(g.cursor)
			w += ansi.StringWidth(g.cursor)
		}
		if fitsAt(w) {
			hits = append(hits, wallHit{x0: x, y0: y, x1: x + w, y1: y + 1, kind: wallHitAction, arg: int(wallActFilter)})
		}
		put(paint, w)
		put(" ", 1)
		button(wallButton{act: wallActFilterClear, label: "Clear", key: "esc"})
	default:
		put(pal.dim("Spaces  "), 8)
		sep := pal.dim("|")
		if !pal.ascii {
			sep = pal.dim("│")
		}
		first := true
		segment := func(at int, name string, count int, on bool) bool {
			dotted := at >= 0
			nw := ansi.StringWidth(name)
			cw := len(strconv.Itoa(count))
			w := 1 + nw + 1 + cw + 1
			if dotted {
				w += 2 + 2 // the dot and its space, and the kept tail
			}
			if !first {
				if !fitsAt(1 + w + 1) {
					return false
				}
				put(sep, 1)
			} else if !fitsAt(w + 1) {
				return false
			}
			first = false
			hot := (v.hover.kind == wallHitChip || v.hover.kind == wallHitChipMenu) && v.hover.arg == at
			menuHot := v.hover.kind == wallHitChipMenu && v.hover.arg == at
			ground := func(s string) string { return s }
			switch {
			case on:
				ground = func(s string) string { return pal.selected(s, 0) }
			case hot:
				ground = func(s string) string { return pal.cursor(s, 0) }
			}
			nameInk := pal.muted
			if on || hot {
				nameInk = pal.ink
			}
			var parts []wallPart
			x0 := x
			if dotted {
				parts = append(parts, wallPart{s: " " + wallSpaceMark(pal, v, at, "●") + " ", hot: menuHot})
				hits = append(hits, wallHit{x0: x0, y0: y, x1: x0 + 3, y1: y + 1, kind: wallHitChipMenu, arg: at})
				x0 += 3
				parts = append(parts, wallPart{s: nameInk(name) + " " + pal.dim(strconv.Itoa(count)) + " "})
			} else {
				parts = append(parts, wallPart{s: " " + nameInk(name) + " " + pal.dim(strconv.Itoa(count)) + " "})
			}
			end := x + w
			switch {
			case dotted && hot:
				hits = append(hits, wallHit{x0: x0, y0: y, x1: end - 2, y1: y + 1, kind: wallHitChip, arg: at})
				parts = append(parts, wallPart{s: pal.ink(k.menu) + " ", hot: menuHot})
				hits = append(hits, wallHit{x0: end - 2, y0: y, x1: end, y1: y + 1, kind: wallHitChipMenu, arg: at})
			case dotted:
				// The kept cells are the segment's until the pointer is on it, so
				// moving onto them is moving onto the segment.
				hits = append(hits, wallHit{x0: x0, y0: y, x1: end, y1: y + 1, kind: wallHitChip, arg: at})
				parts = append(parts, wallPart{s: "  "})
			default:
				hits = append(hits, wallHit{x0: x0, y0: y, x1: end, y1: y + 1, kind: wallHitChip, arg: at})
			}
			put(wallCompose(pal, parts, ground), w)
			return true
		}
		segment(-1, "All", v.total, v.space == "")
		for i, name := range v.spaces {
			on := name == v.space
			if ansi.StringWidth(name) > wallChipCap {
				name = ansi.Truncate(name, wallChipCap, g.more)
			}
			count := 0
			if i < len(v.counts) {
				count = v.counts[i]
			}
			if !segment(i, name, count, on) {
				break
			}
		}
		add := " + New space "
		if fitsAt(2 + len(add)) {
			put("  ", 2)
			s := pal.muted(add)
			if v.hover.kind == wallHitAddSpace {
				s = pal.cursor(pal.ink(add), 0)
			}
			hits = append(hits, wallHit{x0: x, y0: y, x1: x + len(add), y1: y + 1, kind: wallHitAddSpace})
			put(s, len(add))
		}
		// A space just made says so for a moment, in the row it now sits in.
		if v.made != "" && !v.madeAt.IsZero() && v.now.Sub(v.madeAt) < wallMadeFor {
			word := "  Made " + v.made + " " + g.sep + " " + strconv.Itoa(v.madeN)
			if fitsAt(ansi.StringWidth(word)) {
				put(pal.dim(word), ansi.StringWidth(word))
			}
		}
	}

	left := b.String()
	lw := x
	// The minimap says where the screen sits among the conversations, which
	// is news only when they do not all fit.
	if last-first >= len(v.tiles) {
		return ansi.Truncate(left, width, ""), wallHitsWithin(hits, width)
	}
	mm, cells := wallMinimap(pal, g, v, c, first, last, width-lw-3)
	mw := ansi.StringWidth(mm)
	if mm == "" || lw+1+mw+1 > width {
		return ansi.Truncate(left, width, ""), wallHitsWithin(hits, width)
	}
	at := width - 1 - mw
	for i, cx := range cells {
		if cx >= 0 {
			hits = append(hits, wallHit{x0: at + cx, y0: y, x1: at + cx + 1, y1: y + 1, kind: wallHitMini, arg: i})
		}
	}
	return left + strings.Repeat(" ", at-lw) + mm + " ", hits
}

// wallHitsWithin keeps the hits that end inside the row.
func wallHitsWithin(hits []wallHit, width int) []wallHit {
	kept := hits[:0]
	for _, h := range hits {
		if h.x1 <= width {
			kept = append(kept, h)
		}
	}
	return kept
}

func wallCloseMark(ascii bool) string {
	if ascii {
		return "x"
	}
	return "×"
}

// ── A TILE'S OWN CONTROLS AND ITS SPACES ────────────────────────────────────

// wallCtl is one control on a tile's top border, x cells from the first.
type wallCtl struct {
	kind wallHitKind
	word string
	x    int
}

// wallTileCtlsFull is the least tile width that spells `open ↗` out; below it
// the controls are their glyphs alone.
const wallTileCtlsFull = 44

// wallTileCtls is the controls a tile w wide carries at the right of its top
// border, and the cells they take: its spaces, open, and close. The answer
// depends on the width alone, never on hover or focus, so the cells are the
// same whether the controls are drawn or not.
func wallTileCtls(ascii bool, w int) ([]wallCtl, int) {
	k := wallKeysFor(ascii)
	open := "↗"
	if ascii {
		open = ">"
	}
	var words []string
	switch {
	case w >= wallTileCtlsFull:
		words = []string{" " + k.spaces + " ", " open " + open + " ", " " + wallCloseMark(ascii) + " "}
	case w >= 24:
		words = []string{" " + k.spaces + " ", " " + open + " ", " " + wallCloseMark(ascii) + " "}
	default:
		return nil, 0
	}
	kinds := []wallHitKind{wallHitSpaces, wallHitOpen, wallHitClose}
	ctls := make([]wallCtl, len(words))
	x := 0
	for i, word := range words {
		ctls[i] = wallCtl{kind: kinds[i], word: word, x: x}
		x += ansi.StringWidth(word)
	}
	return ctls, x
}

// wallTileDotsMax is the most dots a tile's border carries before the rest
// are a count.
const wallTileDotsMax = 3

// wallTileDots is the spaces a tile is in, as the dots on its border: up to
// three in their colours, then +N, then a space. It is "" for a tile in no
// space, which keeps no cells at all.
func wallTileDots(pal palette, v wallView, t wallTile) (string, int) {
	if len(t.spaces) == 0 {
		return "", 0
	}
	var b strings.Builder
	w := 0
	for i, sp := range t.spaces {
		if i == wallTileDotsMax {
			more := "+" + strconv.Itoa(len(t.spaces)-wallTileDotsMax)
			b.WriteString(pal.dim(more))
			w += len(more)
			break
		}
		b.WriteString(wallSpaceMark(pal, v, sp, "●"))
		w++
	}
	b.WriteString(" ")
	return b.String(), w + 1
}

// wallTileHits is where tile i's targets landed, its top-left corner at x0,y0.
// The first hit is always the one on the corner. A control is a target only
// while it is drawn; hidden, its cells are the tile's body, and moving onto
// them is hovering the tile, which draws it.
func wallTileHits(pal palette, v wallView, t wallTile, i int, focused bool, x0, y0, w, h int) []wallHit {
	look := wallLookFor(pal, v, t, i, focused)
	type seg struct {
		kind   wallHitKind
		x0, x1 int
	}
	var segs []seg
	push := func(kind wallHitKind, a, b int) {
		if b <= a {
			return
		}
		if n := len(segs); n > 0 && kind == wallHitTile && segs[n-1].kind == wallHitTile && segs[n-1].x1 == a {
			segs[n-1].x1 = b
			return
		}
		segs = append(segs, seg{kind, a, b})
	}
	at := x0
	if wallSelFits(w) && look.boxOn {
		push(wallHitTile, at, at+2)
		push(wallHitSelect, at+2, at+2+wallSelW)
		at += 2 + wallSelW
	}
	ctls, cw := wallTileCtls(pal.ascii, w)
	if cw > 0 && look.ctlOn {
		cx := x0 + w - 2 - cw
		push(wallHitTile, at, cx)
		for _, c := range ctls {
			push(c.kind, cx+c.x, cx+c.x+ansi.StringWidth(c.word))
		}
		at = cx + cw
	}
	push(wallHitTile, at, x0+w)
	hits := make([]wallHit, 0, len(segs)+1)
	for _, s := range segs {
		hits = append(hits, wallHit{x0: s.x0, y0: y0, x1: s.x1, y1: y0 + 1, kind: s.kind, arg: i})
	}
	if h > 1 {
		hits = append(hits, wallHit{x0: x0, y0: y0 + 1, x1: x0 + w, y1: y0 + h, kind: wallHitTile, arg: i})
	}
	return hits
}

// ── CARDS ───────────────────────────────────────────────────────────────────

// wallCard is a rounded card floated over the grid: its rows, its top-left
// cell, its width, and its own targets in frame cells.
type wallCard struct {
	rows []string
	x, y int
	w    int
	hits []wallHit
}

// wallCardLine is one row inside a card: painted words, and the targets on
// them in cells from the row's first inner cell. A rule line is drawn as the
// card's own divider.
type wallCardLine struct {
	s    string
	hits []wallHit
	rule bool
}

// wallCardBuild draws a card w wide at x,y: a title on the top border, then
// the lines, each padded padX cells inside the border and cut to fit.
func wallCardBuild(pal palette, title string, lines []wallCardLine, x, y, w, padX int) wallCard {
	box := wallBoxLight
	if pal.ascii {
		box = wallBoxLightASCII
	}
	edge := pal.muted
	inner := w - 2 - 2*padX
	card := wallCard{x: x, y: y, w: w}
	top := edge(box.tl + strings.Repeat(box.h, w-2) + box.tr)
	if title != "" {
		t := " " + title + " "
		top = edge(box.tl+box.h) + pal.ink(t) + edge(strings.Repeat(box.h, max(w-3-ansi.StringWidth(t), 0))+box.tr)
	}
	card.rows = append(card.rows, top)
	pad := strings.Repeat(" ", padX)
	for k, ln := range lines {
		if ln.rule {
			card.rows = append(card.rows, edge(box.v)+pad+pal.dim(strings.Repeat(box.h, inner))+pad+edge(box.v))
			continue
		}
		card.rows = append(card.rows, edge(box.v)+pad+wallFit(ln.s, inner)+pad+edge(box.v))
		for _, h := range ln.hits {
			if h.x1 > inner {
				continue
			}
			h.x0 += x + 1 + padX
			h.x1 += x + 1 + padX
			h.y0, h.y1 = y+1+k, y+2+k
			card.hits = append(card.hits, h)
		}
	}
	card.rows = append(card.rows, edge(box.bl+strings.Repeat(box.h, w-2)+box.br))
	return card
}

// wallCardPadX is the blank cells inside a card's border either side.
const wallCardPadX = 2

// wallTray is the selection tray: while any conversation is picked, a card
// just above the toolbar says how many, and holds everything that can be done
// to them.
func wallTray(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	n := wallMarked(v)
	if n == 0 || height < 8 {
		return wallCard{}
	}
	k := wallKeysFor(pal.ascii)
	word := strconv.Itoa(n) + " selected"
	lead := pal.accent(g.marked) + " " + pal.ink(word) + "  "
	leadW := ansi.StringWidth(g.marked) + 1 + len(word) + 2
	bs := []wallButton{
		{act: wallActMakeSpace, label: "Make space", key: "s"},
		{act: wallActAddTo, label: "Add to" + k.more + " " + k.caret},
		{act: wallActCloseViews, label: "Close views"},
		{act: wallActClear, label: "Clear", key: "esc"},
	}
	room := width - 2 - 2 - 2*wallCardPadX
	// Close views leaves first, then Add to: Make space and Clear are the two a
	// selection cannot do without.
	for _, act := range []wallAct{wallActCloseViews, wallActAddTo} {
		if leadW+wallBarWidth(bs, 1) <= room {
			break
		}
		for i, b := range bs {
			if b.act == act {
				bs = append(bs[:i], bs[i+1:]...)
				break
			}
		}
	}
	if leadW+wallBarWidth(bs, 1) > room {
		return wallCard{}
	}
	s, bw, hits := wallLay(pal, bs, v.hover, leadW, 0, 1)
	w := leadW + bw + 2 + 2*wallCardPadX
	x := (width - w) / 2
	return wallCardBuild(pal, "", []wallCardLine{{s: lead + s, hits: hits}}, x, height-1-3, w, wallCardPadX)
}

// wallNameCardRows is the new-space card's height: two borders and four lines.
const wallNameCardRows = 6

// wallNameCardFits reports whether the new-space card has room on a frame.
func wallNameCardFits(width, height int) bool {
	return width >= 44 && height >= wallChromeRows+wallNameCardRows+2
}

// wallSwatches is a row of colour choices, the one taken drawn ringed, and a
// target on each. It says how wide it is.
func wallSwatches(pal palette, v wallView, choices []spaceHueSpec, choice, x int) (string, int, []wallHit) {
	var b strings.Builder
	var hits []wallHit
	w := 0
	for j, c := range choices {
		if j > 0 {
			b.WriteString(" ")
			w++
		}
		glyph := "●"
		if j == choice {
			glyph = "◉"
		}
		ink := pal.spaceInk(c)
		switch {
		case ink != nil:
			glyph = ink(glyph)
		case j == choice:
			glyph = pal.ink("@")
		default:
			glyph = pal.dim("o")
		}
		if v.hover.kind == wallHitSwatch && v.hover.arg == j {
			glyph = pal.background(glyph, 0, pal.ramp.mark)
		}
		b.WriteString(glyph)
		hits = append(hits, wallHit{x0: x + w, y0: 0, x1: x + w + 1, y1: 1, kind: wallHitSwatch, arg: j})
		w++
	}
	return b.String(), w, hits
}

// wallNameCard is the card a new space is named in:
//
//	╭─ New space ──────────────────────────────────╮
//	│  Name    harbor▌                  ↻ Shuffle  │
//	│  Colour  ◉ ● ● ● ● ●                         │
//	│  3 · the tree walk, ship the port, relay au… │
//	│                        Cancel esc   Create ↵ │
//	╰──────────────────────────────────────────────╯
//
// A name the wall filled in is drawn selected, as a text field draws a
// suggestion: the first key typed replaces it.
func wallNameCard(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	if !wallNameCardFits(width, height) {
		return wallCard{}
	}
	k := wallKeysFor(pal.ascii)
	w := min(60, width-4)
	inner := w - 2 - 2*wallCardPadX

	shuffle := wallButton{act: wallActShuffle, label: k.shuffle + " Shuffle", key: "ctrl+r"}
	if inner < 46 {
		shuffle.key = ""
	}
	sw := wallButtonW(shuffle)
	const labelW = 8
	nameRoom := max(inner-labelW-sw-2-ansi.StringWidth(g.cursor), 1)
	name := v.name
	if ansi.StringWidth(name) > nameRoom {
		// The end of a long name is the part being typed.
		name = ansi.TruncateLeft(name, ansi.StringWidth(name)-nameRoom, "")
	}
	field := pal.ink(name)
	if v.nameFresh && name != "" {
		field = pal.selected(pal.ink(name), 0)
	}
	field += pal.ink(g.cursor)
	fieldW := labelW + ansi.StringWidth(name) + ansi.StringWidth(g.cursor)
	s, _, sh := wallLay(pal, []wallButton{shuffle}, v.hover, inner-sw, 0, 1)
	l1 := wallCardLine{s: pal.dim("Name    ") + field + strings.Repeat(" ", max(inner-sw-fieldW, 0)) + s, hits: sh}

	sws, _, swh := wallSwatches(pal, v, v.choices, v.choice, labelW)
	l2 := wallCardLine{s: pal.dim("Colour  ") + sws, hits: swh}

	var names []string
	for _, t := range v.tiles {
		if t.marked {
			names = append(names, t.name)
		}
	}
	who := strconv.Itoa(len(names)) + " " + g.sep + " " + strings.Join(names, ", ")
	if ansi.StringWidth(who) > inner {
		who = ansi.Truncate(who, inner, g.more)
	}
	l3 := wallCardLine{s: pal.dim(who)}

	bs := []wallButton{
		{act: wallActCancel, label: "Cancel", key: "esc"},
		{act: wallActSave, label: "Create", key: k.enter},
	}
	bw := wallBarWidth(bs, 1)
	bstr, _, bh := wallLay(pal, bs, v.hover, inner-bw, 0, 1)
	l4 := wallCardLine{s: strings.Repeat(" ", max(inner-bw, 0)) + bstr, hits: bh}

	x := (width - w) / 2
	gridH := height - wallChromeRows
	y := wallGridTop + max((gridH-wallNameCardRows)/2, 0)
	return wallCardBuild(pal, "New space", []wallCardLine{l1, l2, l3, l4}, x, y, w, wallCardPadX)
}

// ── POPOVERS ────────────────────────────────────────────────────────────────

// wallPopPadX is a popover's padding: one cell, since it is smaller than a
// card and sits against the control that opened it.
const wallPopPadX = 1

// wallPopCard is the popover that is up, placed under its control, or over it
// when there is no room below, and never past the frame's edges.
func wallPopCard(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	var title string
	var lines []wallCardLine
	var inner int
	switch v.pop.kind {
	case wallPopMembers:
		title, lines, inner = wallMembersLines(pal, g, v)
	case wallPopSettings:
		title, lines, inner = wallSettingsLines(pal, g, v)
	default:
		return wallCard{}
	}
	w := inner + 2 + 2*wallPopPadX
	h := len(lines) + 2
	if w > width || h > height-1 {
		return wallCard{}
	}
	x := min(max(v.pop.x, 0), width-w)
	y := v.pop.y1
	if y+h > height-1 {
		y = v.pop.y0 - h
	}
	y = min(max(y, 0), height-1-h)
	return wallCardBuild(pal, title, lines, x, y, w, wallPopPadX)
}

// wallPopRowPaint lays one popover row across the inner width, on the cursor
// ground when the keyboard or the pointer is on it.
func wallPopRowPaint(pal palette, s string, inner int, lit bool) string {
	s = wallFit(s, inner)
	if lit {
		return pal.cursor(s, 0)
	}
	return s
}

// wallMembersLines is the spaces popover: every space with a box saying
// whether the targets are in it (partly, when some are and some are not),
// and a way to a new one.
//
//	╭─ Spaces ─────────────────╮
//	│ ☑ ● harbor             3 │
//	│ ☐ ● orbit              5 │
//	│ ──────────────────────── │
//	│ + New space…             │
//	╰──────────────────────────╯
func wallMembersLines(pal palette, g wallGlyphs, v wallView) (string, []wallCardLine, int) {
	k := wallKeysFor(pal.ascii)
	in := map[string][]int{}
	for _, t := range v.tiles {
		in[t.tab.key] = t.spaces
	}
	inner := 24
	for _, name := range v.spaces {
		inner = max(inner, ansi.StringWidth(k.boxOff)+3+min(ansi.StringWidth(name), wallChipCap)+6)
	}
	var lines []wallCardLine
	row := func(code int, s string, cursorAt int) {
		lit := v.pop.cursor == cursorAt || (v.hover.kind == wallHitPopRow && v.hover.arg == code)
		lines = append(lines, wallCardLine{
			s:    wallPopRowPaint(pal, s, inner, lit),
			hits: []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: code}},
		})
	}
	for i, name := range v.spaces {
		held := 0
		for _, key := range v.pop.targets {
			for _, sp := range in[key] {
				if sp == i {
					held++
				}
			}
		}
		box := pal.muted(k.boxOff)
		switch {
		case held > 0 && held == len(v.pop.targets):
			box = pal.ink(k.boxOn)
		case held > 0:
			box = pal.ink(k.boxSome)
		}
		if ansi.StringWidth(name) > wallChipCap {
			name = ansi.Truncate(name, wallChipCap, g.more)
		}
		count := 0
		if i < len(v.counts) {
			count = v.counts[i]
		}
		left := box + " " + wallSpaceMark(pal, v, i, "●") + " " + pal.ink(name)
		cs := strconv.Itoa(count)
		gap := inner - ansi.StringWidth(left) - len(cs)
		row(i, left+strings.Repeat(" ", max(gap, 1))+pal.dim(cs), i)
	}
	if len(v.spaces) > 0 {
		lines = append(lines, wallCardLine{rule: true})
	}
	row(wallPopNew, pal.muted("+ New space"+k.more), len(v.spaces))
	return "Spaces", lines, inner
}

// wallSettingsLines is a space's settings: its name, being edited as it is
// typed, its colour among the others it could have, and its deletion, which
// asks first and says the conversations stay open.
func wallSettingsLines(pal palette, g wallGlyphs, v wallView) (string, []wallCardLine, int) {
	i := v.pop.space
	name := ""
	if i >= 0 && i < len(v.spaces) {
		name = v.spaces[i]
	}
	inner := 30
	const labelW = 8
	field := v.pop.name
	if room := inner - labelW - 1; ansi.StringWidth(field) > room {
		field = ansi.TruncateLeft(field, ansi.StringWidth(field)-room, "")
	}
	l1 := wallCardLine{s: pal.dim("Name    ") + pal.ink(field) + pal.ink(g.cursor)}
	sws, _, swh := wallSwatches(pal, v, v.pop.choices, v.pop.choice, labelW)
	l2 := wallCardLine{s: pal.dim("Colour  ") + sws, hits: swh}
	lines := []wallCardLine{l1, l2, {rule: true}}
	hit := func(code int) []wallHit {
		return []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: code}}
	}
	lit := func(code int) bool { return v.hover.kind == wallHitPopRow && v.hover.arg == code }
	if !v.pop.confirm {
		lines = append(lines, wallCardLine{s: wallPopRowPaint(pal, pal.bad("Delete space"), inner, lit(wallPopDelete)), hits: hit(wallPopDelete)})
	} else {
		ask := "Delete " + name + "?"
		if ansi.StringWidth(ask) > inner {
			ask = ansi.Truncate(ask, inner, g.more)
		}
		lines = append(lines,
			wallCardLine{s: pal.ink(ask)},
			wallCardLine{s: pal.dim("Its conversations stay open.")})
		del, keep := " Delete ", " Keep "
		ds, ks := pal.bad(del), pal.ink(keep)
		if lit(wallPopConfirm) {
			ds = pal.cursor(ds, 0)
		}
		if lit(wallPopKeep) {
			ks = pal.cursor(ks, 0)
		}
		lines = append(lines, wallCardLine{s: ds + " " + ks, hits: []wallHit{
			{x0: 0, y0: 0, x1: len(del), y1: 1, kind: wallHitPopRow, arg: wallPopConfirm},
			{x0: len(del) + 1, y0: 0, x1: len(del) + 1 + len(keep), y1: 1, kind: wallHitPopRow, arg: wallPopKeep},
		}})
	}
	return "Space", lines, inner
}

// ── LAYING A CARD OVER THE FRAME ────────────────────────────────────────────

// wallSplice lays a card's row over a frame row at column x. The row is padded
// to the frame's width first, so a card can float over a blank row, and the
// card is fenced by resets so no ground leaks in or out of it.
func wallSplice(row, card string, x, width int) string {
	row = wallFit(row, width)
	cw := ansi.StringWidth(card)
	left := ansi.Cut(row, 0, x)
	right := ansi.Cut(row, x+cw, width)
	if !strings.Contains(row, "\x1b") && !strings.Contains(card, "\x1b") {
		return left + card + right
	}
	return left + "\x1b[0m" + card + "\x1b[0m" + right
}

// wallCarve takes the rectangle x0,y0..x1,y1 out of every hit, keeping what is
// left of each around it, so nothing under a card answers the pointer.
func wallCarve(hits []wallHit, x0, y0, x1, y1 int) []wallHit {
	out := make([]wallHit, 0, len(hits))
	for _, h := range hits {
		if h.x1 <= x0 || h.x0 >= x1 || h.y1 <= y0 || h.y0 >= y1 {
			out = append(out, h)
			continue
		}
		if h.y0 < y0 {
			a := h
			a.y1 = y0
			out = append(out, a)
		}
		if h.y1 > y1 {
			a := h
			a.y0 = y1
			out = append(out, a)
		}
		mid := h
		mid.y0, mid.y1 = max(h.y0, y0), min(h.y1, y1)
		if h.x0 < x0 {
			a := mid
			a.x1 = x0
			out = append(out, a)
		}
		if h.x1 > x1 {
			a := mid
			a.x0 = x1
			out = append(out, a)
		}
	}
	return out
}

// wallOverlay lays a card over the frame: its rows spliced in, what it covers
// taken out of the hits, its own targets added.
func wallOverlay(rows []string, hits []wallHit, card wallCard, width int) []wallHit {
	if len(card.rows) == 0 {
		return hits
	}
	for dy, cr := range card.rows {
		if y := card.y + dy; y >= 0 && y < len(rows) {
			rows[y] = wallSplice(rows[y], cr, card.x, width)
		}
	}
	hits = wallCarve(hits, card.x, card.y, card.x+card.w, card.y+len(card.rows))
	return append(hits, card.hits...)
}
