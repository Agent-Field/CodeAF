package session

import (
	"context"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE SECOND STAGE OF A STOP, AND WHY IT HAD TO BE BUILT FROM THE BOTTOM UP.
//
// A person presses esc and the turn's context is cut. That is the FIRST stage
// and it is enough almost always: the provider request is aborted by the
// transport, a tool that reads its context returns, and the loop closes the
// turn's hub three or four seconds later. What it is not enough for is a wait
// that never looks at a context at all, and this package used to contain
// several — the tool batch's own `wg.Wait()`, the jobs registry's SIGTERM and
// SIGKILL graces, a `cmd.Wait` parked on a pipe a grandchild had escaped with.
// A turn parked on any of them left the surface saying `stopping` for as long
// as the wait lasted, which on the measured run in issue #265 was four minutes,
// after which the only key left was the one that takes the whole process and
// every other conversation in it.
//
// So there is a bound now, and behind the bound there is a door. [Agent.Abandon]
// is that door. It does four things and each of them is load-bearing:
//
//   - it CLOSES THE ABANDON SIGNAL, which is what ends the waits that a context
//     could not reach ([abandonOf], loop.go's [waitBatch], jobs.go's
//     [waitDoneUnder]);
//   - it CANCELS THE TURN AGAIN, which aborts whatever HTTP is still in flight —
//     every provider request is built with the turn's context on it
//     (internal/provider's client.go), so the socket is closed by the transport
//     and the reader parked inside Read is released;
//   - it DISOWNS THE TURN, so the session is free the instant this returns and
//     the very next thing a person types opens a turn of its own;
//   - and it WRITES ONE LINE saying so, with what the turn had spent by then.
//
// THE LINE IS THE ACCEPTANCE AND NOT THE DECORATION. A turn that is let go of
// while it may still be spending is a turn nobody could otherwise account for:
// the seal that carries a turn's cost is written when a turn ENDS, and the whole
// definition of an abandoned turn is that nobody waited for its end. The money
// itself already reaches the machine's ledger per call rather than per turn
// (usage_ledger.go, issue #269), so the bill is not lost — what was lost was any
// record that a turn had been let go of at all.
//
// WHAT THIS DOES NOT DO is claim the goroutine is gone. It may not be: the whole
// reason the door exists is that some waits cannot be ended from outside. What
// it guarantees is that nothing is WAITING for it — not the surface, not the
// session, not the person — and that the file says so.

// abandonKey is the turn context's carrier for the abandon signal. It is a
// context value rather than a field read off the agent because the waits that
// need it are several call frames down inside tools that hold no reference to
// the agent, and threading one parameter through every one of them would be a
// parameter that the next wait forgets to take.
type abandonKey struct{}

// withAbandon puts one turn's abandon signal on its context. It is called once,
// where the turn's context is made ([Agent.startTurnLocked]).
func withAbandon(ctx context.Context, gone <-chan struct{}) context.Context {
	if gone == nil {
		return ctx
	}
	return context.WithValue(ctx, abandonKey{}, gone)
}

// abandonOf is the signal for the turn this context belongs to, or nil.
//
// NIL IS THE ORDINARY ANSWER OUTSIDE A TURN and every caller has to read it as
// "this wait has no second stage", not as an error: a completion driven with no
// turn around it, a tool called from a test, and every task node's own context
// all arrive here without one. A nil channel in a select blocks forever, which
// is exactly the right behaviour — the arm is simply never taken.
func abandonOf(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	gone, _ := ctx.Value(abandonKey{}).(<-chan struct{})
	return gone
}

// AbandonReason is why a turn was let go of, in the words the journal keeps.
// There is one today; it is a named type so the file's reader can tell a bound
// that fired from whatever else may come to use this door.
type AbandonReason string

// AbandonStopTimeout is the bound: a person stopped the turn and the engine had
// not let go of it by the time the surface's deadline came round.
const AbandonStopTimeout AbandonReason = "stop timeout"

// Abandon lets go of the in-flight turn and reports what it had spent.
//
// It reports false when there is nothing to abandon, which is both the ordinary
// case and the IDEMPOTENCE the one-line law needs: a turn is abandoned once, so
// a second call — a duplicated deadline, a surface asking twice — writes no
// second line and disturbs nothing.
//
// THE SESSION IS FREE WHEN THIS RETURNS. running is cleared, the queues are
// dropped and the turn's channels are let go of under the same lock, so a Submit
// arriving on the next keystroke opens an ordinary turn and cannot be refused
// for a turn nobody is waiting for.
func (a *Agent) Abandon(reason AbandonReason) (Usage, bool) {
	a.mu.Lock()
	if a.closed || !a.running {
		a.mu.Unlock()
		return Usage{}, false
	}
	hub, cancel, done, gone := a.hub, a.cancel, a.done, a.abandon
	spend := a.turnSpend
	// THE SESSION MOVES ON FROM THIS TURN HERE, and the number is what says so.
	// The goroutine still running under it captured the old one and will find
	// them different, which is its instruction to clean up nothing (see
	// [Agent.startTurnLocked]).
	a.turnSeq++
	// WHATEVER THE PERSON TYPED AT THIS TURN STILL BELONGS TO THE TRANSCRIPT,
	// which is the same law the ordinary ending keeps and for the same reason: a
	// sentence they typed is part of the record whether or not the model ever
	// read it. The lift comes first, so a steer aimed at a step that never came
	// becomes a message waiting for a turn of its own rather than a claim that
	// this turn was told it (steer.go).
	a.liftSteersLocked(hub)
	a.drainSteeringLocked(hub)
	// AND NOTHING IS QUEUED BEHIND IT. A stop that was followed by the session
	// working again is not a stop — [Agent.Interrupt] already dropped these at
	// the keypress, and this is the same law said at the second stage, because a
	// follow-up queued DURING the winding down would otherwise start here.
	a.dropFollowUpsLocked()
	// The turn's number has already moved on above, which is what makes an
	// armed second look inert; this is the same act said as a release
	// (steer_grace.go).
	a.stopSteerGraceLocked()
	a.running = false
	a.cancel, a.hub, a.done, a.abandon = nil, nil, nil, nil
	a.turnSpend = Usage{}
	file := a.file
	// And the other windows learn the turn is over on the beat it ends, exactly
	// as they learn it ends ordinarily (taskpresence.go). It takes no lock, which
	// is what lets it sit here.
	a.nudgePresence()
	a.mu.Unlock()

	// THE WAITS END FIRST, then the request is cut, then the surface is freed.
	// The order is what makes the freeing honest: a hub closed before the waits
	// were released would be the surface looking away from a turn that was still
	// running, which is precisely the thing this door exists not to be.
	if gone != nil {
		close(gone)
	}
	if cancel != nil {
		cancel()
	}
	// AND EVERY FORKED HAND WITH IT, on [Agent.Interrupt]'s own reasoning: a hand
	// runs on a context of its own, so the cancel above does not reach it, and a
	// hand still finishing a reply for a turn nobody is waiting for is spend with
	// nothing at the end of it.
	a.jobs.stopHands()
	if hub != nil {
		hub.close()
	}
	if done != nil {
		close(done)
	}
	file.appendAbandoned(journalAbandoned{
		Reason:     string(reason),
		Input:      spend.Input,
		Output:     spend.Output,
		CacheRead:  spend.CacheRead,
		CacheWrite: spend.CacheWrite,
		CostUSD:    spend.CostUSD,
		Calls:      spend.Calls,
	})
	return spend, true
}

// waitBatch waits for one tool batch and reports whether the batch is finished.
//
// IT IS THE ONE WAIT IN THIS PACKAGE THAT THE SECOND STAGE HAD TO REACH. The
// tool loop cannot assemble the next step of a transcript with a hole in it, so
// it waits for every call in the batch unconditionally — which is correct, and
// which is also exactly why a turn holding one uncancellable tool could not be
// ended from the keyboard. So the wait now has an escape, and the escape is not
// the turn's context: it is the abandon signal, which is closed only after the
// person's stop has been given its whole settling window.
//
// false is a batch that is still running, and its caller reports the calls that
// never came back rather than pretending they returned nothing ([batchSlots]).
func waitBatch(ctx context.Context, finished <-chan struct{}) bool {
	gone := abandonOf(ctx)
	if gone == nil {
		<-finished
		return true
	}
	select {
	case <-finished:
		return true
	case <-gone:
		return false
	}
}

// ── the tool batch's slots ──────────────────────────────────────────────────

// batchSlots is one tool batch's results, written by the goroutines that run
// the calls and readable while they are still running.
//
// IT EXISTS BECAUSE THE BATCH CAN NOW BE LEFT. Until the second stage the slice
// was a plain `[]toolResult` and reading it needed no lock at all, because the
// only read happened after a `wg.Wait()` that could not return early. The whole
// point of [waitBatch] is that it can, and a read racing a goroutine still
// writing its slot is a data race in the middle of a person's stop.
//
// EVERY SLOT IS SEEDED BEFORE ITS GOROUTINE, which is the law the tool loop
// already kept and states at length: a tool that panics must not leave the zero
// value, because the zero value is an empty SUCCESS and the model reads it as a
// call that ran and returned nothing.
type batchSlots struct {
	mu   sync.Mutex
	at   []toolResult
	back []bool
}

func newBatchSlots(calls []ai.ToolCall) *batchSlots {
	slots := &batchSlots{
		at:   make([]toolResult, len(calls)),
		back: make([]bool, len(calls)),
	}
	for index, call := range calls {
		slots.at[index] = toolResult{
			text:    "tool panicked: " + call.Function.Name + " did not return a result",
			isError: true,
		}
	}
	return slots
}

// put files one call's answer. It is safe after the batch has been left: the
// goroutine that calls it has no way of knowing, and a write nobody will read is
// cheaper than a goroutine that has to check.
func (s *batchSlots) put(index int, result toolResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.at) {
		return
	}
	s.at[index] = result
	s.back[index] = true
}

// taken is the batch as the turn will report it, copied out under the lock.
//
// WHEN THE BATCH WAS LEFT, THE CALLS THAT NEVER CAME BACK SAY SO. The seeded
// panic sentence would be a lie about them — nothing panicked, the person
// stopped the turn and nobody waited any longer — and the law the tool loop
// keeps is that a call which starts always ends journaled with something true.
func (s *batchSlots) taken(finished bool) []toolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]toolResult, len(s.at))
	copy(out, s.at)
	if finished {
		return out
	}
	for index := range out {
		if s.back[index] {
			continue
		}
		out[index] = toolResult{text: toolAbandonedWord, isError: true}
	}
	return out
}

// toolAbandonedWord is what a call that was still running when the turn was let
// go of is recorded as. It is the model's sentence and not a person's, so it
// says the one thing the model could act on if it ever read it: the call did not
// finish and nothing is coming.
const toolAbandonedWord = "the person stopped this turn and it was not waited for; this call did not finish"

// abandoned reports that the turn this context belongs to has been let go of.
//
// IT IS A NON-BLOCKING READ and it is the answer to "may I still write?". A turn
// that has been abandoned must not touch the session's transcript again: the
// session has moved on and a.messages belongs to whatever turn the person
// started next, so an append from here is one conversation growing another's
// rows. False is the ordinary answer and false is what every context with no
// abandon signal on it gets.
func abandoned(ctx context.Context) bool {
	gone := abandonOf(ctx)
	if gone == nil {
		return false
	}
	select {
	case <-gone:
		return true
	default:
		return false
	}
}
