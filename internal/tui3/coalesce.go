package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// THE INPUT STORM, AND THE ONE ANSWER IT IS ALLOWED.
//
// A pointer swept across this window sends ONE MESSAGE PER CELL it crosses. A
// slow diagonal over a hundred-column terminal is two hundred messages in the
// time it takes to move a hand; a fast one is six hundred, and they arrive in
// ONE WRITE — the terminal fills the pipe and the surface reads a burst.
//
// The positions in between are not information. They are the same claim, made
// six hundred times, and every one but the last of them was already false when
// it was read. So this file folds them: the newest position is kept, the rest
// are dropped, and the surface answers ONCE PER FRAME.
//
// WHAT IT COSTS TO ANSWER THEM ALL, MEASURED. Answering a motion is cheap now —
// reasoninglevel.go took the far machine out of the pointer's way, and PERF.md's
// "connection laws" hold it there. What is NOT cheap is that Bubble Tea builds a
// frame after every message it delivers and writes one every sixtieth of a
// second, so a burst is six hundred frames built for the sixty a person could
// possibly have seen. On the loopback client through a 20ms round trip, a
// character typed straight after a six-hundred-motion write took 45ms to appear
// and after three thousand, 204ms; with the fold, 13ms and 16ms — the same as
// with no motion at all. The BEFORE column is linear in the burst and the AFTER
// column is flat, which is the whole claim: THE COST OF A STORM NO LONGER
// DEPENDS ON HOW BIG THE STORM IS, and a per-message cost added back tomorrow
// cannot resurrect the stall.
//
// WHAT MAY NEVER BE FOLDED IS A KEY. Keys are meaning, one apiece, and they are
// ordered with respect to each other and to everything else — a `q` behind a
// `:` is a different message from a `q` in front of one. So no key is ever
// coalesced, none is ever reordered, and — the part that made this file worth
// writing — A KEY NEVER WAITS BEHIND A SWEEP. Folding a motion costs a struct
// copy and an integer, so the six hundredth motion of a storm and the keystroke
// behind it are handled in the same frame the keystroke arrived in.
// [TestSixHundredMotionsThenAKeyCostOneSweepAndTheKey] is that law, counted.
//
// WHY A FOLD AND NOT A DRAIN. internal/session's stream is folded by taking
// events off a channel until it would block (app.go's [waitEvent]), which is
// the honest way to coalesce a backlog: the backlog is IN HAND. A Bubble Tea
// program has no such channel to reach — `Program.msgs` is unbuffered and
// private, the input reader hands over one message at a time and blocks until
// the model has taken it, and the rest of a storm is unparsed bytes in the
// terminal's own pipe. There is nothing queued to drain and no way to look
// ahead. So the fold is made the only way it can be made from inside a model:
// forward, by keeping the newest position and answering it on a clock of its
// own.
//
// AND THE FIRST MOTION IS NEVER FOLDED. A pointer ARRIVING somewhere — the
// first motion after a key, a press, a frame, a token — is answered where it
// stands, instantly, with the whole of what it always cost and nothing added;
// it is the SECOND motion in a row that says a sweep is happening and opens the
// window the rest of it collapses into. Somebody moving a pointer onto a row
// and stopping pays nothing for this file. Somebody sweeping across the screen
// pays one answer per frame.

// pointerEvery is how often a sweeping pointer is answered, and it is
// [frameInterval] because the answer is only ever seen in a frame: a hover
// resolved twice between two paints is a hover resolved once, and the reader
// saw neither of them. It is not a second cadence on this surface — it is the
// one this surface already has, asked for by the one message that arrives far
// faster than it.
const pointerEvery = frameInterval

// pointerFold is the sweep's buffer: what the pointer has done that the router
// has not been told about yet.
type pointerFold struct {
	// last is the newest position the pointer reached, and have says there is
	// one waiting. There is never more than one, because a position that has
	// been overwritten was never worth keeping.
	last tea.MouseMotionMsg
	have bool

	// wheel is the run of notches a wheel is being turned through and notches is
	// how many of them are still owed. A run is one button at one cell: a turn
	// that changes direction, or crosses onto something else, is a different
	// gesture and spends what the last one owed before it starts.
	wheel   tea.MouseWheelMsg
	notches int

	// moving and turning say the LAST message this surface handled was a motion,
	// or a notch of the run in hand, which is what makes the next one part of a
	// gesture rather than the start of one. Every other message clears them — a
	// pointer that stopped long enough for anything else to happen has arrived
	// somewhere.
	moving  bool
	turning bool

	// settling says a [pointerMsg] is already on its way, so a storm asks for
	// one wakeup and not six hundred. It is [app.painting]'s idea, held
	// separately because the two clocks mean different things.
	settling bool

	// still says the message just handled mutated NOTHING the frame reads — it
	// went into the fold and stopped there — so the frame Bubble Tea is about to
	// ask for is the frame it was given last time (view.go's [app.View]). It is
	// set by the three paths below that provably touch nothing but the fields of
	// this struct, and cleared by every other message there is.
	still bool

	// folded counts the messages this surface never had to answer and answered
	// counts the ones it did. They exist so a test can state the ceiling as a
	// COUNT — six hundred motions cost two answers — rather than as a stopwatch,
	// which PERF.md's doctrine forbids.
	folded, answered int
}

// update is the fold, and it does exactly two things: it takes the pointer's
// storms, and it hands everything else to [app.route] untouched, in the order it
// arrived. It is what [app.Update] calls (app.go).
//
// KEYS ARE NOT MENTIONED HERE, and that is the point. A key falls straight
// through to the router, ahead of any sweep still folded behind it, because the
// fold holds a POSITION and not a queue: there is nothing for a key to wait for.
func (a *app) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The frame memo is off until something earns it back, so a path that forgets
	// to say it changed nothing draws a frame rather than skipping one.
	a.ptr.still = false
	switch msg := msg.(type) {
	case tea.MouseMotionMsg:
		return a, a.pointerMoved(msg)

	case tea.MouseWheelMsg:
		return a, a.wheelTurned(msg)

	case pointerMsg:
		return a, a.pointerSettled()

	case tea.MouseClickMsg, tea.MouseReleaseMsg:
		// A PRESS IS ANSWERED WHERE THE SWEEP ENDED. The press carries its own
		// cell so it would land in the right place either way, but the surface
		// under it does not: a drag beginning on a row whose hover the fold is
		// still holding would start from the row before it. So the fold is spent
		// first, in the order the two arrived.
		spent := a.pointerSpend()
		a.ptr.moving, a.ptr.turning = false, false
		_, cmd := a.route(msg)
		return a, tea.Batch(spent, cmd)
	}
	// ANYTHING ELSE ENDS THE GESTURE IT INTERRUPTED. A key, a frame, a token, a
	// resize: the pointer has been still for as long as that took, so the next
	// motion is somebody arriving somewhere and is answered on the spot.
	a.ptr.moving, a.ptr.turning = false, false
	return a.route(msg)
}

// pointerMoved is the fold itself.
func (a *app) pointerMoved(msg tea.MouseMotionMsg) tea.Cmd {
	if !a.ptr.moving && !a.ptr.settling {
		// THE POINTER ARRIVING, not sweeping.
		a.ptr.moving = true
		a.ptr.answered++
		_, cmd := a.route(msg)
		return cmd
	}
	// A SWEEP. The position the fold held before this one was already false, so
	// it is dropped rather than answered.
	if a.ptr.have {
		a.ptr.folded++
	}
	a.ptr.last, a.ptr.have = msg, true
	a.ptr.still = true
	return a.pointerWake()
}

// wheelTurned folds a run of notches the same way. A wheel is coarser than a
// pointer — a notch is three rows, not one cell — but it arrives in the same
// shape: somebody spinning a trackpad sends a notch every few milliseconds, and
// every one of them used to lay the list out again for a frame nobody was shown.
//
// THE NOTCHES ARE KEPT AND NEVER AVERAGED. Scrolling is not a position, it is a
// distance, so folding a run means owing its whole length: the run is spent by
// putting every notch of it through the router at the frame, which moves
// exactly as far as the notches asked and lays the frame out once.
func (a *app) wheelTurned(msg tea.MouseWheelMsg) tea.Cmd {
	if !a.ptr.turning || !sameNotch(a.ptr.wheel, msg) {
		// The wheel arriving, or turning somewhere else, or turning back: the run
		// this one interrupted is spent first, and this notch goes through.
		spent := a.pointerSpend()
		a.ptr.turning, a.ptr.wheel, a.ptr.notches = true, msg, 0
		a.ptr.answered++
		_, cmd := a.route(msg)
		return tea.Batch(spent, cmd)
	}
	a.ptr.notches++
	a.ptr.folded++
	a.ptr.still = true
	return a.pointerWake()
}

// sameNotch reports whether two wheel messages are the same gesture continuing:
// the same button, at the same cell, under the same modifiers. Anything else is
// a different scroll, possibly of a different list.
func sameNotch(prev, next tea.MouseWheelMsg) bool {
	p, n := prev.Mouse(), next.Mouse()
	return p.Button == n.Button && p.X == n.X && p.Y == n.Y && p.Mod == n.Mod
}

// pointerWake asks for the one wakeup a whole storm gets.
func (a *app) pointerWake() tea.Cmd {
	if a.ptr.settling {
		return nil
	}
	a.ptr.settling = true
	return tea.Tick(pointerEvery, func(time.Time) tea.Msg { return pointerMsg{} })
}

// pointerSettled is the frame boundary: whatever the gesture piled up since the
// last one is spent here, and nowhere else.
func (a *app) pointerSettled() tea.Cmd {
	a.ptr.settling = false
	if a.ptr.notches == 0 && !a.ptr.have {
		// The gesture ended inside the last frame, and this is the frame that
		// found out. The next motion is somebody arriving somewhere again.
		a.ptr.moving, a.ptr.turning = false, false
		a.ptr.still = true
		return nil
	}
	// ONE MORE FRAME IS ALWAYS ASKED FOR after a frame that spent something,
	// because a sweep's true end cannot be seen from inside it: the last motion
	// of a gesture looks exactly like the middle of one, and only a frame that
	// finds nothing waiting proves the pointer stopped.
	return tea.Batch(a.pointerSpend(), a.pointerWake())
}

// pointerSpend hands the router everything the fold is holding, in the order it
// arrived: the notches first, then the position the pointer ended at. It does
// nothing at all — and returns nil — when the fold is empty, which is the case
// on every message this surface handles while nobody is sweeping.
func (a *app) pointerSpend() tea.Cmd {
	if a.ptr.notches == 0 && !a.ptr.have {
		return nil
	}
	var cmds []tea.Cmd
	// THE NOTCHES ARE REPLAYED WHOLE. Each is the message the router already
	// knows how to answer, put back through the same door it came in by, so a
	// folded run scrolls precisely as far as an unfolded one would have. What is
	// saved is the frames in between, which nobody was ever shown.
	for owed := a.ptr.notches; owed > 0; owed-- {
		a.ptr.answered++
		if _, cmd := a.route(a.ptr.wheel); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	a.ptr.notches = 0
	if a.ptr.have {
		a.ptr.have = false
		a.ptr.answered++
		if _, cmd := a.route(a.ptr.last); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}
