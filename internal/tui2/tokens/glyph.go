package tokens

// The glyph vocabulary (5.17, 5.21). No emoji in chrome: emoji are
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
	// GlyphPaused is 5.17's replacement for the banned ⏸: a paused row is
	// "=" (or a dim GlyphQueued, at the renderer's choice).
	GlyphPaused = "="

	// Attention and blocking.
	GlyphNeedsHuman = "?" // always amber (5.16)
	GlyphWaitsOn    = "⚑" // waiting on a sibling (waits-on edge)

	// Disclosure and navigation.
	GlyphCollapsed = "▸"
	GlyphExpanded  = "▾"
	GlyphScopeUp   = "‹" // scope header / go up
	GlyphTruncated = "⋯" // clickable overflow; plain "…" marks static overflow

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

	// Meta.
	GlyphBoosted   = "⇡" // transient escalation of the work-role binding (8.2.16)
	GlyphSeparator = "·" // telemetry separator
	GlyphMissing   = "—" // missing data — never an estimate (10.2.8)
	GlyphEstimate  = "~" // estimated number (10.2.8)

	// Structure (5.21). The accent rail groups a card's lines in its identity
	// hue; the drag handle marks a reorderable pending row (5.22).
	GlyphAccentRail = "▎"
	GlyphDragHandle = "⋮"

	// Step dots: plan progress as one dot per step (5.21). Display only on
	// narrow rails — too small to hit honestly (5.22).
	GlyphStepDone    = "●"
	GlyphStepRunning = "◐"
	GlyphStepPending = "○"
	GlyphStepBlocked = "⚑"

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
	GlyphModel = "◇"
	GlyphSpend = "$"
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

// Gauge maps a fraction in [0,1] to one cell of [GaugeCells]. Out-of-range
// values clamp: a gauge that ran past its window still reads full rather than
// panicking a render.
func Gauge(fraction float64) string {
	switch {
	case !(fraction > 0): // also catches NaN
		return GaugeCells[0]
	case fraction >= 1:
		return GaugeCells[len(GaugeCells)-1]
	}
	i := int(fraction * float64(len(GaugeCells)))
	if i >= len(GaugeCells) {
		i = len(GaugeCells) - 1
	}
	return GaugeCells[i]
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
		{"Paused", GlyphPaused, '=', false},
		{"NeedsHuman", GlyphNeedsHuman, '?', false},
		{"WaitsOn", GlyphWaitsOn, '⚑', false},
		{"Collapsed", GlyphCollapsed, '▸', false},
		{"Expanded", GlyphExpanded, '▾', false},
		{"ScopeUp", GlyphScopeUp, '‹', false},
		{"Truncated", GlyphTruncated, '⋯', false},
		{"Cut", GlyphCut, '╌', false},
		{"PromptChat", GlyphPromptChat, '›', false},
		{"PromptSteer", GlyphPromptSteer, '↦', false},
		{"Boosted", GlyphBoosted, '⇡', false},
		{"Separator", GlyphSeparator, '·', true},
		{"Missing", GlyphMissing, '—', true},
		{"Estimate", GlyphEstimate, '~', false},
		{"AccentRail", GlyphAccentRail, '▎', true},
		{"DragHandle", GlyphDragHandle, '⋮', false},
		{"StepDone", GlyphStepDone, '●', true},
		{"StepRunning", GlyphStepRunning, '◐', true},
		{"StepPending", GlyphStepPending, '○', true},
		{"StepBlocked", GlyphStepBlocked, '⚑', false},
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
