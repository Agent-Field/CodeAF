package blocks

import (
	"strings"
	"time"
)

// DefaultWindow is how many blocks above and below the visible range keep their
// rendered rows. It buys a screenful or two of instant scrolling in either
// direction without letting a long transcript pin its whole history in memory.
const DefaultWindow = 24

// Transcript is the ordered block list plus the live seam, the one
// immutable-block cache, and the viewport over the assembled rows.
//
// It owns the anchor-preserving scroll ported from internal/tui — the one
// genuinely good invention in the old renderer (4.4). The reader's place is
// stored as "which block was under the top line, and how far into it", never as
// a raw line number, so it survives a re-render, a block above it changing
// height, a block being appended, and a resize.
//
// Because we stay inside alt screen (8.1.1 rejects native scrollback for v1),
// we keep our own viewport, so we keep our own anchor — and a byte we own can
// be re-emitted, which is what makes post-final mutation by Version coherent.
//
// A Transcript is not safe for concurrent use.
type Transcript struct {
	// Window is how many blocks past the visible range keep rendered rows.
	// Zero means [DefaultWindow].
	Window int
	// Strict turns on the finalized-block invariant check. It defaults to on
	// under `go test` and off everywhere else; see strict.go.
	Strict bool
	// Alias resolves a block id that has vanished to the block that absorbed
	// it, so the reader's place survives a message being folded into a card.
	// This is the port of the old renderer's cardForMessageSeq fallback.
	// Return "" when there is no successor.
	Alias func(id string) string

	blocks []Block
	ents   []entry
	index  map[string]int

	clock  *Clock
	width  int
	height int

	yOffset     int
	follow      bool
	anchor      Anchor
	total       int
	validStarts int

	resLo, resHi int

	out     []string
	prev    []string
	dirty   []int
	scratch []string

	stats Stats
}

// New returns an empty transcript sized to width x height, pinned to the
// bottom. Width or height below 1 are clamped to 1: a one-column, one-row
// terminal is a rendering problem, never a panic.
func New(width, height int) *Transcript {
	t := &Transcript{
		index:  make(map[string]int),
		clock:  NewClock(0),
		width:  max1(width),
		height: max1(height),
		follow: true,
		Strict: strictDefault,
	}
	return t
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// Clock is the transcript's shared animation clock. Every live glyph in a frame
// must derive from it, so they are phase-locked (8.1.3).
func (t *Transcript) Clock() *Clock { return t.clock }

// SetClock replaces the animation clock — for deterministic goldens, and for a
// shell that drives several surfaces off one clock.
func (t *Transcript) SetClock(c *Clock) {
	if c != nil {
		t.clock = c
	}
}

// Width is the width blocks are rendered at.
func (t *Transcript) Width() int { return t.width }

// Height is the viewport height in rows.
func (t *Transcript) Height() int { return t.height }

// SetSize resizes the viewport. A width change is the only full cache
// invalidation there is: every cached row is a function of the width that
// produced it. A height change costs nothing but a clamp.
func (t *Transcript) SetSize(width, height int) {
	width, height = max1(width), max1(height)
	if width != t.width {
		t.width = width
		t.invalidateAll()
	}
	t.height = height
	t.clampOffset()
}

// Len is the number of blocks.
func (t *Transcript) Len() int { return len(t.blocks) }

// Block returns block i, or nil when i is out of range.
func (t *Transcript) Block(i int) Block {
	if i < 0 || i >= len(t.blocks) {
		return nil
	}
	return t.blocks[i]
}

// Append adds a block at the end of the transcript. Appending while the reader
// is scrolled up does not move them: the new block lands below everything the
// anchor refers to.
func (t *Transcript) Append(b Block) {
	if b == nil {
		return
	}
	id := b.ID()
	if old, ok := t.index[id]; ok && t.Strict {
		panic("blocks: duplicate block id " + id + " (already at index " + itoa(old) + ")")
	}
	t.blocks = append(t.blocks, b)
	t.ents = append(t.ents, entry{id: id})
	t.index[id] = len(t.blocks) - 1
}

// Replace swaps the block at index i. Use it when a live block is superseded by
// its finalized form; a block that merely mutated should bump its Version
// instead.
func (t *Transcript) Replace(i int, b Block) {
	if b == nil || i < 0 || i >= len(t.blocks) {
		return
	}
	if old := t.ents[i].id; old != "" && old != b.ID() {
		delete(t.index, old)
	}
	t.blocks[i] = b
	t.ents[i] = entry{id: b.ID(), rows: t.ents[i].rows[:0]}
	t.index[b.ID()] = i
	t.invalidateStartsFrom(i)
}

// ReplaceLast swaps the last block, the common shape of a live turn settling.
func (t *Transcript) ReplaceLast(b Block) { t.Replace(len(t.blocks)-1, b) }

// Truncate drops every block from index n onward.
func (t *Transcript) Truncate(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(t.blocks) {
		return
	}
	for i := n; i < len(t.ents); i++ {
		delete(t.index, t.ents[i].id)
		t.ents[i].rows = nil
	}
	t.blocks = t.blocks[:n]
	t.ents = t.ents[:n]
	t.invalidateStartsFrom(n)
	if t.resHi > n {
		t.resHi = n
	}
	if t.resLo > n {
		t.resLo = n
	}
}

// Reset empties the transcript and re-pins it to the bottom.
func (t *Transcript) Reset() {
	t.blocks = t.blocks[:0]
	t.ents = t.ents[:0]
	clear(t.index)
	t.yOffset, t.total, t.validStarts = 0, 0, 0
	t.resLo, t.resHi = 0, 0
	t.follow = true
	t.anchor = Anchor{}
}

// Invalidate forces block id to be re-rendered on the next frame. The ordinary
// route is a Version bump, which the cache notices by itself; this is the door
// for a caller that mutated a block it cannot version.
func (t *Transcript) Invalidate(id string) {
	i, ok := t.index[id]
	if !ok {
		return
	}
	t.ents[i].valid = false
	t.invalidateStartsFrom(i + 1)
}

// IndexOf returns the index of block id.
func (t *Transcript) IndexOf(id string) (int, bool) {
	i, ok := t.index[id]
	return i, ok
}

// LiveSeam is the index of the first block that is not finalized: blocks before
// it are the committed prefix, blocks from it on are the live region. It is
// len(blocks) when everything has settled.
func (t *Transcript) LiveSeam() int {
	for i := range t.blocks {
		if !t.blocks[i].IsFinalized() {
			return i
		}
	}
	return len(t.blocks)
}

func (t *Transcript) invalidateStartsFrom(i int) {
	if i < 0 {
		i = 0
	}
	if i < t.validStarts {
		t.validStarts = i
	}
}

// -- scrolling ---------------------------------------------------------------

// YOffset is the first visible row of the assembled transcript.
func (t *Transcript) YOffset() int { return t.yOffset }

// Total is the assembled transcript's height in rows, as of the last frame.
func (t *Transcript) Total() int { return t.total }

// AtBottom reports whether the last row is visible.
func (t *Transcript) AtBottom() bool { return t.yOffset >= t.maxOffset() }

// AtTop reports whether the first row is visible.
func (t *Transcript) AtTop() bool { return t.yOffset <= 0 }

// Following reports whether new output will pull the view down. Follow is a
// consequence of where the reader is, never a mode they have to manage: it is
// on exactly while they are at the bottom.
func (t *Transcript) Following() bool { return t.follow }

func (t *Transcript) maxOffset() int {
	if m := t.total - t.height; m > 0 {
		return m
	}
	return 0
}

func (t *Transcript) clampOffset() {
	if t.yOffset > t.maxOffset() {
		t.yOffset = t.maxOffset()
	}
	if t.yOffset < 0 {
		t.yOffset = 0
	}
}

// ScrollTo moves to an absolute row offset and re-derives follow from it.
func (t *Transcript) ScrollTo(offset int) {
	t.yOffset = offset
	t.clampOffset()
	t.follow = t.AtBottom()
	t.anchor = t.captureAnchorAt(t.yOffset)
}

// ScrollBy moves by delta rows.
func (t *Transcript) ScrollBy(delta int) { t.ScrollTo(t.yOffset + delta) }

// PageUp moves up one screenful less an overlap row.
func (t *Transcript) PageUp() { t.ScrollBy(-max1(t.height - 1)) }

// PageDown moves down one screenful less an overlap row.
func (t *Transcript) PageDown() { t.ScrollBy(max1(t.height - 1)) }

// GotoTop shows the first row and stops following.
func (t *Transcript) GotoTop() { t.ScrollTo(0) }

// GotoBottom shows the last row and resumes following.
func (t *Transcript) GotoBottom() {
	t.yOffset = t.maxOffset()
	t.follow = true
	t.anchor = t.captureAnchorAt(t.yOffset)
}

// -- frames ------------------------------------------------------------------

// Frame is one assembled screenful.
type Frame struct {
	// Rows are exactly Height strings, padded with "" past the end of the
	// transcript. They alias the transcript's own buffer and are valid until
	// the next call to [Transcript.Frame].
	Rows []string
	// Dirty are the indices into Rows that differ from the previous frame. An
	// empty Dirty on a frame that was rendered is the proof that the render
	// tick and the glyph tick coincided (8.1.3) — the shell can skip the
	// repaint entirely.
	Dirty []int
	// LiveFrom is the index into Rows where the live region starts, or -1 when
	// no live block is on screen. Everything before it is committed prefix.
	LiveFrom int

	Width    int
	Height   int
	YOffset  int
	Total    int
	AtTop    bool
	AtBottom bool
}

// Unchanged reports that nothing on screen moved.
func (f Frame) Unchanged() bool { return len(f.Dirty) == 0 }

// String joins the frame's rows, for tests and goldens. It allocates; the
// render path does not use it.
func (f Frame) String() string { return strings.Join(f.Rows, "\n") }

// Frame assembles the screenful at instant now.
//
// The order is the discipline, and it is the order the old renderer's
// refreshChat used for the same reason: the reader's place is CAPTURED from the
// layout the previous frame left behind, before anything is rebuilt, and
// RESTORED against the fresh layout afterwards. Reading it back from a raw line
// number is what made the old node page yank readers around every poll.
//
// In the steady state — no live block, nothing appended, nothing resized — this
// costs zero allocations and zero block renders.
func (t *Transcript) Frame(now time.Time) Frame {
	t.clock.Latch(now)

	// 1. Where is the reader? The anchor was recorded against the layout the
	// last frame left behind — NOT re-derived now, because the caller has been
	// mutating the transcript since, and a half-mutated layout cannot answer
	// the question honestly.
	anchor := t.anchor
	follow := t.follow

	// 2. Rebuild what must be rebuilt and re-lay the document out.
	t.seedWindow(anchor)
	t.refresh()
	t.layout()

	// 3. Put the reader back.
	if follow {
		t.yOffset = t.maxOffset()
	} else {
		t.restoreAnchor(anchor)
	}
	t.clampOffset()

	// 4. Hold rows for the visible range plus the window; drop the rest.
	lo, hi := t.visibleRange()
	window := t.Window
	if window <= 0 {
		window = DefaultWindow
	}
	t.reside(lo-window, hi+window)

	// 5. Materialize and diff.
	live := t.materialize(lo, hi)
	t.diff()

	// 6. Record where the reader ended up, against a layout that is now exact.
	t.anchor = t.captureAnchorAt(t.yOffset)

	return Frame{
		Rows:     t.out,
		Dirty:    t.dirty,
		LiveFrom: live,
		Width:    t.width,
		Height:   t.height,
		YOffset:  t.yOffset,
		Total:    t.total,
		AtTop:    t.AtTop(),
		AtBottom: t.AtBottom(),
	}
}

func (t *Transcript) windowSize() int {
	if t.Window > 0 {
		return t.Window
	}
	return DefaultWindow
}

// seedWindow guesses the retention window before the height pass runs, on the
// frames where there is no window yet: the first frame, and the frame after a
// resize. Without it, every block the height pass measures is measured with its
// rows thrown away and then rendered a second time by [Transcript.reside] —
// correct, but twice the work on exactly the frames that already cost the most.
//
// The guess is cheap and good: the reader is either at the bottom (following)
// or on the block their anchor names.
func (t *Transcript) seedWindow(a Anchor) {
	if t.resLo != t.resHi || len(t.ents) == 0 {
		return
	}
	w := t.windowSize()
	center := len(t.ents) - 1
	if !t.follow && a.ok {
		if i, ok := t.index[a.id]; ok {
			center = i
		}
	}
	t.resLo = max(0, center-w)
	t.resHi = min(len(t.ents), center+w+1)
}

// layout recomputes block start offsets from the first one that moved. Appends
// and live-block growth touch only the tail, so this is O(1) amortized; a width
// change is the only thing that walks the whole list.
func (t *Transcript) layout() {
	for i := t.validStarts; i < len(t.ents); i++ {
		if i == 0 {
			t.ents[0].start = 0
			continue
		}
		t.ents[i].start = t.ents[i-1].start + t.ents[i-1].height
	}
	t.validStarts = len(t.ents)
	if n := len(t.ents); n > 0 {
		t.total = t.ents[n-1].start + t.ents[n-1].height
	} else {
		t.total = 0
	}
}

// blockAt returns the index of the block containing row, or -1. It searches
// only the prefix whose start offsets are known good: past validStarts the
// starts are not yet a fact, and a binary search over them would be a guess
// dressed as an answer.
func (t *Transcript) blockAt(row int) int {
	n := t.validStarts
	if n > len(t.ents) {
		n = len(t.ents)
	}
	if n == 0 || row < 0 {
		return -1
	}
	lo, hi := 0, n // last index with start <= row
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if t.ents[mid].start <= row {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo - 1
}

// BlockAtScreenRow answers which block a VIEWPORT row belongs to, and how far
// into that block the row is.
//
// It is the transcript's whole contribution to the click layer, and it is a
// pure lookup against the layout the last Frame produced: the caller hands in
// a pane-local y, the scroll offset turns it into a document row, and the same
// binary search the renderer uses names the block. Nothing is recorded and
// nothing is recomputed — a click cannot disagree with the picture because it
// is reading the picture's own arithmetic.
//
// The second return is the line's index INSIDE the block, which is what lets a
// block that knows its own shape (an option list, an artifact row) say which of
// its rows was pointed at without ever learning where it is on screen.
func (t *Transcript) BlockAtScreenRow(y int) (index, line int, ok bool) {
	if y < 0 || y >= t.height {
		return 0, 0, false
	}
	row := t.yOffset + y
	if row >= t.total {
		return 0, 0, false
	}
	i := t.blockAt(row)
	if i < 0 || i >= len(t.ents) {
		return 0, 0, false
	}
	return i, row - t.ents[i].start, true
}

// visibleRange is the half-open block range the viewport touches.
func (t *Transcript) visibleRange() (int, int) {
	if len(t.ents) == 0 {
		return 0, 0
	}
	lo := t.blockAt(t.yOffset)
	if lo < 0 {
		lo = 0
	}
	end := t.yOffset + t.height
	hi := lo
	for hi < len(t.ents) && t.ents[hi].start < end {
		hi++
	}
	if hi == lo && lo < len(t.ents) {
		hi = lo + 1
	}
	return lo, hi
}

// materialize copies the visible rows into the frame buffer and reports where
// the live region begins. It appends into a slice it already owns, so a
// steady-state frame allocates nothing.
func (t *Transcript) materialize(lo, hi int) int {
	t.out = t.out[:0]
	live := -1
	end := t.yOffset + t.height
	for i := lo; i < hi; i++ {
		e := &t.ents[i]
		if e.rows == nil {
			continue
		}
		from := t.yOffset - e.start
		if from < 0 {
			from = 0
		}
		to := e.height
		if e.start+to > end {
			to = end - e.start
		}
		if to > len(e.rows) {
			to = len(e.rows)
		}
		if from >= to {
			continue
		}
		if live < 0 && !e.final {
			live = len(t.out)
		}
		t.out = append(t.out, e.rows[from:to]...)
	}
	for len(t.out) < t.height {
		t.out = append(t.out, "")
	}
	if len(t.out) > t.height {
		t.out = t.out[:t.height]
	}
	if live >= len(t.out) {
		live = -1
	}
	return live
}

// diff records which rows changed since the previous frame and keeps a copy for
// the next one. Both buffers are reused; a frame in which nothing moved
// produces an empty Dirty and no allocation at all.
func (t *Transcript) diff() {
	t.dirty = t.dirty[:0]
	if len(t.prev) != len(t.out) {
		for i := range t.out {
			t.dirty = append(t.dirty, i)
		}
	} else {
		for i := range t.out {
			if t.out[i] != t.prev[i] {
				t.dirty = append(t.dirty, i)
			}
		}
	}
	t.prev = append(t.prev[:0], t.out...)
}

// itoa is strconv.Itoa without the import, for panic messages only.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
