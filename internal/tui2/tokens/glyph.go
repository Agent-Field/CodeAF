package tokens

// The glyph vocabulary (5.17, 5.21), and the whole of what a person sees drawn
// as a mark anywhere in this product. Its law, its three tiers, the table as it
// landed and how to add to it are docs/design/icons/DESIGN.md; the short of it
// is that every mark is a SLOT with a plain, a nerd-font and an ASCII spelling,
// that GlyphSet.Glyph(id) is the one door to them, and that a surface spelling a
// mark as a literal is a build failure rather than a matter of taste.
//
// No emoji in chrome: emoji are
// double-width, render inconsistently, carry their own untintable colors, and
// read as notification confetti rather than as an instrument. Everything here
// is single-width, tintable and metric-safe — and glyph_test.go proves the
// width claim against the same library the renderer measures with, so a
// tempting new glyph cannot enter the vocabulary without passing the ruler.
//
// AMENDMENT TO 5.17 (measured, not argued): the section lists ⚡ for "boosted",
// but ⚡ (U+26A1) has East_Asian_Width=Wide and measures TWO cells under both
// x/ansi and go-runewidth — it fails the very law the section states. Boost is
// an escalation, so it ships as ⇡ (U+21E1, one cell everywhere).
const (
	// State vocabulary. Shape encodes state CATEGORY and may change at a true
	// state transition; shape never animates on a long-lived row (8.1.6).
	GlyphQueued  = "○"
	GlyphWorking = "◐"
	GlyphSettled = "✓"
	GlyphFailed  = "✕"
	// GlyphStopped is work A PERSON ENDED, and it is deliberately neither
	// [GlyphSettled] nor [GlyphFailed]: a tick is a finding that the work came
	// off and a cross is a finding that it did not, and nobody found anything
	// about work somebody stopped. The filled square is the mark every device a
	// person owns stops with, it is one cell under both shipping rulers, and it
	// is the shape the nerd-font tier has an exact icon for (nf-fa-stop).
	GlyphStopped = "■"
	// GlyphPaused is 5.17's replacement for the banned ⏸: a paused row is
	// "=" (or a dim GlyphQueued, at the renderer's choice).
	GlyphPaused = "="

	// Attention and blocking.
	GlyphNeedsHuman = "?" // always amber (5.16)
	GlyphWaitsOn    = "⚑" // waiting on a sibling (waits-on edge)
	// GlyphWithdrawn is a question that STOPPED BEING A QUESTION — its subject
	// went away, the plan changed, or another answer made it moot, so the asker
	// took it back (docs/design/questions/DESIGN.md's WITHDRAWN, WITH A REASON).
	//
	// It is deliberately none of the three marks it sits nearest. A tick says
	// somebody decided; a cross says the answer was no; a filled square says a
	// person ended it. Nobody decided anything here and nobody ended anything —
	// the decision simply stopped needing to be made — and the circled slash is
	// the one shape in the geometric register that says "this does not apply"
	// without claiming an outcome.
	GlyphWithdrawn = "⊘"
	// GlyphAssumed is the ladder's SECOND rung drawn: the asker has taken
	// something for granted, said so, and gone on — and everything on the card
	// stands until somebody strikes it (docs/design/questions/DESIGN.md names
	// this mark for the assumption kind).
	//
	// IT IS NOT [GlyphNeedsHuman], and that is the whole reason the slot exists.
	// `?` means "waiting on a person" and is the one mark on this surface that
	// is always amber; an assumptions card is waiting on nobody — it is going
	// ahead, and the offer to strike a line is a courtesy rather than a gate.
	// Drawing it with the attention mark told a person to answer something that
	// was not asking them anything, which is the fastest way to make the amber
	// mark stop meaning what it says.
	//
	// It is also not [GlyphEstimate]. A tilde is bound to a NUMBER — 10.2.8's
	// "estimated number" — and this stands alone at the head of a card; the two
	// are near neighbours in shape and say different things, which is exactly
	// the distinction the one-glyph-one-meaning gate exists to keep.
	GlyphAssumed = "≈"

	// Disclosure and navigation.
	GlyphCollapsed = "▸"
	GlyphExpanded  = "▾"
	GlyphScopeUp   = "‹" // scope header / go up

	// GlyphPointer is THE PERSON'S POINTER ON A QUESTION: the answer `enter`
	// takes. It is the fold mark's own small triangle, and deliberately so —
	// both say "this row, and your key goes into it" — but it is a SLOT of its
	// own because a question's pointer is amber and moves under a hand, where a
	// fold mark is a dim fact about a section; one slot for both was the
	// question's page drawing `▸` for "folded" beside `▸` for "you are here"
	// (docs/design/questions/DESIGN.md, 2026-09-11).
	GlyphPointer = "▸"
	// GlyphRecommended is THE ASKER'S PICK: the answer the thing asking would
	// take, drawn at the right edge of that answer's row with the word
	// `recommended` beside it. It is amber, like the other two marks of a
	// question, and it is a filled shape where the pointer is a triangle so the
	// two can stand on one row and never be read as each other.
	GlyphRecommended = "◆"

	// The one frame (internal/tui3/frame.go): a rounded, dim edge around the
	// one object on the surface that is waiting for a person (a question), and
	// around the few sheets raised over the page on purpose. Box drawing is
	// already the right character for a grid, so these are geometry: the tier
	// never touches them, and a terminal refused box drawing gets the frame's
	// own ASCII run (two plain rules, no sides), which the frame draws itself.
	GlyphFrameTopLeft     = "╭"
	GlyphFrameTopRight    = "╮"
	GlyphFrameBottomLeft  = "╰"
	GlyphFrameBottomRight = "╯"
	GlyphFrameEdge        = "─"
	GlyphFrameSide        = "│"
	// The two junctions are where a frame's rule meets the seam between two
	// panes laid side by side (internal/tui3/panes.go): the rule above the panes
	// drops into the seam and the rule below closes it. They are the same
	// geometry as the rest of the frame, so they are drawn at the same floor.
	GlyphFrameTeeDown = "┬"
	GlyphFrameTeeUp   = "┴"

	// GlyphTarget is WHERE THE NEXT THING GOES, and it is the one mark in this
	// vocabulary about a destination rather than about a state. Home's rule wears
	// it in front of the folder and the model the next conversation will open on
	// (internal/tui3's homedraft.go), and the whole of its meaning is the
	// difference between "where I am" and "where this is going" — which is why it
	// is neither [GlyphScopeUp], a header pointing back up a tree, nor
	// [GlyphPromptSteer], a composer's own prompt.
	//
	// IT IS GEOMETRY ON PURPOSE. An arrow is already the right character for a
	// grid, exactly as the tree corners and the rails are, and a Font Awesome
	// arrow in its place would buy nothing and spend a private-use codepoint.
	GlyphTarget    = "→" // where the next thing goes
	GlyphTruncated = "⋯" // clickable overflow; [GlyphEllipsis] marks static overflow

	// GlyphEllipsis is §16's ONE ELLIPSIS GRAMMAR as a slot: the mark text
	// leaves behind when it was too long for its column. It is deliberately NOT
	// [GlyphTruncated] — an ellipsis says "the rest is off the edge", a ⋯ says
	// "there is more, click for it", and a cut ([GlyphCut]) says "this stopped
	// and should not have". Three marks, three sentences.
	//
	// It was the most-drawn mark in the product with no name here: prose,
	// placeline, modelui, palette, footer and composer each spelled the
	// byte themselves, and three of them wrote a comment explaining which of
	// the other two marks they did NOT mean. The slot ends the explaining.
	//
	// U+2026 is Ambiguous width and one cell under both shipping rulers. It has
	// no nerd-font twin on purpose: the tier's ellipsis icon is already spent on
	// [GlyphTruncated], and this mark lands inside sentences a user wrote, where
	// a rewrite would be an edit rather than a repertoire swap (12.7 D.2).
	GlyphEllipsis = "…"

	// GlyphCut is the truncation law's visible mark (12.5.2): a turn ended by
	// anything other than its own completion renders VISIBLY CUT. The severed
	// double-dash rule is deliberately NOT [GlyphTruncated] — an ellipsis says
	// "there is more, ask for it", and a cut says "this stopped and should not
	// have". Conflating the two is exactly the lie of omission 12.5 found.
	// Colour comes from [CutToken]; U+254C is Neutral width, one cell under
	// every ruler including a CJK locale.
	GlyphCut = "╌"

	// Composers. The two prompts differ so the affordance never lies about
	// which surface the draft will land in (5.15).
	GlyphPromptChat  = "›"
	GlyphPromptSteer = "↦"
	// GlyphReplyIn is AN ANSWER DRAWN UNDER THE THING IT ANSWERS: the reply to a
	// question a person put back to the asker, the response landing on the row
	// that asked for it (docs/design/questions/DESIGN.md's room form). It is a
	// prompt mark in the same family as the two above — punctuation saying whose
	// turn a line is — which is why it is geometry and neither tier swaps it.
	//
	// It is deliberately NOT [GlyphPromptChat]: `›` is the person typing and this
	// is what came back, and a page that drew both with one mark would make an
	// exchange unreadable at exactly the moment it matters. U+21B3 is
	// East_Asian_Width=Neutral and one cell under both shipping rulers.
	GlyphReplyIn = "↳"
	// GlyphDraftUnsent is a message a person typed and then CLEARED the whole
	// box into — the mark the /drafts page draws in front of each line that
	// is still waiting to come back (internal/tui3's draftpage.go).
	//
	// IT IS PENCIL-SHAPED LIKE [GlyphWrite] AND NOT THE SAME PENCIL. ✎ is the
	// step gutter's "a call wrote something down"; ✐ (U+2710 LOWER RIGHT PENCIL)
	// is a thing the hand still holds. It is also deliberately NOT the steer
	// prompt's mapped-into arrow one slot up: a draft has gone nowhere yet, it
	// sits on the wrong side of the line that one draws.
	GlyphDraftUnsent = "✐"
	// GlyphPinned is a crew seat a PERSON PINNED, beside the seats the router
	// picks per task (internal/tui3's crew.go). It marks the one seat on a crew
	// line nothing will move for the next task — the same fact the headless
	// line marks with a pin. U+2316 POSITION INDICATOR is a fixed point, which is
	// what a pin is, and it is East_Asian_Width=Neutral and one cell under both
	// shipping rulers.
	GlyphPinned = "⌖"

	// The execution voices (5.5). A work record is four speakers and no
	// labels: the model thinking, the tools it reached for, the reader
	// steering, and what came back. The reader's voice is [GlyphPromptChat]
	// — the same mark they typed at — and what came back wears the quote
	// gutter, so the slots this adds are the three the vocabulary was short:
	// the model's own thought and the two tool kinds that are not a shell.
	//
	// All three measure one cell under both shipping rulers and all three are
	// East_Asian_Width=Neutral, so they are the rare glyphs that do not even
	// cost a reserved column under a CJK locale.
	GlyphThought = "✳" // the model's own words between calls
	GlyphShell   = "$" // a shell call — the prompt a person types at
	GlyphSearch  = "⌕" // a call that went out to the world
	GlyphFilter  = "⌕" // narrowing what is already on the page
	GlyphWrite   = "✎" // a call that wrote something down

	// The action families (internal/tui3's step gutter). One still, monochrome
	// mark per FAMILY of work — searching, editing, running a command — keyed
	// off the closed vocabulary the engine carries in session.ActionCategory.
	//
	// THEY ARE SLOTS HERE AND NOT CHARACTERS THERE. The surface used to hold
	// its own three-tier table, with the private-use codepoints spelled inline
	// beside the plain marks, so ten icons and ten plain glyphs lived outside
	// the width gate, outside the ban list and outside the one-meaning law —
	// which is how ▤, ◎ and ◷ came to be drawn product-wide without ever having
	// been measured. Four of the families reuse a slot this table already owns
	// (search, write, shell, and the diff-add byte for a thing that was not
	// there); the rest are named here, and the surface keeps only the map from
	// a family to a slot.
	GlyphActionRead        = "▤" // a box with lines in it — a page of text, opened
	GlyphActionCreate      = "+" // something that was not there is
	GlyphActionTest        = "◎" // a target being aimed at — NEVER a checkmark
	GlyphActionBrowse      = "↗" // out of here and onto a page somewhere else
	GlyphActionTransfer    = "⇄" // bytes going the other way as well
	GlyphActionCommunicate = "»" // the guillemet: something being SAID, to a person
	GlyphActionCoordinate  = "⇉" // work handed out, or this mind copied to run beside itself
	GlyphActionPlan        = "≡" // three level lines, an outline
	GlyphActionWait        = "◷" // a quarter of a clock face, still
	GlyphActionWork        = "▪" // a small square: "a step", which is all it knows

	// Meta.
	GlyphBoosted   = "⇡" // transient escalation of the work-role binding (8.2.16)
	GlyphSeparator = "·" // telemetry separator
	GlyphMissing   = "—" // missing data — never an estimate (10.2.8)
	GlyphEstimate  = "~" // estimated number (10.2.8)

	// Structure (5.21). The accent rail groups a card's lines in its identity
	// hue; the drag handle marks a reorderable pending row (5.22).
	GlyphAccentRail = "▎"
	GlyphDragHandle = "⋮"

	// GlyphHugEdge is the composer hug's left edge: the one cell at column 0 of
	// the row you type into, carrying the composer's state colour.
	//
	// It is U+258D LEFT THREE EIGHTHS BLOCK and NOT [GlyphAccentRail], and the
	// distinction is the vocabulary's whole point rather than a shade of taste.
	// §4 spends `▎` on one meaning product-wide — "a finished answer" — and says
	// nothing else may wear it. The hug's edge means something else entirely:
	// "this is the live surface, and here is what it is doing right now". Two
	// meanings may not share a mark, so the hug takes the next rung of the same
	// eighth-block ladder the code gutter (`▏`) and the accent rail already
	// stand on: same family, so the three read as one system, different width,
	// so they are told apart at a glance and by the table.
	GlyphHugEdge = "▍"

	// GlyphChipCapLeft and GlyphChipCapRight are the two cells that soften a
	// filled chip's ends: U+2590 RIGHT HALF BLOCK opens it and U+258C LEFT HALF
	// BLOCK closes it, each painted with the CHIP's ground as its FOREGROUND
	// over the surface's own ground. Half a cell of chip and half a cell of
	// floor, which is as close to a rounded corner as a terminal gets.
	//
	// They are geometry rather than iconography — the shapes ARE the meaning,
	// there is nothing for a patched font to improve, and both measure one cell
	// under both shipping rulers like every other eighth/half block already in
	// this table. They are named here rather than spelled inline for §16's flat
	// reason: a mark drawn from a literal escapes the width gate, and the day a
	// second surface wants a filled chip it must get the same two cells.
	GlyphChipCapLeft  = "▐"
	GlyphChipCapRight = "▌"

	// Step dots: plan progress as one dot per step (5.21). Display only on
	// narrow rails — too small to hit honestly (5.22).
	GlyphStepDone    = "●"
	GlyphStepRunning = "◐"
	GlyphStepPending = "○"
	GlyphStepBlocked = "⚑"

	// Run progress cells summarize task state across a whole run.
	GlyphDoneCell    = "●"
	GlyphRunningCell = "◐"
	GlyphEmptyCell   = "○"
	GlyphFailedCell  = "✘"

	// Queue pills: one glyph per queued item, capped (10.3.13).
	GlyphQueuePill = "▶"

	// Diff micro-stats on settle rows (5.21), green and coral, tabular. The
	// minus is U+2212, which is one cell in every width mode and lines up with
	// the plus; ASCII '-' does not.
	GlyphDiffAdd = "+"
	GlyphDiffDel = "−"

	// The compact inline spawn tree written into the committed transcript at
	// birth and settle (10.3.11).
	GlyphTreeBranch = "├"
	GlyphTreeLast   = "└"
	GlyphTreeVert   = "│"
	GlyphTreeDash   = "─"

	// The place line (5.19): where work lands on disk. These three were drawn
	// in 5.19's own example before they had names here, and they are plain-tier
	// glyphs in their own right — the glyph TIER (12.7) upgrades them, it did
	// not invent them.
	//
	// GlyphHome is U+2302 HOUSE, Neutral width and universally covered, which
	// is why 5.19 reached for it. GlyphFolder is the ASCII slash, because a
	// slash already means "directory" in every shell anyone has ever used and
	// no font can fail to draw it. GlyphGitBranch is U+22D4 PITCHFORK, Neutral
	// and one cell; where a font cannot draw it the documented substitute is
	// ":" — the "git:main" convention — which is ASCII and the same width.
	GlyphHome      = "⌂"
	GlyphFolder    = "/"
	GlyphGitBranch = "⋔"

	// The status line (5.17's "K3 ▄ $8.65"). The model mark is U+25C7 WHITE
	// DIAMOND, one cell under both rulers and Ambiguous exactly as the gauge
	// beside it already is. The spend mark is the dollar the money cell was
	// already carrying, named so the tier can swap it as a slot rather than as
	// a substring.
	//
	// The `$` is spelled twice on purpose and it is the one collision in the
	// vocabulary worth stating out loud: [GlyphShell] is the same byte saying a
	// different thing (a shell call, 5.5). They are told apart by what follows —
	// the spend mark is bound to a number and the shell mark is followed by a
	// space — never by shape, and the nerd-font tier separates them outright
	// (nf-fa-dollar against nf-fa-terminal). glyphvocab_test.go carries the pair
	// in its named-exceptions table so a THIRD `$` slot has to be argued for.
	GlyphModel = "◇"
	GlyphSpend = "$"

	// The file kinds. A chip says WHAT KIND OF THING is on the end of a path
	// before it says the path, and so does the gutter beside a call that made
	// one or opened one; the four kinds a person can hand this program are the
	// four here.
	//
	// GlyphFileDocument is [GlyphActionRead]'s byte on purpose — a page of text
	// is a page of text whether a call opened it or a person dragged it in, and
	// the tier draws the SAME icon for both rather than inventing a distinction
	// the floor does not draw. It is the one collision in this block, and it is
	// carried in glyphvocab_test.go's named-exceptions table.
	//
	// GlyphFileVideo is U+25B7 WHITE RIGHT-POINTING TRIANGLE and NOT the filled
	// U+25B6 a chip used to draw: the filled triangle is [GlyphQueuePill]'s, one
	// plain byte may upgrade exactly one way, and an outline triangle is the
	// right weight beside three outlined file icons anyway.
	//
	// GlyphFileImage is U+233E APL FUNCTIONAL SYMBOL CIRCLE JOT and
	// GlyphFileAudio is U+266A EIGHTH NOTE, both one cell under both rulers.
	GlyphFileDocument = "▤"
	GlyphFileImage    = "⌾"
	GlyphFileAudio    = "♪"
	GlyphFileVideo    = "▷"
)

// GaugeCells is the one-cell context gauge (5.17): context % as a single
// eighth-block, so "K3 ▄ $8.65" reads as "half the window gone" at a glance and
// costs one column. The precise percentage belongs to the focused-card tier.
var GaugeCells = [5]string{"▁", "▂", "▄", "▆", "█"}

// SpinnerFrames is the shared-clock spinner for TRANSIENT tool rows only
// (8.1.6): rows that live for seconds, where motion is honest. Never on a rail
// card or an agent row.
//
// AMENDMENT TO 5.21: the section proposes ◐◓◑◒ as the rotating glyph. Those
// four disagree about East-Asian width — ◐ and ◑ are Ambiguous (two cells under
// a CJK-locale terminal), ◓ and ◒ are not — so the set would make a spinning
// row change width mid-spin, which is exactly the width instability 5.17 bans.
// Braille is width-homogeneous under every mode and is already the house
// spinner.
var SpinnerFrames = [10]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SparklineCells is the braille burn-trend ramp for a focused card's cost or
// token history (5.21) — six to eight cells, one row.
var SparklineCells = [7]string{"⣀", "⣄", "⣤", "⣦", "⣶", "⣷", "⣿"}

// Gauge maps a fraction in [0,1] to one cell of [GaugeCells] by walking
// [GaugeThresholds] — the ladder is a table so it can be read and quoted, not
// an arithmetic expression only the compiler sees. Out-of-range values clamp: a
// gauge that ran past its window still reads full rather than panicking a
// render, and a reading that does not exist (NaN) reads empty rather than
// guessing (16's EMPTINESS).
//
// The cell says HOW FULL. It never says whether that is a problem; colour is a
// separate judgement and deliberately not a sixth cell.
func Gauge(fraction float64) string {
	switch {
	case !(fraction > 0): // also catches NaN
		return GaugeCells[0]
	case fraction >= 1:
		return GaugeCells[len(GaugeCells)-1]
	}
	for i := len(GaugeThresholds) - 1; i > 0; i-- {
		if fraction >= GaugeThresholds[i] {
			return GaugeCells[i]
		}
	}
	return GaugeCells[0]
}

// Sparkline maps a fraction in [0,1] to one cell of [SparklineCells].
func Sparkline(fraction float64) string {
	switch {
	case !(fraction > 0):
		return SparklineCells[0]
	case fraction >= 1:
		return SparklineCells[len(SparklineCells)-1]
	}
	i := int(fraction * float64(len(SparklineCells)))
	if i >= len(SparklineCells) {
		i = len(SparklineCells) - 1
	}
	return SparklineCells[i]
}

// Spinner picks the frame for a moment on the ONE shared animation clock
// (8.1.3): frame = floor(now/interval) % frames, so every live glyph on the
// screen ticks in lockstep as one organism instead of N competing pulses.
// Callers pass the already-divided tick, not a time — the clock lives above
// this package, and a token layer that read the wall clock would be a token
// layer with a state.
func Spinner(tick int) string {
	return SpinnerFrames[((tick%len(SpinnerFrames))+len(SpinnerFrames))%len(SpinnerFrames)]
}

// GlyphInfo is one row of the vocabulary with its measured properties.
type GlyphInfo struct {
	// Name is the constant's name without the "Glyph" prefix.
	Name string
	// Glyph is the character itself.
	Glyph string
	// Rune is the single rune it consists of.
	Rune rune
	// AmbiguousWidth records that this rune's East_Asian_Width is Ambiguous:
	// one cell in every terminal we render for (x/ansi and go-runewidth both
	// measure it as one), but two cells in a terminal running a CJK locale
	// with ambiguous-wide enabled. The existing chat already fights this
	// (clampNodeLines sacrifices a column when it detects one). The flag is
	// exported so a shell can reserve that column deliberately instead of
	// discovering the ghost at runtime; glyph_test.go verifies every flag
	// against the width library, so this data cannot go stale.
	AmbiguousWidth bool
}

// Glyphs returns the whole vocabulary, in the order it is declared above.
// Tests, the `?` help surface, and a glyph-preview screen all walk it.
func Glyphs() []GlyphInfo {
	return []GlyphInfo{
		{"Queued", GlyphQueued, '○', true},
		{"Working", GlyphWorking, '◐', true},
		{"Settled", GlyphSettled, '✓', false},
		{"Failed", GlyphFailed, '✕', false},
		{"Stopped", GlyphStopped, '■', true},
		{"Paused", GlyphPaused, '=', false},
		{"NeedsHuman", GlyphNeedsHuman, '?', false},
		{"WaitsOn", GlyphWaitsOn, '⚑', false},
		{"Withdrawn", GlyphWithdrawn, '⊘', false},
		{"Assumed", GlyphAssumed, '≈', true},
		{"Collapsed", GlyphCollapsed, '▸', false},
		{"Expanded", GlyphExpanded, '▾', false},
		{"ScopeUp", GlyphScopeUp, '‹', false},
		{"Pointer", GlyphPointer, '▸', false},
		{"Recommended", GlyphRecommended, '◆', true},
		{"FrameTopLeft", GlyphFrameTopLeft, '╭', true},
		{"FrameTopRight", GlyphFrameTopRight, '╮', true},
		{"FrameBottomLeft", GlyphFrameBottomLeft, '╰', true},
		{"FrameBottomRight", GlyphFrameBottomRight, '╯', true},
		{"FrameEdge", GlyphFrameEdge, '─', true},
		{"FrameSide", GlyphFrameSide, '│', true},
		{"FrameTeeDown", GlyphFrameTeeDown, '┬', true},
		{"FrameTeeUp", GlyphFrameTeeUp, '┴', true},
		{"Target", GlyphTarget, '→', true},
		{"Truncated", GlyphTruncated, '⋯', false},
		{"Ellipsis", GlyphEllipsis, '…', true},
		{"Cut", GlyphCut, '╌', false},
		{"PromptChat", GlyphPromptChat, '›', false},
		{"PromptSteer", GlyphPromptSteer, '↦', false},
		{"ReplyIn", GlyphReplyIn, '↳', false},
		{"DraftUnsent", GlyphDraftUnsent, '✐', false},
		{"Pinned", GlyphPinned, '⌖', false},
		{"Thought", GlyphThought, '✳', false},
		{"Shell", GlyphShell, '$', false},
		{"Search", GlyphSearch, '⌕', false},
		{"Filter", GlyphFilter, '⌕', false},
		{"Write", GlyphWrite, '✎', false},
		{"ActionRead", GlyphActionRead, '▤', true},
		{"ActionCreate", GlyphActionCreate, '+', false},
		{"ActionTest", GlyphActionTest, '◎', true},
		{"ActionBrowse", GlyphActionBrowse, '↗', true},
		{"ActionTransfer", GlyphActionTransfer, '⇄', false},
		{"ActionCommunicate", GlyphActionCommunicate, '»', false},
		{"ActionCoordinate", GlyphActionCoordinate, '⇉', false},
		{"ActionPlan", GlyphActionPlan, '≡', true},
		{"ActionWait", GlyphActionWait, '◷', false},
		{"ActionWork", GlyphActionWork, '▪', false},
		{"Boosted", GlyphBoosted, '⇡', false},
		{"Separator", GlyphSeparator, '·', true},
		{"Missing", GlyphMissing, '—', true},
		{"Estimate", GlyphEstimate, '~', false},
		{"AccentRail", GlyphAccentRail, '▎', true},
		{"HugEdge", GlyphHugEdge, '▍', true},
		{"ChipCapLeft", GlyphChipCapLeft, '▐', false},
		{"ChipCapRight", GlyphChipCapRight, '▌', true},
		{"DragHandle", GlyphDragHandle, '⋮', false},
		{"StepDone", GlyphStepDone, '●', true},
		{"StepRunning", GlyphStepRunning, '◐', true},
		{"StepPending", GlyphStepPending, '○', true},
		{"StepBlocked", GlyphStepBlocked, '⚑', false},
		{"DoneCell", GlyphDoneCell, '●', true},
		{"RunningCell", GlyphRunningCell, '◐', true},
		{"EmptyCell", GlyphEmptyCell, '○', true},
		{"FailedCell", GlyphFailedCell, '✘', false},
		{"QueuePill", GlyphQueuePill, '▶', true},
		{"DiffAdd", GlyphDiffAdd, '+', false},
		{"DiffDel", GlyphDiffDel, '−', false},
		{"TreeBranch", GlyphTreeBranch, '├', true},
		{"TreeLast", GlyphTreeLast, '└', true},
		{"TreeVert", GlyphTreeVert, '│', true},
		{"TreeDash", GlyphTreeDash, '─', true},
		{"Home", GlyphHome, '⌂', false},
		{"Folder", GlyphFolder, '/', false},
		{"GitBranch", GlyphGitBranch, '⋔', false},
		{"Model", GlyphModel, '◇', true},
		{"Spend", GlyphSpend, '$', false},
		{"FileDocument", GlyphFileDocument, '▤', true},
		{"FileImage", GlyphFileImage, '⌾', false},
		{"FileAudio", GlyphFileAudio, '♪', true},
		{"FileVideo", GlyphFileVideo, '▷', true},
		// The prose slots (code.go). They are named there because a slot is a
		// MEANING and not a byte, and they are walked HERE because the width
		// gate is the one place a glyph may not hide: GlyphCodeGutter arrived
		// with no byte twin elsewhere in this list and escaped the sweep
		// entirely until it was added. It has one now — the blockquote's gutter
		// moved onto it, off the box rule it shared with the spawn tree.
		{"ProseBullet", GlyphProseBullet, '·', true},
		{"ProseQuote", GlyphProseQuote, '▏', true},
		{"CodeGutter", GlyphCodeGutter, '▏', true},
		{"Gauge0", GaugeCells[0], '▁', true},
		{"Gauge1", GaugeCells[1], '▂', true},
		{"Gauge2", GaugeCells[2], '▄', true},
		{"Gauge3", GaugeCells[3], '▆', true},
		{"Gauge4", GaugeCells[4], '█', true},
		{"Spinner0", SpinnerFrames[0], '⠋', false},
		{"Spinner1", SpinnerFrames[1], '⠙', false},
		{"Spinner2", SpinnerFrames[2], '⠹', false},
		{"Spinner3", SpinnerFrames[3], '⠸', false},
		{"Spinner4", SpinnerFrames[4], '⠼', false},
		{"Spinner5", SpinnerFrames[5], '⠴', false},
		{"Spinner6", SpinnerFrames[6], '⠦', false},
		{"Spinner7", SpinnerFrames[7], '⠧', false},
		{"Spinner8", SpinnerFrames[8], '⠇', false},
		{"Spinner9", SpinnerFrames[9], '⠏', false},
		{"Spark0", SparklineCells[0], '⣀', false},
		{"Spark1", SparklineCells[1], '⣄', false},
		{"Spark2", SparklineCells[2], '⣤', false},
		{"Spark3", SparklineCells[3], '⣦', false},
		{"Spark4", SparklineCells[4], '⣶', false},
		{"Spark5", SparklineCells[5], '⣷', false},
		{"Spark6", SparklineCells[6], '⣿', false},
		// The breathe's three sizes (motion.go). Two of them are bytes this
		// table already owns under other names — `·` is Separator and
		// ProseBullet, `●` is StepDone — and they are named again here for the
		// reason the prose slots are: a slot is a MEANING, and the width gate
		// may not have a hole where an animated cell is. The frame list is
		// [PulseFrames]; the fourth frame repeats Pulse1 on the way back down,
		// so only the three distinct sizes are walked.
		{"Pulse0", PulseFrames[0], '·', true},
		{"Pulse1", PulseFrames[1], '•', true},
		{"Pulse2", PulseFrames[2], '●', true},
	}
}

// BannedGlyphs are runes that may never appear in chrome, with the reason each
// one is out. glyph_test.go fails if any of them turns up anywhere in this
// package's vocabulary — the ban is enforced by the build, not by review.
//
// The general rules the list instantiates (also enforced by the test, so a
// rune not named here cannot sneak in either): nothing wider than one cell,
// nothing in the emoji planes, nothing carrying a variation selector.
var BannedGlyphs = []struct {
	Rune   rune
	Reason string
}{
	{'⏸', "media-control pictograph: width-unstable, emoji-presentation in many fonts (5.17)"},
	{'⏵', "media-control pictograph: width-unstable (5.17)"},
	{'⏹', "media-control pictograph: width-unstable (5.17)"},
	{'⏯', "media-control pictograph: width-unstable (5.17)"},
	{'⏭', "media-control pictograph: width-unstable (5.17)"},
	{'⏮', "media-control pictograph: width-unstable (5.17)"},
	{'⌛', "hourglass: two cells, and it lies about liveness on a detached row (8.1.6)"},
	{'⏳', "hourglass: two cells (5.17)"},
	{'⚡', "measured two cells under x/ansi and go-runewidth; 5.17's own width law refuses it"},
	{'☰', "measured two cells; also a hamburger menu, which this surface does not have"},
	{'★', "dingbat: Ambiguous width and no meaning in the five-word vocabulary (5.16)"},
	{'❯', "powerline-adjacent prompt chevron: font-fragile (8.3); the prompt is ›"},
	{'\uFE0F', "variation selector: forces emoji presentation and desynchronizes width"},

	// The powerline separator block (12.7 G). These measure one cell like every
	// other private-use codepoint, so the width law alone would let them in \u2014
	// they are banned on a different ground, and it is worth stating precisely,
	// because the glyph TIER admits their neighbour U+E0A0.
	//
	// A separator triangle is not an icon. It is a shape-join that must tile
	// pixel-exactly against a NEIGHBOURING BACKGROUND to look like anything, so
	// it drags a background-colour grammar in behind it, it breaks the line
	// grid wherever the tiling is off, and it is the single most common source
	// of "my prompt looks wrong" (8.3, 10.1.2, 5.19). \u00B7 remains the separator,
	// in both tiers. The branch symbol U+E0A0 is adopted precisely because it
	// is an icon that stands alone and joins to nothing.
	{'\uE0B0', "powerline separator: a shape-join that needs a neighbouring background to tile against (8.3); the separator is ·"},
	{'\uE0B1', "powerline thin separator: same join, same refusal"},
	{'\uE0B2', "powerline separator, left-facing: same join, same refusal"},
	{'\uE0B3', "powerline thin separator, left-facing: same join, same refusal"},
	{'\uE0B8', "powerline slant seam: same join, same refusal"},
	{'\uE0B9', "powerline slant seam: same join, same refusal"},
	{'\uE0BA', "powerline slant seam: same join, same refusal"},
	{'\uE0BB', "powerline slant seam: same join, same refusal"},
	{'\uE0BC', "powerline slant seam: same join, same refusal"},
	{'\uE0BD', "powerline slant seam: same join, same refusal"},
	{'\uE0BE', "powerline slant seam: same join, same refusal"},
	{'\uE0BF', "powerline slant seam: same join, same refusal"},
}
