package provider

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── DRIVING ONE ARM'S CONTROLLER FROM THE READ LOOP ─────────────────────────

// streamWatch is what the read loop reports to: one arm's controller, the arm
// it belongs to, and the lock that makes it safe to drive from the loop and
// from the silence beat at the same time.
//
// It is the ONLY thing the read loop knows about waiting. Every hook in
// client.go is a nil-safe method call on this type, so a call with no
// controller installed pays one nil check per delta and nothing else.
type streamWatch struct {
	race *hedgeRace
	arm  int

	mu      sync.Mutex
	control control.Controller
	// deadline is the moment the controller last said it wanted waking at. The
	// beat sleeps until it rather than polling, so it is compared on every
	// reading and the beat is kicked only when it MOVES.
	deadline time.Time
	// armed is the first deadline this arm was given, as a duration from when
	// it went out. It is what the call log's `deadline_ms` records: the rows
	// where a deadline was set and not reached are what say the deadline was
	// set in the right place.
	armed time.Duration
	// tokens is every delta of progress this arm has delivered and visible the
	// same count restricted to the ones a person can read. Hidden work keeps a
	// stream alive and buys no commitment: it is money already spent and
	// nothing anybody would watch disappear.
	tokens  int
	visible int
	// beats is how many router comment lines arrived: proof about the PATH and
	// about nothing else, which is exactly what tells a dead path from a slow
	// lane.
	beats int
	// served is the lane the stream named, and began, first and last the timing
	// this arm's sighting is built from.
	served string
	began  time.Time
	first  time.Time
	last   time.Time
	gap    time.Duration
	// acted records what was done about this arm's wait and the numbers it was
	// decided on, for the row.
	acted   control.Act
	silence time.Duration
	fault   bool
}

type streamWatchContextKey struct{}

func withStreamWatch(ctx context.Context, watch *streamWatch) context.Context {
	return context.WithValue(ctx, streamWatchContextKey{}, watch)
}

// streamWatchFrom is the watch driving this call, nil on every call that is not
// an arm of a race.
func streamWatchFrom(ctx context.Context) *streamWatch {
	watch, _ := ctx.Value(streamWatchContextKey{}).(*streamWatch)
	return watch
}

// note folds one moment of the stream into the controller and does whatever it
// says.
//
// ONE READING PER EVENT, AND THE THREE COUNTS ARE NOT INTERCHANGEABLE.
// [control.Reading.Visible] is text on the screen and is the only thing that
// can reset the deadline while its measured rate keeps up; Hidden is the
// endpoint writing where nobody can read; Beat is the router's own comment
// line, which proves the path is alive and never that the model has started. A
// clock a beat reset would be a clock a router could hold open forever by
// saying nothing in a well-formed way.
func (w *streamWatch) note(reading control.Reading) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if reading.Beat {
		w.beats++
	}
	if reading.Visible > 0 || reading.Hidden > 0 {
		if w.first.IsZero() {
			w.first = reading.At
		} else if gap := reading.At.Sub(w.last); gap > w.gap {
			w.gap = gap
		}
		w.last = reading.At
		w.tokens += reading.Visible + reading.Hidden
		w.visible += reading.Visible
	}
	spoke := reading.Visible > 0
	act := w.after(w.control.Note(reading), reading.At)
	arm, race := w.arm, w.race
	w.mu.Unlock()

	// FIRST VISIBLE PROGRESS TAKES THE VOICE. Whichever arm writes the first
	// word a person can read is the arm they hear; the rest are held.
	if spoke {
		race.voice(arm)
	}
	race.act(arm, act)
}

// quiet is the beat: nothing has arrived by now, and the controller is asked
// the same question it is asked of every reading.
func (w *streamWatch) quiet(now time.Time) {
	if w == nil {
		return
	}
	w.mu.Lock()
	act := w.after(w.control.Quiet(now), now)
	arm, race := w.arm, w.race
	w.mu.Unlock()
	race.act(arm, act)
}

// after records what the controller said, names the fault where there is one,
// and moves the deadline. It runs with the lock held, from the two callers above
// and from nowhere else, and it hands back the act as it will be reported.
//
// THE PATH CLAIM IS NOT THE CONTROLLER'S. It knows how long a silence has run
// and what it would cost to act; it does not know the difference between an
// endpoint that is thinking and a connection that never opened, because a
// heartbeat is proof about the path and moves nothing in the arithmetic. This
// layer is where both facts are held, so this is where they are joined.
func (w *streamWatch) after(act control.Act, now time.Time) control.Act {
	if act.Kind != control.None {
		// AN ACT TAKEN WITH NO SIGN OF LIFE AT ALL IS ABOUT THE PATH. A router
		// that is working says so in comments long before the model does, so a
		// stream with neither a comment nor a byte says nothing about the
		// machine at the other end and must not be charged to it — and the row
		// says which of the two it was, because "this lane is slow" and "this
		// connection never opened" are different autopsies.
		dead := w.tokens == 0 && w.beats == 0 && act.Silence >= lanes.DeadPathFloor
		if dead {
			act.Reason = pathFaultReason
		}
		if w.acted.Kind == control.None {
			w.acted, w.silence, w.fault = act, act.Silence, dead
		}
	}
	if next := w.control.Deadline(); !next.Equal(w.deadline) {
		w.deadline = next
		w.race.rearm()
	}
	return act
}

// pathFaultReason is what a row says when nothing at all reached this stream.
//
// THE FLOOR UNDER THE CLAIM IS [lane.DeadPathFloor] and it is what separates the
// two readings. A lane that has said nothing for thirty milliseconds is a lane
// that is slow; one that has said nothing at all for seconds — no token, no
// comment — is a connection that never opened, and a handshake on a cold path
// can honestly take longer than a fast lane's whole believed wait.
const pathFaultReason = "no heartbeat"

// opened is the moment this arm's request really went out, taken from the
// stream loop's own reading so that nothing here spends a clock read of its
// own.
func (w *streamWatch) opened(at time.Time) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.began.IsZero() {
		w.began = at
	}
}

// serve records who the stream said was answering it, and re-points the
// controller at the machine that is really writing.
//
// A ROUTER HONOURS ANY OF AN ORDER. Until the first chunk names a lane the only
// belief anybody holds is the head of the order's, and every gap after that
// would be judged as a surprise about a machine that was never asked. The
// ledger read is memory only by its own contract, which is what makes it safe
// on the read loop.
func (w *streamWatch) serve(lane string) {
	if w == nil {
		return
	}
	lane = strings.TrimSpace(lane)
	w.mu.Lock()
	first := w.served == "" && lane != ""
	if first {
		w.served = lane
	}
	model := ""
	if w.race != nil {
		model = w.race.model
	}
	w.mu.Unlock()
	if !first || model == "" {
		return
	}
	now := waitNow()
	pace := lanes.PaceFor(lanes.ID{Model: model, Lane: lane}, now)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.control.Serving(lane, pace.First, pace.Gap, now)
	if next := w.control.Deadline(); !next.Equal(w.deadline) {
		w.deadline = next
		w.race.rearm()
	}
}

// sighting is what this arm measured, and whether it is worth writing down.
//
// A stream that never named a lane is anonymous and is dropped under the
// attribution law: crediting an unnamed measurement to some lane is how a
// ledger learns a fact about a machine that was not involved. A stream acted on
// for a dead path is dropped for the other reason — there is no fact about the
// machine in it at all.
func (w *streamWatch) sighting(model string, tokens int) (lanes.Sighting, bool) {
	if w == nil {
		return lanes.Sighting{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.served == "" || w.first.IsZero() || w.fault {
		return lanes.Sighting{}, false
	}
	if tokens <= 0 {
		tokens = w.tokens
	}
	return lanes.Sighting{
		ID:     lanes.ID{Model: model, Lane: w.served},
		TTFT:   w.first.Sub(w.began),
		Gen:    w.last.Sub(w.first),
		Gap:    w.gap,
		Tokens: tokens,
		At:     w.last,
	}, true
}

// consequence is the moment this arm will be acted on and the lane the act
// would go to, both empty on a request with nowhere to go.
//
// IT IS WHAT THE PHASE CLOCK IS ALLOWED TO PROMISE. A countdown on the screen
// has to be a moment at which this build really acts, and this pair is the only
// place in the process where that moment exists. A rescue nobody can afford is
// not a consequence: the purse is what finally decides whether a second request
// goes out, and a countdown drawn over one that was always going to be refused
// is a countdown that expires and does nothing.
func (w *streamWatch) consequence() (time.Time, string) {
	if w == nil || w.race == nil {
		return time.Time{}, ""
	}
	alt, affordable := w.race.affordableAlt()
	if !affordable {
		return time.Time{}, ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.deadline, alt
}

// canWalk reports whether this request will start another machine behind the
// same model when its refusal reaches the race.
//
// It is what stops the relax ladder from running too early: a refusal is
// evidence about ONE endpoint, and the ladder's rungs are about the request
// itself. See [Client.sendRecovered] for the whole argument.
func (w *streamWatch) canWalk() bool {
	if w == nil || w.race == nil {
		return false
	}
	return w.race.canWalk()
}

// speaking reports whether this arm is the one the person is hearing.
//
// An unwatched stream always is — there is nobody else. An arm of a race is
// only while it holds the voice, which is the same rule [hedgeRace.emit] keeps
// about the deltas themselves: one request is one story, and a story told from
// the arm whose text is being HELD would be about words nobody is reading.
func (w *streamWatch) speaking() bool {
	if w == nil || w.race == nil {
		return true
	}
	return w.race.hears(w.arm)
}

// quietFor is how long this arm has been silent, spelled the way a person says
// it, and empty when it has never written at all — a stream that never started
// is not a stream that stopped.
func (w *streamWatch) quietFor(now time.Time) string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	quiet := now.Sub(w.last)
	if w.last.IsZero() {
		quiet = now.Sub(w.began)
	}
	if quiet < time.Second {
		return ""
	}
	return strconv.Itoa(int(quiet.Round(time.Second)/time.Second)) + "s"
}

// lane is who answered this arm, empty when nothing said.
func (w *streamWatch) lane() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.served
}

// written is how many tokens this arm delivered.
func (w *streamWatch) written() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tokens
}
