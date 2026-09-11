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
	// it went out. It is what the call log's `hazard_ceiling_ms` records: the rows
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
	// applied is THE BOUND THAT ACTUALLY ENDED THIS ATTEMPT and appliedWord is
	// what to call it, both zero on an attempt no bound ended.
	//
	// THEY EXIST BECAUSE `deadline_ms` IS FICTION. [streamWatch.armed] above is
	// the hazard's FIRST deadline — when this arm was going to start thinking
	// about a second machine — and the log has been recording it under a name
	// that reads as "when this call was going to be ended". The census of
	// 2026-09-10 measured what that costs: 2,720 of 11,841 finished attempts
	// ran more than twice their recorded `deadline_ms`, the field reads 10,000
	// on 7,937 rows, and the worst row is `deadline_ms 10000` against `ms
	// 937777`. Six hundred rows cut by a bound set OUTSIDE this package were
	// read by that census as the stream wall cutting live streams, which the
	// wall never did.
	//
	// SO THE TWO FACTS ARE SEPARATE FIELDS AND THE ROW MAY CARRY BOTH: what was
	// planned, and what happened. This pair is filled in by the guard when one
	// of its bounds fires ([stallWatch] through [streamWatch.boundApplied]) and
	// is left empty by every attempt that ended for any other reason — an
	// answer, a refusal, the caller leaving — because an empty here is the
	// honest reading of "no bound of ours ended this".
	applied     time.Duration
	appliedWord string
	// lost says this arm was cancelled because another arm answered first.
	//
	// IT IS THE DIFFERENCE BETWEEN EXHAUST AND FAILURE. A losing arm's request
	// really was made and really was cut off, so it writes an end row like
	// every other attempt — and that row says `context canceled`, which every
	// reading of the log has counted as a failure. It is 1,204 of 3,906 bad
	// rows in the 2026-09-10 census, the single largest "cause family" in it,
	// and not one of them is a thing that went wrong: they are the price of a
	// race this build chose to run and won. A census that counts them as
	// failures is measuring its own hedging policy and calling it provider
	// health.
	lost bool
	// Recovery suspends the controller; recovered calls cannot teach lane timing.
	recovering bool
	recovered  bool
	// guard is the silence watch over this same request, and free says the
	// ceiling has decided to use it. See THE CEILING PICKS ONE OR THE OTHER,
	// NEVER NEITHER below.
	guard *stallWatch
	free  bool
	// progress is whoever asked to watch this call run (callprogress.go), and
	// nil on every call nobody is watching — which is nearly all of them.
	//
	// IT IS SET ONCE, BEFORE THIS WATCH IS SHARED ([withStreamWatch] below, from
	// the goroutine that goes on to start the arm), so it is read without the
	// lock: taking the lock to reach it would put the reader's callback inside
	// the critical section the read loop and the beat are already contending
	// for, which is the one thing a seam called per delta may not do.
	progress *callProgress
}

// ── THE CEILING PICKS ONE OR THE OTHER, NEVER NEITHER ───────────────────────
//
// A hedge is the PAID way to act on a silence — a second request, out of a purse
// that is deliberately small (the waiting design's §B: two rescues per twenty
// calls). A cut is the FREE way: end this attempt and let the layer above ask
// another machine at once, paying the prompt again and nothing else.
//
// UNTIL THIS WAVE THE CEILING COULD CHOOSE NEITHER, and that is a hole rather
// than a trade-off. `docs/design/waiting/DESIGN.md` §A clause 1 says the role's
// ceiling is hard "regardless of belief"; it has to be hard regardless of PURSE
// too, or the clause means "regardless of belief, when we happen to be able to
// afford it". Measured on 2026-09-10: 2,186 attempts fired the ceiling, had the
// purse refuse the arm, and then had NOTHING act — 648 of them went on for more
// than six times the silence that had just been refused, to a ninety-ninth
// percentile of 272 seconds and a worst case of 938. Four quick tasks that
// evening waited on one machine for six and seven MINUTES before its first
// token, every one of them ten seconds past a ceiling that had already fired.
//
// BUT THE FREE MOVE IS NOT FREE OF REGRET, so it is not taken on every refusal,
// and the discriminator is one this layer already computes. Of the 2,186:
//
//	reason           n      ended cleanly anyway
//	drift          1,268    92 %
//	ceiling          518    75 %
//	no heartbeat     250    31 %
//
// Cutting the first two would throw away nine calls in ten that were about to
// answer and pay every one of their prompts again — strictly worse than waiting,
// which is why the purse exists at all. `no heartbeat` is the other animal
// entirely: it is [pathFaultReason], meaning not one byte has reached this
// stream — no token, not even a router comment — and across the whole log those
// attempts end cleanly 24 % of the time with a first token at the ninety-ninth
// percentile of 505 seconds. Cutting them at the ceiling forfeits about three
// calls in ten and rescues seven from waits measured in minutes.
//
// So: the ceiling acts. It hedges when it can afford to; when it cannot, and the
// wire has said NOTHING AT ALL, it cuts and the next machine gets the question.
//
// AND THE CUT NEEDS NOTHING NEW TO CARRY IT. It is an ordinary [StreamCut], so
// it leaves through the ordinary door: client.go stamps it with the machine the
// stream named and the ledger's strike sets [StreamCut.Rerouted], which the
// attempt loop already reads as a no-backoff move that vetoes the served
// endpoint (`TestABlindCutIsFiledAgainstTheMachineWeAskedFor` is that shape, and
// a cut that named nobody falls back to the machine the request asked for). So
// the free move IS a move to another machine and not merely an early ending.

type streamWatchContextKey struct{}

func withStreamWatch(ctx context.Context, watch *streamWatch) context.Context {
	// AND THIS IS WHERE THE QUESTION'S REPORTER IS PICKED UP, because it is the
	// one line in the process that has both the caller's context and the arm that
	// is about to be run from it (callprogress.go). Doing it here rather than at
	// the construction site is what lets any caller attach the seam without
	// hedge.go being changed for them — and the reporter is the QUESTION'S, so
	// both arms of a race report into one row under their own arm numbers.
	if watch != nil && watch.progress == nil {
		watch.progress = callProgressOn(ctx)
	}
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
	// The two counts as they now stand, read here rather than by the seam below
	// so that nothing outside this package ever takes this lock. Hidden is the
	// remainder because [streamWatch.tokens] is both channels together.
	visible, hidden := w.visible, w.tokens-w.visible
	w.mu.Unlock()
	// AND WHOEVER ASKED TO WATCH THIS CALL HEARS THE SAME MOMENT. It is the one
	// forwarding this seam needs: every streamed delta in the process reaches
	// this method, so there is no second decoder and no second count
	// (callprogress.go).
	//
	// A BEAT IS NOT A FIRST TOKEN. The router's comment line proves the path and
	// nothing else, exactly as it moves nothing in the controller above; passing
	// it on would have a surface announce that the model had started writing
	// because the gateway said hello.
	if reading.Visible > 0 || reading.Hidden > 0 {
		w.progress.note(w.arm, reading.At, visible, hidden)
	}
	w.takeFreeMove()

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
	if w.recovering {
		w.mu.Unlock()
		return
	}
	act := w.after(w.control.Quiet(now), now)
	arm, race := w.arm, w.race
	w.mu.Unlock()
	w.takeFreeMove()
	race.act(arm, act)
}

// takeFreeMove ends the attempt the ceiling gave up on, OUTSIDE every lock.
//
// The cut runs here rather than in [streamWatch.after] for the reason
// [stallWatch.fire] gives about its own: cancelling a context runs whatever the
// caller hung off it, and this process's locks may not be underneath somebody
// else's code. It is idempotent — the guard refuses a second trip — so both
// callers may run it unconditionally.
func (w *streamWatch) takeFreeMove() {
	if w == nil {
		return
	}
	w.mu.Lock()
	take, guard, waited := w.free, w.guard, w.silence
	w.free = false
	w.mu.Unlock()
	if !take || guard == nil {
		return
	}
	guard.cutIdle(waited)
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
			// AND THE CEILING ACTS EVEN WHEN THE PURSE SAYS NO. A report is the
			// controller saying it has weighed a second request and will not
			// make one — because everything is believed slow, or because the
			// purse is empty. On a path fault that leaves the question with a
			// machine that has sent nothing at all, so the free move is taken
			// instead: cut here, and the layer above asks somewhere else. See
			// THE CEILING PICKS ONE OR THE OTHER, NEVER NEITHER.
			if act.Kind == control.Report {
				w.free = true
			}
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
	if first {
		// The machine that is really answering, forwarded to whoever is watching
		// this call run (callprogress.go). It is said once per request.
		w.progress.serving(w.arm, lane)
	}
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
	// A PATH FAULT THAT LATER PRODUCED TOKENS WAS NOT A PATH FAULT. The claim
	// is made the moment something is acted on, from what had arrived by then —
	// nothing — and a stream that went on to write from a named machine has
	// disproved it. Suppressing that measurement is how the WORST first tokens
	// this build has ever seen taught the ledger nothing: four streams on one
	// machine on 2026-09-10 took 260, 370, 375 and 428 seconds to their first
	// token and every one of them was dropped here, so the sheet went on saying
	// that machine answers in eight. A late first token from a named lane is a
	// fact about that lane, and [LagTTFT] is already the line it falls the wrong
	// side of.
	if w.served == "" || w.first.IsZero() || w.recovered {
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

// walkAvailable reports whether this request would start another machine behind
// the same model if its refusal reached the race.
//
// IT IS A READING AND NOT A HANDOFF ([hedgeRace.walkAvailable] states the
// difference and why the two must not share a call). Its one caller is the
// attempt loop deciding whether to keep replaying an encoded body.
func (w *streamWatch) walkAvailable() bool {
	if w == nil || w.race == nil {
		return false
	}
	return w.race.walkAvailable()
}

// takeRefusal hands this arm's routing refusal to the race and reports whether
// the race took it.
//
// IT IS ONE CALL BECAUSE IT IS ONE DECISION. The pair it replaces — ask whether
// the walk can carry this, then record that you relied on the answer — was a
// prediction and a promise about it, and everything that can change between two
// lock acquisitions could make the second false. What comes back now is a
// commitment: the race owns this refusal and will either answer it or climb the
// ladder this call just deferred ([hedgeRace.takeRefusal]).
//
// A call with no race has nobody to hand to and answers false — its door climbs
// the ladder itself, which is the legal empty state of a build with no
// controller installed.
func (w *streamWatch) takeRefusal(body []byte) bool {
	if w == nil || w.race == nil {
		return false
	}
	return w.race.takeRefusal(body)
}

// ranLadder tells the race that this arm's door climbed the ladder itself, so
// nothing is owed: a question may climb it once, never twice.
func (w *streamWatch) ranLadder() {
	if w == nil || w.race == nil {
		return
	}
	w.race.ranLadder()
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

// boundApplied records the bound that ended this attempt, from the guard that
// fired it. It is the honest half of the pair [streamWatch.armed] is the
// planned half of, and it is written once: the first bound to fire is the one
// that ended the stream and a second could only be a timer unwinding behind it.
//
// A CUT IS THE ONLY THING THAT KNOWS ITS OWN FIGURE. [StreamCut.Waited] is the
// constant the timer was actually set to, role band and lane derivation
// included, and [CutReason.word] is the ledger's name for it — so the two
// fields are taken from the cut rather than recomputed here, and a row can be
// checked against the decision it records.
func (w *streamWatch) boundApplied(cut *StreamCut) {
	if w == nil || cut == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.appliedWord != "" {
		return
	}
	w.applied, w.appliedWord = cut.Waited, cut.Reason.word()
}

// lostRace marks this arm as the exhaust of a race another arm won, so its end
// row is not read as a failure. See [streamWatch.lost].
//
// IT IS CALLED FROM THE CANCEL SITE and from nowhere else: the race is the only
// thing that knows an arm was cut off rather than failed, and this layer is the
// only thing the row is composed from.
func (w *streamWatch) lostRace() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lost = true
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

// callOpened and callLanded are the two ends of ONE ATTEMPT, forwarded to
// whoever asked to watch this call run (callprogress.go).
//
// THEY ARE DRIVEN FROM THE MODEL-CALL LOG'S OWN TWO ROWS (calllog.go's
// [Client.record]) and from nowhere else, because those rows are the two
// moments this whole process already agrees on: the log's law is that every
// attempt writes a start row and exactly one row that ends it, kept by a door
// no path can return past. A seam with its own idea of when a request went out
// would be a second answer to a question that already has one, free to disagree
// with the row a person autopsies.
func (w *streamWatch) callOpened(at time.Time) {
	if w == nil {
		return
	}
	w.progress.opened(w.arm, at)
}

func (w *streamWatch) callLanded(end CallEnd, err error) {
	if w == nil {
		return
	}
	w.progress.landed(w.arm, end, err)
}

// callPaced is this call parking on a provider's pacing, and leaving that park
// (dispatch.go, which flips both this and the older bool from one site).
func (w *streamWatch) callPaced(parked bool, at time.Time) {
	if w == nil {
		return
	}
	w.progress.paced(w.arm, parked, at)
}

// guardedBy hands this arm the silence watch over the same request, so a
// ceiling the purse refused has something to act WITH. It is set once, by the
// guard's own constructor, and is nil on every call with no guard.
func (w *streamWatch) guardedBy(guard *stallWatch) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.guard = guard
}
