package blocks

// Block is the unit of transcript rendering: a self-contained run of rows with
// an identity, a finalization state, and a version.
//
// The contract, in the words of 8.1.1:
//
//   - Rows(width) renders the whole block at that width. It must be a pure
//     function of the block's state and the width — same state, same width,
//     same bytes. Rows must never contain a newline; one string is one screen
//     row.
//   - IsFinalized reports that the block will never change again except through
//     a Version bump. A finalized block is rendered once per (width, version)
//     and never rebuilt. Finalized means never rebuilt, not never rendered.
//   - SettledRows(width) is the length of the byte-stable HEAD of a still
//     streaming block: rows [0, SettledRows) will never change again, so a
//     streaming reply finalizes its head mid-stream. It must never decrease for
//     a given width while the block stays live, and it is meaningless once
//     IsFinalized reports true. Return 0 if the block cannot promise anything.
//     A live block that changes something INSIDE its own settled head — a
//     header whose meta moved, a fold that opened — must bump Version, which
//     withdraws the promise for that frame and forces a whole rebuild.
//   - Version increments on any post-final mutation. Mutating a finalized block
//     without bumping Version is a bug; see [Transcript.Strict], which detects
//     it in test builds.
//   - End reports HOW a finalized block ended (12.5, the truncation law). A
//     live block returns [EndLive].
//
// A Block is rendered from the render goroutine only.
type Block interface {
	ID() string
	Rows(width int) []string
	IsFinalized() bool
	SettledRows(width int) int
	Version() uint64
	End() EndState
}

// IncrementalBlock is the optional fast path for long streaming blocks. When a
// live block implements it, the cache keeps the settled head from the previous
// frame and asks only for the tail, appending into a buffer it already owns —
// so a streaming frame costs O(live region) and not O(block).
//
// AppendRowsFrom must append rows [start, end) to dst and return the grown
// slice. start is always in [0, SettledRows(width)] and always <= the number of
// rows the block currently has.
type IncrementalBlock interface {
	Block
	AppendRowsFrom(dst []string, width, start int) []string
}

// EndState says how a block stopped. It is the rendering hook for the
// truncation law (12.5): "a turn ended by anything other than its own
// completion (length cap, stream drop, interrupt) journals HOW it ended, and
// renders visibly cut". An unmarked half-answer is a lie of omission, so every
// finalized block carries one of these and every renderer that draws a
// finalized block draws its mark.
type EndState uint8

const (
	// EndLive is the state of a block that has not finalized yet.
	EndLive EndState = iota
	// EndCompleted is the only clean ending: the turn ended by its own
	// completion.
	EndCompleted
	// EndTruncatedByCap means the provider hit an output cap (finish_reason
	// "length"). Session bd3c78ed's failure mode.
	EndTruncatedByCap
	// EndStreamDropped means the stream died before the turn finished.
	EndStreamDropped
	// EndInterrupted means a human stopped it.
	EndInterrupted
)

// Cut reports whether the block ended by anything other than its own
// completion, which is exactly when it must render visibly cut.
func (e EndState) Cut() bool {
	return e == EndTruncatedByCap || e == EndStreamDropped || e == EndInterrupted
}

// Mark is the short word the cut rule carries. It is empty for endings that
// need no mark.
func (e EndState) Mark() string {
	switch e {
	case EndTruncatedByCap:
		return "cut off — output cap"
	case EndStreamDropped:
		return "cut off — stream dropped"
	case EndInterrupted:
		return "stopped by you"
	default:
		return ""
	}
}

// Hue is the colour the mark is drawn in: broken for the two failures, chrome
// for a deliberate interrupt.
func (e EndState) Hue() Hue {
	switch e {
	case EndTruncatedByCap, EndStreamDropped:
		return HueBroken
	default:
		return HueNone
	}
}

func (e EndState) String() string {
	switch e {
	case EndLive:
		return "live"
	case EndCompleted:
		return "completed"
	case EndTruncatedByCap:
		return "truncated-by-cap"
	case EndStreamDropped:
		return "stream-dropped"
	case EndInterrupted:
		return "interrupted"
	default:
		return "unknown"
	}
}

// CutMark is the truncation law's mark (12.5.2), and there is exactly one of
// it. The severed double-dash is deliberately NOT the overflow ellipsis: an
// ellipsis says "there is more, ask for it", a cut says "this stopped and
// should not have", and conflating the two is the lie of omission 12.5 found.
//
// The vocabulary authority for every glyph in this tree is
// internal/tui2/tokens (tokens.GlyphCut). blocks cannot import it — the edge
// runs tokens → blocks so blocks stays a leaf — so the byte lives here twice
// and tokens' own glyph_test pins the two equal. A drift fails a test rather
// than shipping two marks for one meaning.
//
// U+254C is Neutral width: one cell under every ruler, including a CJK locale.
const CutMark = "╌"

// CutRule renders the visible cut a finalized-but-incomplete block ends with:
// a dashed rule carrying the reason, filling the width. It returns "" for
// endings that need no mark, and never panics at width 1.
//
// This is the second half of the truncation law. The first half is the badge
// [Header] draws; this is the row under the body, so a reader who scrolled past
// the header still sees that the text they are reading stops short.
func CutRule(end EndState, width int, s Styler) string {
	mark := end.Mark()
	if mark == "" || width <= 0 {
		return ""
	}
	st := styler(s)
	// Narrow terminals get the mark alone, then a single glyph, then nothing.
	if width < 4 {
		return st.Paint(truncate(CutMark, width), StateChrome, end.Hue())
	}
	var b builder
	b.grow(width + 16)
	markWidth := stringWidth(mark)
	if width < markWidth+4 {
		b.styled(st, truncate(mark, width), StateChrome, end.Hue())
		return b.String()
	}
	b.styled(st, CutMark+" ", StateChrome, end.Hue())
	b.styled(st, mark, StateChrome, end.Hue())
	rest := width - 2 - markWidth - 1
	if rest > 0 {
		b.WriteByte(' ')
		b.styled(st, repeat('─', rest), StateChrome, HueNone)
	}
	return b.String()
}
