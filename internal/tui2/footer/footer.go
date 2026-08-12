package footer

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// InputState is the composer's input-state axis. It survives the three-zone
// rewrite for ONE reading only: a send that failed is coral (§12 — coral means
// broken), and a failure is a thing that is true right now, which is the only
// kind of thing the middle zone draws. Every other input state says its piece in
// the composer's own ghost text (§8: "ghost text is STATE-DRIVEN — the
// composer's empty line always says what is true and doable right now"), and a
// second copy of it one row down was the legend soup this row was rebuilt to
// end.
type InputState uint8

const (
	// InputEmpty is an empty draft. Nothing on this row.
	InputEmpty InputState = iota
	// InputTyped is a non-empty, unsent draft. Nothing on this row: the
	// composer's own line already says what enter does.
	InputTyped
	// InputFailed is the last send attempt failing — the one state that reaches
	// this row, as a coral sentence in the middle zone.
	InputFailed
	// InputQueued is one or more type-ahead chips waiting under the composer.
	// Nothing on this row; the chips are the statement.
	InputQueued
)

// KeyMode says what a bare digit keypress means right now.
type KeyMode uint8

const (
	// KeyModeNone means digits currently do nothing.
	KeyModeNone KeyMode = iota
	// KeyModeAnswer means digits answer the focused open question. It is the
	// one mode this row draws (§7: "`1-3 answer` when a question is open"),
	// because a question is a thing that is live right now.
	KeyModeAnswer
	// KeyModeRooms means digits jump to a rail row in the current scope. It
	// draws NOTHING here: the rail numbers its own rows on screen, so a chip
	// repeating the range would be a standing legend, and standing legends
	// moved to the `?` sheet with the rest of them.
	KeyModeRooms
)

// Place is one of the product's homes as a word in the left zone — `chat`,
// `work`, `notebook` (§7). The words, their ids and which one is current all
// come from the host: this package lays places out and says which word a
// pointer landed on, and knows nothing about what any of them contain.
type Place struct {
	// ID is the target id the host gets back when the word is clicked. The
	// footer never invents one and never parses it.
	ID string
	// Word is the place's name as it is drawn — lowercase, one word (§16's case
	// rule: chrome words are lowercase).
	Word string
	// Current says the reader is in this place. At most one should be; a slice
	// with none collapses to its first word under width pressure rather than
	// guessing.
	Current bool
}

// FocusContext is everything one frame of the footer needs, assembled by the
// host from whatever pane currently holds focus. A zero-valued field always
// means "nothing to say here", never "say nothing loudly".
type FocusContext struct {
	// Places is the left zone: the homes door, moved into the hug (§7). Empty
	// draws no tabs, which is the honest shape for a host that has not wired
	// them yet.
	Places []Place

	// ScopeTail is the breadcrumb — the trail of where the reader has descended
	// to. It rides the MIDDLE zone, beside the tabs rather than instead of them.
	//
	// It used to replace them, on the reasoning that the trail already opens
	// with the place the reader entered from and drawing both would be the same
	// fact twice. That reasoning was wrong in the one way that matters, and a
	// reader found it on the live build: descending into a task took the three
	// page words off the screen, and with them every door back to `work`. A
	// trail is a way UP, not a way ACROSS — pressing esc enough times is not a
	// navigation affordance — so the tabs are permanent now and the trail says
	// how deep inside one of them you are. Empty means "not inside anything".
	ScopeTail string

	// Verbs are live verbs for the current focus, already ordered
	// most-relevant-first and already projected onto the surface the host
	// actually binds them on. They render as verb·key chips in the middle zone.
	//
	// STANDING legends do not belong here any more (§7: the middle holds only
	// what is live RIGHT NOW). A host that fills this with permanent bindings
	// gets the old key-legend soup back, one tier prettier.
	Verbs []registry.Entry

	// Input and Hint are the composer's failed-send state. See [InputState]:
	// only [InputFailed] draws, and it draws Hint, coral.
	Input InputState
	Hint  string

	// EscInterrupts is true only when esc, right now, would interrupt the turn
	// the user is watching — the middle zone's first chip, `interrupt esc`.
	EscInterrupts bool

	// KeyMode and KeyModeCount together render the answer chip. KeyModeCount is
	// the upper end of the `1—3` range; a non-positive count draws nothing,
	// because a range of nothing is not information.
	KeyMode      KeyMode
	KeyModeCount int

	// Attention is the count of open questions blocking on a human in the room
	// the footer is describing. It does not draw a badge of its own any more —
	// the answer chip IS the statement that a human is needed — it TINTS that
	// chip amber (§12: amber only ever means a human is actually needed).
	Attention int

	// Health is the pending-only system state (a visitor's notice, a service
	// that is not up). It is a standing fact about this window, so it rides the
	// right zone ahead of the model word, and it is the first fact there to go.
	// Nil or empty says nothing.
	Health []string

	// Live says a turn is being answered in the room this row describes. It is
	// what makes the turn's own receipts (Elapsed, TurnCost) legal in the middle
	// zone: they are things that are true RIGHT NOW, and a cost left standing
	// after the turn ended would be the middle zone quietly becoming a legend
	// again.
	Live bool

	// Elapsed and TurnCost are THIS TURN's receipts, drawn in the middle zone
	// beside `interrupt esc` and only while Live. HaveTurnCost false draws no
	// money at all — §16: absent is absent, never `$0.00`, and a turn whose cost
	// has not been journalled yet has not cost nothing.
	//
	// They live here rather than in the right zone because the right zone is
	// standing facts and these two are the opposite of standing: they exist for
	// the length of one answer and take their cells back when it lands. §13 puts
	// the DAY total in the bottom bar; this is not that number, and the two are
	// told apart by where they sit and by the word `today` on the other one.
	Elapsed      time.Duration
	TurnCost     float64
	HaveTurnCost bool

	// Dir is where this work lands on disk, in the abbreviated form
	// internal/tui2/placeline renders (`~/a/aforge-v2`). It is the first fact in
	// the right zone and the dimmest thing on the row.
	//
	// It is a STRING rather than a place-line model because the fitting grammar
	// — fish abbreviation, tilde folding, legs dropped from the left — belongs to
	// that package and a second spelling of it here would be 5.19's rule drifting
	// apart from itself. The host renders it there and hands the words over.
	Dir string

	// Spend/HaveSpend are the day's total (§13: "Day total lives in the bottom
	// bar"), dim, right-aligned to the edge. HaveSpend false renders nothing at
	// all — §16 again.
	//
	// THERE IS NO MODEL FIELD, and its absence is a decision rather than an
	// omission. The word was here for one wave and a reader met it as
	// `deepseek-v4-flash-latest`: model names are SLUGS, the shortening that
	// makes one humane is a best effort against strings a vendor invents, and
	// this row is the last surface in the product that can afford to print an
	// identifier and hope. §14 puts ids in the never-shown tier without an
	// exception for "usually readable". The picker is still one keystroke away
	// (`/model`, the palette), and what stands here is what the row can state
	// without guessing: where the work lands, how much of the window is gone,
	// and what the day has cost.
	Spend     float64
	HaveSpend bool

	// CtxUsed, CtxWindow and HaveCtx are the context gauge: one cell of
	// [tokens.GaugeCells] filling with usage, the percentage beside it, and the
	// window it is a percentage OF — because 5% of 1M and 5% of 128K are
	// different situations and a bare percentage of an unnamed window is not
	// information.
	//
	// HaveCtx false draws NOTHING — not a dash, not an empty gauge. That is §16's
	// EMPTINESS at the one cell where the old meta strip broke it: it drew `— ctx`
	// on every frame the engine had not journalled a window, which is a surface
	// spending three cells to announce its own ignorance on a row where every
	// cell is contested.
	CtxUsed   int64
	CtxWindow int64
	HaveCtx   bool

	// Dock is §6's hidden sidebar, compressed to its counts. It draws only
	// while the rail is off the frame — see [Dock] for the anatomy and for why
	// the money is not one of its fields.
	Dock Dock

	// Hover is the id of the target the pointer is resting on (hit.go), or ""
	// for none. It brightens that run by one tier and does nothing else.
	//
	// It is in the CONTEXT rather than on the Model because the row is a pure
	// function of this struct and the width, and a pointer position kept beside
	// it would be a second input the render silently read.
	Hover string
}

// Options configures a [Model].
type Options struct {
	// Styler paints every cell this package draws. A nil Styler degrades to
	// unstyled plain text, the same posture every sibling package in this tree
	// takes for the same situation.
	Styler *tokens.Styler
}

// Model is the contextual footer. The zero value is not meaningful; construct
// one with [New].
type Model struct {
	styler *tokens.Styler
}

// New builds a footer from opts. It never fails: a missing Styler degrades to
// plain text.
func New(opts Options) *Model {
	return &Model{styler: opts.Styler}
}

// maxVerbs caps the host's live verbs. A middle that grew without bound would
// stop being scannable, which is the whole reason silence is the default here.
const maxVerbs = 3

// sep joins items WITHIN a zone (§16's one separator, `·`). Zones are separated
// by whitespace instead — §15: grouping is shown by spacing, never by a mark
// that has to be read as a boundary.
const sep = " " + tokens.GlyphSeparator + " "

var sepWidth = blocks.Width(sep)

// zoneGap is the whitespace between two zones. Two cells, so the eye reads a
// break rather than a wider `·`.
const zoneGap = "  "

var zoneGapWidth = blocks.Width(zoneGap)

// enDash renders the answer chip's digit range. §16's glyph discipline is that
// every mark comes from the table and means one thing, so the range is drawn
// with tokens.GlyphMissing, the vocabulary's one dash — U+2014, one cell under
// both shipping rulers.
const enDash = tokens.GlyphMissing

// interruptVerb/interruptKey and answerVerb are the two chips this package
// composes itself, named once because the tests read them and because §7 words
// them: `interrupt esc` while streaming, `answer 1—3` when a question is open.
const (
	interruptVerb = "interrupt"
	interruptKey  = "esc"
	answerVerb    = "answer"
)

// spendSuffix is what the day total is a total OF. The figure is the receipt;
// this word is what keeps it from reading as this turn's cost.
const spendSuffix = " today"

// run is one painted span of the row: already-composed plain text (no escape
// sequences — painting happens after the fitting pass, never before), the token
// it paints with, and the target id it belongs to ("" for a run that is not a
// door). A chip is two runs sharing one id, which is how the whole chip becomes
// one click target while only its verb carries the brighter tier.
type run struct {
	id   string
	text string
	tok  tokens.Token
	// bg is the ground this run is painted ON, and onBg says it has one. Exactly
	// one thing on this row uses it: the current place tab's filled pill. A run
	// without a ground keeps whatever floor the strip beneath it painted, which
	// is how the row stays composable with the hug's own two-tone ground without
	// this package learning what that ground is.
	bg   tokens.Token
	onBg bool
}

// placed is a run with the column it starts at. The zones are positioned, not
// concatenated — the right zone right-aligns to the edge (§16: the right edge
// is a column) — so a run's x is a fact the paint and the hit test must share
// rather than each re-derive by adding up separators.
type placed struct {
	run
	from int
}

func runsWidth(runs []run) int {
	w := 0
	for _, r := range runs {
		w += blocks.Width(r.text)
	}
	return w
}

// Zone ids and their priorities, which are the degradation order §7 and §16
// ask for: THE MIDDLE EMPTIES FIRST (silence is already its resting state, so
// losing it costs the reader nothing they did not have a moment ago), then the
// places collapse to the current word alone (the left zone is measured at that
// floor and grows back into whatever the other zones did not need), then the
// right drops. The last thing standing is where you are.
// The left zone is TWO columns to the fitter and one word to the reader: the
// floor it is never dropped below (the current word alone) and the growth on top
// of it (the other tabs, the ancestors of a trail). Splitting it is what lets one
// priority pass express a ladder with a continuous shrink in the middle of it —
// the middle empties while the places are still whole, the places collapse while
// the standing facts are still there — without this file re-implementing the
// drop mechanic that already lives in [tokens.FitFooter].
// THE RIGHT ZONE IS NOT ONE COLUMN ANY MORE. It carries four standing facts of
// very different worth — where this work lands, which model is answering, how
// much of the window is gone, what the day has cost — and dropping them as a
// block meant a terminal one cell too narrow lost the day's spend to save a
// directory. Each is its own column now, and the context gauge is two (the
// reading, and the window word that says what the reading is a percentage of),
// so the row sheds `of 262K` before it sheds `▂ 3%`.
//
// The resulting ladder, lowest first, is the order §7 asks for read straight
// down: the middle empties (silence is already its resting state), then the
// health notice, then the gauge's window word, then the directory, then the
// places collapse to the current word alone, then the gauge, the model word and
// the day's spend go in that order. The last thing standing is where you are.
const (
	zonePlaces = "places"
	zoneGrowth = "places-growth"
	zoneTrail  = "trail"
	zoneMiddle = "middle"
	zoneHealth = "right-health"
	zoneDir    = "right-dir"
	zoneGauge  = "right-gauge"
	zoneWindow = "right-window"
	// zoneDock is §6's hidden sidebar, collapsed onto this row. It sits between
	// the window word and the day's money so the row reads as §6 writes the dock
	// — `◐ 2 working · 1 question · $0.31 today` — with the money it already
	// carried serving as the dock's own tail rather than being said twice (§19).
	zoneDock  = "right-dock"
	zoneSpend = "right-spend"
)

func priorityOf(id string) int {
	switch id {
	case zonePlaces:
		return 100
	case zoneSpend:
		return 90
	// The dock outranks every other standing fact and yields only to the money
	// and to where you are. It is not telemetry: it is the ONLY thing on the
	// frame saying that live work exists at all while the rail is off it, and
	// 10.3.15 forbids a lane going silent. A row that dropped the dock to keep a
	// directory would be hiding running jobs to show a path.
	case zoneDock:
		return 85
	// The trail outranks every telemetry cell and yields to the tabs. Both
	// halves are the same rule: what a lost reader needs is WHERE THEY ARE, and
	// the tabs answer it for the whole product while the trail answers it for
	// this corner of it — so the trail goes before the gauge and after the word
	// `chat`.
	case zoneTrail:
		return 75
	case zoneGauge:
		return 70
	case zoneGrowth:
		return 60
	case zoneDir:
		return 50
	case zoneWindow:
		return 40
	case zoneHealth:
		return 35
	case zoneMiddle:
		return 30
	}
	return 0
}

// Render draws the footer at width cells. It is a pure function of ctx and
// width: never more than one row, never wider than width, never a panic, down
// to width=1 and a zero-valued ctx alike.
//
// The layout lives in [Model.layout] because the pointer needs the same answer:
// which zones are on this row, and which column each word sits at. Two copies of
// that would be a click landing on a word the paint had dropped or moved.
//
// The row opens at the lens's left edge ([tokens.LensIndent]) rather than at
// column 0: this row is the bottom of a room whose other surfaces all begin one
// depth in, and a footer flush against the frame was the room disagreeing with
// itself about where it started. Nothing here paints a background — the seam's
// ground under this row belongs to the lane that owns the strip.
func (m *Model) Render(ctx FocusContext, width int) string {
	runs, ok := m.layout(ctx, width)
	if !ok {
		return ""
	}
	// Assemble the PLAIN line first — painting happens only once the exact
	// surviving text is known, so the defensive truncate below (which should
	// never fire; the layout's own accounting already guarantees the fit) can
	// never cut a painted string and orphan its reset sequence.
	var plain, painted strings.Builder
	x := 0
	for _, p := range runs {
		if pad := p.from - x; pad > 0 {
			gap := strings.Repeat(" ", pad)
			plain.WriteString(gap)
			painted.WriteString(gap)
			x += pad
		}
		tok := p.tok
		if p.id != "" && p.id == ctx.Hover {
			tok = tokens.Promote(tok)
		}
		plain.WriteString(p.text)
		if p.onBg {
			painted.WriteString(m.paintOn(p.text, tok, p.bg))
		} else {
			painted.WriteString(m.paint(p.text, tok))
		}
		x += blocks.Width(p.text)
	}
	if x > width {
		return blocks.Truncate(plain.String(), width)
	}
	return painted.String()
}

// lensPad is the left edge written as cells.
var lensPad = strings.Repeat(" ", tokens.LensIndent)

// indentAt is the edge this row can afford and the width left over for the
// zones. A terminal too narrow to hold the gutter AND a word gives the gutter
// up: knowing where you are outranks the rhythm.
func indentAt(width int) (pad string, inner int) {
	if width <= tokens.LensIndent {
		return "", width
	}
	return lensPad, width - tokens.LensIndent
}

// layout is the whole row: which zones survive this width, what the left zone
// says at the cells it was left, and the column every run starts at.
//
// The drop pass is [tokens.FitFooter] — the priority-drop mechanic already
// lives in tokens, seeded from the same law, and a second copy here would be
// two spellings of one rule drifting apart. What this function adds is the
// shape §7 gives the row: the left zone is measured at its FLOOR (the current
// word alone) rather than at what it wants, so it is never the zone that leaves;
// the slack the fit did not spend is handed back to it, and it grows into as
// much trail or as many tabs as it can afford.
//
// THE HAND-BACK IS DELIBERATE AND IT IS NOT MONOTONIC. A narrower terminal can
// show MORE of the trail than a wider one did, because the wider one was still
// spending those cells on the standing facts and the narrower one has dropped
// them. The alternative — leaving the reclaimed cells blank so the row only ever
// shrinks — would be paying for tidiness with the one question this row exists
// to answer.
func (m *Model) layout(ctx FocusContext, width int) ([]placed, bool) {
	if width <= 0 {
		return nil, false
	}
	pad, inner := indentAt(width)
	if inner <= 0 {
		return nil, false
	}
	x0 := blocks.Width(pad)

	leftFloor, leftFull := m.leftWidths(ctx)
	trailFloor, trailFull := trailWidths(ctx)
	middle := middleRuns(ctx)
	facts := rightFacts(ctx)

	cols := make([]tokens.FooterColumn, 0, 5+len(facts))
	if leftFloor > 0 {
		cols = append(cols, tokens.FooterColumn{ID: zonePlaces, MinWidth: leftFloor, Priority: priorityOf(zonePlaces)})
		if grow := leftFull - leftFloor; grow > 0 {
			cols = append(cols, tokens.FooterColumn{ID: zoneGrowth, MinWidth: grow, Priority: priorityOf(zoneGrowth)})
		}
	}
	// The trail is fitted at its FLOOR — the way out, the mark for everything
	// above it, and enough of the name to recognise it — and grows into the
	// slack afterwards, exactly as the tabs do. Fitting it at what it wants
	// would let a three-deep trail push the gauge and the day's money off a
	// terminal that had room for all three.
	if trailFloor > 0 {
		cols = append(cols, tokens.FooterColumn{ID: zoneTrail,
			MinWidth: trailFloor + zoneGapWidth, Priority: priorityOf(zoneTrail)})
	}
	if w := runsWidth(middle); w > 0 {
		cols = append(cols, tokens.FooterColumn{ID: zoneMiddle, MinWidth: w + zoneGapWidth, Priority: priorityOf(zoneMiddle)})
	}
	// Each standing fact is measured with the separator that would precede it,
	// and the FIRST one carries the gap that holds the whole zone off the middle
	// instead. Which fact is first depends on which ones survive, so the
	// accounting is deliberately pessimistic — every fact pays for a separator,
	// and the one that turns out to be first spends its allowance on the two-cell
	// gap instead, which is one cell cheaper. The zone therefore asks for exactly
	// one cell more than it uses, and that cell goes back to the left zone's
	// growth in the hand-back below; over-asking by one is the price of fitting
	// a zone whose first member is not known until the fit has run.
	for _, f := range facts {
		cols = append(cols, tokens.FooterColumn{ID: f.zone,
			MinWidth: blocks.Width(f.text) + sepWidth, Priority: priorityOf(f.zone)})
	}
	if len(cols) == 0 {
		return nil, false
	}

	kept := tokens.FitFooter(cols, inner)
	keep := make(map[string]bool, len(kept))
	spent := 0
	for _, c := range kept {
		keep[c.ID] = true
		spent += c.MinWidth
	}
	if len(keep) == 0 {
		return nil, false
	}
	if !keep[zoneMiddle] {
		middle = nil
	}
	// The window word is not a fact of its own — it is the gauge's tail — so it
	// cannot survive its own reading. Nothing enforces that in the priority
	// table because a priority table cannot express a dependency; this line is
	// where the dependency is stated.
	if !keep[zoneGauge] {
		delete(keep, zoneWindow)
	}
	right := rightRuns(facts, keep)
	// The slack the fit did not spend goes to the zones that can use it, and the
	// TRAIL is asked first. The tabs are three fixed words — they are either
	// whole or collapsed to one, and no number of spare cells makes them say
	// more — while the trail has a continuous ladder of forms between its floor
	// and its full length, so a cell handed to it turns into a visible ancestor.
	// Handing the same cell to the tabs would turn it into a space.
	slack := inner - spent
	if slack < 0 {
		slack = 0
	}
	var trail []run
	if keep[zoneTrail] {
		budget := trailFloor + slack
		if budget > trailFull {
			budget = trailFull
		}
		trail = trailRuns(ctx, budget)
		if grown := runsWidth(trail) - trailFloor; grown > 0 {
			slack -= grown
		}
		if slack < 0 {
			slack = 0
		}
	}
	var left []run
	if keep[zonePlaces] {
		budget := leftFloor
		if keep[zoneGrowth] {
			budget = leftFull
		}
		left = m.leftRuns(ctx, budget+slack)
	}
	// The trail leads the middle: it says WHERE, and the chips beside it say
	// what is happening there. Reading it the other way round would put a
	// transient `interrupt esc` in front of the reader's own location.
	if len(trail) > 0 {
		if len(middle) > 0 {
			trail = append(trail, run{text: sep, tok: tokens.TextTertiary})
		}
		middle = append(trail, middle...)
	}

	return placeZones(x0, inner, left, middle, right), true
}

// placeZones puts the three zones on the row: the left at the lens edge, the
// middle a gap after it, the right hard against the edge (§16's right-edge
// column). A right zone that would collide with what is already drawn is
// dropped rather than pushed — the fit above makes that unreachable, and a row
// that overlapped would be worse than one that said less.
func placeZones(x0, inner int, left, middle, right []run) []placed {
	out := make([]placed, 0, len(left)+len(middle)+len(right))
	x := x0
	for _, r := range left {
		out = append(out, placed{run: r, from: x})
		x += blocks.Width(r.text)
	}
	if len(middle) > 0 {
		if len(out) > 0 {
			x += zoneGapWidth
		}
		for _, r := range middle {
			out = append(out, placed{run: r, from: x})
			x += blocks.Width(r.text)
		}
	}
	if w := runsWidth(right); w > 0 {
		from := x0 + inner - w
		if from >= x || len(out) == 0 {
			if from < x0 {
				from = x0
			}
			for _, r := range right {
				out = append(out, placed{run: r, from: from})
				from += blocks.Width(r.text)
			}
		}
	}
	return out
}

// leftWidths is what the left zone WANTS and the least it is worth keeping at:
// the whole trail and its floor, or every tab and the current one alone.
func (m *Model) leftWidths(ctx FocusContext) (floor, full int) {
	return runsWidth(m.placeRuns(ctx.Places, 0)), runsWidth(m.placeRuns(ctx.Places, -1))
}

// trailWidths is what the breadcrumb wants and the least it is worth keeping
// at: the whole trail, and the way out plus enough of the name the reader is
// standing on to recognise it (scope.go's own elision ladder).
func trailWidths(ctx FocusContext) (floor, full int) {
	if strings.TrimSpace(ctx.ScopeTail) == "" {
		return 0, 0
	}
	trail := parseScopeTail(ctx.ScopeTail)
	return blocks.Width(trail.shortest()), blocks.Width(trail.text(0, 0))
}

// leftRuns is the left zone at a budget: the breadcrumb when the reader is
// inside something, the place tabs when they are not.
//
// budget 0 asks for the FLOOR — the least this zone is worth keeping at, which
// is the current word alone (for a trail: the way out, the mark for everything
// above it, and enough of the name to recognise it). Every other budget asks
// for the most it can say inside that many cells.
func (m *Model) leftRuns(ctx FocusContext, budget int) []run {
	return m.placeRuns(ctx.Places, budget)
}

// trailRuns is the breadcrumb at a budget, elided by scope.go's own ladder.
func trailRuns(ctx FocusContext, budget int) []run {
	if strings.TrimSpace(ctx.ScopeTail) == "" {
		return nil
	}
	if budget <= 0 {
		return crumbRuns(parseScopeTail(ctx.ScopeTail).shortest())
	}
	return crumbRuns(fitScopeTail(ctx.ScopeTail, budget))
}

// crumbRuns splits a fitted trail into the two tiers the zone reads in: the
// ancestors and the way-out marks dim, the name the reader is standing on one
// tier up — the same brightness the current tab carries, because it is the same
// fact. Both runs answer to [ScopeTarget]: clicking anywhere on the trail is one
// step out, which is what the rail's ‹ and esc do.
func crumbRuns(text string) []run {
	if text == "" {
		return nil
	}
	head, name := text, ""
	if at := strings.LastIndex(text, crumbSep); at >= 0 {
		head, name = text[:at+len(crumbSep)], text[at+len(crumbSep):]
	} else if lead := tokens.GlyphScopeUp + " "; strings.HasPrefix(text, lead) {
		head, name = lead, text[len(lead):]
	} else {
		head, name = "", text
	}
	out := make([]run, 0, 2)
	if head != "" {
		out = append(out, run{id: ScopeTarget, text: head, tok: tokens.TextTertiary})
	}
	if name != "" {
		out = append(out, run{id: ScopeTarget, text: name, tok: tokens.TextSecondary})
	}
	return out
}

// tabGap is the whitespace between two place words. TWO CELLS, and no mark.
//
// The tabs used to be joined by the telemetry separator, and that was the row
// treating navigation as a list of readings. §7 is explicit that these are tabs
// — `chat  work  notebook` — and §15 is the reason: a dot between two doors is a
// label doing structure's job, when the structure (three peers, one of them the
// one you are in) is already legible from the spacing and the ink. Two cells is
// the same break the zones themselves are separated by, which is what makes the
// row read as three groups rather than as one long sentence.
const tabGap = "  "

// The current tab is a FILLED PILL, not a brighter word.
//
// A tab strip has to answer "which one am I in" without being read, and tier
// alone answers it too quietly at the bottom of a busy window: three lowercase
// words in two greys is a difference a reader finds only by comparing them. A
// ground is found peripherally, which is the whole reason §16 spends grounds on
// the delivery card and on this strip.
//
// The two caps are what keep it from being a rectangle. `▐` and `▌` are half-
// block cells painted with the CHIP's colour as their FOREGROUND over whatever
// floor the row is standing on, so each end of the pill is half filled and half
// floor — as close to a rounded corner as a terminal has, and it costs one cell
// per end rather than a box-drawing idiom §16 forbids outright.
//
// The whole thing — caps, padding and word — is one target, because the reader
// points at the tab and not at the geometry of its left edge.
const (
	pillPad  = " "
	pillWord = tokens.TextPrimary
	// pillGround is the chip's own rung. It is the [tokens.Band] rather than a
	// rung of its own for the reason 5.16 gives selection everywhere else: this
	// IS a selection — one of three peers, the one you are standing in — and a
	// second raised ground a shade off the band would be two vocabularies for one
	// idea.
	pillGround = tokens.Band
)

// pillWidth is the cells a filled tab costs beyond its word: two caps and two
// spaces.
var pillWidth = blocks.Width(tokens.GlyphChipCapLeft) + blocks.Width(pillPad)*2 +
	blocks.Width(tokens.GlyphChipCapRight)

// pilled reports whether this profile has a raised ground worth filling a chip
// with. It is [tokens.SelectionBand] and not [tokens.Profile.SheetGround]
// because a pill IS a band: at 16 colours and none there is no trustworthy
// raised background, and a chip drawn in reverse video would be a black slab
// with a word in it — louder than the answer it is giving. Those profiles get
// §7's floor instead, which the row has always had: the current word bright and
// the others dim.
func (m *Model) pilled() bool {
	return m.styler != nil && m.styler.Profile().SelectionStyle() == tokens.SelectionBand
}

// placeRuns is the tabs: every word when they fit, the current word alone when
// they do not. The current one wears the pill, the rest are bare dim words, and
// every one of them is its own target — the homes door is these words and
// nothing else was added to the screen to carry it.
func (m *Model) placeRuns(places []Place, budget int) []run {
	if len(places) == 0 {
		return nil
	}
	pill := m.pilled()
	full := make([]run, 0, len(places)*4)
	for _, p := range places {
		if p.Word == "" {
			continue
		}
		if len(full) > 0 {
			full = append(full, run{text: tabGap, tok: tokens.TextTertiary})
		}
		full = append(full, tabRuns(p, pill)...)
	}
	if len(full) == 0 {
		return nil
	}
	// A negative budget asks what the zone WANTS; zero asks for its floor.
	if budget < 0 || (budget > 0 && runsWidth(full) <= budget) {
		return full
	}
	one := places[0]
	for _, p := range places {
		if p.Current {
			one = p
			break
		}
	}
	if one.Word == "" {
		return nil
	}
	// AT THE FLOOR THE PILL COMES OFF. One word standing alone is unambiguously
	// the one you are in — there is nothing for it to be distinguished FROM —
	// so the four cells the chip costs buy nothing, and on a row this narrow
	// four cells are the difference between naming the place and not.
	word := one.Word
	if budget > 0 && blocks.Width(word) > budget {
		word = blocks.Truncate(word, budget)
	}
	if word == "" {
		return nil
	}
	return []run{{id: one.ID, text: word, tok: placeToken(one)}}
}

// tabRuns draws one tab: a filled pill for the place the reader is in, a bare
// word for the others.
func tabRuns(p Place, pill bool) []run {
	if !p.Current || !pill {
		return []run{{id: p.ID, text: p.Word, tok: placeToken(p)}}
	}
	cap := func(glyph string) run {
		// The cap is the chip's ground worn as INK. No background is asserted,
		// so the other half of the cell keeps the floor the strip painted — the
		// half-filled cell is the whole trick.
		return run{id: p.ID, text: glyph, tok: pillGround}
	}
	return []run{
		cap(tokens.GlyphChipCapLeft),
		{id: p.ID, text: pillPad + p.Word + pillPad, tok: pillWord, bg: pillGround, onBg: true},
		cap(tokens.GlyphChipCapRight),
	}
}

func placeToken(p Place) tokens.Token {
	if p.Current {
		return tokens.TextSecondary
	}
	return tokens.TextTertiary
}

// middleRuns is the only zone that is usually empty, and that emptiness is the
// design (§7): the standing legends live on the `?` sheet now, and what is left
// here is what is true RIGHT NOW — a send that just failed, an interrupt that
// would land, a question waiting on a digit, and whatever live verbs the host
// says the current focus really has.
func middleRuns(ctx FocusContext) []run {
	out := make([]run, 0, 6)
	add := func(runs []run) {
		if len(runs) == 0 {
			return
		}
		if len(out) > 0 {
			out = append(out, run{text: sep, tok: tokens.TextTertiary})
		}
		out = append(out, runs...)
	}
	if ctx.Input == InputFailed && ctx.Hint != "" {
		// A failure is a whole coloured sentence, not a chip: there is no key
		// to press on it, and §12 spends coral on exactly this.
		add([]run{{text: ctx.Hint, tok: tokens.Coral}})
	}
	if ctx.EscInterrupts {
		add(chipRuns(InterruptTarget, registry.ChipFor(interruptVerb, interruptKey), tokens.TextSecondary))
	}
	if ctx.KeyMode == KeyModeAnswer && ctx.KeyModeCount > 0 {
		// Amber when a human really is blocked on it, which is what the count
		// of open questions says. The chip is the badge now: one statement,
		// with the keys that end it written on the same words.
		tok := tokens.TextSecondary
		if ctx.Attention > 0 {
			tok = tokens.Amber
		}
		add(chipRuns("", registry.ChipFor(answerVerb, answerRange(ctx.KeyModeCount)), tok))
	}
	// THIS TURN's receipts, and only while there is a turn. The composer's meta
	// strip used to hold them on a permanent row of its own, which meant the
	// window carried an elapsed cell and a `$—` on every frame of every minute
	// nobody was waiting for anything. They belong beside `interrupt esc`
	// because they are the same kind of fact — what is happening right now —
	// and they leave with it.
	//
	// The ELAPSED reading ages, which is allowed here and nowhere in the
	// committed transcript (8.1.2's corollary puts an ageing cell in live
	// regions only, and the middle zone is the only live region on this row).
	if ctx.Live {
		if ctx.Elapsed > 0 {
			add([]run{{text: tokens.Elapsed(ctx.Elapsed), tok: tokens.TextTertiary}})
		}
		if ctx.HaveTurnCost {
			add([]run{{text: tokens.Money(ctx.TurnCost), tok: tokens.Green}})
		}
	}
	for _, e := range boundedVerbs(ctx.Verbs) {
		add(chipRuns(e.ID, registry.ChipOn(e, registry.SurfaceDefault), tokens.TextSecondary))
	}
	return out
}

// answerRange is the digit range the open question answers to.
func answerRange(n int) string { return "1" + enDash + strconv.Itoa(n) }

// boundedVerbs is the host's live verbs, capped.
func boundedVerbs(entries []registry.Entry) []registry.Entry {
	if len(entries) > maxVerbs {
		return entries[:maxVerbs]
	}
	return entries
}

// chipRuns draws one [registry.Chip] in the two tiers §16's grammar asks for,
// and adds the one thing this surface needs on top of them: the WHOLE chip — the
// gap between the halves included — carries one id, because a reader points at
// the words and not at the key.
//
// The tiers themselves are [keychip.Of]'s, which is §16's "one renderer, every
// surface" taken literally. This function used to BE that renderer, privately,
// and the composer's hint chips, the consent dialog's key strip and the empty
// room's teaching rows each had their own copy — two of which had drifted into
// key-first, which is the `esc close` bug the law was written about.
func chipRuns(id string, chip registry.Chip, verb tokens.Token) []run {
	spans := keychip.Of(chip, verb)
	if len(spans) == 0 {
		return nil
	}
	out := make([]run, 0, len(spans))
	for _, span := range spans {
		out = append(out, run{id: id, text: span.Text, tok: span.Tok})
	}
	return out
}

// fact is one standing statement in the right zone: the column it fits as, the
// words, the tier, and the target id when the words are also a door.
type fact struct {
	zone string
	text string
	tok  tokens.Token
	id   string
}

// rightFacts is the right zone before fitting, in the order it is read:
// `~/aforge-v2 · ▂ 3% of 262K · ◐ 2 working · 1 question · $0.31 today`.
//
// The ORDER is worth stating because it is not the priority order. It runs
// broadest to narrowest — where the work lands, how much room is left, what it
// has cost — so the eye travelling in from the edge meets the
// figure it came for first and the context for it after. The priority order,
// which decides what leaves, is almost the reverse: money is the last thing to
// go (5.9: money "is the one number the user never forgives us for hiding") and
// the directory is nearly the first.
//
// ONE of these is a door, and exactly one. A directory, a gauge and a day's
// total are statements about this window, and there is nothing to open on a
// statement; the DOCK is not a statement, it is the sidebar put away, and a
// drawer with no handle is worse than either. See [DockTarget] for the whole of
// that argument — the rule the row still keeps is that a reader can say in one
// sentence which words do something.
func rightFacts(ctx FocusContext) []fact {
	out := make([]fact, 0, 4+len(ctx.Health))
	for _, h := range ctx.Health {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, fact{zone: zoneHealth, text: h, tok: tokens.TextTertiary})
		}
	}
	if dir := strings.TrimSpace(ctx.Dir); dir != "" {
		out = append(out, fact{zone: zoneDir, text: dir, tok: tokens.TextTertiary})
	}
	// The gauge is TWO facts sharing one tier: the reading, and the window word
	// it is a reading of. Splitting them is what lets a narrowing row keep `▂ 3%`
	// — which still says "there is room" or "there is not" — after it can no
	// longer afford to name the window. Both are absent entirely when the window
	// is unknown; §16's EMPTINESS, and see [FocusContext.HaveCtx].
	if ctx.HaveCtx && ctx.CtxWindow > 0 {
		tok := tokens.ContextToken(ctx.CtxUsed, ctx.CtxWindow)
		frac := ctxFraction(ctx.CtxUsed, ctx.CtxWindow)
		out = append(out, fact{zone: zoneGauge,
			text: tokens.Gauge(frac) + " " + tokens.Percent(frac), tok: tok})
		out = append(out, fact{zone: zoneWindow, text: windowWord(ctx.CtxWindow), tok: tok})
	}
	if d, ok := dockFact(ctx.Dock); ok {
		out = append(out, d)
	}
	if ctx.HaveSpend {
		out = append(out, fact{zone: zoneSpend, text: tokens.Money(ctx.Spend) + spendSuffix,
			tok: tokens.TextTertiary})
	}
	return out
}

// windowWord names the window the gauge is a percentage of. It reads `of 262K`
// — the preposition, because a bare count beside a percentage reads as a second
// figure rather than as the first one's denominator.
//
// The number itself comes from [tokens.AppendCount], the product's one count
// ladder, so this cell cannot drift from every other place a token count is
// written.
func windowWord(window int64) string { return "of " + tokens.Count(window) }

// ctxFraction is used/window, clamped, for the one-cell gauge and the percent
// beside it. It is one function so the cell and the number can never disagree
// about how full the window is.
func ctxFraction(used, window int64) float64 {
	if window <= 0 || used <= 0 {
		return 0
	}
	if used >= window {
		return 1
	}
	return float64(used) / float64(window)
}

// rightRuns is the surviving facts, joined by the separator, in display order.
// The gauge and its window word are joined by a SPACE rather than by the
// separator, because they are one reading and a dot between them would say they
// were two.
func rightRuns(facts []fact, keep map[string]bool) []run {
	out := make([]run, 0, len(facts)*2)
	for _, f := range facts {
		if !keep[f.zone] {
			continue
		}
		if len(out) > 0 {
			join := sep
			if f.zone == zoneWindow {
				join = " "
			}
			out = append(out, run{text: join, tok: tokens.TextTertiary})
		}
		out = append(out, run{id: f.id, text: f.text, tok: f.tok})
	}
	return out
}

func (m *Model) paint(text string, tok tokens.Token) string {
	if m.styler == nil || text == "" {
		return text
	}
	return m.styler.PaintToken(text, tok)
}

// paintOn draws a run that carries its own ground — the current tab's pill, and
// nothing else on this row. A nil Styler degrades to plain text on the same
// posture every other paint here takes.
func (m *Model) paintOn(text string, fg, bg tokens.Token) string {
	if m.styler == nil || text == "" {
		return text
	}
	return m.styler.PaintOn(text, fg, bg)
}
