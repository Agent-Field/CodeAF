package tui3

import (
	"time"
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
// THE SURFACE PACES THE WIRE'S LUMPS, NEVER ITS OWN FOLD.
//
// That is the whole of which bursts walk, and the distinction is the one this
// file got wrong once already. A model streaming token by token is ALREADY
// writing: the wire is delivering a few characters at a time and the page
// should say them as they land. What pops is a single wire event carrying a
// paragraph — an endpoint that buffers, a link that stalled and then dumped —
// and that is the only thing worth walking.
//
// A FOLD IS NOT A LUMP. [waitEvent] joins the run of short deltas that piled up
// while the last frame was being built, and that run is an artefact of THIS
// side of the channel: the wire delivered it fine-grained. Holding it back
// would be the surface inventing latency the connection never had, which is
// exactly what it did — a token-by-token stream drew in slow motion, and
// twelve fixtures that assert what one delta puts on the page saw twelve bytes
// of it. So the fold is drawn as its parts would have drawn: whole, however
// long the run. Only a single wire event past [revealAtOnce] opens a walk, and
// the bytes of any later burst join a walk that is already running.
//
// A settle snaps whatever is left, because a finished answer that is still
// revealing is a lie — and so does every other door out of the live state
// (livestate.go states that law and owns the six of them). The linear tier
// never paces: it is the screen-reader tier, and an animation is a still
// photograph there (tui3.go's Options.Linear).
//
// THE WALK IS A FUNCTION OF TIME, NOT OF PICTURES. Every step below is taken
// in SLOTS of the paint clock, and the slots are measured from the surface's
// own clock seam ([app.now], which a test pins) rather than counted in frames
// painted. A frame is when the walk is DRAWN; how far it has got is what the
// clock says. That is what makes the pace the same over a link that paints ten
// times a second as at the machine that paints thirty, and it is why nothing
// in this file ever calls [time.Now].
//
//
// THE SAME CURVE WALKS THE METERS. A token count or a bill that jumps from
// one reading to the next is the same pop in a different column. The
// accounting stays exact; what is drawn eases toward it on this clock, and
// snaps the moment the turn is no longer running.

// revealAtOnce is how much a single wire event may carry and still land whole.
//
// THE REASON IS WHAT A PERSON CAN SEE, not a guess about wire shapes. Forty-
// eight bytes is about a short line, a dozen tokens; walking one takes a
// couple of frames and moves the edge by a few characters twice, which is
// motion nobody perceives as writing — it reads as the line simply appearing,
// only later. There is nothing to show, so there is nothing to hold back. Past
// this length a walk has something to say, and a burst that arrives in one
// piece is a burst the wire buffered rather than one it wrote.
const revealAtOnce = 48

// revealHead is the first cells of a lump, shown on the event itself so the
// edge moves the instant the stream speaks rather than a frame later.
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

// isLump reports whether a burst of n bytes is one the wire buffered rather
// than one it wrote. It is the ONE place the threshold is read, so the append
// path, the fold and the tests cannot disagree about what a lump is.
func isLump(n int) bool { return n > revealAtOnce }

// catchReveal opens or extends the live edge after `added` bytes were just
// appended to text. shown == 0 means "not pacing, draw everything" — the
// default on every settled and historical block.
//
// lump is what the DELIVERY said, not what the length says: [waitEvent] knows
// whether the run it folded contained a single wire event big enough to be one,
// and a fold of short deltas is not a lump however long the fold is. snap is
// the linear tier and every door out of the live state: nothing is held back.
//
// A BURST THAT ARRIVES WHILE A LUMP IS STILL WALKING JOINS THE WALK. It does
// not open a second edge and it does not jump the first one to the end — the
// unread remainder simply grew, and the stride below covers it. That is the
// `*shown >= was` test: it fires only when the edge had already caught up.
func catchReveal(shown *int, text string, added int, lump, snap bool) {
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
		*shown = revealOpen(text, lump)
		return
	}
	if *shown >= was {
		*shown = was + revealOpen(text[was:], lump)
	}
}

// revealOpen is how much of a newly arrived burst is drawn on the event itself.
// Everything the wire wrote lands at once; a lump shows a few characters — one
// short word — so the edge is already moving, and the rest waits for the clock.
func revealOpen(added string, lump bool) int {
	if !lump || len(added) <= revealHead {
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

func (e *entry) catchReveal(added int, lump, snap bool) {
	if e == nil {
		return
	}
	catchReveal(&e.edge, e.text, added, lump, snap)
}

func (e *entry) revealed() string {
	if e == nil {
		return ""
	}
	return revealedText(e.text, e.edge, e.settled)
}

func (e *entry) advanceReveal(slots int, snap bool) bool {
	if e == nil {
		return false
	}
	if !advanceReveal(&e.edge, e.text, slots, snap || e.settled) {
		return false
	}
	e.stale = true
	return true
}

func (e *entry) revealing() bool {
	if e == nil {
		return false
	}
	return revealing(e.edge, e.text, e.settled)
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
			if row.kind == exchangeReply && revealing(row.edge, row.text, row.settled) {
				return true
			}
		}
	}
	return a.meterChasing
}

// slotsSince is how many slots of the paint clock have passed between two
// readings of the surface's clock. It takes both times rather than asking the
// wall clock, which is what keeps [time.Now] out of this file entirely.
//
// A PART SLOT IS NO SLOT. The remainder is left on the stamp by the caller, so
// a frame that fires early moves nothing and the time it was early by is not
// lost — it is spent by the next frame.
func slotsSince(from, now time.Time) int {
	if from.IsZero() {
		return 0
	}
	d := now.Sub(from)
	if d < frameInterval {
		return 0
	}
	return int(d / frameInterval)
}

// tickReveal is one turn of the edge and the meters.
//
// IT IS CALLED FROM THE PAINT AND IS NOT PACED BY IT. `now` is the surface's
// own clock ([app.now]); how far the walk has got is the time since it last
// moved, so a link that paints ten times a second walks three slots per frame
// and the machine that paints thirty walks one, and both finish a lump in the
// same quarter of a second. [app.revealMoved] carries the remainder, so a frame
// that arrives early moves nothing rather than rounding a slot into existence.
func (a *app) tickReveal(now time.Time) {
	snap := a.linear
	// A WALK WITH NO CLOCK BEHIND IT WOULD NEVER FINISH. [app.wake] starts the
	// stamp with the frames, but a surface that was handed an event before it
	// ever woke has none — and a zero stamp measures no slots, which would
	// freeze the edge exactly where it opened. The walk starts here instead.
	if a.revealMoved.IsZero() {
		a.revealMoved = now
		return
	}
	slots := slotsSince(a.revealMoved, now)
	if slots <= 0 && !snap {
		return
	}
	a.revealMoved = a.revealMoved.Add(time.Duration(slots) * frameInterval)
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
		if advanceReveal(&row.edge, row.text, slots, snap || row.settled) {
			a.dirty = true
		}
	}
	a.tickMeters(slots, snap)
}

// armMeters starts the figures chasing, FROM the readings a person is looking at
// rather than from the ones that have just landed. Every caller therefore hands
// in what was on the screen a moment ago; nothing here reads the books, because
// by the time this is called the books have already moved.
//
// IT DECLINES OUTSIDE A RUNNING TURN, which is the snap rule said once instead
// of at every call site: a restore, a switch, a settle and the screen-reader
// tier all want the exact figure, because nobody is watching those numbers grow.
// It also declines while a chase is already running — that chase is already
// walking toward whatever the books now say, and re-arming it would drag the
// figure backwards to where it started.
func (a *app) armMeters(cost float64, tokens, ctx int) {
	if a.linear || a.meterChasing || a.state != stateWorking {
		return
	}
	a.shownCost, a.shownTokens, a.shownCtx = cost, tokens, ctx
	a.meterChasing = true
}

// snapMeters puts the drawn figures on the books and stops the chase. It is the
// other half of the rule and the only way a chase ever ends.
func (a *app) snapMeters() {
	a.shownCost, a.shownTokens, a.shownCtx = a.spendShown(), a.tokens, a.ctxTokens
	a.meterChasing = false
}

// tickMeters walks the drawn cost, token total and context weight toward
// the books. It chases only while a turn is running and something has asked
// it to ([app.take] sets the flag); every other reading — a restore, a
// switch, a test that wrote the field directly — is the exact figure, so a
// status line drawn on the first frame of a resumed conversation is the
// bill that conversation already had.
func (a *app) tickMeters(slots int, snap bool) {
	if !a.meterChasing || snap || a.state != stateWorking {
		a.snapMeters()
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
	if !a.chasingMeters() {
		return a.spendShown()
	}
	return a.shownCost
}

// tokensDrawn is the session's token total in motion — the figure the task
// column's foot paints beside the bill (task.go's [app.railFootRows]), which is
// the one place on this surface a running total of tokens is drawn while it is
// still growing. /status prints the exact books instead, as it does for money.
func (a *app) tokensDrawn() int {
	if !a.chasingMeters() {
		return a.tokens
	}
	return a.shownTokens
}

// ctxDrawn is the conversation weight the meter paints, on the same terms.
func (a *app) ctxDrawn() int {
	if !a.chasingMeters() || a.shownCtx == 0 {
		return a.ctxTokens
	}
	return a.shownCtx
}

// chasingMeters reports whether a figure in motion is the one to draw.
//
// A FIGURE ONLY MOVES WHILE THE TURN DOES, AND THE STATE SAYS SO RATHER THAN
// A FRAME. [app.tickMeters] snaps the drawn figures the moment the turn stops
// working, but it only runs when a frame does — and the frame clock stops when
// there is nothing left animating. A turn that settled on its last paint would
// leave [app.meterChasing] set over a figure that had not finished easing, and
// the status line would draw that stale reading for as long as the surface sat
// idle. Asking the state here means the exact bill is drawn on the instant the
// turn ends, whether or not another frame ever arrives. It is livestate.go's
// law in the status line's column: a thing stops pacing when it stops moving.
func (a *app) chasingMeters() bool {
	return a.meterChasing && a.state == stateWorking
}
