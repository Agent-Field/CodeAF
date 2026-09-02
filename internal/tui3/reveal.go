package tui3

import (
	"unicode/utf8"
)

// THE LIVE EDGE.
//
// A stream arrives in lumps. waitEvent folds every delta that piled up while
// the last frame was being built, and some endpoints send a paragraph at a
// time. Showing the lump whole is honest — and it is also a block that pops.
// The eye reads that as the machine stalling and then dumping, not as writing.
//
// So the bytes are kept, and the EDGE is what moves. Each frame the live tail
// grows by a stride that is fast when the unread remainder is large and finer
// when it is small — catch up, then write. The curve is the welcome sweep's
// own ease-out (1 − (1 − t)²) said as a per-slot fraction: most of the debt
// goes in the first two frames, the last few characters write themselves.
//
// THE ARRIVAL SHAPE MUST NOT BE THE DRAWING SHAPE. A token, a folded line,
// a paragraph that piled up behind latency — those are facts about the
// wire. What the eye reads is one edge walking at one pace. A short burst
// that landed whole was honest about the wire and a pop on the page; a
// late blob that walked in was the other face of the same defect. So every
// unread remainder is walked, and only a few characters — one short word —
// may finish on the event itself. That is [revealHead], not a line.
//
// The first cells of a new burst are never held — the first motion is never
// folded (coalesce.go). A settle snaps whatever is left, because a finished
// answer that is still revealing is a lie. The linear tier never paces: it is
// the screen-reader tier, and an animation is a still photograph there
// (tui3.go's Options.Linear).
//
// THE SAME CURVE WALKS THE METERS. A token count or a bill that jumps from
// one reading to the next is the same pop in a different column. The
// accounting stays exact; what is drawn eases toward it on this clock, and
// snaps the moment the turn is no longer running.

// revealHead is the first cells of a new burst, shown on the event itself so
// the edge moves the instant the stream speaks. It is a word, not a line:
// anything longer is already a lump the clock has to walk, whether it
// arrived as one delta or as twenty that folded.
const revealHead = 12

// revealCatch is the fraction of the unread remainder one frame-slot takes,
// in thousandths. 450 is a strong ease-out: just under half the debt each
// slot, so a four-hundred-byte lump is gone in about 200ms — under the 300ms
// UI budget — and a forty-byte tail writes itself a few characters at a time.
const revealCatch = 450

// revealFloor is the smallest stride, in bytes. Eight is two or three short
// words at the growing edge — fine enough to read as writing, never so fine
// that a remainder lingers past a couple of frames.
const revealFloor = 8

// revealSlots is the most slots a lump may take to catch up. Eight slots is
// about 260ms locally, the same second and a quarter over a connection that
// the welcome sweep takes (welcome.go counts in slots too). A 14k server-side
// lump still arrives in that window rather than typing itself out for seconds.
const revealSlots = 8

// catchReveal opens or extends the live edge after `added` bytes were just
// appended to text. shown == 0 means "not pacing, draw everything" — the
// default on every settled and historical block. A new burst shows its
// head on the event; the rest is walked on the clock, whatever its size.
//
// snap is the linear tier, and a settle: nothing is held back.
func catchReveal(shown *int, text string, added int, snap bool) {
	if shown == nil || added <= 0 {
		return
	}
	if snap {
		*shown = len(text)
		return
	}
	was := len(text) - added
	if was < 0 {
		was = 0
	}
	if *shown == 0 && was == 0 {
		*shown = revealOpen(text)
		return
	}
	if *shown >= was {
		*shown = was + revealOpen(text[was:])
	}
}

// revealOpen is how much of a newly arrived burst is drawn on the event
// itself. A few characters — one short word — land so the edge is already
// moving; anything past the head waits for the clock. A burst no longer
// than the head is the whole of it, which is how a single token writes.
func revealOpen(added string) int {
	if len(added) <= revealHead {
		return len(added)
	}
	return cutUTF8(added, revealHead)
}

// revealedText is the bytes of a block the frame may paint. A settled block,
// a block that is not pacing, and a block whose edge has caught up are the
// whole text. A live lump is the prefix the clock has walked so far.
func revealedText(text string, shown int, settled bool) string {
	if settled || shown <= 0 || shown >= len(text) {
		return text
	}
	return text[:cutUTF8(text, shown)]
}

// advanceReveal walks the live edge forward by `slots` of the paint clock.
// It reports whether the cursor moved, so the caller can mark the block
// stale — a cache that still holds the previous prefix would freeze the
// animation into a still photograph.
func advanceReveal(shown *int, text string, slots int, snap bool) bool {
	if shown == nil {
		return false
	}
	if snap || *shown <= 0 {
		if *shown != 0 && *shown != len(text) {
			*shown = len(text)
			return true
		}
		return false
	}
	if *shown >= len(text) {
		return false
	}
	next := cutUTF8(text, *shown+revealStride(len(text)-*shown, slots))
	if next > len(text) {
		next = len(text)
	}
	if next == *shown {
		return false
	}
	*shown = next
	return true
}

// revealStride is how many unread bytes one paint takes. The catch fraction
// is the ease-out; the slot ceiling is the snappy bound — a lump may never
// take more than [revealSlots] to arrive, whatever its size.
func revealStride(unread, slots int) int {
	if unread <= 0 || slots <= 0 {
		return 0
	}
	// A remainder no larger than the floor finishes this frame — one short
	// word, not a line. Dumping a whole line here is how a 40-byte fold
	// still popped after the lump path started walking.
	if unread <= revealFloor {
		return unread
	}
	take := unread * revealCatch / 1000
	if take < revealFloor {
		take = revealFloor
	}
	if need := (unread + revealSlots - 1) / revealSlots; take < need {
		take = need
	}
	take *= slots
	if take > unread {
		return unread
	}
	return take
}

// revealing reports whether a live block still has unread bytes. The paint
// clock stays up for as long as this is true, even after the stream has gone
// quiet — otherwise the last lump would freeze mid-word until something
// unrelated asked for a frame.
func revealing(shown int, text string, settled bool) bool {
	if settled || shown <= 0 {
		return false
	}
	return shown < len(text)
}

// cutUTF8 walks n back to a rune boundary so the edge never splits a
// character. n is a byte index into s.
func cutUTF8(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	if n <= 0 {
		return 0
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// meterCatch is [revealCatch] said for a number: the same ease-out, so a
// cost and a token count arrive in the same breath as the words they are
// about.
const meterCatch = revealCatch

// easeInt walks shown toward target by the catch fraction, once per slot.
// A difference of one is taken whole — the emptiness law's cousin: a meter
// that flickers between two neighbouring integers is motion, not a figure.
func easeInt(shown, target, slots int) int {
	if shown == target || slots <= 0 {
		return target
	}
	delta := target - shown
	if delta < 0 {
		delta = -delta
	}
	if delta <= 1 {
		return target
	}
	step := delta * meterCatch / 1000
	if step < 1 {
		step = 1
	}
	step *= slots
	if step >= delta {
		return target
	}
	if target > shown {
		return shown + step
	}
	return shown - step
}

// easeCost is [easeInt] for a dollar figure. The smallest step is a tenth
// of a cent — below that [dollars] cannot show the motion anyway, and a
// figure that changed in its fifth decimal would be a figure nobody read.
func easeCost(shown, target float64, slots int) float64 {
	if shown == target || slots <= 0 {
		return target
	}
	delta := target - shown
	if delta < 0 {
		delta = -delta
	}
	const grain = 0.0001
	if delta <= grain {
		return target
	}
	step := delta * float64(meterCatch) / 1000
	if step < grain {
		step = grain
	}
	step *= float64(slots)
	if step >= delta {
		return target
	}
	if target > shown {
		return shown + step
	}
	return shown - step
}

// (e *entry) helpers. They exist so the conversation, a room and a thought
// block share one spelling of the edge.

func (e *entry) catchReveal(added int, snap bool) {
	if e == nil {
		return
	}
	catchReveal(&e.shown, e.text, added, snap)
}

func (e *entry) revealed() string {
	if e == nil {
		return ""
	}
	return revealedText(e.text, e.shown, e.settled)
}

func (e *entry) advanceReveal(slots int, snap bool) bool {
	if e == nil {
		return false
	}
	if !advanceReveal(&e.shown, e.text, slots, snap || e.settled) {
		return false
	}
	e.stale = true
	return true
}

func (e *entry) revealing() bool {
	if e == nil {
		return false
	}
	return revealing(e.shown, e.text, e.settled)
}

// advanceLive walks the conversation's live answer and its thinking block.
// A room's page is walked the same way, because it is the same two blocks
// on a different list.
func (a *app) advanceLive(entries []entry, live, think, slots int, snap bool) bool {
	moved := false
	if live >= 0 && live < len(entries) {
		moved = (&entries[live]).advanceReveal(slots, snap) || moved
	}
	if think >= 0 && think < len(entries) {
		moved = (&entries[think]).advanceReveal(slots, snap) || moved
	}
	return moved
}

// liveRevealing reports whether the conversation or the open room still has
// an unread edge.
func (a *app) liveRevealing() bool {
	if a.live >= 0 && a.live < len(a.entries) && a.entries[a.live].revealing() {
		return true
	}
	if a.think >= 0 && a.think < len(a.entries) && a.entries[a.think].revealing() {
		return true
	}
	if a.room != nil {
		if a.room.live >= 0 && a.room.live < len(a.room.entries) && a.room.entries[a.room.live].revealing() {
			return true
		}
		if a.room.think >= 0 && a.room.think < len(a.room.entries) && a.room.entries[a.room.think].revealing() {
			return true
		}
	}
	for _, ex := range a.exchanges {
		if ex == nil {
			continue
		}
		if ex.live >= 0 && ex.live < len(ex.rows) {
			row := ex.rows[ex.live]
			if row.kind == exchangeReply && revealing(row.shown, row.text, row.settled) {
				return true
			}
		}
	}
	return a.meterChasing
}

// tickReveal is one turn of the edge and the meters, on the paint clock.
func (a *app) tickReveal() {
	slots := a.frameStride()
	snap := a.linear
	if a.advanceLive(a.entries, a.live, a.think, slots, snap) {
		a.dirty = true
	}
	if a.room != nil && a.advanceLive(a.room.entries, a.room.live, a.room.think, slots, snap) {
		a.room.dirty = true
	}
	for _, ex := range a.exchanges {
		if ex == nil || ex.live < 0 || ex.live >= len(ex.rows) {
			continue
		}
		row := &ex.rows[ex.live]
		if row.kind != exchangeReply {
			continue
		}
		if advanceReveal(&row.shown, row.text, slots, snap || row.settled) {
			a.dirty = true
		}
	}
	a.tickMeters(slots, snap)
}

// tickMeters walks the drawn cost, token total and context weight toward
// the books. It chases only while a turn is running and something has asked
// it to ([app.take] sets the flag); every other reading — a restore, a
// switch, a test that wrote the field directly — is the exact figure, so a
// status line drawn on the first frame of a resumed conversation is the
// bill that conversation already had.
func (a *app) tickMeters(slots int, snap bool) {
	if !a.meterChasing || snap || a.state != stateWorking {
		a.shownCost = a.spendShown()
		a.shownTokens = a.tokens
		a.shownCtx = a.ctxTokens
		a.meterChasing = false
		return
	}
	cost := a.spendShown()
	nextCost := easeCost(a.shownCost, cost, slots)
	nextTok := easeInt(a.shownTokens, a.tokens, slots)
	nextCtx := easeInt(a.shownCtx, a.ctxTokens, slots)
	if nextCost != a.shownCost || nextTok != a.shownTokens || nextCtx != a.shownCtx {
		a.dirty = true
	}
	a.shownCost, a.shownTokens, a.shownCtx = nextCost, nextTok, nextCtx
	if a.shownCost == cost && a.shownTokens == a.tokens && a.shownCtx == a.ctxTokens {
		a.meterChasing = false
	}
}

// spendDrawn is the bill the status line paints. The books stay on
// [app.spendShown]; this is only the figure in motion.
func (a *app) spendDrawn() float64 {
	if !a.meterChasing {
		return a.spendShown()
	}
	return a.shownCost
}

// ctxDrawn is the conversation weight the meter paints, on the same terms.
func (a *app) ctxDrawn() int {
	if !a.meterChasing || a.shownCtx == 0 {
		return a.ctxTokens
	}
	return a.shownCtx
}
