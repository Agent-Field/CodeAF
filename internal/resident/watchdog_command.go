package resident

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// commandWall bounds the whole application of one command — every model call
// it makes, plus the store work between them.
//
// Ten minutes is the outer rail, not the working number. The per-call wall
// (internal/provider/pool) is what actually catches a stalled completion, and
// it catches it in four; this exists because a command is a sequence of calls
// and a pathology that stalls each of them a little is a queue that never
// drains without any single call ever looking wrong. A splice is three or four
// round-trips including a fanned contract pass, so ten minutes is roughly twice
// the worst honest case and nowhere near any observed one.
//
// It is a per-command wall, not a per-tick one. A tick applying four
// independent splices concurrently gives each of them its own ten minutes; the
// point is that no single command can own the queue forever, not that the
// resident must finish a batch by a clock.
const commandWall = 10 * time.Minute

// commandStrikeLimit is how many times a command may stall before the person is
// told it is not going to happen.
//
// Two, because the failure this guards is a hung socket and the cure for a hung
// socket is a new one. A first stall is not evidence about the request — it is
// evidence about one connection to one provider — so terminally rejecting a
// perfectly good ask on it would be the watchdog inventing a failure the system
// did not have. A second stall is a different claim: whatever is wrong is not
// the connection, and continuing to retry silently is how a card pulses
// "creating task…" forever.
const commandStrikeLimit = 2

// stalledReceipt is what the person reads when a command has stopped twice. It
// names what happened in their terms and hands back the one action that
// actually requeues the work, because a receipt that only reports a failure
// leaves somebody staring at a card wondering whether to retype the request.
const stalledReceipt = "I couldn't get this planned — the model stopped answering twice. " +
	"Say 'try again' to requeue it."

// stalled reports that a command died of time rather than of anything about
// the request.
//
// The two clauses are both load-bearing. errors.Is against DeadlineExceeded is
// what catches both walls — the per-call one wraps it deliberately so this
// question can be asked without internal/resident importing the provider pool.
// The check on the outer context is what keeps a shutdown from being read as a
// provider stall: when the process is going away every command in flight
// reports a deadline, and striking them would rewrite an orderly exit as a
// workforce failure.
func stalled(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// applyBounded applies one command under the command wall.
//
// The wall is installed here rather than inside applyCommand so that every
// path into a command — the serial one, the concurrent group, and any future
// caller — inherits it without having to remember to, and so that the context
// a command's model calls actually receive is the bounded one.
func (r *Reconciler) applyBounded(ctx context.Context, command store.Command) (commandOutcome, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandWall)
	defer cancel()
	return r.applyCommand(commandCtx, command)
}

// settleOrStrike is the watchdog's settlement half: it decides whether what
// came back from applying a command is an answer or a stall.
//
// An answer — applied, refused, or failed for any reason that is about the
// request — settles exactly as it always did. A stall does not settle at all,
// and that is the whole mechanism: a command is only ever removed from the
// pending queue by ResolveCommand, so declining to call it leaves the command
// exactly where it was, and the next tick re-reads it and tries again. There is
// no re-pend to write, and nothing durable to keep in step — a restart re-reads
// the same pending row and starts the count over, which is the right answer for
// a count that is about one process's luck with one provider.
//
// It reports whether the command was left pending, which the drain loop reads:
// re-reading the queue in the same tick would retry a stall against the same
// wedged provider within microseconds, which is not a retry, it is the second
// strike arriving early.
//
// Callers hold r.mu, which is what makes the strike map safe to touch here.
func (r *Reconciler) settleOrStrike(ctx context.Context, command store.Command,
	outcome commandOutcome, err error) (bool, error) {
	if !stalled(ctx, err) {
		r.clearStrike(command.Seq)
		return false, r.settleCommand(command, outcome, err)
	}

	strikes := r.recordStrike(command.Seq)
	if strikes < commandStrikeLimit {
		// Left pending on purpose. The person's card keeps saying the work is
		// being made, because it is: the next tick owns this command.
		r.noteCommandStage(command, stageStalled, fmt.Sprintf(
			"the model stopped answering — trying again (attempt %d)", strikes+1))
		return true, nil
	}

	r.clearStrike(command.Seq)
	// A rejection speaks, whoever else spoke first — receiptVoice is what makes
	// this reach the person rather than being filed as grey furniture, and this
	// is the one line that will ever correct the head's cheerful "on it".
	return false, r.settleCommand(command, commandOutcome{
		status:  store.CommandRejected,
		result:  fmt.Sprintf("stalled %d times: %v", strikes, err),
		receipt: stalledReceipt,
	}, nil)
}

// recordStrike counts one stall against a command and returns the new total.
// Callers hold r.mu.
func (r *Reconciler) recordStrike(seq int64) int {
	if r.commandStrikes == nil {
		r.commandStrikes = map[int64]int{}
	}
	r.commandStrikes[seq]++
	return r.commandStrikes[seq]
}

// clearStrike forgets a command that reached an answer. A command that stalled
// once and then applied is not carrying a strike into a future it has nothing
// to do with, and the map must not grow for the life of the process.
// Callers hold r.mu.
func (r *Reconciler) clearStrike(seq int64) {
	if r.commandStrikes == nil {
		return
	}
	delete(r.commandStrikes, seq)
}
