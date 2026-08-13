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
//	  K3 · ▄ · $8.65 · 41m · 2◐ 2✓                   dim, tabular, money always
//
// A focused card expands IN PLACE by at most [previewLines]: the artifact it
// produced (12.5.1), and on a tree row its own words. Full depth is still one
// room away — the card never becomes the room.
//
// LINE 3 CARRIES A CENSUS, NEVER A FRACTION AND NEVER A HEADCOUNT (§14). It
// used to end `4 workers` or `atomic`, and both were the machinery describing
// itself: a reader does not act on how many hands a job has, and a job with one
// part has nothing to say about its shape at all. What replaces them is
// [Telemetry.Counts] — `2◐ 2✓`, glyph and count, dim — which answers the
// question the headcount was standing in for and stays true across a replan
// that a denominator could not survive.
//
// PER-WORKER ROWS ARE NOT IN THE PREVIEW, and the reason is 7.2 rather than
// taste. A preview is a bounded thing: it is drawn because a cursor paused, and
// whatever it costs is charged to the rows around it (see the detail reserve in
// renderMap). The census and an artifact are SUMMARIES — one cell and one line,
// whatever the plan's size. A worker list is not — it used to spend six rows on
// one card — and every row it adds is a card the reader was about to click
// sliding into the fold, out from under the pointer already aimed at it. The
// workers themselves are the TREE one room down, which is where 5.9 puts full
// depth — internal/tui2/chat already blanks the surface row's workers there so
// the room never draws them twice.
//
// Plan steps and workers inside a task scope are not cards; they are tree rows,
// one line each: a connector into the row, the state glyph, the name, and the
// receipt flush right, plus a `waits: <deps>` line when they are blocked behind
// a sibling. That is the shape 5.15's wireframe draws and it is what makes the
// DAG legible in 28 columns.

// rowShape is which lines a row draws. Height and rendering both read it, so
// the two can never disagree about how tall a row is — a fold that budgets one
// height and a render that draws another is a frame that overflows its pane.
type rowShape struct {
	status   bool
	meta     bool
	waits    bool
	artifact bool
}

func (s rowShape) height() int {
	h := 1
	for _, on := range [...]bool{s.status, s.meta, s.waits, s.artifact} {
		if on {
			h++
		}
	}
	return h
}

// treeGuide is a member row's place in the plan tree (§3): which ancestor
// levels still have a branch running past it, and whether it is the last child
// of its own parent.
//
// It is computed once per frame from the rows' depths (see [View.sizeGuides])
// and never carried on a [Row], because it is a fact about a row's NEIGHBOURS.
// A source asked to state it would be stating something it can get wrong, and a
// tree drawn with a ├ where a ╰ belongs is a picture of a plan that does not
// exist.
type treeGuide struct {
	// on is whether this row draws a connector at all. A JOB SCOPE's members
	// all do, and at home the rows that carry [Row.Tree] do: the home rail is a
	// list of conversations, jobs and doors, and only the plan steps hanging off
	// a job card have a parentage to draw. A tree over the rest would claim a
	// structure those rows do not have.
	on bool
	// last says the row is the last child at its depth: its branch is the
	// corner, and the guide under it is blank.
	last bool
	// level is how many tree columns stand to the left of this row's branch: 0
	// for a first-level limb, 1 for its child.
	//
	// It is measured from the shallowest connector row in the SCOPE rather than
	// from the row's own depth, because the same tree is drawn at two altitudes.
	// Inside a job the steps are the surface row's children and sit at depth 0;
	// at home they hang off a card that is itself a member, so they sit at depth
	// 1 — and a tree that indented by six columns at home and three inside the
	// job would be one grammar with two spellings, in a column that has 28.
	level int
	// open is the set of ancestor LEVELS whose branch continues below this row,
	// one bit per level. A set bit draws the vertical guide in that column; a
	// clear one draws the three spaces that say the ancestor is finished.
	open uint8
}

// The connector grammar, in the tokens table's own geometry (5.17: box drawing
// is structure, not iconography, so these are the slots the glyph tier leaves
// alone and the ones an ASCII repertoire would rewrite in one place).
const (
	// treeStep is what one level of the tree costs. Three cells, because the
	// branch IS the indent: `├─ ` says both "one deeper" and "there is more
	// below" in the columns a plain indent would have spent saying neither.
	treeStep   = 3
	guideVert  = tokens.GlyphTreeVert + "  "
	guideBlank = "   "
	branchMid  = tokens.GlyphTreeBranch + tokens.GlyphTreeDash + " "
	branchLast = tokens.GlyphTreeLast + tokens.GlyphTreeDash + " "
)

// rowIndent is where a row's cells begin: the chrome to its left, its depth,
// and the connector that depth is drawn as. Height and every line builder read
// the same value, so a row's first line and its detail lines cannot disagree
// about which column the content starts in.
type rowIndent struct {
	// base is the gutter plus the merged scope header's lead — everything the
	// row shares with every other row, whatever its depth.
	base  int
	depth int
	guide treeGuide
}

// glyphCol is where the row's state glyph goes.
func (a rowIndent) glyphCol() int {
	if a.guide.on {
		return a.base + (a.guide.level+1)*treeStep
	}
	return a.base + a.depth*indentStep
}

// textCol is where a detail line under the row goes: under the name, past the
// glyph column the first line spent.
func (a rowIndent) textCol() int { return a.glyphCol() + indentStep }

// indentTo puts the line at a column, drawing the tree on the way when the row
// has one. sub asks for the CONTINUATION rather than the branch — a detail line
// under a row that has siblings below it keeps the vertical guide running past
// it, and one under the last child draws blank, which is the same rule the row
// above it obeyed.
func (v *View) indentTo(l *lineBuf, at rowIndent, sub bool) {
	l.padTo(at.base)
	if at.guide.on {
		for k := 0; k < at.guide.level; k++ {
			if at.guide.open&(1<<uint(k)) != 0 {
				l.add(guideVert, tokens.TextTertiary)
			} else {
				l.add(guideBlank, tokens.TextTertiary)
			}
		}
		switch {
		case sub && at.guide.last:
			l.add(guideBlank, tokens.TextTertiary)
		case sub:
			l.add(guideVert, tokens.TextTertiary)
		case at.guide.last:
			l.add(branchLast, tokens.TextTertiary)
		default:
			l.add(branchMid, tokens.TextTertiary)
		}
	}
	if sub {
		l.padTo(at.textCol())
		return
	}
	l.padTo(at.glyphCol())
}

// shapeOf decides a row's lines. The progressive-disclosure rule of 5.9 is the
// whole of it: collapsed is the summary, focused expands in place.
//
// detail is the line budget the SELECTION may spend, and the preview rows claim
// it in the order they are drawn in — so a reserve of one buys a tree row its
// own words and a reserve of none leaves the row exactly as it was. It is passed in
// rather than read from a constant here because it is the renderer, not the row,
// that knows what a preview may cost the rows around it (renderMap).
func (v *View) shapeOf(r Row, sel bool, detail int) rowShape {
	var s rowShape
	// spend claims one of the reserved lines for a preview row, and reports
	// whether the row gets to exist at all.
	spend := func(want bool) bool {
		if !want || detail <= 0 {
			return false
		}
		detail--
		return true
	}
	switch r.Kind {
	case RowSection, RowNote:
		// Chrome is one line and stays one line. A heading that grew under the
		// cursor could not — the cursor cannot rest on one.
		return s
	case RowThread:
		// TWO LINES, ALWAYS. The left-at line is not a preview: "what was this
		// conversation saying when I walked away" is the question the threads
		// list exists to answer, and a status that appeared only under the
		// cursor would answer it one row at a time, which is a list the reader
		// has to interrogate instead of read. The relative time rides line 1
		// beside the name, so there is no third line to drop.
		s.status = v.clean(r.Status) != ""
	case RowStep, RowWorker:
		// A tree row is one line, telemetry included: it carries its cells on
		// the right of its own name rather than on a line of its own, which is
		// what makes a five-node DAG legible in 28 columns. It grows only under
		// focus, where the reader has asked for it.
		s.waits = len(r.WaitsOn) > 0
		if sel {
			s.status = spend(v.clean(r.Status) != "")
			s.artifact = spend(!r.Artifact.Empty())
		}
	default:
		s.status = v.clean(r.Status) != ""
		s.meta = !r.Meta.Empty()
		s.waits = len(r.WaitsOn) > 0
		if sel {
			s.artifact = spend(!r.Artifact.Empty())
		}
	}
	return s
}

// appendRow draws one row's lines into the View's buffer, stopping at the
// height limit. Every line it produces is at most width printable cells.
func (v *View) appendRow(r Row, sel bool, width, limit, detail int,
	band tokens.Token, banded bool, ident tokens.Token, guide treeGuide) {

	s := v.shapeOf(r, sel, detail)
	banded = banded && sel
	at := rowIndent{base: gutterFor(width) + v.leadWidth(), depth: r.Depth, guide: guide}

	switch r.Kind {
	case RowSection:
		v.push(v.sectionLine(r, width), limit)
		return
	case RowNote:
		v.push(v.noteLine(r, width, at), limit)
		return
	case RowThread:
		v.push(v.threadLine(r, width, at, sel, banded, band, ident), limit)
	case RowStep, RowWorker:
		v.push(v.treeLine(r, width, at, sel, banded, band, ident), limit)
	default:
		v.push(v.cardLine(r, width, at, sel, banded, band, ident), limit)
	}
	if s.status {
		v.push(v.statusLine(r, width, at, banded, band), limit)
	}
	if s.meta {
		v.push(v.metaLine(v.telemetry(r), width, at, banded, band), limit)
	}
	if s.waits {
		v.push(v.waitsLine(r, width, at, banded, band), limit)
	}
	if s.artifact {
		v.push(v.artifactLine(r.Artifact, width, at, banded, band), limit)
	}
}

// cardLine is line 1 of a card: the attention glyph, the name, an optional
// count chip, and the composer-mode mark at the right edge (5.11). It is the
// only saturated colour on the card.
func (v *View) cardLine(r Row, width int, at rowIndent, sel, banded bool, band, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, sel, banded, ident)
	// The scope header's ‹ , when this row has absorbed it (renderMap). It sits
	// between the gutter and the glyph so the row still reads left to right as
	// "out of here · what this is · what it is called", and the indent it takes
	// was already budgeted by [View.leadWidth] so the lines under it line up.
	if v.lead != "" && l.room() > 0 {
		// The lead's column range is recorded as it is drawn, because a click
		// on this row means two different things either side of the glyph: on
		// the ‹ it pops the scope, anywhere else it is row 0 like any other row.
		// Measuring the span from the buffer rather than from an assumed gutter
		// width is what keeps that true at the narrow widths where the gutter
		// is not there at all (see gutterFor).
		from := l.w
		l.add(v.lead, tokens.TextTertiary)
		if l.w > from {
			// The target is the glyph AND the space [View.leadWidth] budgeted
			// after it. One cell is a target you have to aim at; two is a target
			// you can hit, and the second cell belongs to the lead already —
			// nothing else is ever drawn there.
			v.upLine, v.upFrom, v.upTo = len(v.lines), from, from+v.leadWidth()
		}
	}
	l.padTo(at.glyphCol())
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
	l.add(blocks.Truncate(v.clean(r.Name), room), v.nameToken(r))
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

// treeLine is a plan step or a worker inside a task scope: the connector into
// the row, the state glyph, the name, and as much of the receipt as the row can
// afford, flush right. The cells claim their room BEFORE the name does, so a
// name growing by a character never pushes a number off the row — the
// width-stability law of 5.21 applied to a whole line rather than to one cell.
//
// THE DEPTH IS THE CONNECTOR (§3). v1 drew the plan as `├─`/`╰─` with `│`
// guides running down past the rows that still have siblings below them, and it
// is the right grammar for the reason a plain indent is not: two spaces say a
// row is deeper than the one above and nothing about WHOSE it is, so a reader
// counting columns has to hold the whole subtree in their head to know where a
// branch ended. The guides draw that fact instead.
func (v *View) treeLine(r Row, width int, at rowIndent, sel, banded bool, band, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, sel, banded, ident)
	v.indentTo(l, at, false)
	v.addGlyph(l, r, ident)

	n := 0
	rightW := 0
	if !r.Meta.Empty() {
		budget := l.room() - minNameWidth - 1
		n = v.fitMetaInto(v.telemetry(r), budget)
		if n > 0 {
			rightW = metaWidth(v.meta[:n]) + 1
		}
	}
	l.add(blocks.Truncate(v.clean(r.Name), l.room()-rightW), v.nameToken(r))
	if n > 0 && l.room() >= rightW {
		l.padTo(l.max - rightW + 1)
		v.addMeta(l, n)
	}
	return v.emit(width, banded, band)
}

// sectionLine is one of the rail's two headings: a single faint word, hanging
// at column zero.
//
// IT IS NOT A RULE, and that is a decision rather than an omission. 5.13 allows
// rules only at ROOM boundaries and the rail already spends its one on the seam
// between a scope's surface and its members; two more inside the same room would
// make a 28-column column look like a form. The register does the work instead —
// the heading is the same tertiary tier as the connectors and the fold line —
// which is also why it takes no blank line above it: a rail buys vertical space
// with rows of work, and a word in the chrome tier is already a boundary to the
// eye without one.
//
// It hangs at column zero, one further left than every row, because the gutter
// belongs to the selection accent and a heading is never selected. A hanging
// heading is a piece of typography this surface can afford; an indented one
// would read as an item in the list it is naming.
func (v *View) sectionLine(r Row, width int) string {
	l := &v.line
	l.reset(width)
	l.add(blocks.Truncate(v.clean(r.Name), width), tokens.TextTertiary)
	return v.emit(width, false, tokens.Ground)
}

// noteLine is a section with nothing in it, said once: `nothing running`.
//
// IT MUST NOT READ AS AN ERROR (§5). It is drawn where its rows would have been,
// in their own tier's dimmest neighbour, with no glyph — there is no state to
// carry a state glyph, and ○ on an absence would be claiming that something is
// pending. A rail with nothing live is a composed rail, not a broken one.
func (v *View) noteLine(r Row, width int, at rowIndent) string {
	l := &v.line
	l.reset(width)
	l.padTo(at.base)
	l.add(blocks.Truncate(v.clean(r.Name), l.room()), tokens.TextTertiary)
	return v.emit(width, false, tokens.Ground)
}

// threadLine is line 1 of a conversation: the ornament column, the thread's
// name, and when it last moved, flush right.
//
// THE ORNAMENT COLUMN IS ALWAYS RESERVED AND ALMOST ALWAYS EMPTY. The dot is
// the one thing on this rail that is allowed to pull an eye, so it takes the
// strongest position a row has — first cell, leading the name — and every other
// thread indents past a blank there. Alignment is what makes it work: a dot that
// pushed its own row's name one column right would be an ornament the reader
// finds by noticing that a line is crooked.
//
// It is CYAN, and palette's switcher paints the same dot the same colour for the
// same reason: 5.16 spends amber on one thing only, a human actually being
// needed, and a delivery that landed is the opposite of a demand. It is news.
// Spending the product's one alarm colour on good outcomes is how a reader
// learns to stop trusting it on the row where it matters.
func (v *View) threadLine(r Row, width int, at rowIndent, sel, banded bool, band, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, sel, banded, ident)
	l.padTo(at.glyphCol())
	if r.Unseen {
		l.add(roomDot, tokens.Cyan)
	} else {
		l.padTo(at.glyphCol() + 1)
	}
	l.add(" ", tokens.TextTertiary)

	when := v.clean(r.When)
	rightW := blocks.Width(when)
	if rightW > 0 {
		rightW++
	}
	room := l.max - l.w - rightW
	l.add(blocks.Truncate(v.clean(r.Name), room), v.nameToken(r))
	if when != "" && l.max-l.w >= rightW {
		l.padTo(l.max - rightW + 1)
		l.add(when, tokens.TextTertiary)
	}
	return v.emit(width, banded, band)
}

// statusLine is line 2: what the row is doing, in its own words. A cut turn
// ends the line visibly cut (12.5.2) — the mark is sticky, so it survives on a
// narrow rail after the words have gone.
func (v *View) statusLine(r Row, width int, at rowIndent, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	v.indentTo(l, at, true)
	cutW := 0
	if r.Cut != tokens.CutNone {
		cutW = 2 // a space and the mark
	}
	room := l.max - l.w - cutW
	l.add(blocks.Truncate(v.clean(r.Status), room), tokens.TextSecondary)
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
// 5.15 asks a task scope to make visible, in names rather than in edges. `waits:`
// rather than `waits on` is §3's own spelling, and it is v1's: two fewer cells
// on the one line that is competing with a list of names for them.
func (v *View) waitsLine(r Row, width int, at rowIndent, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	v.indentTo(l, at, true)
	l.add("waits: ", tokens.TextTertiary)
	for i, name := range r.WaitsOn {
		if l.room() <= 0 {
			break
		}
		if i > 0 {
			l.add(sep, tokens.TextTertiary)
		}
		l.add(blocks.Truncate(v.clean(name), l.room()), tokens.TextTertiary)
	}
	return v.emit(width, banded, band)
}

// artifactLine is the artifact law made visible (12.5.1): the deliverable is on
// disk and the card points at it. The path is middle-ellipsised, because the
// filename is the information (5.21).
func (v *View) artifactLine(ref Ref, width int, at rowIndent, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	v.indentTo(l, at, true)
	l.add(tokens.GlyphTreeLast, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	text := ref.Path
	if text == "" {
		text = ref.Label
	}
	l.add(blocks.TruncatePath(v.clean(text), l.room()), tokens.TextSecondary)
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
func (v *View) metaLine(t Telemetry, width int, at rowIndent, banded bool, band tokens.Token) string {
	l := &v.line
	l.reset(width)
	v.gutter(l, false, banded, tokens.Token(255))
	v.indentTo(l, at, true)
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
		model := v.clean(t.Model)
		if t.Boosted {
			model = model + tokens.GlyphBoosted
		}
		if t.Effort != "" {
			add(model+sep+v.clean(t.Effort), model, tokens.TextTertiary, prioModel)
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
	add(countsCell(t.Counts), "", tokens.TextTertiary, prioCounts)
	return n
}

// countsCell is the census as glyph-and-count pairs: `2◐ 1✓`, in the state
// vocabulary of 5.17 and the chrome tier of 5.16.
//
// A state nobody is in is not mentioned — a job with nothing broken says
// nothing about breakage — and an empty census returns the empty string, which
// [buildMeta]'s add drops. That is how a single-part job ends up saying nothing
// at all about its shape (§14), without a special case for it anywhere.
//
// Failed and cancelled share the ✕ pair for the reason [AttnCancelled] gives:
// to a reader scanning a rail they are one fact, which is that the work is not
// going to happen.
func countsCell(c StateCounts) string {
	if c.Empty() {
		return ""
	}
	var buf [48]byte
	out := buf[:0]
	for _, pair := range [...]struct {
		n     int
		glyph string
	}{
		{c.Running, tokens.GlyphWorking},
		{c.Queued, tokens.GlyphQueued},
		{c.Done, tokens.GlyphSettled},
		{c.Failed + c.Cancelled, tokens.GlyphFailed},
	} {
		if pair.n <= 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, ' ')
		}
		out = strconv.AppendInt(out, int64(pair.n), 10)
		out = append(out, pair.glyph...)
	}
	return string(out)
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

// leadWidth is what the merged scope header costs the row it rides on: the ‹
// and the space after it. Zero when there is no lead, which is every row but
// one and every scope that gave its surface a word of its own.
func (v *View) leadWidth() int {
	if v.lead == "" {
		return 0
	}
	return blocks.Width(v.lead) + 1
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
