package session

// THE SECOND LOOK: a steer that arrived while the command was still young.
//
// [steerBashAge] is a bargain about the COMMAND — a two-second `git status` may
// finish its batch, because waiting two seconds is cheaper than throwing the
// call's own answer away. It was being read as a bargain about the PERSON. A
// correction typed half a second into a build was measured once, found to be
// young, and never looked at again: the steer then waited for the command to
// END, or for the session's background-after clock to end it for them — thirty
// seconds in a live chat (config's DefaultBashBackgroundAfter). A question
// somebody typed at half a second answered at thirty is the wait this file
// exists to bound.
//
// SO THE MEASUREMENT IS TAKEN AGAIN, ONCE, WHEN IT CAN CHANGE ITS ANSWER. One
// timer is armed for the moment the youngest running call crosses
// [steerBashAge], and when it fires it walks the SAME adoption path the steer
// itself walked ([Agent.steerRunningBashLocked]) with the same lock held. There
// is no second rule about which commands may be adopted, no second sentence for
// the tool result, and no second kind of job — the only new thing is WHEN the
// one question is asked. A person's correction therefore reaches the model
// inside [steerBashAge] whatever the command turns out to be, and a command
// that finishes on its own inside that window is still never touched.
//
// ── WHAT MAKES A TIMER SAFE TO FIRE LATE ──
//
// A timer is a promise about the future made with facts from the past, and
// every one of those facts can be stale by the time it fires. So the watch
// carries the identities it was armed for and re-checks all of them under a.mu
// before it touches anything:
//
//	THE TURN. [Agent.turnSeq] is taken at arming and compared at firing, so a
//	timer left over from a turn that has ended cannot adopt the commands of the
//	turn that replaced it — the case where the old promise is not merely useless
//	but wrong.
//	THE COMMANDS. The exact [bare.BashCall] pointers are captured, and only a
//	call still in flight AND in that set may be adopted. A command that finished
//	naturally inside the grace leaves the timer with nothing to do, which is the
//	whole of how a short command stays untouched.
//	THE WORDS. The queue is re-read at firing rather than captured, because a
//	person who typed twice has said the second thing more recently. A steer that
//	has already landed is not steered again, and a queue with nothing left on it
//	makes the firing inert.
//
// AND A STOP IS NEVER DEMOTED INTO CONTINUING WORK. If any correction still
// waiting is one of the unmistakable stop phrases ([steerStopsBash]), the stop
// arm is taken even when a later sentence was typed after it: a person who said
// `stop` and then said something else has not un-said the stop, and a mechanism
// that promoted their command into a healthy background job would be answering
// them with the opposite of what they asked for.
//
// ── ONE TIMER, AND THE LOCK IT DOES NOT TAKE THE LONG WAY ROUND ──
//
// There is at most one watch on the agent at a time: arming replaces whatever
// was there and stops it, so two steers into one batch cannot leave two timers
// racing to adopt the same call (the claim inside [bare.BashCall.Adopt] would
// make the loser a no-op, but a duplicate timer is a duplicate promise and this
// file would rather not own one). It is stopped by the turn's own cleanup and
// by Close, so nothing outlives the session that armed it.
//
// The firing goroutine takes a.mu directly and calls into the job registry with
// it held, exactly as [Agent.Steer] does. It is NOT reached through a registry
// callback: a lane that got here from inside the registry's own lock would be
// taking the two locks in the opposite order from every other road, which is a
// deadlock and was one.

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// steerGraceMargin is the hair of slack between the timer and the age test it
// exists to satisfy. The timer is armed for the instant a call turns
// [steerBashAge] old by this process's clock, and [bare.BashCall.RunningFor]
// asks the same clock — so a firing that landed a microsecond early would find
// the call still young, adopt nothing, and silently give the person back the
// thirty-second wait. It costs ten milliseconds to never find out.
const steerGraceMargin = 10 * time.Millisecond

// steerWatch is one armed second look: the timer, and the identities that make
// it inert everywhere except the situation it was armed for.
type steerWatch struct {
	timer *time.Timer
	// turn is [Agent.turnSeq] at arming.
	turn uint64
	// calls is the exact set of foreground bash calls this watch may adopt.
	calls []*bare.BashCall
}

// armSteerGraceLocked schedules the second look, from [Agent.Steer] and with
// a.mu held. It is called on BOTH of Steer's bash roads: a steer that adopted
// an old call may still be waiting on a young one beside it in the same
// parallel batch, and that call needs the same second look as the one that had
// no old call to adopt at all.
//
// Nothing is armed when no call is too young to adopt — which is the ordinary
// case of a short non-bash tool, and the case where the steer's own pass
// already took everything there was.
func (a *Agent) armSteerGraceLocked() {
	var young []*bare.BashCall
	// THE DEADLINE IS THE LAST CALL TO RIPEN, not the first. Adopting one call
	// out of a parallel batch leaves the step waiting on its siblings, so the
	// one firing is placed where every call in flight has crossed the age and
	// the whole batch can be taken in a single pass. It is still bounded by
	// [steerBashAge], because that is the oldest any of them can be short of.
	var wait time.Duration
	for _, call := range a.inFlightBash.snapshot() {
		age := call.RunningFor()
		if age >= steerBashAge {
			continue
		}
		young = append(young, call)
		if left := steerBashAge - age; left > wait {
			wait = left
		}
	}
	if len(young) == 0 {
		return
	}
	a.stopSteerGraceLocked()
	watch := &steerWatch{turn: a.turnSeq, calls: young}
	watch.timer = time.AfterFunc(wait+steerGraceMargin, func() { a.steerGraceFired(watch) })
	a.steerGrace = watch
}

// stopSteerGraceLocked lets go of the armed watch. Every exit calls it — the
// turn's cleanup, an abandoned turn, Close — because a timer whose situation
// has ended has nothing left to be right about, and the re-check inside the
// firing is the belt to this braces rather than the other way round.
func (a *Agent) stopSteerGraceLocked() {
	if a.steerGrace == nil {
		return
	}
	a.steerGrace.timer.Stop()
	a.steerGrace = nil
}

// steerGraceFired is the second look itself.
func (a *Agent) steerGraceFired(watch *steerWatch) {
	a.mu.Lock()
	if a.steerGrace == watch {
		a.steerGrace = nil
	}
	// A GENERATION IN FLIGHT MEANS THE BOUNDARY IS ALREADY COMING. The batch
	// this watch was armed inside has ended, the request that carries the
	// correction is out, and adopting anything now would be this timer
	// interfering with a turn that has moved on.
	if a.closed || !a.running || a.turnSeq != watch.turn || a.generation != nil {
		a.mu.Unlock()
		return
	}
	steer := a.pendingSteerLocked()
	live := watch.stillRunning(a.inFlightBash.snapshot())
	if steer == nil || len(live) == 0 {
		a.mu.Unlock()
		return
	}
	landed, jobs := a.steerRunningBashLocked(steer.note.Words, live)
	if landed != "" {
		// The acceptance the person already read said the step was still
		// running, and it was true when it was sent; nothing rewrites it. What
		// this updates is the note the RECORD keeps of where the correction
		// landed ([userMessage.steerRecord]), which is written at the drain a
		// moment from now and would otherwise say the wait it no longer had.
		steer.note.Landing = landed
	}
	a.mu.Unlock()
	// Announced outside the lock, as [Agent.Steer]'s own deferred announcement
	// is: the row is person-visible news and the claim above is not.
	for _, started := range jobs {
		a.jobs.announceRow(started)
	}
}

// pendingSteerLocked is the correction this second look acts for: the one whose
// words decide the arm and whose record carries the landing.
//
// It is the LAST one still waiting, so a person who typed twice is answered by
// what they said most recently — unless one of them was a stop, which wins
// wherever it sits in the queue for the reason at the top of this file.
func (a *Agent) pendingSteerLocked() *turnSteer {
	var last *turnSteer
	for _, message := range a.steering {
		if message.steer == nil {
			continue
		}
		if steerStopsBash(message.steer.note.Words) {
			return message.steer
		}
		last = message.steer
	}
	return last
}

// stillRunning is the command identity: the calls this watch was armed for that
// are still in flight, in the order the registry offers them.
func (w *steerWatch) stillRunning(inFlight []*bare.BashCall) []*bare.BashCall {
	if len(w.calls) == 0 || len(inFlight) == 0 {
		return nil
	}
	armed := make(map[*bare.BashCall]bool, len(w.calls))
	for _, call := range w.calls {
		armed[call] = true
	}
	var live []*bare.BashCall
	for _, call := range inFlight {
		if armed[call] {
			live = append(live, call)
		}
	}
	return live
}
