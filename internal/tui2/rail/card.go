package rail

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Card anatomy (5.9). A card answers three questions in priority order — does
// it need me? what is it doing? what is it costing? — and each gets a line:
//
//	◐ wisp-parity                                ›   glyph + name + composer mark
//	  reworking NavCtx after the worker died         first person, one line
//	  K3 · ▄ · $8.65 · 41m · 4w                      dim, tabular, money always
//
// A focused card expands IN PLACE: step dots (5.21), per-worker rows, and the
// artifact it produced (12.5.1). Full depth is still one room away — the card
// never becomes the room.
//
// Plan steps and workers inside a task scope are not cards; they are tree rows,
// one line each with the elapsed flush right, plus a waits-on line when they are
// blocked behind a sibling. That is the shape 5.15's wireframe draws and it is
// what makes the DAG legible in 28 columns.

// rowShape is which lines a row draws. Height and rendering both read it, so
// the two can never disagree about how tall a row is — a fold that budgets one
// height and a render that draws another is a frame that overflows its pane.
type rowShape struct {
	status   bool
	meta     bool
	waits    bool
	dots     bool
	artifact bool
	workers  int
	more     int
}

func (s rowShape) height() int {
	h := 1
	for _, on := range [...]bool{s.status, s.meta, s.waits, s.dots, s.artifact} {
		if on {
			h++
		}
	}
	h += s.workers
	if s.more > 0 {
		h++
	}
	return h
}

// shapeOf decides a row's lines. The progressive-disclosure rule of 5.9 is the
// whole of it: collapsed is the summary, focused expands in place.
func shapeOf(r Row, sel bool) rowShape {
	var s rowShape
	switch r.Kind {
	case RowStep, RowWorker:
		// A tree row is one line, telemetry included: it carries its cells on
		// the right of its own name rather than on a line of its own, which is
		// what makes a five-node DAG legible in 28 columns. It grows only under
		// focus, where the reader has asked for it.
		s.waits = len(r.WaitsOn) > 0
		if sel {
			s.status = clean(r.Status) != ""
			s.artifact = !r.Artifact.Empty()
		}
	default:
		s.status = clean(r.Status) != ""
		s.meta = !r.Meta.Empty()
		s.waits = len(r.WaitsOn) > 0
		if sel {
			s.dots = len(r.Steps) > 0
			s.artifact = !r.Artifact.Empty()
			s.workers = len(r.Workers)
			if s.workers > maxCardWorkers {
				s.more = s.workers - maxCardWorkers
				s.workers = maxCardWorkers
			}
		}
	}
	return s
}

// appendRow draws one row's lines into the View's buffer, stopping at the
// height limit. Every line it produces is at most width printable cells.
func (v *View) appendRow(r Row, sel bool, width, limit int, band tokens.Token, banded bool, ident tokens.Token) {
	s := shapeOf(r, sel)
	banded = banded && sel
	indent := gutterFor(width) + r.Depth*indentStep
	sub := indent + indentStep

	switch r.Kind {
	case RowStep, RowWorker:
		v.push(v.treeLine(r, width, indent, sel, banded, band, ident), limit)
	default:
		v.push(v.cardLine(r, width, indent, sel, banded, band, ident), limit)
	}
	if s.status {
		v.push(v.statusLine(r, width, sub, banded, band), limit)
	}
	if s.meta {
		v.push(v.metaLine(r.Meta, width, sub, banded, band), limit)
	}
	if s.waits {
		v.push(v.waitsLine(r, width, sub, banded, band), limit)
	}
	if s.dots {
		v.push(v.dotsLine(r, width, sub, banded, band), limit)
	}
	for i := 0; i < s.workers; i++ {
		w := r.Workers[i]
		w.Kind = RowWorker
		w.Depth = r.Depth + 1
		v.push(v.treeLine(w, width, sub, false, banded, band, ident), limit)
	}
	if s.more > 0 {
		v.push(v.moreLine(s.more, width, sub, banded, band), limit)
	}
	if s.artifact {
		v.push(v.artifactLine(r.Artifact, width, sub, banded, band), limit)
	}
}

// cardLine is line 1 of a card: the attention glyph, the name, an optional
// count chip, and the composer-mode mark at the right edge (5.11). It is the
// only saturated colour on the card.
func (v *View) cardLine(r Row, width, indent int, sel, banded bool, band, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, sel, banded, ident)
	l.padTo(indent)
	v.addGlyph(l, r, ident)

	chip := ""
	if r.Questions > 1 {
		var buf [16]byte
		out := append(buf[:0], tokens.GlyphNeedsHuman...)
		out = strconv.AppendInt(out, int64(r.Questions), 10)
		chip = string(out)
	}
	mark := r.EffectiveComposer().Mark()
	rightW := blocks.Width(chip) + blocks.Width(mark)
	if chip != "" {
		rightW++
	}
	if mark != "" {
		rightW++
	}
	room := l.max - l.w - rightW
	l.add(blocks.Truncate(clean(r.Name), room), v.nameToken(r))
	if rightW > 0 && l.max-l.w >= rightW {
		l.padTo(l.max - rightW)
		if chip != "" {
			l.add(chip, tokens.Amber)
			l.add(" ", tokens.TextTertiary)
		}
		if mark != "" {
			l.add(mark, tokens.TextTertiary)
		}
	}
	return v.emit(width, banded, band)
}

// treeLine is a plan step or a worker inside a task scope: glyph, name, and as
// much telemetry as the row can afford, flush right. The cells claim their room
// BEFORE the name does, so a name growing by a character never pushes a number
// off the row — the width-stability law of 5.21 applied to a whole line rather
// than to one cell.
func (v *View) treeLine(r Row, width, indent int, sel, banded bool, band, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, sel, banded, ident)
	l.padTo(indent)
	v.addGlyph(l, r, ident)

	n := 0
	rightW := 0
	if !r.Meta.Empty() {
		budget := l.room() - minNameWidth - 1
		n = v.fitMetaInto(r.Meta, budget)
		if n > 0 {
			rightW = metaWidth(v.meta[:n]) + 1
		}
	}
	l.add(blocks.Truncate(clean(r.Name), l.room()-rightW), v.nameToken(r))
	if n > 0 && l.room() >= rightW {
		l.padTo(l.max - rightW + 1)
		v.addMeta(l, n)
	}
	return v.emit(width, banded, band)
}

// statusLine is line 2: what the row is doing, in its own words. A cut turn
// ends the line visibly cut (12.5.2) — the mark is sticky, so it survives on a
// narrow rail after the words have gone.
func (v *View) statusLine(r Row, width, indent int, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	l.padTo(indent)
	cutW := 0
	if r.Cut != tokens.CutNone {
		cutW = 2 // a space and the mark
	}
	room := l.max - l.w - cutW
	l.add(blocks.Truncate(clean(r.Status), room), tokens.TextSecondary)
	if r.Cut != tokens.CutNone {
		tok := tokens.CutToken(r.Cut)
		if word := cutMark(r.Cut); word != "" && l.max-l.w > blocks.Width(word)+3 {
			l.add(" ", tokens.TextTertiary)
			l.add(tokens.GlyphCut, tok)
			l.add(" ", tokens.TextTertiary)
			l.add(blocks.Truncate(word, l.max-l.w), tok)
		} else if l.max-l.w >= cutW {
			l.add(" ", tokens.TextTertiary)
			l.add(tokens.GlyphCut, tok)
		}
	}
	return v.emit(width, banded, band)
}

// waitsLine names the siblings a row is blocked behind — the waits-on structure
// 5.15 asks a task scope to make visible, in names rather than in edges.
func (v *View) waitsLine(r Row, width, indent int, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	l.padTo(indent)
	l.add("waits on ", tokens.TextTertiary)
	for i, name := range r.WaitsOn {
		if l.room() <= 0 {
			break
		}
		if i > 0 {
			l.add(sep, tokens.TextTertiary)
		}
		l.add(blocks.Truncate(clean(name), l.room()), tokens.TextTertiary)
	}
	return v.emit(width, banded, band)
}

// dotsLine is plan progress (5.21): one dot per step, filled done, half
// running, amber ⚑ blocked — discrete and honest, mapping 1:1 to steps. When
// the dots will not fit, it falls back to 5.17's gauge form (`▆ 5/7`), because
// a truncated dot row would lie about how many steps there are. The numbers
// stay the primary encoding either way (5.13).
func (v *View) dotsLine(r Row, width, indent int, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	l.padTo(indent)
	done, total := StepProgress(r.Steps)
	var buf [24]byte
	out := strconv.AppendInt(buf[:0], int64(done), 10)
	out = append(out, '/')
	out = strconv.AppendInt(out, int64(total), 10)
	progress := string(out)
	room := l.room() - blocks.Width(progress) - 2
	if total > 0 && room >= total {
		for i := range r.Steps {
			l.add(r.Steps[i].Dot(), r.Steps[i].token())
		}
	} else if total > 0 {
		frac := 0.0
		if total > 0 {
			frac = float64(done) / float64(total)
		}
		l.add(tokens.Gauge(frac), tokens.TextSecondary)
	}
	l.add("  ", tokens.TextTertiary)
	l.add(blocks.Truncate(progress, l.room()), tokens.TextTertiary)
	return v.emit(width, banded, band)
}

// moreLine accounts for the workers a focused card did not expand into. It is
// the fold-line grammar of 8.1.7 at card scale.
func (v *View) moreLine(more, width, indent int, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	l.padTo(indent)
	var buf [40]byte
	out := append(buf[:0], "… "...)
	out = strconv.AppendInt(out, int64(more), 10)
	out = append(out, " more"...)
	l.add(blocks.Truncate(string(out), l.room()), tokens.TextTertiary)
	return v.emit(width, banded, band)
}

// artifactLine is the artifact law made visible (12.5.1): the deliverable is on
// disk and the card points at it. The path is middle-ellipsised, because the
// filename is the information (5.21).
func (v *View) artifactLine(ref Ref, width, indent int, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	l.padTo(indent)
	l.add(tokens.GlyphTreeLast, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	text := ref.Path
	if text == "" {
		text = ref.Label
	}
	l.add(blocks.TruncatePath(clean(text), l.room()), tokens.TextSecondary)
	return v.emit(width, banded, band)
}

// metaCell is one telemetry cell: the form it would like to draw, the shorter
// form it will settle for, and the priority that decides whether it survives a
// narrow rail at all.
type metaCell struct {
	text string
	alt  string
	tok  tokens.Token
	prio int
}

// metaLine is line 3: the dimmest tier, tabular, and money is never dropped
// (5.9 — it is the one number the user never forgives us for hiding).
func (v *View) metaLine(t Telemetry, width, indent int, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	l.padTo(indent)
	v.addMeta(l, v.fitMetaInto(t, l.room()))
	return v.emit(width, banded, band)
}

// fitMetaInto fills the cell array with as much telemetry as room allows and
// returns how many cells survived. It degrades before it drops, in that order,
// because a shorter form of a number still answers the question and an absent
// one does not:
//
//  1. FULL forms — `K3 · high`, `17%/1M`, `4 workers`.
//  2. COMPACT forms — `K3`, the one-cell gauge ▄ (5.17's ambient form for
//     context), `4w`.
//  3. DROP the lowest priority, one at a time, until the rest fit. Money is
//     pinned at the top of the ladder and is the last thing standing.
//
// This is 10.5.22's priority-drop discipline at card scale, and the reason it
// is room-driven rather than mode-driven is 5.15: the rail and the full-pane
// list render the SAME rows. What differs between them is how many columns
// there are, which is exactly what this function is reading.
func (v *View) fitMetaInto(t Telemetry, room int) int {
	if room <= 0 {
		return 0
	}
	n := v.buildMeta(t)
	if metaWidth(v.meta[:n]) > room {
		for i := 0; i < n; i++ {
			if v.meta[i].alt != "" {
				v.meta[i].text = v.meta[i].alt
			}
		}
	}
	return fitMeta(v.meta[:n], room)
}

// addMeta writes the first n fitted cells, separated by ` · `.
func (v *View) addMeta(l *lineBuf, n int) {
	for i := 0; i < n; i++ {
		if i > 0 {
			l.add(sep, tokens.TextTertiary)
		}
		l.add(v.meta[i].text, v.meta[i].tok)
	}
}

// buildMeta fills the cell array in display order, both forms at once, and
// returns how many cells there are. Building both is cheaper than building
// twice: the cells whose two forms are the same string — money, elapsed, a
// model word with no effort beside it — are built once and share it.
func (v *View) buildMeta(t Telemetry) int {
	n := 0
	add := func(text, alt string, tok tokens.Token, prio int) {
		if text == "" || n >= len(v.meta) {
			return
		}
		v.meta[n] = metaCell{text: text, alt: alt, tok: tok, prio: prio}
		n++
	}
	if t.Model != "" {
		model := clean(t.Model)
		if t.Boosted {
			model = model + tokens.GlyphBoosted
		}
		if t.Effort != "" {
			add(model+sep+clean(t.Effort), model, tokens.TextTertiary, prioModel)
		} else {
			add(model, "", tokens.TextTertiary, prioModel)
		}
	}
	if t.ContextWindow > 0 {
		tok := tokens.ContextToken(t.ContextUsed, t.ContextWindow)
		add(tokens.Context(t.ContextUsed, t.ContextWindow),
			tokens.Gauge(float64(t.ContextUsed)/float64(t.ContextWindow)), tok, prioContext)
	}
	if t.HasCost {
		add(tokens.Money(t.Cost), "", tokens.Green, prioMoney)
	}
	if t.HasElapsed {
		add(tokens.ElapsedCell(t.Elapsed), "", tokens.ElapsedToken(t.Elapsed, t.Estimate), prioElapsed)
	}
	switch {
	case t.Atomic:
		add("atomic", "", tokens.TextTertiary, prioWorkers)
	case t.HasWorkers:
		var buf [32]byte
		compact := append(strconv.AppendInt(buf[:0], int64(t.Workers), 10), 'w')
		full := append(strconv.AppendInt(buf[16:16], int64(t.Workers), 10), " worker"...)
		if t.Workers != 1 {
			full = append(full, 's')
		}
		add(string(full), string(compact), tokens.TextTertiary, prioWorkers)
	}
	return n
}

// metaWidth is what the cells cost, separators included.
func metaWidth(cells []metaCell) int {
	if len(cells) == 0 {
		return 0
	}
	total := sepWidth * (len(cells) - 1)
	for i := range cells {
		total += blocks.Width(cells[i].text)
	}
	return total
}

// fitMeta drops the lowest-priority cells until the rest fit, then unpads a
// trailing elapsed cell — the pad exists so the cells to its RIGHT do not
// dance (5.21), and when there are none it is only a hole.
func fitMeta(cells []metaCell, room int) int {
	n := len(cells)
	for n > 0 && metaWidth(cells[:n]) > room {
		victim, worst := -1, 1<<31-1
		for i := 0; i < n; i++ {
			if cells[i].prio < worst {
				victim, worst = i, cells[i].prio
			}
		}
		if victim < 0 {
			return 0
		}
		copy(cells[victim:], cells[victim+1:n])
		n--
	}
	if n > 0 && cells[n-1].prio == prioElapsed {
		cells[n-1].text = strings.TrimLeft(cells[n-1].text, " ")
	}
	return n
}

// gutterFor is the left column's width at a given row width. Below
// [gutterFloor] there is no column to spare and the content takes it.
func gutterFor(width int) int {
	if width < gutterFloor {
		return 0
	}
	return gutterWidth
}

// gutter draws the left column. It carries the ▎ accent rail (5.21) that marks
// the selection while the pane is unfocused, because a dimmed foreground may
// not sit on a selection band (tokens.Legal) — so an unfocused pane marks its
// cursor with an accent instead of a ground.
func (v *View) gutter(l *lineBuf, sel, banded bool, ident tokens.Token) {
	if l.max < gutterFloor {
		return
	}
	if sel && !banded {
		tok := ident
		if tok < tokens.Identity0 || tok > tokens.Identity7 {
			tok = tokens.TextSecondary
		}
		l.add(tokens.GlyphAccentRail, tok)
		return
	}
	l.padTo(gutterWidth)
}
