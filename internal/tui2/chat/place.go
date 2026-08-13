package chat

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE PLACE LINE: one persistent breadcrumb, directly above the composer, and
// the spatial truth of this surface.
//
//	aforge ‹ pricing ideation ‹ 30-page story ‹ chapter two          ◐ 1
//
// WHY IT SITS THERE AND NOWHERE ELSE. In a terminal the eye's resting place is
// the row you type into, so the row immediately above it is the only piece of
// chrome a reader is looking at without being asked to. That makes it two
// sentences at once — WHERE YOU ARE, and THE ADDRESS THE WORDS YOU ARE ABOUT
// TO TYPE ARE GOING TO — and it is the second of those that decides the
// position. A breadcrumb parked in the bottom bar answers the first question
// beside four unrelated numbers; this one answers both, over the caret.
//
// WHAT IT ABSORBED, and what therefore left the screen (§15's one fact, shown
// once, in one place):
//
//   - the composer's ghost line naming the target room when the pane and the
//     mouth had come apart (`typing goes to the wisp parity push`). Its duty is
//     the mouth mark below — one cell on the segment the words land in, rather
//     than a sentence under them.
//   - the bar's thread TITLE CHIP. The thread is a segment here, always, named
//     or not ([untitledRoom]).
//   - the bar's `‹` breadcrumb trail, and with it the one back affordance on
//     that row. Every separator here is the same step, and every segment is the
//     button the trail only ever was as a whole.
//
// The bar row is pages and meters now, which is what §7 wanted of it.
//
// THE PATH IS DERIVED, NEVER KEPT. [App.placePath] reads the same four facts
// the rest of the surface already navigates by — the page enum, the session's
// name, the rail's scope stack, the main pane's view chain — and builds the
// slice fresh. A stored path is a path that drifts: the surface has five doors
// into a task room and one of them would eventually forget to write it.
//
// DEPTH IS UNBOUNDED. A record page drills into a part, which drills into a
// part; the model is a slice for that reason and not a set of fields.

// placeKind says what kind of place one segment names. It decides the segment's
// tier, what clicking it does, and nothing else.
type placeKind uint8

const (
	// placeRoot is `aforge` — the surface itself, and the only segment esc
	// never reaches. See [App.jumpPlace] for where its click goes.
	placeRoot placeKind = iota
	// placeThread is the working conversation this window is in, by the
	// scribe's name for it or by [untitledRoom].
	placeThread
	// placePage is a lens that is a SIBLING of the thread rather than a place
	// inside it — the work board, the notebook. The words are the tabs' own
	// ([page.String]), because a second spelling of a place the strip already
	// names would be the same fact under two names.
	placePage
	// placeScope is a scope the rail descended into: a task's own room.
	placeScope
	// placePart is a record or part page inside a task room — the drill chain,
	// and the reason depth is unbounded.
	placePart
)

// placeSeg is one segment of the path.
type placeSeg struct {
	// Word is the segment as it is drawn. Never an id (13.3.4).
	Word string
	Kind placeKind
}

// placeRootWord is the first segment, and it is the rail home scope's own title
// (scope.go's buildHome) spelled once so the map and this line cannot disagree
// about what the product calls itself.
const placeRootWord = "aforge"

// placePath is THE derivation: the path from the root to wherever the reader is
// standing, read off state the surface already holds.
//
// The order of the reads is the order of the nesting, and each rung is the
// question the rung above it cannot answer:
//
//	root      — always
//	page      — a lens that is not the thread is a SIBLING of it, and stops here
//	thread    — which conversation
//	scopes    — which task inside it (the rail's own stack, minus its root)
//	parts     — which record inside that (the main pane's view chain)
//
// A view title that repeats the crumb above it is dropped, which is 12.13.2's
// merge law: an entered room says its name ONCE. Without it a task room drew
// `‹ wisp-parity ‹ wisp-parity` and read as a level of nesting that does not
// exist. The rule is the one [App.breadcrumb] kept before this line replaced it.
func (a *App) placePath() []placeSeg {
	segs := make([]placeSeg, 0, 6)
	segs = append(segs, placeSeg{Word: placeRootWord, Kind: placeRoot})

	// A PAGE IS A SIBLING OF THE THREAD, NOT A PLACE INSIDE IT. Arriving at one
	// leaves whatever room the reader had descended into ([App.showPage] homes
	// the rail and drops the lens), so there is nothing below it to say.
	if a.page != pageThread {
		return append(segs, placeSeg{Word: a.page.String(), Kind: placePage})
	}

	name := a.threadName(a.session)
	if name == "" {
		// The chip could decline to name an unnamed thread because it was one
		// column among many and absence read as "no chip". A segment cannot:
		// the path would lose its second rung and the room the reader is in
		// would have no name at all. [untitledRoom] is the true answer and a
		// truncated uuid is not.
		name = untitledRoom
	}
	segs = append(segs, placeSeg{Word: name, Kind: placeThread})

	if a.railModel != nil {
		crumbs := a.railModel.Breadcrumb()
		for i := 1; i < len(crumbs); i++ {
			segs = appendPlace(segs, placeSeg{Word: crumbs[i], Kind: placeScope})
		}
	}
	for _, view := range viewChain(a.view) {
		segs = appendPlace(segs, placeSeg{Word: view.title, Kind: placePart})
	}
	return segs
}

// appendPlace adds one segment unless it is blank or repeats the one before it.
// See [App.placePath] for why the repeat is dropped rather than drawn.
func appendPlace(segs []placeSeg, seg placeSeg) []placeSeg {
	word := strings.TrimSpace(seg.Word)
	if word == "" {
		return segs
	}
	if n := len(segs); n > 0 && strings.EqualFold(strings.TrimSpace(segs[n-1].Word), word) {
		return segs
	}
	seg.Word = word
	return append(segs, seg)
}

// viewChain is the main pane's lens chain, outermost first. A record page
// drilled into from a record page keeps its parent whole (rooms.go's mainView),
// so the chain is already on the heap and this only turns it the right way up.
func viewChain(view *mainView) []*mainView {
	if view == nil {
		return nil
	}
	depth := 0
	for v := view; v != nil; v = v.parent {
		depth++
	}
	out := make([]*mainView, depth)
	for v := view; v != nil; v = v.parent {
		depth--
		out[depth] = v
	}
	return out
}

// placeMouth is which segment the composer's words would land in, when that is
// NOT the segment the reader is standing on. It returns -1 for the ordinary
// case, which is the two agreeing.
//
// THIS IS THE GHOST LINE'S DUTY, MOVED. 13.19's rule is that the composer must
// always name where words go when the eyes and the mouth are in different
// places; the old answer was a sentence under the draft (`typing goes to …`)
// and the honest one is a mark on the segment itself, because the path is
// already naming every room on screen and a sentence repeating one of them is
// §15's same fact twice. The mark is [tokens.GlyphHugEdge] — the composer's own
// left edge, the cell one row down that already means "this is the live surface"
// — so a reader who sees it here has already learned what it says.
//
// IT IS NOT THE PROMPT GLYPH, and the first cut of this was. `›` beside the
// path's own `‹` put two opposite chevrons in adjacent cells (`aforge ‹ ›
// pricing ideation`), which reads as a typo before it reads as a mark. The hug
// edge is one cell, needs no space after it, and belongs to the surface it is
// pointing at.
//
// The frames it fires on are exactly the frames the sentence fired on: a CARD
// or a HOME in the main pane, which is a look at something the composer is not
// bound to. A steer line answers -1, because a steered draft names its node on
// the card above it and a second mark would be a second answer.
func (a *App) placeMouth(segs []placeSeg) int {
	if a.composerMode().mode != rail.ComposerChat || a.view == nil {
		return -1
	}
	// A ROOM BEING MINTED IS ALREADY THE LEAF. The `+ new` door's card is what
	// the pane is showing and the mint is already committed to it, so the eyes
	// and the mouth are in the same place and there is nothing to mark. The old
	// sentence had to say [newRoomCardTitle] out loud precisely because it could
	// not point at the card the reader was looking at.
	if a.mint.active {
		return -1
	}
	if a.source == nil {
		return -1
	}
	name := strings.TrimSpace(a.source.RoomTitle(a.session))
	if name == "" {
		return -1
	}
	// The mouth is the THREAD segment: a card is a look at a sibling row, and
	// the words still go to the conversation this window is in. Searching by
	// name rather than by index keeps the two derivations from drifting — the
	// segment is the one that says the room's name, whatever rung it landed on.
	for i := len(segs) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(segs[i].Word), name) {
			if i == len(segs)-1 {
				return -1
			}
			return i
		}
	}
	return -1
}

// -- the component -----------------------------------------------------------

// placeHidden is the id of the `…` that stands for the segments width pressure
// collapsed away. It is not a segment index, so it cannot be mistaken for one.
const placeHidden = -1

// placeHit is one clickable run of the last render, in the pane's own columns.
// It is derived from the fitted text rather than recorded while painting, which
// is footer/hit.go's rule and for its reason: a hit table built from a second
// measurement is a click landing on a word that had moved.
type placeHit struct {
	// seg is the segment index this run jumps to, or [placeHidden] for the mark.
	seg      int
	from, to int
}

// contains reports whether column x lands on this run.
func (h placeHit) contains(x int) bool { return x >= h.from && x < h.to }

// placeLine is the row. It owns no navigation state of its own: the path, the
// mouth mark and the ornament are all pushed in by [App.refresh] from state the
// app already holds, and everything below is fitting and paint.
type placeLine struct {
	style *tokens.Styler

	segs  []placeSeg
	mouth int
	// focus is the segment the keyboard is walking, or -1 when the line does not
	// hold the keyboard. See [App.placeKey].
	focus int

	// unseen and running are the one honest ornament: something landed where you
	// were not looking, and how much work is moving. Both are read from the
	// sources the rail's handle and the bar's dock already read, so a third
	// count cannot disagree with the two that exist.
	unseen  bool
	running int

	hits      []placeHit
	lastWidth int
}

// newPlaceLine builds the row. A nil styler renders plain text, which is what
// every sibling component in this tree does for the same situation.
func newPlaceLine(style *tokens.Styler) *placeLine {
	return &placeLine{style: style, mouth: -1, focus: -1}
}

// setPath replaces everything the row draws. The caller's slice is copied.
func (p *placeLine) setPath(segs []placeSeg, mouth int, unseen bool, running int) {
	p.segs = append(p.segs[:0], segs...)
	p.mouth, p.unseen, p.running = mouth, unseen, running
	if p.focus >= len(p.segs) {
		// The path shortened under the keyboard — a turn settled, a room closed.
		// The walk lands on the deepest segment that still exists rather than
		// being dropped, because the reader did not ask to leave.
		p.focus = len(p.segs) - 1
	}
}

// focused reports whether the keyboard is walking this row.
func (p *placeLine) focused() bool { return p.focus >= 0 }

// setFocus enters or leaves the walk. Entering lands on the CURRENT segment —
// where the reader already is — because a walk that started at the root would
// move the cursor before the reader had asked for anything.
func (p *placeLine) setFocus(on bool) {
	if !on || len(p.segs) == 0 {
		p.focus = -1
		return
	}
	p.focus = len(p.segs) - 1
}

// walk slides the focused segment by delta, clamping at both ends. It clamps
// rather than wrapping for [rail.Model.Move]'s reason: a cursor that wraps
// teleports the eye, and the gesture the reader meant was "further out".
func (p *placeLine) walk(delta int) {
	if p.focus < 0 || len(p.segs) == 0 {
		return
	}
	p.focus = min(max(p.focus+delta, 0), len(p.segs)-1)
}

// placeItem is one thing the fitted row draws: a segment, or the mark standing
// for the ones it dropped.
//
// word is the FINAL text, mouth mark and all. Folding the mark in here rather
// than adding it at paint time is what keeps the three measurements of this row
// — the fit, the hit table and the ink — from being three different numbers: a
// mark added after the fitting is a cell the fitting never paid for, and the
// row overflows by exactly its width one breakpoint early.
type placeItem struct {
	seg  int
	word string
}

// placeSep joins segments. It is [tokens.GlyphScopeUp] because every separator
// on this row is a step a reader can take back — the same reading the rail's
// scope header and the trail this line replaced both spend it on.
const placeSep = " " + tokens.GlyphScopeUp + " "

// placeNameFloor is the fewest cells of the current segment worth keeping,
// borrowed from footer/scope.go's own floor for the same judgement: below it
// the row stops being an answer and becomes a shape.
//
// It is what the ORNAMENT is measured against rather than a floor the path
// itself refuses to go below. The path has no choice — it is the only statement
// of where the reader is and it says it however few cells it is given — but the
// dot and the count do have one, so they are asked to justify their cells
// against this number and give them back when they cannot.
const placeNameFloor = 8

// placeFit collapses the MIDDLE and never the ends.
//
// Root and current are the two segments that always survive: the root is what
// the surface is, and the current is what the reader is looking at, and a row
// that dropped either would have stopped answering the question it exists for.
// Everything between them goes, oldest first, behind one [tokens.GlyphEllipsis]
// — the STATIC overflow mark, deliberately not [tokens.GlyphTruncated], which
// means "click for more" everywhere else on this surface. Here the mark IS
// clickable, and the distinction still holds: `⋯` marks text that continues off
// an edge, `…` marks a count of things that are not on the row. The mark's own
// door is the picker ([App.openPlacePicker]).
//
// When even `aforge ‹ … ‹ current` will not fit, the current segment's own name
// gives, in the middle — a title is recognised by its head and disambiguated by
// its tail (footer/scope.go's middleCut, blocks.TruncatePath's reasoning for a
// path applied to the other kind of name this surface draws).
func placeFit(segs []placeSeg, mouth, width int) []placeItem {
	if width <= 0 || len(segs) == 0 {
		return nil
	}
	last := len(segs) - 1
	// The whole path, then one more ancestor collapsed each time round.
	for drop := 0; drop <= last-1; drop++ {
		items := placeForm(segs, mouth, drop)
		if placeWidth(items) <= width {
			return items
		}
	}
	// Root and current alone, with the current name giving from the middle. The
	// name shrinks as far as ONE cell rather than the ancestors' own floor,
	// because the law here is that the two ends survive: `aforge ‹ … ‹ c…` still
	// says what the surface is and that there is more above you, and a row that
	// dropped the root to keep eight cells of a title would have kept the less
	// useful half.
	items := placeForm(segs, mouth, max(last-1, 0))
	if len(items) > 1 {
		frame := placeWidth(items) - blocks.Width(items[len(items)-1].word)
		if room := width - frame; room >= 1 {
			items[len(items)-1].word = placeCut(segs[last].Word, room)
			return items
		}
	}
	// Not even the root, the mark and one cell of the name. The reader keeps the
	// one word that answers "what am I looking at" (footer/scope.go's same last
	// resort, for the same reason).
	return []placeItem{{seg: last, word: placeCut(segs[last].Word, width)}}
}

// placeForm is the path with drop ancestors collapsed behind the mark. drop is
// counted from segment 1, so the root is never among them.
func placeForm(segs []placeSeg, mouth, drop int) []placeItem {
	items := make([]placeItem, 0, len(segs)+1)
	items = append(items, placeItem{seg: 0, word: placeWord(segs, mouth, 0)})
	if drop > 0 {
		items = append(items, placeItem{seg: placeHidden, word: tokens.GlyphEllipsis})
	}
	for i := 1 + drop; i < len(segs); i++ {
		items = append(items, placeItem{seg: i, word: placeWord(segs, mouth, i)})
	}
	return items
}

// placeWord is one segment's final text: the mouth mark, when the words being
// typed land there, and then the name. See [App.placeMouth].
func placeWord(segs []placeSeg, mouth, i int) string {
	if i == mouth {
		return tokens.GlyphHugEdge + segs[i].Word
	}
	return segs[i].Word
}

// placeHiddenSegs is which segments a fitted row collapsed away — exactly the
// rows the picker offers, so the mark and the list it opens cannot disagree.
func placeHiddenSegs(segs []placeSeg, items []placeItem) []int {
	shown := make(map[int]bool, len(items))
	for _, it := range items {
		if it.seg != placeHidden {
			shown[it.seg] = true
		}
	}
	out := make([]int, 0, len(segs))
	for i := range segs {
		if !shown[i] {
			out = append(out, i)
		}
	}
	return out
}

// affordsOrnament reports whether the path can still say what it is for beside
// the ornament: it opens with the root, and the segment the reader is standing
// on has not been cut below [placeNameFloor].
//
// Both halves are law 3 read as a budget question. A row that kept a dot and a
// count while the answer had collapsed to `aforge ‹ … ‹ c…` would have spent
// its last cells on the fact the reader did not ask for.
func (p *placeLine) affordsOrnament(items []placeItem) bool {
	if len(items) == 0 || items[0].seg != 0 {
		return false
	}
	last := items[len(items)-1]
	if last.seg != len(p.segs)-1 {
		return false
	}
	floor := min(placeNameFloor, blocks.Width(p.segs[last.seg].Word))
	return blocks.Width(last.word) >= floor
}

// placeWidth is what a fitted row costs, separators included.
func placeWidth(items []placeItem) int {
	if len(items) == 0 {
		return 0
	}
	total := blocks.Width(placeSep) * (len(items) - 1)
	for i := range items {
		total += blocks.Width(items[i].word)
	}
	return total
}

// placeCut keeps both ends of a name and marks the missing middle.
func placeCut(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := blocks.Width(s)
	if w <= width {
		return s
	}
	if width == 1 {
		return tokens.GlyphEllipsis
	}
	keep := width - 1
	head := (keep + 1) / 2
	return ansi.Cut(s, 0, head) + tokens.GlyphEllipsis + ansi.Cut(s, w-(keep-head), w)
}

// ornament is the row's right edge: the unseen dot, then the running count.
//
// ONE ORNAMENT, AND IT IS THE ONE THE REST OF THE SURFACE ALREADY DRAWS. The
// dot is [tokens.GlyphStepDone] in [tokens.Cyan], which is the exact pair the
// rail's thread line, the collapsed rail's handle and the switcher's rows all
// use — cyan because §12 spends amber on a human actually being needed and a
// delivery that landed is news, not a demand. The count is
// [tokens.GlyphWorking] and a number, the bar dock's own glyph for "this is
// moving", without the dock's `working` word: the dock is a sentence in a row of
// columns and this is a mark at the end of a line.
//
// NO MONEY AND NO CONTEXT GAUGE. Those are standing facts about the window and
// they have a home in the bar; a second copy over the caret would be the strip
// this line replaced growing back one number at a time.
func (p *placeLine) ornament() (text string, tok tokens.Token) {
	var b strings.Builder
	if p.unseen {
		b.WriteString(tokens.GlyphStepDone)
	}
	if p.running > 0 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(tokens.GlyphWorking)
		b.WriteString(" ")
		b.WriteString(strconv.Itoa(p.running))
	}
	if p.unseen && p.running <= 0 {
		return b.String(), tokens.Cyan
	}
	return b.String(), tokens.TextTertiary
}

// placeGap is the least whitespace between the path and the ornament. Two cells
// is the bar row's own zone gap, and the ornament is dropped whole rather than
// crowding the path below it (tokens.FitFooter's law: a column is dropped whole
// and never truncated).
const placeGap = 2

// Render draws the row at width. It is a pure function of the pushed state: at
// most one row, never wider than width, never a panic — down to width 1 and an
// empty path alike.
//
// It opens at the lens's left edge ([tokens.LensIndent]) rather than at column
// zero, because the composer's own draft opens there too and the two rows are
// one surface. The indent comes out of the fitting width, never out of the
// terminal's.
func (p *placeLine) Render(width int) string {
	items, orn, ornTok, pad := p.layout(width)
	if len(items) == 0 && orn == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(pad)
	for i, item := range items {
		if i > 0 {
			b.WriteString(p.paint(placeSep, tokens.TextTertiary))
		}
		b.WriteString(p.paintItem(item))
	}
	if orn == "" {
		return b.String()
	}
	drawn := blocks.Width(pad) + placeWidth(items)
	b.WriteString(strings.Repeat(" ", max(width-drawn-blocks.Width(orn), 0)))
	b.WriteString(p.paint(orn, ornTok))
	return b.String()
}

// layout solves the row once. Render paints it and [placeLine.retarget] turns
// it into a hit table, so the eye and the pointer are answering about the same
// columns by construction.
func (p *placeLine) layout(width int) (items []placeItem, orn string, ornTok tokens.Token, pad string) {
	if width <= 0 || len(p.segs) == 0 {
		return nil, "", tokens.TextTertiary, ""
	}
	inner := width
	if width > tokens.LensIndent {
		pad, inner = strings.Repeat(" ", tokens.LensIndent), width-tokens.LensIndent
	}
	orn, ornTok = p.ornament()
	if orn != "" {
		// THE ORNAMENT GIVES ITS CELLS BACK WHOLE the moment the path cannot
		// keep both its ends beside it (tokens.FitFooter's law: a column is
		// dropped whole and never truncated). A row that kept a dot and a count
		// while the answer collapsed to one cut word would have spent the cells
		// on the fact the reader did not ask for.
		room := inner - blocks.Width(orn) - placeGap
		if tight := placeFit(p.segs, p.mouth, room); room > 0 && p.affordsOrnament(tight) {
			return tight, orn, ornTok, pad
		}
		orn = ""
	}
	return placeFit(p.segs, p.mouth, inner), orn, ornTok, pad
}

// paintItem inks one segment: the mouth mark if the words land there, then the
// word at its own tier.
//
// THE TIERS ARE BRIGHTNESS AND NOTHING ELSE (§16's grey ramp). The separators
// sit in the faintest register, ancestors in the ordinary one, and the segment
// the reader is standing on one step up — the same "current is brighter" the
// bar's tabs and the rail's selection already say. Colour is spent on the
// ornament and on nothing else here, because every hue in this product means a
// state and a place is not one.
func (p *placeLine) paintItem(item placeItem) string {
	if item.seg == placeHidden {
		return p.paint(item.word, tokens.TextTertiary)
	}
	text := item.word
	tok := tokens.TextSecondary
	if item.seg == len(p.segs)-1 {
		tok = tokens.TextPrimary
	}
	if item.seg == p.focus {
		// The walk's own band, and it is the SELECTION band the rail and the
		// bar's current tab already stand on rather than a colour of its own.
		return p.paintOn(text, tokens.TextPrimary, tokens.Band)
	}
	return p.paint(text, tok)
}

func (p *placeLine) paint(text string, tok tokens.Token) string {
	if p.style == nil || text == "" {
		return text
	}
	return p.style.PaintToken(text, tok)
}

func (p *placeLine) paintOn(text string, fg, bg tokens.Token) string {
	if p.style == nil || text == "" {
		return text
	}
	return p.style.PaintOn(text, fg, bg)
}

// retarget rebuilds the hit table for width. It re-solves the SAME layout the
// paint solves, which is footer/hit.go's discipline rather than a second
// measurement of a painted string.
func (p *placeLine) retarget(width int) {
	p.lastWidth = width
	p.hits = p.hits[:0]
	items, _, _, pad := p.layout(width)
	at := blocks.Width(pad)
	for i, item := range items {
		if i > 0 {
			at += blocks.Width(placeSep)
		}
		// The mark is already part of the word ([placeWord]), so it is already
		// part of the button: a reader who clicks the glyph clicked the room it
		// is marking.
		w := blocks.Width(item.word)
		p.hits = append(p.hits, placeHit{seg: item.seg, from: at, to: at + w})
		at += w
	}
}

// targetAt is which segment column x landed on. It reports [placeHidden] for
// the collapse mark, and false for the ground between and beside the words.
func (p *placeLine) targetAt(x int) (int, bool) {
	for _, hit := range p.hits {
		if hit.contains(x) {
			return hit.seg, true
		}
	}
	return 0, false
}

// hiddenSegs is which segments the row last collapsed away, for the picker the
// mark opens.
func (p *placeLine) hiddenSegs() []int {
	items, _, _, _ := p.layout(p.lastWidth)
	return placeHiddenSegs(p.segs, items)
}

// -- the acts ----------------------------------------------------------------

// syncPlace brings the row up to date with the state that decides it. It is
// called from [App.refresh] and nowhere else: the path is derived, so there is
// exactly one moment it can be wrong in, and this is it.
func (a *App) syncPlace() {
	if a.placeBar == nil {
		return
	}
	segs := a.placePath()
	// The ornament's two facts, from the two places that already answer them:
	// the rail's own scope says whether something landed where the reader was
	// not looking (the same question the collapsed handle's dot asks), and the
	// board's live split says how much is moving (the same question the bar's
	// dock asks). Neither is a count this line keeps.
	unseen := a.railModel != nil && a.railModel.Scope().Unseen()
	working, _ := a.boardJobs()
	a.placeBar.setPath(segs, a.placeMouth(segs), unseen, len(working))
}

// jumpPlace is LAW 2: every segment is a button. Clicking an ancestor walks
// straight there; clicking the one you are standing on does nothing, because a
// button that repeats where you already are is a button that lies about having
// done something.
//
// It walks by taking [App.popPlace] as many times as the path is deep rather
// than by teleporting, so a jump and a held-down esc leave the surface in
// exactly the same state — the rail's stack, the composer's binding and the
// main pane's lens all unwound by the one function that knows how.
func (a *App) jumpPlace(seg int) tea.Cmd {
	if a.placeBar == nil {
		return nil
	}
	segs := a.placeBar.segs
	if seg < 0 || seg >= len(segs) {
		return nil
	}
	if seg == len(segs)-1 {
		return nil
	}
	// THE ROOT SEGMENT GOES TO THE BOARD, and that is the one target on this row
	// that is not a pop. `aforge` is the surface itself, and the surface's own
	// home is the overview the work page already is — so the root is the one
	// segment esc never reaches (see [App.navigate]'s floor) and the one a
	// pointer reaches in a single step. It is deliberately one call: the day the
	// overview page lands, this line points at it and nothing else moves.
	if segs[seg].Kind == placeRoot {
		return a.showPage(pageBoard)
	}
	var cmds []tea.Cmd
	for i := len(segs) - 1; i > seg; i-- {
		cmd, popped := a.popPlace()
		if !popped {
			break
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	a.refresh()
	return tea.Batch(cmds...)
}

// -- the walk ----------------------------------------------------------------

// placeChord focuses the place line so the keyboard can walk it.
//
// ctrl+g, and the spelling is the point. A bare letter is draft text in a
// composer-first room, and an alt chord is a key a macOS terminal composes into
// a glyph before the program ever sees it (threads.go's `†` incident) — so the
// door has to be a ctrl chord, and it has to be one nothing else squats on:
// this surface's own chords are ctrl+space/@/k, ctrl+o, ctrl+t, ctrl+r, ctrl+y
// and ctrl+u; the draft's readline set is ctrl+a/e/k/n/p/u/w; and the terminal
// itself keeps ctrl+s, ctrl+q, ctrl+z and ctrl+d. ctrl+g is free in all three,
// it is BEL rather than a printable character, and `go` is what it does.
const placeChord = "ctrl+g"

// placeEntryID is the registry row the chord is declared on, so the `?` sheet
// and the palette teach the key that actually fires (5.22: nothing typed-only).
const placeEntryID = "key.place-line"

// focusPlace hands the keyboard to the row, or takes it back. It is
// [App.focusScope] for the breadcrumb and keeps the same promise: one cursor
// (5.14), and the composer drawn unfocused is the picture telling the truth
// about where a keystroke will go.
func (a *App) focusPlace(on bool) tea.Cmd {
	if a.placeBar == nil {
		return nil
	}
	if on {
		if len(a.placeBar.segs) <= 1 {
			// A path with nothing but the root has nowhere to walk to. Refusing
			// is honest; focusing a row with one word on it would be a mode the
			// reader could not tell they were in.
			return nil
		}
		a.focusScope(false)
		a.setPageFocus(false)
	}
	a.placeBar.setFocus(on)
	if a.composer != nil {
		a.composer.Focus(!on && !a.railFocus && !a.pageFocus)
	}
	a.refresh()
	return nil
}

// placeKey is the walk's own keyboard, and it is asked BEFORE anything else on
// this surface except quit and a raised overlay.
//
// It claims four keys and refuses the rest. ←/→ walk, enter jumps, esc leaves —
// and a key it does not claim drops the focus and carries on down the ladder,
// which is the mint card's own rule read for a different mode (rooms.go's
// mintOnType): once a reader starts typing, they have stopped navigating, and a
// keystroke that did nothing would be the mode holding a keyboard it was not
// using.
func (a *App) placeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.placeBar == nil || !a.placeBar.focused() {
		return nil, false
	}
	switch msg.String() {
	case "left", "shift+tab":
		a.placeBar.walk(-1)
		a.shell.Invalidate()
		return nil, true
	case "right", "tab":
		a.placeBar.walk(1)
		a.shell.Invalidate()
		return nil, true
	case "enter":
		seg := a.placeBar.focus
		cmd := a.focusPlace(false)
		return tea.Batch(cmd, a.jumpPlace(seg)), true
	case "esc", placeChord:
		// The chord that entered the walk leaves it, which is what every other
		// mode door on this surface does (ctrl+o is the same round trip for the
		// map). Claiming it here rather than letting it fall through to the
		// registry is the difference between one round trip and a flicker: the
		// row would otherwise drop focus on the way down and take it back one
		// rung later, having done nothing.
		return a.focusPlace(false), true
	}
	return a.focusPlace(false), false
}

// -- the picker behind the collapse mark -------------------------------------

// placePickPrefix is how a hidden segment's index is carried through the
// palette, which holds ids and never learns what they mean (palette/result.go).
// The prefix is this package's own and reaches no other executor, because
// [App.choosePlace] answers these rows rather than [App.runEntry].
const placePickPrefix = "place.seg."

// openPlacePicker is the `…`'s own door: the segments width pressure collapsed
// away, as a list.
//
// It is a [palette.Palette] and not a widget of its own, because this product
// has ONE overlay grammar — a raised plane, a filtered list, esc to close — and
// a second one for three rows would be a second thing to learn. The rows are
// handed over as Actions, which the catalog takes verbatim, so nothing about
// the registry is consulted for a list that is not made of registry rows.
func (a *App) openPlacePicker() tea.Cmd {
	if a.placeBar == nil {
		return nil
	}
	hidden := a.placeBar.hiddenSegs()
	if len(hidden) == 0 {
		return nil
	}
	if a.placePicker == nil {
		a.placePicker = palette.New(palette.Options{
			Styler:     a.style,
			Linear:     a.linear,
			Invalidate: a.shell.Invalidate,
			OnChoose:   a.choosePlace,
			OnClose:    a.closeOverlay,
		})
	}
	rows := make([]palette.Action, 0, len(hidden))
	for _, seg := range hidden {
		rows = append(rows, palette.Action{Entry: registry.Entry{
			ID:          placePickPrefix + strconv.Itoa(seg),
			Verb:        a.placeBar.segs[seg].Word,
			Description: placeRowNote(a.placeBar.segs[seg].Kind),
		}})
	}
	a.placePicker.Reset()
	a.placePicker.SetCatalog(palette.Catalog{Actions: rows, Title: placeRootWord})
	return a.raise(overlayPlace, a.placePicker)
}

// placeRowNote says what kind of place one picker row is, in the surface's own
// words. It is the description column and never an id (13.3.4).
func placeRowNote(kind placeKind) string {
	switch kind {
	case placeRoot:
		return "the board"
	case placeThread:
		return "this conversation"
	case placePage:
		return "a page"
	case placeScope:
		return "a task's room"
	}
	return "a record inside it"
}

// choosePlace performs one picker row. It is the picker's OWN executor rather
// than [App.runEntry], because these ids name segments of a path and nothing in
// the registry answers to them.
func (a *App) choosePlace(result palette.Result) tea.Cmd {
	run, ok := result.(palette.RunEntry)
	if !ok {
		return nil
	}
	seg, err := strconv.Atoi(strings.TrimPrefix(run.ID, placePickPrefix))
	if err != nil {
		return nil
	}
	closed := a.closeOverlay()
	return tea.Batch(closed, a.jumpPlace(seg))
}
