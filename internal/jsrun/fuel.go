package jsrun

import (
	"context"
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// FUEL: three ceilings, one ending, and never a hang.
//
// A run stops when it has made too many host calls, when it has spent more
// tokens than it was granted, or when it has run past its deadline. All three
// end the same way — [exec.RunResult] with Incomplete set to a sentence saying
// what ran out — and none of them is catchable by the program, which is the
// point of doing it through [goja.Runtime.Interrupt] rather than by throwing a
// JavaScript error. A program that could `try { … } catch` its way past its own
// budget would be a program with no budget.
//
// THE DEADLINE IS THE ONLY ONE THAT CAN STOP A LOOP THAT CALLS NOTHING, and it
// is why it exists rather than being folded into the step count. `while (true)
// {}` makes no host calls and spends no tokens; the watchdog interrupts it
// anyway, mid-instruction, because goja checks its interrupt flag between
// instructions no matter what the instruction is.

// halt is why a run was stopped, in the words a person reads. It travels through
// [goja.Runtime.Interrupt] as the interrupt's value and comes back out of
// [goja.InterruptedError.Value], which is how the reason survives the unwinding.
type halt struct{ why string }

// watch starts the deadline watchdog and returns the function that stops it.
//
// IT IS THE ONE OTHER GOROUTINE A RUN HAS, and everything it touches is either
// its own or atomic: the context's Done channel, a timer, and
// [goja.Runtime.Interrupt], which goja documents as safe from any goroutine. It
// reads no part of the recorder and no part of the interpreter's state, which is
// what lets the rest of this package state that host calls are serial and mean
// it.
func watch(ctx context.Context, vm *goja.Runtime, wall time.Duration) func() {
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(wall)
		defer timer.Stop()
		select {
		case <-done:
		case <-ctx.Done():
			// A CANCELLED RUN IS INCOMPLETE, NOT BROKEN. Somebody pressed the
			// ✕, or the turn that owned this went away; the run happened and did
			// not finish, which is exactly what Incomplete means. Reporting it
			// as an error would send the caller's deoptimization path off to do
			// the work the long way after a person asked for it to stop.
			vm.Interrupt(halt{why: "it was stopped before it finished."})
		case <-timer.C:
			vm.Interrupt(halt{why: fmt.Sprintf("it ran out of time — it had %s.", spellDuration(wall))})
		}
	}()
	return func() { close(done) }
}

// spendStep books one host call against the operation budget.
//
// IT IS CHECKED BEFORE THE CALL GOES OUT, not after, because the ceiling is on
// what the run may DO and a call that has already been made cannot be un-made —
// nor un-billed. The step is counted whether the call succeeds or not: a refused
// tool and a failed model call are both things the program did.
func (h *host) spendStep() bool {
	if h.rec.steps >= h.fuel.steps() {
		h.stop(halt{why: fmt.Sprintf("it ran out of room — it had %d steps to work in.", h.fuel.steps())})
		return false
	}
	h.rec.steps++
	return true
}

// spendTokens checks the ledger against the grant after a call has been folded
// in.
//
// IT READS THE SPEND FOLD AND NOTHING ELSE, which is the whole reason the fold
// is one object: the tokens a run has spent are the tokens its journal says it
// spent, and a budget checked against a second tally would be a budget about a
// different run. Zero is no ceiling — a caller that was given no grant does not
// get one invented for it.
func (h *host) spendTokens() {
	if h.fuel.Tokens <= 0 {
		return
	}
	if spent := h.rec.spend.Input + h.rec.spend.Output; spent >= h.fuel.Tokens {
		h.stop(halt{why: fmt.Sprintf("it ran out of what it had to spend — %d tokens.", h.fuel.Tokens)})
	}
}

// stop ends the run at the next instruction with a reason a person can read.
//
// It interrupts rather than panicking, and returns rather than unwinding, for
// one reason worth stating: goja checks its interrupt flag between every pair of
// instructions, so the program gets no further, while a Go panic thrown out of a
// bound function would escape the interpreter's own recovery and land in this
// package as a crash. The value this host call is about to return is never used.
func (h *host) stop(reason halt) {
	h.vm.Interrupt(reason)
}
