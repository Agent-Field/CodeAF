package blocks

import "time"

// TextBlock is the reference [Block]: an optional header, a wrapped prose body
// that can grow while the turn streams, and — when the turn ended by anything
// other than its own completion — the cut rule the truncation law requires.
//
// It exists in this package because it is where the three disciplines have to
// meet and be shown to compose: the settled head of 8.1.1 (its stable rows are
// its settled rows), freeze-at-commit of 8.1.2 (its elapsed meta stops at
// finalization), and the truncation law of 12.5 (its header badge and its cut
// rule are drawn from one EndState). Richer parts — tool cards, plan cards,
// receipts — are the shell's to write against the same contract.
type TextBlock struct {
	// Head is the block's header. It goes through [Header.Render]; there is
	// no other header renderer.
	Head Header
	// HeaderLive marks a header that changes on its own (a running elapsed, a
	// spinner). Such a block promises no settled rows, because its first row
	// is not byte-stable.
	HeaderLive bool
	// Styler paints the body rows. Nil means [Plain].
	Styler Styler
	// BodyState is the liveness the body is painted at.
	BodyState State
	// Indent inches the whole block right by that many cells — header, body,
	// and cut rule alike — and takes the cells out of its own wrap width, so
	// an indented block is a narrower block and not an overflowing one.
	//
	// It exists for one shape: a streamed preview of a reply and the journaled
	// twin it becomes must sit at the SAME depth, or the moment the stream
	// finalizes the text jumps sideways and the eye reads it as a different
	// thing happening. Depth is the surface's to decide (a reply nested under
	// its turn, a tool row under its card); the block only has to be able to
	// hold one.
	//
	// Zero is flush left and costs nothing: no padding is measured, built, or
	// concatenated, and every row is the string it was before this field
	// existed. An Indent that would leave less than one cell of content is
	// clamped rather than refused — a block on a 4-column terminal renders
	// something honest instead of nothing.
	Indent int

	id      string
	body    string
	final   bool
	end     EndState
	version uint64

	elapsed TimeCell

	// Wrap state. stable holds the body rows that can never change again;
	// tail holds the last row, which every append can rewrite.
	width    int
	measured bool
	stable   []string
	tail     []string
	tailFrom int

	rows []string
}

var _ IncrementalBlock = (*TextBlock)(nil)

// NewText starts a live text block.
func NewText(id string, head Header) *TextBlock {
	return &TextBlock{id: id, Head: head}
}

// ID is the block's stable identity: the anchor key and the cache key.
func (b *TextBlock) ID() string { return b.id }

// Version increments on post-final mutation.
func (b *TextBlock) Version() uint64 { return b.version }

// IsFinalized reports whether the block has settled.
func (b *TextBlock) IsFinalized() bool { return b.final }

// End is how the block ended.
func (b *TextBlock) End() EndState { return b.end }

// Body is the accumulated text.
func (b *TextBlock) Body() string { return b.body }

// Elapsed is the block's freeze-at-commit time cell. Sample it from the frame
// clock while the block is live; it freezes itself at finalization.
func (b *TextBlock) Elapsed() *TimeCell { return &b.elapsed }

// StartClock starts the elapsed cell.
func (b *TextBlock) StartClock(at time.Time) { b.elapsed = NewTimeCell(at) }

// Write appends body text. It is the streaming door.
func (b *TextBlock) Write(text string) {
	if text == "" || b.final {
		return
	}
	b.body += text
	b.measured = false
}

// Finalize settles the block with the ending it actually had. Passing
// [EndCompleted] is a claim that the turn ended by itself; anything else makes
// the block render visibly cut, which is the entire point of 12.5.
func (b *TextBlock) Finalize(end EndState) {
	if b.final {
		return
	}
	if end == EndLive {
		end = EndCompleted
	}
	b.final, b.end = true, end
	b.Head.End = end
	b.Head.State = StateSettled
	b.BodyState = StateSettled
	b.elapsed.Freeze()
	b.measured = false
	b.version++
}

// Mutate applies a change to an already-finalized block and bumps its version,
// which is the only coherent way to change committed bytes. Doing it without
// the bump is what [Transcript.Strict] catches.
func (b *TextBlock) Mutate(apply func(*TextBlock)) {
	apply(b)
	b.measured = false
	b.version++
}

// SettledRows is the byte-stable head: the header (when it is not itself live)
// plus every body row but the last, which the next Write can still rewrite.
func (b *TextBlock) SettledRows(width int) int {
	if b.final {
		return b.rowCount(width)
	}
	if b.HeaderLive {
		return 0
	}
	b.reflow(b.inner(width))
	return b.headRows() + len(b.stable)
}

// Rows renders the whole block.
func (b *TextBlock) Rows(width int) []string {
	b.rows = b.AppendRowsFrom(b.rows[:0], width, 0)
	return b.rows
}

// AppendRowsFrom renders rows [start, end) into dst — the fast path the cache
// uses to rebuild only what a stream actually changed.
func (b *TextBlock) AppendRowsFrom(dst []string, width, start int) []string {
	if width < 1 {
		width = 1
	}
	inner := b.inner(width)
	// repeat slices a preallocated run, so the indent itself never allocates.
	pad := repeat(' ', width-inner)
	b.reflow(inner)
	st := styler(b.Styler)
	// No closure: a captured dst would escape to the heap and cost an
	// allocation on every streaming frame.
	row := 0
	if b.hasHead() {
		if row >= start {
			dst = append(dst, shift(pad, b.Head.Render(inner, st)))
		}
		row++
	}
	for _, line := range b.stable {
		if row >= start {
			dst = append(dst, shift(pad, st.Paint(truncate(line, inner), b.BodyState, HueNone)))
		}
		row++
	}
	for _, line := range b.tail {
		if row >= start {
			dst = append(dst, shift(pad, st.Paint(truncate(line, inner), b.BodyState, HueNone)))
		}
		row++
	}
	if rule := CutRule(b.end, inner, st); rule != "" && row >= start {
		dst = append(dst, shift(pad, rule))
	}
	return dst
}

// inner is the width the block's own content is laid out in: the terminal's
// width less the indent, and never less than one cell. Every measurement in
// this type goes through it, so the wrap state, the settled-head count and the
// rendered rows can never disagree about how wide the block is.
func (b *TextBlock) inner(width int) int {
	if b.Indent <= 0 {
		return width
	}
	if inner := width - b.Indent; inner >= 1 {
		return inner
	}
	return 1
}

func (b *TextBlock) hasHead() bool {
	return b.Head.Glyph != "" || b.Head.Title != "" || b.Head.Desc != "" ||
		len(b.Head.Badges) > 0 || len(b.Head.Meta) > 0 || b.Head.End.Mark() != ""
}

func (b *TextBlock) headRows() int {
	if b.hasHead() {
		return 1
	}
	return 0
}

func (b *TextBlock) rowCount(width int) int {
	b.reflow(b.inner(width))
	n := b.headRows() + len(b.stable) + len(b.tail)
	if b.end.Mark() != "" {
		n++
	}
	return n
}

// reflow brings the wrap state up to date, re-wrapping only from the last row's
// byte offset when text was merely appended.
func (b *TextBlock) reflow(width int) {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return
	}
	if b.width != width {
		// A new width makes every earlier wrap decision a lie: the settled
		// head is settled per width, not absolutely.
		b.width = width
		b.stable = b.stable[:0]
		b.tailFrom = 0
	}
	b.tail = b.tail[:0]
	if b.body == "" {
		b.stable, b.tailFrom, b.measured = b.stable[:0], 0, true
		return
	}
	rows, last := Wrap(b.tail, b.body[b.tailFrom:], width)
	// Everything but the final row is settled forever; move it across.
	if len(rows) > 1 {
		b.stable = append(b.stable, rows[:len(rows)-1]...)
		b.tailFrom += last
		b.tail = append(b.tail[:0], rows[len(rows)-1])
	} else {
		b.tail = rows
	}
	b.measured = true
}
