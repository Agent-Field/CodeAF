package tokens

// Responsive breakpoints, written as numbers before building (10.5.24) rather
// than discovered later as magic constants inside a render.
//
// Every number below carries the arithmetic that produced it. Where a number is
// a revision of what the current chat does, the revision is deliberate and says
// so — 10.5.24 asks for a table, not for a restatement.

// The rail budget, and RailAtWidth derived from it.
//
// The rail is a scope map, not a menu (5.15): a row is a glyph, a space, a task
// word, and a right-aligned meta cell, with a second line for the live summary.
// 28 columns fits `◐ wisp-parity` on the first line and `K3 ▄ $8.65 · 21m` on
// the second without truncating a task word to initials.
//
// The transcript floor is 60: below that, prose wraps into a column narrower
// than any book, code blocks start folding, and the place line (5.19) has to
// middle-ellipsize every path.
//
// REVISION OF RECORD: the current chat sets railAtWidth = 100 (tui/model.go),
// which Part 9.12 flags as making the narrow path the primary experience at an
// ordinary 80-column terminal. 100 is not derived from anything — 60 + 2 + 28 =
// 90 is. Dropping the threshold to 90 hands the rail to every 90-to-99 column
// terminal (a very common shape: a half-screen split on a 15" laptop) without
// changing what happens at 80, where the scope map still renders as a full-pane
// list with identical keys and identical selection semantics.
const (
	// RailTranscriptFloor is the narrowest transcript the rail may leave behind.
	RailTranscriptFloor = 60
	// RailWidth is the rail's own width in columns.
	RailWidth = 28
	// RailGutter is the whitespace seam between transcript and rail — one
	// column of space and one of breathing room. 5.13: cards are separated by
	// whitespace, not boxes, and that applies to panes too.
	RailGutter = 2
	// RailAtWidth is the width at or above which the rail is drawn.
	// breakpoints_test.go asserts it equals the three numbers above summed, so
	// the derivation cannot rot into a magic number.
	RailAtWidth = RailTranscriptFloor + RailGutter + RailWidth // 90
)

// SplitDiffAtWidth is the width at or above which a diff renders side by side
// instead of unified.
//
// Arithmetic: a code column is legible at 48 characters and each side needs a
// 4-column gutter for line number and change marker, so a side is 52; two sides
// plus a 1-column separator is 105. A diff shown inside a task room also pays
// for the rail, so the practical threshold is 105 + RailGutter + RailWidth =
// 135; but a diff opened as a full-width dialog does not, and 105 is the number
// that governs the diff itself. Below it, unified — which is also the better
// reading for a narrow terminal, not merely the affordable one.
const SplitDiffAtWidth = 105

// Dialog thresholds (10.4.17: "forced fullscreen below a stated size
// threshold" — this is the stated threshold).
//
// A consent dialog carries: a one-line title, up to 8 preview rows (the diff or
// the command), a blank, and a 3-row mnemonic strip (a allow / s session /
// d deny), plus a border row top and bottom. That is 15 rows minimum, and a
// floating dialog needs at least 3 rows of transcript visible above it to still
// be a dialog rather than a takeover — so 18 rows is where floating stops being
// honest, rounded to 20 for the composer and status line.
//
// The width number is the widest thing a consent dialog must show without
// wrapping: a middle-ellipsized path (5.21) at 44 columns plus the 2-column
// mnemonic gutters plus 4 columns of dialog padding is 52; below 72 total
// (52 + 20 of surrounding context) the floating form has nothing to float over.
const (
	DialogFullscreenBelowWidth  = 72
	DialogFullscreenBelowHeight = 20
)

// Paste-to-attachment thresholds. A paste taller than the composer's own
// maximum height would push the transcript off screen while it is being
// composed, so at that point the paste becomes an attachment chip instead of
// draft text. Bytes are a second door for one enormous line (a base64 blob, a
// minified bundle) that is short in rows and ruinous in columns.
const (
	// ComposerMaxRows is how tall the composer may grow before it scrolls
	// internally. Six rows is a paragraph; more than that and the transcript
	// stops being the main surface.
	ComposerMaxRows = 6
	// PasteAttachRows is the paste height that becomes an attachment.
	PasteAttachRows = ComposerMaxRows + 2 // 8
	// PasteAttachBytes is the paste size that becomes an attachment regardless
	// of how few rows it occupies. 4 KiB is roughly two screens of prose.
	PasteAttachBytes = 4096
)

// HUDRowCap is the bounded sticky HUD's row budget below RailAtWidth (8.2.8):
// at most 8 rows of live work plus a fold line, above the composer. The cap is
// what makes it bounded — an unbounded HUD is the rail again, badly.
const HUDRowCap = 8

// Dual context thresholds (8.2.17): warn at min(percent, absolute) so a
// 1M-window model warns at 150k tokens rather than at 500k, where "half the
// window" is still an enormous amount of unspent room. The gauge (5.17) turns
// amber at this point.
const (
	ContextWarnFraction = 0.75
	ContextWarnTokens   = 150_000
)

// ContextWarnPoint returns the token count at which a window of this size
// should turn amber.
func ContextWarnPoint(window int64) int64 {
	byFraction := int64(float64(window) * ContextWarnFraction)
	if ContextWarnTokens < byFraction {
		return ContextWarnTokens
	}
	return byFraction
}

// ContextAlarm reports whether a context reading has crossed its warn point.
func ContextAlarm(used, window int64) bool {
	if window <= 0 {
		return false
	}
	return used >= ContextWarnPoint(window)
}

// ContextToken is the color the gauge and the percentage take: amber past the
// warn point (5.16: amber is "needs a human", and a window about to compact is
// exactly that), the chrome tier before it.
func ContextToken(used, window int64) Token {
	if ContextAlarm(used, window) {
		return Amber
	}
	return TextTertiary
}

// Footer columns with priority-based dropping (10.5.22). The contextual footer
// is a registry of columns that SHORTENS, never wraps: when the terminal is too
// narrow, the lowest-priority column leaves first.
//
// The health-versus-cost split (10.5.23) is why some obvious columns are absent:
// this-turn cost and context live on the composer's meta strip, not here. Two
// homes, never mixed.
type FooterColumn struct {
	// ID is the stable name a renderer keys on.
	ID string
	// MinWidth is the columns this entry needs, including the separator that
	// precedes it when it is not first.
	MinWidth int
	// Priority is the drop order: higher survives longer.
	Priority int
	// Why is the one-line reason this column has the priority it has.
	Why string
}

// FooterColumnOrder is the priority table. Order in the slice is DISPLAY order
// (left to right); Priority is DROP order, and the two are deliberately
// different — the attention badge is drawn first because the eye lands there,
// and it is dropped last because a blocked human is the most expensive state
// this product has.
var FooterColumnOrder = []FooterColumn{
	{"attention", 4, 100, "an amber ?2 means a human is blocked; nothing outranks that"},
	{"keymode", 12, 80, "'1-3 answer' vs '1-9 rooms' — 5.22 resolves the digit ambiguity on screen, never in the user's head"},
	{"verbs", 22, 70, "the 2-3 contextual verbs for the current focus; the discoverability surface itself"},
	{"health", 8, 60, "services and pending system states, shown only when pending (10.5.23)"},
	{"toast", 20, 50, "transient receipts from other rooms, which decay on their own anyway (5.21)"},
	{"scope", 16, 40, "the breadcrumb tail, already shown above the transcript — the first thing that can go"},
	{"help", 2, 90, "the permanent visible door to the ? surface; a capability-honesty surface cannot be a memory test (5.22)"},
}

// FitFooter drops the lowest-priority columns until the rest fit in width, and
// returns what survives IN DISPLAY ORDER. It never wraps and never truncates a
// column: a half-written verb is worse than an absent one.
//
// cols is not modified. Passing nil uses [FooterColumnOrder].
func FitFooter(cols []FooterColumn, width int) []FooterColumn {
	if cols == nil {
		cols = FooterColumnOrder
	}
	keep := make([]bool, len(cols))
	total := 0
	for i := range cols {
		keep[i] = true
		total += cols[i].MinWidth
	}
	for total > width {
		victim, worst := -1, 1<<31-1
		for i := range cols {
			if keep[i] && cols[i].Priority < worst {
				victim, worst = i, cols[i].Priority
			}
		}
		if victim < 0 {
			break
		}
		keep[victim] = false
		total -= cols[victim].MinWidth
	}
	out := make([]FooterColumn, 0, len(cols))
	for i := range cols {
		if keep[i] {
			out = append(out, cols[i])
		}
	}
	return out
}
