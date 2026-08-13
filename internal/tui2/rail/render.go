package rail

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Mode is which of the three renderings of one scope model to draw (5.15,
// 8.2.8). Scope is not a rail feature; the rail is one rendering of it.
type Mode uint8

// ModeRail and ModeList name two SURFACES and share one implementation, which
// is 5.15 taken literally: "narrow terminals render the same scope rows, with
// the same keys and the same selection semantics, as a full-pane list". The
// same rows means the same code. What differs between a 28-column rail and an
// 80-column pane is how many columns the row has, and the renderer reads that
// from the width it was handed rather than from a flag — which is also why a
// card's telemetry degrades and drops by ROOM (see [View.fitMetaInto]) instead
// of by mode.
//
// The constants stay distinct because the shell speaks them: it is choosing a
// surface, and a caller that says ModeRail should not have to also compute the
// breakpoint that made it true.
const (
	// ModeAuto picks between [ModeRail] and [ModeList] with [ModeFor]. It is
	// the zero value, so a shell that never thinks about breakpoints still gets
	// a sane rendering.
	ModeAuto Mode = iota
	// ModeRail is the right rail at or above [tokens.RailAtWidth].
	ModeRail
	// ModeList is the scope as a full-pane list below that breakpoint. At 80
	// columns this is the primary experience, not a fallback (Part 9.12).
	ModeList
	// ModeHUD is the bounded sticky summary above the composer (8.2.8): at most
	// [tokens.HUDRowCap] rows plus a fold line. It carries the live summary and
	// never the scope map, which is why it draws no surface row, no scope
	// header and no selection.
	ModeHUD
)

// String names the mode.
func (m Mode) String() string {
	switch m {
	case ModeAuto:
		return "auto"
	case ModeRail:
		return "rail"
	case ModeList:
		return "list"
	case ModeHUD:
		return "hud"
	}
	return "invalid"
}

// ModeFor is the breakpoint decision for a TERMINAL width, and the number lives
// in the tokens table rather than in this file (10.5.24: the numbers are
// written down before the code). A shell laying out a rail pane passes the
// terminal's width here and [ModeRail] to the pane it then creates.
func ModeFor(width int) Mode {
	if width >= tokens.RailAtWidth {
		return ModeRail
	}
	return ModeList
}

// Layout constants that are this package's own, with the arithmetic that
// produced them.
const (
	// gutterWidth is the left column a map rendering reserves. It carries the
	// ▎ accent rail (5.21) that marks the selection while the pane is
	// UNFOCUSED — tokens' contrast law forbids a dimmed foreground on a raised
	// band, so a dimmed pane marks its selection with the accent instead. The
	// column is reserved in both focus states so nothing shifts sideways when
	// focus moves.
	gutterWidth = 1
	// gutterFloor is the width below which the gutter is spent on content
	// instead. At seven columns a row is a glyph and four letters, and a column
	// given to a selection marker is a column taken from the name — see
	// [gutterFor].
	gutterFloor = 8
	// indentStep is 5.13's spacing rhythm: two spaces per depth.
	indentStep = 2
	// sep is the telemetry separator (5.17) and its display width.
	sep      = " " + tokens.GlyphSeparator + " "
	sepWidth = 3
	// previewLines bounds how far SELECTING a row expands it in place, and the
	// rail reserves that many lines whichever row the cursor is on (see the
	// detail reserve in [View.renderMap]). Two, because two is what a preview
	// has to say that the collapsed row does not: a tree row's own words and
	// the artifact it produced. A card that expands to forty rows has stopped
	// being a card, and a card that expands at all at its neighbours' expense
	// has stopped being a map (7.2).
	previewLines = 2
	// minNameWidth is the narrowest a name may be squeezed to before the cells
	// competing with it start dropping instead.
	minNameWidth = 6
)

// Telemetry drop priorities (5.9: "money is always visible; the rest can
// truncate on narrow rails"). Higher survives longer — the discipline of
// 10.5.22's footer registry, applied to a card's third line.
//
// The order below is what a reader does something about, in order. Money is
// pinned because it is the one number the user never forgives us for hiding.
// Elapsed outranks the model word because "how long has this been going" is a
// question that gets asked of a rail every few seconds and "which model" is one
// that gets asked once a session — and 5.15's own wireframe spends a narrow
// worker row on the clock. Context outranks the census because it is a health
// signal (5.9) and a census is a shape the branches already draw (§3).
const (
	prioMoney   = 100
	prioElapsed = 80
	prioModel   = 70
	prioContext = 60
	prioCounts  = 50
)

// View renders a [Model]. It owns the buffers a repaint needs, so a rail that
// repaints on a timer allocates almost nothing per frame: the line slice, the
// span slice, the fold buffers and the string builder are all reused.
//
// A View is not safe for concurrent use and is not meant to be — it belongs to
// the pane that draws with it.
type View struct {
	profile tokens.Profile
	focus   tokens.Focus

	lines []string
	buf   strings.Builder
	line  lineBuf

	fold    folder
	heights []int
	guides  []treeGuide

	hudRows   []int
	hudStates []blocks.ItemState
	hudFolder blocks.Folder

	meta  [5]metaCell
	rule  string
	ruleW int

	// drift is how far the model's rows have aged since they were measured,
	// latched once per render. See [View.telemetry].
	drift time.Duration

	// lead is the glyph the surface row carries when it has absorbed the scope
	// header (see renderMap). It is render-scoped state, set immediately before
	// row 0 is appended and cleared immediately after, so no other row can pick
	// it up.
	lead string

	// The pointer table (hit.go). marks runs parallel to lines and says which
	// model row each screen line belongs to; up* is where the way out of the
	// scope was drawn. Both are written by push and by cardLine as a side
	// effect of the ONE loop that already knows both facts, which is what keeps
	// the map from drifting away from the picture — the old surface's 30
	// hand-maintained rectangles are the failure this avoids.
	marks                []int32
	mark                 int32
	upLine, upFrom, upTo int

	// hover is the row the pointer is resting on, and hovered says the line
	// being painted right now is that row. They are separate because the paint
	// happens several calls below the loop that knows which row it is on, and
	// threading a row index through six line builders to reach one boolean
	// would be the wrong kind of honesty.
	hover   int32
	hovered bool
}

// NewView returns a View painting for a Styler's profile and focus. A nil
// Styler means no colour, which is the honest degradation for a terminal that
// would not say what it can do.
func NewView(st *tokens.Styler) *View {
	// A zero hover would be row 0, so it is set explicitly: a View nobody has
	// pointed at must light nothing.
	v := &View{hover: markNoHover}
	v.SetStyler(st)
	return v
}

// SetStyler re-points the View at a profile and focus. Dimming is a property of
// the pane (8.3), so a pane that just lost focus calls this and repaints; it
// does not re-resolve anything per row.
func (v *View) SetStyler(st *tokens.Styler) {
	if st == nil {
		v.profile, v.focus = tokens.NoColor, tokens.FocusNormal
		return
	}
	v.profile, v.focus = st.Profile(), st.Focus()
}

// Profile and Focus report what the View paints for.
func (v *View) Profile() tokens.Profile { return v.profile }

// Focus reports the pane focus this View paints for.
func (v *View) Focus() tokens.Focus { return v.focus }

// Render draws the model at a size and returns at most height lines, each at
// most width printable cells.
//
// The returned slice aliases the View's buffer and is valid until the next
// Render. Callers that keep lines across frames must copy them.
func (v *View) Render(m *Model, mode Mode, width, height int) []string {
	v.lines = v.lines[:0]
	v.marks = v.marks[:0]
	v.mark = markChrome
	v.hovered = false
	v.upLine, v.upFrom, v.upTo = -1, 0, 0
	if m == nil || width <= 0 || height <= 0 {
		return v.lines
	}
	// The drift is latched ONCE per render, so every row in a frame ages by the
	// same amount and two cards that started together stay together (8.1.3's
	// phase lock, said for a number instead of a glyph).
	v.drift = m.Drift()
	if mode == ModeAuto {
		mode = ModeFor(width)
	}
	if mode == ModeHUD {
		v.renderHUD(m, width, height)
		return v.lines
	}
	v.renderMap(m, width, height)
	return v.lines
}

// Rail, List and HUD are the three renderings by name, for a caller that has
// already decided.
func (v *View) Rail(m *Model, width, height int) []string {
	return v.Render(m, ModeRail, width, height)
}

// List renders the scope as a full-pane list.
func (v *View) List(m *Model, width, height int) []string {
	return v.Render(m, ModeList, width, height)
}

// HUD renders the bounded live summary.
func (v *View) HUD(m *Model, width, height int) []string {
	return v.Render(m, ModeHUD, width, height)
}

// handleDotRow is where the collapsed rail's one signal sits: the top line,
// which is where the threads section's own dot would have been had the column
// been open. A mark that moved when the rail collapsed would make the reader
// look for it.
const handleDotRow = 0

// Handle draws the COLLAPSED rail: a column of ground carrying at most one
// thing.
//
// IT CARRIES EXACTLY ONE SIGNAL and it is the unseen dot. Everything else the
// open rail says — how many jobs are running, what they cost, which
// conversation you are in — is a fact the reader collapsed the rail in order to
// stop being told, and a handle that kept one of them would be the drawer
// re-opening itself a column at a time. What survives is the one statement a
// hidden rail genuinely cannot make: something landed where you were not
// looking.
//
// A blank handle is therefore the ordinary case, and that is correct rather than
// unfinished. Discoverability is not this column's job — the chord and the bar's
// dock are both doors to the same map — and a handle that drew furniture to be
// noticed would be noticed exactly as often as the dot.
//
// The returned slice aliases the View's buffer, like [View.Render]'s.
func (v *View) Handle(unseen bool, width, height int) []string {
	v.lines = v.lines[:0]
	v.marks = v.marks[:0]
	v.mark = markChrome
	v.hovered = false
	v.upLine, v.upFrom, v.upTo = -1, 0, 0
	if width <= 0 || height <= 0 {
		return v.lines
	}
	for y := 0; y < height; y++ {
		l := &v.line
		l.reset(width)
		if y == handleDotRow && unseen {
			l.add(roomDot, tokens.Cyan)
		}
		v.push(v.emit(width, false, tokens.Ground), height)
	}
	return v.lines
}

// renderMap draws the scope map: scope header, the surface row that never
// folds, a hairline at the room boundary, then the members under the overflow
// policy.
func (v *View) renderMap(m *Model, width, height int) {
	scope := m.Scope()
	rows := scope.Rows
	sel := m.Cursor()
	ident := v.identity(scope.Seed, tokens.Token(255))
	band := tokens.BandFor(ident)
	// Whether this row wears a band at all. Two things can take it away, and
	// both hand the selection to the gutter's accent rail instead (see
	// [View.gutter]):
	//
	//   - FOCUS. The contrast law (tokens.Legal): a dimmed foreground may sit
	//     only on the ground, so an unfocused pane draws no band.
	//   - PROFILE. tokens.SelectionMarker is the answer at `--color none`, where
	//     there are no escape bytes to spend on a ground. Without this the
	//     no-colour tier lost selection entirely: the band was the ONLY thing
	//     saying which row the cursor was on, and it resolved to nothing.
	banded := v.focus == tokens.FocusNormal &&
		v.profile.SelectionStyle() != tokens.SelectionMarker

	// The scope header and the surface row are two different objects, and 5.15's
	// wireframe says two different things in them: `‹ wisp-parity` names the
	// ROOM and is the way out of it; `● orchestrator` says what row 0 IS inside
	// that room. Drawn that way they are both worth their line.
	//
	// A source with only ONE word for both draws it twice, and a pty screenshot
	// caught the result: an entered task room said "Permanent Aforge spine" in
	// the header, again on row 0, and a third time on the detail card in the
	// main pane. It is not one source's habit — chat's taskScope and the homes
	// package both name row 0 after the scope, and Scope.normalize fills the
	// name in from the title when a source leaves it empty, so the rail is the
	// last place that can see the collision at all.
	//
	// So the rail draws ONE row instead of the same word twice: the surface row,
	// with the header's ‹ riding on it. It does NOT invent the second word.
	// 5.15 supplies "orchestrator" for a task room and nothing for a notebook,
	// and a word chosen here would be this package claiming to know what a room
	// it has never heard of contains — which is the affordance lying (5.20) with
	// extra steps. 5.14's litmus settles which of the two lines goes: a line
	// whose whole content is the line above it answers "what would the user do
	// with this right now?" with nothing.
	//
	// Every affordance survives the merge. The row is still row 0, still
	// selectable, still carries its lifecycle glyph, its status and meta lines
	// and its composer mark (5.11); the ‹ is still there to click and esc still
	// pops the scope. The one thing dropped is 10.3.10's `(2 of 5)` counter,
	// because the merged row's right edge belongs to the composer mark — an
	// affordance outranks a count (5.14).
	merged := m.Depth() > 0 && surfaceRepeatsScope(scope)
	if m.Depth() > 0 && !merged {
		// The whole header line is the way out, because the whole header line
		// says the name of the room you would be leaving (5.15: "the rail's
		// scope header is the breadcrumb tail and is clickable to go up").
		v.mark = markChrome
		v.upLine, v.upFrom, v.upTo = len(v.lines), 0, width
		v.hovered = v.hover == markScopeUp
		v.push(v.scopeHeader(scope, sel, len(rows)-1, ident, width), height)
		v.hovered = false
		if v.upLine >= len(v.lines) {
			v.upLine = -1
		}
	}

	// THE DETAIL RESERVE (7.2, and the half of 8.1.7 the fold cannot state).
	//
	// A selected card expands in place (5.9), and for as long as the fold
	// measured it AT that expanded height, the cursor decided how many other
	// rows fit: resting the pointer on a card pushed its neighbours — the very
	// cards the reader was reaching for — down into `… 7 more`. 7.2's "cards
	// never re-sort themselves while visible" was kept to the letter (nothing
	// re-sorted) and broken in spirit, because a row that MOVES OR VANISHES
	// because the cursor paused above it is the same betrayal as a row that
	// re-sorts, and it is worse under a pointer: the target moves out from under
	// the click that was aimed at it.
	//
	// So the fold budgets every row at its COLLAPSED height, and the expansion
	// is paid out of a reserve that is the same size wherever the cursor is:
	// [previewLines] at most, and no more than the deepest preview this scope
	// actually has, so a scope with nothing to preview pays nothing. What the
	// reserve buys is exactly the invariant: which rows are on screen, in what
	// order, at what height, does not depend on the selection — only the
	// previewed card's own lines appear, directly beneath it.
	members := rows[1:]
	// THE PLAN IS A TREE, AND ONLY INSIDE A JOB (§3). A scope the reader has
	// descended into is a plan, and its members are drawn with v1's connector
	// grammar; the home rail is a list of jobs, rooms and doors that have no
	// parentage to draw. The guides are measured over EVERY member, not over the
	// ones that survive the fold, so a last child that folded away cannot turn
	// its sibling's ├ into a ╰ and redraw a branch that is still there.
	v.sizeGuides(members, m.Depth() > 0)
	head := len(v.lines) + v.shapeOf(rows[0], false, 0).height()
	detail, budget, hair := previewLines, 0, false
	if len(members) > 0 {
		total, deepest := v.sizeMembers(members)
		if p := v.previewOf(rows[0]); p > deepest {
			// Row 0 expands too, and its lines land ABOVE the members — so the
			// reserve has to cover the surface or a selected row 0 would push
			// the whole list down into the same fold.
			deepest = p
		}
		// The hairline is the room boundary (5.13), and it is worth a row only
		// when there are at least two left for the members it separates — one
		// for a row and one for the fold line that accounts for the rest. It is
		// decided from the collapsed head for the same reason as everything
		// else here: a rule that appeared and disappeared with the cursor would
		// move every member row by one.
		if head+2 < height {
			hair, head = true, head+1
		}
		budget = height - head
		detail = deepest
		switch {
		case total <= budget:
			// Slack. The preview spends lines no row wanted, costs nothing, and
			// nothing folds that was not folding already.
			if slack := budget - total; detail > slack {
				detail = slack
			}
		default:
			// The list is folding anyway, so the reserve is paid in rows. It may
			// never take the last row or the fold line that accounts for the
			// rest — at that size the map's job is to say where the cursor is
			// and how much is off screen, and a preview outranks neither.
			if room := budget - 2; detail > room {
				detail = room
			}
		}
		if detail < 0 {
			detail = 0
		}
		budget -= detail
	}

	// Row 0 is the conversational surface and is never folded away: a scope you
	// cannot speak into is not a scope (5.15).
	if merged {
		v.lead = tokens.GlyphScopeUp
	}
	v.mark = 0
	v.hovered = v.hover == 0 && sel != 0
	v.appendRow(rows[0], sel == 0, width, height, detail, band, banded, ident, treeGuide{})
	v.hovered = false
	v.lead = ""
	v.mark = markChrome

	if len(members) == 0 {
		return
	}
	if hair {
		v.push(v.hairline(width), height)
	}
	if budget <= 0 {
		return
	}

	p := v.fold.fit(members, v.heights, budget, sel-1, scope.Live())
	if p.atTop && p.fold != "" {
		v.push(v.foldLine(p.fold, width), height)
	}
	prev := ident
	for _, i := range p.shown {
		r := members[i]
		rowIdent := ident
		if r.Kind == RowTask {
			rowIdent = v.identity(seedOf(r, scope.Seed), prev)
			prev = rowIdent
		}
		// The band is the SCOPE's, never the row's: 5.16 spends the identity
		// tint on "which room am I in", so it is plain at home and tinted
		// inside a task's scope. A home rail that tinted each selection with
		// the selected card's hue would answer a question nobody asked and
		// make the band change colour as the cursor moves.
		v.mark = int32(i + 1)
		// The band already says which row the cursor is on, so a selected row
		// is never also drawn as hovered: one target, one statement.
		v.hovered = v.hover == v.mark && sel != i+1
		v.appendRow(r, sel == i+1, width, height, detail, band, banded, rowIdent, v.guides[i])
		v.hovered = false
		v.mark = markChrome
	}
	if !p.atTop && p.fold != "" {
		v.push(v.foldLine(p.fold, width), height)
	}
}

// surfaceRepeatsScope reports that row 0 has nothing to say that the scope
// header is not already saying. The comparison is on the CLEANED, trimmed words
// rather than on the raw strings, because a source that pads or that lets a
// model's own spacing through would otherwise dodge the check and draw the
// duplicate anyway.
func surfaceRepeatsScope(s Scope) bool {
	if len(s.Rows) == 0 {
		return false
	}
	title := strings.TrimSpace(s.Title)
	return title != "" && title == strings.TrimSpace(s.Rows[0].Name)
}

// sizeMembers measures every member so the fold can budget in lines, and
// reports the total and the deepest preview any one of them would open.
//
// The heights are COLLAPSED heights — the height every row has when nobody is
// looking at it — and that is the whole of the fix the detail reserve is the
// other half of: a fold fed the selected row's expanded height is a fold the
// cursor is steering.
func (v *View) sizeMembers(members []Row) (total, preview int) {
	if cap(v.heights) < len(members) {
		v.heights = make([]int, len(members))
	}
	v.heights = v.heights[:len(members)]
	for i := range members {
		collapsed := v.shapeOf(members[i], false, 0).height()
		v.heights[i] = collapsed
		total += collapsed
		if p := v.shapeOf(members[i], true, previewLines).height() - collapsed; p > preview {
			preview = p
		}
	}
	return total, preview
}

// sizeGuides works out every member's connector from the DEPTHS ALONE, in one
// backwards pass, and it is the whole of what the rail needs to know about the
// plan's shape.
//
// The reading is the one a tree drawn in display order allows: a row is the last
// child at its depth when no row of that same depth follows it before something
// shallower does, and an ancestor's guide continues past a row exactly when that
// ancestor has a later sibling. Walking backwards answers both at once — the
// state carried is "have I seen a row at this depth yet", and a row at depth d
// cuts off everything deeper, because those rows were its own children.
//
// It never asks the source which of its siblings is last. The alternative — a
// Row field saying "I am the last one" — would be a fact about a row's
// NEIGHBOURS stored on the row, and a refresh that dropped a sibling would leave
// a ╰ above three more branches. What the source DOES say is whether a row has a
// parent at all ([Row.Tree]), which is a fact about the row itself.
//
// A ROW WITH NO CONNECTOR IS A WALL. Walking backwards, anything that is not a
// limb — a job card, a conversation, a section heading — ends the tree below it,
// because the limbs above it belong to something else. Without the wall the home
// rail's two lists would be read as one shape: the four rows behind the homes
// lid sit at depth 1 like a plan step does, and the last step of the last job
// would lose its ╰ to a sibling three sections away that it has never met.
func (v *View) sizeGuides(members []Row, tree bool) {
	if cap(v.guides) < len(members) {
		v.guides = make([]treeGuide, len(members))
	}
	v.guides = appendGuides(v.guides[:0], members, tree)
}

// Guides is [View.sizeGuides] for a caller outside this package: the connectors
// a row list wears, one per row, worked out from the depths alone.
//
// tree says the rows are the members of a JOB SCOPE, where every step and worker
// is a limb; at home only the rows the source marked [Row.Tree] are. It is the
// same argument the rail passes itself, and it is the only thing about the
// caller's altitude this needs to know.
//
// IT EXISTS SO A SECOND SURFACE CAN DRAW THE RAIL'S TREE WITHOUT BUILDING ONE.
// The overview page shows the same plan under the same job at page altitude; two
// independent readings of "which of these is the last child" is two pictures of
// one plan, and the one that is wrong is the one nobody checks.
func Guides(members []Row, tree bool) []Guide {
	return appendGuides(make([]Guide, 0, len(members)), members, tree)
}

// appendGuides is the walk itself, into a caller's buffer.
func appendGuides(dst []Guide, members []Row, tree bool) []Guide {
	dst = dst[:0]
	for range members {
		dst = append(dst, Guide{})
	}
	// The tree's own root depth, so a plan reads the same whether it is drawn
	// under its card at home or under the surface row inside the job (see
	// [Guide.Level]). Nothing below root can be a limb.
	root := maxIndentDepth + 1
	for i := range members {
		if !connects(members[i], tree) {
			continue
		}
		if depth := clamp(members[i].Depth, 0, maxIndentDepth); depth < root {
			root = depth
		}
	}
	var seen [maxIndentDepth + 1]bool
	for i := len(members) - 1; i >= 0; i-- {
		depth := clamp(members[i].Depth, 0, maxIndentDepth)
		limb := connects(members[i], tree) && depth >= root
		g := Guide{Last: !seen[depth]}
		if limb {
			g.On = true
			g.Level = depth - root
			for k := root; k < depth; k++ {
				if seen[k] {
					g.Open |= 1 << uint(k-root)
				}
			}
		}
		dst[i] = g
		if !limb {
			// The wall. Everything the walk had accumulated belonged to the
			// tree that has just ended.
			seen = [maxIndentDepth + 1]bool{}
		}
		seen[depth] = true
		for k := depth + 1; k <= maxIndentDepth; k++ {
			seen[k] = false
		}
	}
	return dst
}

// connects reports whether a row wears a connector: every step and worker
// inside a job scope, and anywhere at all a row the source has named a limb.
func connects(r Row, tree bool) bool {
	if r.Tree {
		return true
	}
	switch r.Kind {
	case RowStep, RowWorker:
		return tree
	}
	return false
}

// previewOf is how many lines SELECTING a row would add to it: the difference
// between its two shapes, which is the only definition of "the preview" that
// cannot drift from what [View.appendRow] actually draws.
func (v *View) previewOf(r Row) int {
	return v.shapeOf(r, true, previewLines).height() - v.shapeOf(r, false, 0).height()
}

// renderHUD draws the bounded sticky summary (8.2.8). It is a different object
// from the rail with a different job: one line per live thing, capped, with a
// fold line that accounts for the rest. No cursor, no surface row, no map.
//
// The fold here is [blocks.Folder] verbatim — equal-height rows and no
// selection to pin is exactly the shape that package's 8.1.7 implementation
// was written for.
func (v *View) renderHUD(m *Model, width, height int) {
	budget := height
	if budget > tokens.HUDRowCap {
		budget = tokens.HUDRowCap
	}
	if budget <= 0 {
		return
	}
	scope := m.Scope()
	members := scope.Members()
	// Indices rather than copies: a Row is a wide struct and the HUD re-filters
	// on every frame.
	v.hudRows = v.hudRows[:0]
	v.hudStates = v.hudStates[:0]
	live := false
	for i := range members {
		if !hudWorthy(members[i]) {
			continue
		}
		v.hudRows = append(v.hudRows, i)
		v.hudStates = append(v.hudStates, members[i].itemState())
		if members[i].Attention().Live() {
			live = true
		}
	}
	if len(v.hudRows) == 0 {
		return
	}
	var p blocks.Plan
	if live {
		p = v.hudFolder.Live(v.hudStates, budget)
	} else {
		p = v.hudFolder.Finalized(v.hudStates, budget)
	}
	if p.FoldAtTop && p.Fold != "" {
		v.push(v.foldLine(p.Fold, width), budget)
	}
	prev := tokens.Token(255)
	for _, i := range p.Shown {
		r := &members[v.hudRows[i]]
		ident := prev
		if r.Kind == RowTask {
			ident = v.identity(seedOf(*r, scope.Seed), prev)
			prev = ident
		}
		v.push(v.hudLine(*r, width, ident), budget)
	}
	if !p.FoldAtTop && p.Fold != "" {
		v.push(v.foldLine(p.Fold, width), budget)
	}
}

// hudWorthy is what the bounded summary carries: everything that is not over.
// A settled or cancelled row has nothing left to say in a live summary, and
// 10.3.15 is explicit that everything else must be carried — background work
// with no panel goes invisible.
func hudWorthy(r Row) bool {
	switch r.Kind {
	case RowSection, RowNote, RowThread:
		// The HUD is the live summary of WORK. A heading has nothing to
		// summarise, and a conversation is not a thing that finishes — carrying
		// every thread in the store into a bounded eight-row summary would spend
		// the whole budget on rows that will never leave it.
		return false
	}
	if r.Tree {
		// A LIMB IS ALREADY IN THE SUMMARY, as a number. The card above it
		// carries the census of everything under it (`2◐ 1✓`), so a HUD that
		// listed the parts as well would spend its whole budget saying the same
		// thing twice — and at eight rows that is the difference between a
		// summary of the running work and a fold line where the summary was.
		return false
	}
	if r.Questions > 0 {
		return true
	}
	switch r.Life {
	case LifeSettled, LifeCancelled:
		return false
	}
	return true
}

// hudLine is one bounded summary row: glyph, name, status, and the two numbers
// a person actually acts on.
func (v *View) hudLine(r Row, width int, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	att := r.Attention()
	l.add(att.Glyph(), v.glyphToken(r, ident))
	l.add(" ", tokens.TextTertiary)

	right := v.rightCell(v.telemetry(r))
	rightW := blocks.Width(right)
	nameRoom := l.max - l.w - rightW
	if rightW > 0 {
		nameRoom--
	}
	name := v.clean(r.Name)
	if status := v.clean(r.Status); status != "" && nameRoom > blocks.Width(name)+minNameWidth+sepWidth {
		l.add(blocks.Truncate(name, nameRoom), v.nameToken(r))
		room := l.max - l.w - rightW
		if rightW > 0 {
			room--
		}
		l.add(sep, tokens.TextTertiary)
		l.add(blocks.Truncate(status, room-sepWidth), tokens.TextSecondary)
	} else {
		l.add(blocks.Truncate(name, nameRoom), v.nameToken(r))
	}
	if right != "" {
		l.padTo(l.max - rightW)
		l.add(right, tokens.TextTertiary)
	}
	return v.emit(width, false, tokens.Ground)
}

// telemetry is one row's numbers AT THIS FRAME: what the snapshot measured,
// with a live row's clock aged forward to now.
//
// ONLY A LIVE ROW AGES. A settled card's elapsed is a finished measurement and
// adding to it would invent time nobody spent (8.2.20); a queued row has not
// started, and [Attention.Live] already refuses to call it live for exactly
// that reason. So the one cell that moves on this surface is the one cell that
// is genuinely still running.
func (v *View) telemetry(r Row) Telemetry {
	t := r.Meta
	if v.drift <= 0 || !t.HasElapsed || !r.Attention().Live() {
		return t
	}
	t.Elapsed += v.drift
	return t
}

// rightCell is the HUD's flush-right pair: money then elapsed, both width
// stable so the column never dances (5.21).
func (v *View) rightCell(t Telemetry) string {
	var b strings.Builder
	if t.HasCost {
		b.WriteString(tokens.Money(t.Cost))
	}
	if t.HasElapsed {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tokens.ElapsedCell(t.Elapsed))
	}
	return b.String()
}

// scopeHeader is the breadcrumb tail (5.15) with the position of 10.3.10:
// `‹ wisp-parity            (2 of 5)`.
func (v *View) scopeHeader(scope Scope, sel, members int, ident tokens.Token, width int) string {
	l := &v.line
	l.reset(width)
	l.add(tokens.GlyphScopeUp, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	pos := ""
	if sel > 0 && members > 0 {
		var buf [24]byte
		out := append(buf[:0], '(')
		out = strconv.AppendInt(out, int64(sel), 10)
		out = append(out, " of "...)
		out = strconv.AppendInt(out, int64(members), 10)
		out = append(out, ')')
		pos = string(out)
	}
	posW := blocks.Width(pos)
	room := l.max - l.w - posW
	if posW > 0 {
		room--
	}
	l.add(blocks.Truncate(v.clean(scope.Title), room), ident)
	if pos != "" && l.max-l.w >= posW {
		l.padTo(l.max - posW)
		l.add(pos, tokens.TextTertiary)
	}
	return v.emit(width, false, tokens.Ground)
}

// hairline is the rule under the surface row. 5.13 allows rules only at room
// boundaries, and the seam between a scope's conversational surface and its
// members is exactly one.
func (v *View) hairline(width int) string {
	if v.ruleW != width {
		v.rule = strings.Repeat(tokens.GlyphTreeDash, width)
		v.ruleW = width
	}
	l := &v.line
	l.reset(width)
	l.add(v.rule, tokens.TextTertiary)
	return v.emit(width, false, tokens.Ground)
}

// foldLine draws the overflow accounting (8.1.7). It is chrome: the rows it
// stands for are what the reader is after, not the line itself.
func (v *View) foldLine(text string, width int) string {
	l := &v.line
	l.reset(width)
	l.add(blocks.Truncate(text, width), tokens.TextTertiary)
	return v.emit(width, false, tokens.Ground)
}

// push appends a line while there is room in the height budget, and records
// which row that line belongs to.
//
// Recording it HERE rather than at the call sites is the whole trick: push is
// also what silently drops a line that does not fit, so a table built anywhere
// else would claim a row on a line the frame never got. One append, one mark,
// always the same length.
func (v *View) push(line string, limit int) {
	if len(v.lines) < limit {
		v.lines = append(v.lines, line)
		v.marks = append(v.marks, v.mark)
	}
}

// identity resolves a seed to its pastel, avoiding a collision with the row
// above (5.16: no two ADJACENT rail cards share a hue). A seedless row inherits
// nothing — it gets the plain grey path, which is what [tokens.BandFor] turns
// into an untinted band.
func (v *View) identity(seed string, prev tokens.Token) tokens.Token {
	if seed == "" {
		return tokens.Token(255)
	}
	return tokens.IdentityNext(seed, prev)
}

func seedOf(r Row, fallback string) string {
	if r.Seed != "" {
		return r.Seed
	}
	if r.ID != "" {
		return r.ID
	}
	return fallback
}

// addGlyph writes a row's state glyph and the space after it. It is the one
// place a glyph and its colour are chosen, so the two can never disagree.
//
// The SURFACE row is the exception the vocabulary needs: a conversational
// surface is not a job, and ✓ on an idle `aforge` would claim a success nobody
// achieved. Idle, it draws the room dot the 5.15 wireframes show, in the
// scope's identity pastel — "which room am I in", answered peripherally. Alive
// or blocked, it takes the ordinary vocabulary, because those states mean the
// same thing on every row.
func (v *View) addGlyph(l *lineBuf, r Row, ident tokens.Token) {
	glyph, tok := v.glyphOf(r, ident)
	l.add(glyph, tok)
	l.add(" ", tokens.TextTertiary)
}

func (v *View) glyphOf(r Row, ident tokens.Token) (string, tokens.Token) {
	att := r.Attention()
	if r.Kind == RowSurface {
		switch att {
		case AttnQuestion, AttnWorking, AttnFailed, AttnCancelled:
			// fall through to the ordinary vocabulary
		default:
			return roomDot, v.identityOr(ident, tokens.TextPrimary)
		}
	}
	return att.Glyph(), v.glyphToken(r, ident)
}

// roomDot is the ● the 5.15 wireframes draw beside a room name. It is
// [tokens.GlyphStepDone]'s character — one measured, tintable, single-width
// glyph, reused rather than invented, since a token layer that owned two names
// for one cell would be a token layer with a synonym.
const roomDot = tokens.GlyphStepDone

// glyphToken resolves the glyph's colour, and is the only place identity enters
// a row's foreground (5.16).
func (v *View) glyphToken(r Row, ident tokens.Token) tokens.Token {
	switch h := r.GlyphHue(); h {
	case tokens.HueIdentity:
		return v.identityOr(ident, tokens.TextPrimary)
	case tokens.HueNone:
		// Queued and paused: not moving and not done. Chrome tier, because a
		// bright ○ on twenty pending rows is twenty claims on the eye that
		// nothing has earned.
		return tokens.TextTertiary
	default:
		return tokens.ResolveToken(h, r.State())
	}
}

// identityOr is the identity token when there is one and a fallback when the
// row has no seed — a scope with no identity (home) paints no pastel rather
// than borrowing wheel entry zero.
//
// The PROFILE can withhold it too, and that is the second gate: below 256
// colours the eight pastels collapse onto six chromatic slots, so
// [tokens.Profile.IdentityDistinct] is false and the 5.16 promise — no two
// adjacent cards share a hue — cannot be kept. A lying identity is worse than
// none (5.20): two different rooms painted the same colour is the affordance
// lying in the one cell built to answer "which room am I in". The fallback is
// what the row would have worn had it never had an identity, which is the
// honest thing to say when the terminal cannot carry one. It is the same call
// [palette.list.markerToken] makes for the same cell.
func (v *View) identityOr(ident, fallback tokens.Token) tokens.Token {
	if ident < tokens.Identity0 || ident > tokens.Identity7 || !v.profile.IdentityDistinct() {
		return fallback
	}
	return ident
}

// nameToken carries the state axis (8.1.6): accent while live, plain once
// settled. It never carries a hue — 5.16 is explicit that accent hues do not
// colourise text.
func (v *View) nameToken(r Row) tokens.Token {
	return tokens.ResolveToken(tokens.HueNone, r.State())
}
