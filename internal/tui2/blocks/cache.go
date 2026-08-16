package blocks

import "fmt"

// entry is the cache's record of one block. It is the ONE immutable-block cache
// of 8.1.1, replacing the four caches of the old renderer (block cache, card
// blocks, brief blocks, whole-thread splice).
//
// Two levels of retention, and the distinction is the whole memory story:
//
//   - height + version + start are kept for EVERY block, always. That is
//     ~64 bytes per block: 640 KB for a 10,000-block transcript, and it is what
//     lets the viewport map an offset to a block without rendering anything.
//   - rows are kept only for blocks inside the retention window (visible ±
//     [Transcript.Window]). Outside it the strings are dropped and the entry
//     keeps its height. Scrolling back re-renders from the block, which is
//     cheap and bounded, instead of holding a transcript's worth of strings
//     resident forever.
type entry struct {
	id      string
	version uint64
	final   bool
	valid   bool
	height  int
	start   int
	settled int
	rows    []string
}

// Stats reports what the cache is holding and doing. It is for tests, the
// golden harness, and the eventual /debug surface — not for the UI.
type Stats struct {
	// Blocks is how many blocks the transcript holds.
	Blocks int
	// Resident is how many of them currently hold rendered rows.
	Resident int
	// ResidentRows is how many strings those blocks hold.
	ResidentRows int
	// Renders counts block renders since the transcript was created. A
	// steady state with no live block adds none.
	Renders int
	// Hits counts frames a block was drawn from cached rows.
	Hits int
	// Evictions counts blocks whose rows were dropped for leaving the window.
	Evictions int
	// WidthResets counts full invalidations caused by a width change.
	WidthResets int
}

// invalidateAll drops every rendered row and every recorded height. It is the
// ONLY full invalidation, and a width change is the only thing that causes it:
// a block's rows are a function of (block state, width), so a new width makes
// every cached row a lie.
func (t *Transcript) invalidateAll() {
	for i := range t.ents {
		e := &t.ents[i]
		e.valid = false
		e.height = 0
		e.settled = 0
		e.rows = nil
	}
	t.resLo, t.resHi = 0, 0
	t.validStarts = 0
	t.stats.WidthResets++
}

// refresh brings every entry's height up to date. A finalized entry whose
// version has not moved is left completely alone — never rebuilt, which is the
// law. A live entry is rebuilt every frame, because that is what live means.
func (t *Transcript) refresh() {
	for i := range t.ents {
		e := &t.ents[i]
		b := t.blocks[i]
		switch {
		case !e.valid:
			// Never rendered at this width, or invalidated.
		case !e.final:
			// Live: rebuilt below regardless of version.
		case b.Version() != e.version:
			// Post-final mutation. Coherent precisely because the bytes
			// live in our cache and not in the terminal's scrollback.
			e.valid = false
		default:
			continue
		}
		before := e.height
		t.build(i, t.inWindow(i))
		if e.height != before {
			t.invalidateStartsFrom(i + 1)
		}
	}
}

// build renders block i. keep says whether the rows are worth holding: inside
// the retention window they are, outside it only the height survives.
func (t *Transcript) build(i int, keep bool) {
	e := &t.ents[i]
	b := t.blocks[i]
	final := b.IsFinalized()

	if keep {
		e.rows = t.renderInto(i, e.rows, final)
		e.height = len(e.rows)
	} else {
		t.scratch = t.renderInto(i, t.scratch, final)
		e.height = len(t.scratch)
		if e.rows != nil {
			e.rows = nil
			t.stats.Evictions++
		}
		e.settled = 0
	}
	e.id, e.version, e.final, e.valid = b.ID(), b.Version(), final, true
	t.stats.Renders++
}

// renderInto renders block i into dst, reusing dst's capacity.
//
// The settled-head fast path is 8.1.1's "a streaming reply can finalize its
// byte-stable head mid-stream": rows [0, SettledRows) are promised never to
// change, so an [IncrementalBlock] is asked only for the tail and the head is
// kept verbatim. That is what makes a streaming frame cost O(live region)
// rather than O(block).
func (t *Transcript) renderInto(i int, dst []string, final bool) []string {
	b := t.blocks[i]
	e := &t.ents[i]
	if !final {
		if inc, ok := b.(IncrementalBlock); ok {
			settled := b.SettledRows(t.width)
			if settled < 0 {
				settled = 0
			}
			if settled > len(dst) || settled < e.settled || !e.valid ||
				b.Version() != e.version {
				// No trustworthy head to keep: rebuild the block whole. A
				// version bump counts, because a live block that changed
				// anything above its settled head says so by bumping — that
				// is what the version is FOR, on live blocks as on finalized
				// ones.
				settled = 0
			}
			e.settled = settled
			return inc.AppendRowsFrom(dst[:settled], t.width, settled)
		}
	}
	e.settled = 0
	return append(dst[:0], b.Rows(t.width)...)
}

// inWindow reports whether block i is inside the current retention window.
func (t *Transcript) inWindow(i int) bool { return i >= t.resLo && i < t.resHi }

// reside moves the retention window to [lo, hi): rows outside it are dropped,
// rows missing inside it are rendered. Cost is O(window), never O(transcript).
func (t *Transcript) reside(lo, hi int) {
	if lo < 0 {
		lo = 0
	}
	if hi > len(t.ents) {
		hi = len(t.ents)
	}
	for i := t.resLo; i < t.resHi; i++ {
		if i >= lo && i < hi {
			continue
		}
		if i < len(t.ents) && t.ents[i].rows != nil {
			t.ents[i].rows = nil
			t.ents[i].settled = 0
			t.stats.Evictions++
		}
	}
	t.resLo, t.resHi = lo, hi
	for i := lo; i < hi; i++ {
		if t.ents[i].rows == nil || !t.ents[i].valid {
			before := t.ents[i].height
			t.build(i, true)
			if t.ents[i].height != before {
				t.invalidateStartsFrom(i + 1)
			}
			continue
		}
		t.stats.Hits++
		t.verify(i)
	}
}

// verify is the invariant check of 8.1.1, live in test builds only (see
// [Transcript.Strict], which defaults to on under `go test`): a finalized block
// that changed its bytes WITHOUT bumping its version has broken the contract
// the whole cache rests on, and it must fail loudly at the moment of the lie
// rather than as a mysterious stale row three screens later.
//
// It costs a full re-render per cached block per frame, which is exactly why it
// is off in production and why benchmarks turn it off explicitly.
func (t *Transcript) verify(i int) {
	e := &t.ents[i]
	if !t.Strict || !e.final || e.rows == nil {
		return
	}
	b := t.blocks[i]
	if b.Version() != e.version {
		panic(fmt.Sprintf("blocks: finalized block %q version moved to %d under a cached %d",
			e.id, b.Version(), e.version))
	}
	t.scratch = append(t.scratch[:0], b.Rows(t.width)...)
	if len(t.scratch) != len(e.rows) {
		panic(fmt.Sprintf("blocks: finalized block %q changed height (%d -> %d) without bumping Version",
			e.id, len(e.rows), len(t.scratch)))
	}
	for r := range e.rows {
		if t.scratch[r] != e.rows[r] {
			panic(fmt.Sprintf("blocks: finalized block %q mutated row %d without bumping Version\n cached: %q\n now:    %q",
				e.id, r, e.rows[r], t.scratch[r]))
		}
	}
}

// Stats reports the cache's current holdings and running counters.
func (t *Transcript) Stats() Stats {
	s := t.stats
	s.Blocks = len(t.blocks)
	for i := range t.ents {
		if t.ents[i].rows != nil {
			s.Resident++
			s.ResidentRows += len(t.ents[i].rows)
		}
	}
	return s
}
