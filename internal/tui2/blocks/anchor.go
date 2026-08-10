package blocks

// Anchor is the reader's place, said in the document's own terms rather than in
// line numbers.
//
// This is the port of the one genuinely good invention in the old renderer
// (4.4). Two implementations there said the same thing twice:
//
//   - internal/tui/model.go captureChatAnchor/resolveChatAnchor — the block
//     under the top visible line named by journal seq or card id, plus how far
//     into it the reader was, with a fallback that follows a message into the
//     card that absorbed it.
//   - internal/tui/node.go feedAnchorAt/restoreFeedAnchor — the same shape with
//     a CONTENT-DERIVED block key, because the trace window slides off its own
//     head and a line number is only true of the document that produced it.
//
// Both survive here as one thing: a block id, an optional finer key inside that
// block, the row offset into whichever of the two resolved, and the raw offset
// as the last resort. [Transcript.Alias] carries the card-absorption fallback;
// [AnchorBlock] carries the content-derived key.
type Anchor struct {
	ok     bool
	id     string
	key    string
	within int
	offset int
}

// OK reports whether the anchor names a block. A false anchor still carries a
// usable raw offset.
func (a Anchor) OK() bool { return a.ok }

// ID is the anchored block's id.
func (a Anchor) ID() string { return a.id }

// Key is the finer anchor inside the block, if the block offered one.
func (a Anchor) Key() string { return a.key }

// Within is how many rows into the anchored thing the reader's top line was.
func (a Anchor) Within() int { return a.within }

// Offset is the raw row offset the anchor was captured at — the fallback when
// nothing else resolves.
func (a Anchor) Offset() int { return a.offset }

// AnchorBlock is the optional finer-grained anchoring a block can offer. A
// block whose interior slides — a trace feed that drops its head at a byte
// budget, a list that folds and unfolds — names its rows by content-derived
// keys, so the reader's place survives the block's own churn and not merely the
// transcript's.
type AnchorBlock interface {
	Block
	// AnchorAt names the thing at the given row of this block, and how far
	// into that thing the row is. An empty key means "anchor to the block".
	AnchorAt(row int) (key string, within int)
	// AnchorRow finds the first row of a key in the block's CURRENT rows.
	AnchorRow(key string) (row int, ok bool)
}

// Anchor is the reader's recorded place, taken against the layout the last
// frame left behind. The shell can hold one across an operation that rebuilds
// the transcript wholesale.
func (t *Transcript) Anchor() Anchor { return t.anchor }

func (t *Transcript) captureAnchorAt(offset int) Anchor {
	a := Anchor{offset: offset}
	i := t.blockAt(offset)
	if i < 0 || i >= len(t.ents) {
		return a
	}
	e := &t.ents[i]
	if !e.valid || i >= t.validStarts {
		// The block's rows were never measured at this width; its start is
		// not a fact yet, so the raw offset is the honest answer.
		return a
	}
	a.ok, a.id, a.within = true, e.id, offset-e.start
	if fine, isFine := t.blocks[i].(AnchorBlock); isFine {
		if key, within := fine.AnchorAt(a.within); key != "" {
			a.key, a.within = key, within
		}
	}
	return a
}

// RestoreAnchor puts the reader back on the same words against the current
// layout. It is exported for the shell's wholesale-rebuild path;
// [Transcript.Frame] calls it for you.
func (t *Transcript) RestoreAnchor(a Anchor) {
	t.restoreAnchor(a)
	t.clampOffset()
	t.follow = t.AtBottom()
	t.anchor = t.captureAnchorAt(t.yOffset)
}

func (t *Transcript) restoreAnchor(a Anchor) {
	if !a.ok {
		t.yOffset = a.offset
		return
	}
	i, found := t.index[a.id]
	if !found && t.Alias != nil {
		if alias := t.Alias(a.id); alias != "" {
			if j, ok := t.index[alias]; ok {
				// The block was absorbed by another — land on the absorber's
				// head, which is where its content now begins.
				t.yOffset = t.ents[j].start
				return
			}
		}
	}
	if !found || i >= len(t.ents) {
		t.yOffset = a.offset
		return
	}
	base := t.ents[i].start
	if a.key != "" {
		if fine, isFine := t.blocks[i].(AnchorBlock); isFine {
			if row, ok := fine.AnchorRow(a.key); ok {
				t.yOffset = base + row + a.within
				return
			}
		}
		// The keyed thing is gone from the block. Its head is the nearest
		// truth left.
		t.yOffset = base
		return
	}
	t.yOffset = base + a.within
}
