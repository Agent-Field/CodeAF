package session

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A SYNCHRONOUS ATTEMPT IS A RENDER WITH NO JOB.
//
// A picture is paid for inside the tool call that asked for it: there is no
// background job, no log and no note, so before this file nothing recorded that
// a paid image request was ever sent, how it ended, or what it was charged when
// nothing was saved. The render ledger already says all of that for video and
// music, so an image attempt is written into the SAME ledger, with the same
// states, the same one terminal, the same late receipt and the same restore —
// as `kind:"image"`, `job:0`.
//
// It runs on the tool's own goroutine and owns NO goroutine, pool or map of its
// own. Its memory is the ledger's one live entry and one process retention slot
// (render_retention.go), both of which it holds only between its start row and
// the provider's return; the 64-slot process bound is the bound on how many can
// be in flight.
//
// THE START ROW IS WRITTEN BEFORE THE PROVIDER IS CALLED, AND A START THAT
// CANNOT BE WRITTEN SENDS NOTHING. A paid request with no record of having been
// sent is exactly the gap this closes, so it is never made.
//
// A STOP SIGNALS THE CALL AND CLAIMS NOTHING ABOUT THE PROVIDER. Accepting one
// seals `cancelled` at once with upstream unknown and cancels the attempt's own
// context; whether the provider charged is unknown until it says so, and if it
// says so later the figure lands on the same record as a receipt, never as a
// second ending.

// errRenderStopped is the cause an accepted Stop cancels an attempt with, so
// the attempt can tell its own Stop from a turn that ended under it.
var errRenderStopped = errors.New("the render was stopped")

// errAttemptOver releases an attempt's context once the call is done with it.
// It is a named cause rather than nil so nothing reading the context can take
// the attempt's own tidying for a stop.
var errAttemptOver = errors.New("the image attempt is over")

// renderAttempt is the tool call's handle on its one record. It holds no state
// of its own beyond the context it hands the provider.
type renderAttempt struct {
	ledger *renderLedger
	id     string
	// ctx is the provider call's context: the tool call's, plus this
	// attempt's own Stop.
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// imageAttemptDoor is how this agent records an image attempt, or nil for an
// agent built with no job registry and so no ledger at all: the throwaway agent
// a saved harness's belt is built on (harness_belt.go), which never journals.
// That is a door with nowhere to record, like the command line's, and it keeps
// that door's behavior. It is NOT the closed or unwritable case — a session's
// registry outlives its Close, so those reach [Agent.beginImageAttempt] and are
// refused there with nothing sent.
func (a *Agent) imageAttemptDoor() func(context.Context, string) (*renderAttempt, error) {
	if a.renderJobs() == nil {
		return nil
	}
	return a.beginImageAttempt
}

// beginImageAttempt writes an image attempt's start row and marks this call as
// having made a render record. It refuses, and nothing may be sent, when the
// session is closed or the row cannot be journaled.
func (a *Agent) beginImageAttempt(ctx context.Context, model string) (*renderAttempt, error) {
	jobs := a.renderJobs()
	if jobs == nil {
		return nil, errors.New("the session is closed")
	}
	ledger := jobs.renderLedger()
	callCtx, cancel := context.WithCancelCause(ctx)
	made := madeBy(ctx, nil)
	id, err := ledger.beginWith(0, modalityImage, model, made.Run, made.Tool, cancel, time.Now())
	if err != nil {
		cancel(errAttemptOver)
		return nil, err
	}
	markMediaAttempt(ctx)
	return &renderAttempt{ledger: ledger, id: id, ctx: callCtx, cancel: cancel}, nil
}

// charge hands the ledger what the provider reported, the moment it returns.
func (t *renderAttempt) charge(usage *ai.Usage) {
	t.ledger.charge(t.id, usage, time.Now())
}

// commit is the point of no return before anything is written; false means a
// Stop was accepted first and nothing may be saved.
func (t *renderAttempt) commit() bool {
	return t.ledger.commitAttempt(t.id)
}

// end writes the one terminal, unless a Stop already did.
func (t *renderAttempt) end(patch renderPatch) {
	t.ledger.settle(t.id, patch, time.Now())
}

// stopped says this attempt's own Stop was accepted, as opposed to the turn
// ending under it.
func (t *renderAttempt) stopped() bool {
	return errors.Is(context.Cause(t.ctx), errRenderStopped)
}

// close is every road out of the call: an attempt no branch ended is recorded
// as failed rather than left running, and the slot is given back only now,
// after the provider has returned and the last fact is saved.
func (t *renderAttempt) close() {
	t.ledger.settle(t.id, renderPatch{state: RenderFailed, err: "the attempt ended without an outcome"}, time.Now())
	t.ledger.release(t.id)
	t.cancel(errAttemptOver)
}

// commitAttempt is [job.commitRender] for an attempt with no job.
func (l *renderLedger) commitAttempt(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.live[id]
	if entry == nil || entry.rec.State != RenderRunning {
		return false
	}
	entry.committed = true
	return true
}

// stopAttempt is [job.requestKillRender] for an attempt with no job. held is
// false when the ledger holds no stoppable attempt by that id.
func (l *renderLedger) stopAttempt(id string) (outcome RenderOutcome, held bool) {
	l.mu.Lock()
	entry := l.live[id]
	if entry == nil || entry.stop == nil {
		l.mu.Unlock()
		return "", false
	}
	switch {
	case entry.rec.State != RenderRunning:
		l.mu.Unlock()
		return RenderOutcomeEnded, true
	case entry.committed:
		l.mu.Unlock()
		return RenderOutcomeTooLate, true
	}
	rec, _ := l.settleLocked(id, renderPatch{state: RenderCancelled}, time.Now())
	stop := entry.stop
	l.mu.Unlock()
	l.emit(rec)
	stop(errRenderStopped)
	return RenderOutcomeCancelled, true
}
