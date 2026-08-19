package bare

// PROMOTING A RUNNING FOREGROUND COMMAND, WITHOUT BARE LEARNING WHAT A JOB IS.
//
// A foreground bash call is committed at the moment it is made. When `make`
// turns out to take nine minutes, the timeout kills the process group and the
// caller starts again from nothing — the nine minutes are thrown away, and
// neither the model nor the person could have known in advance which side of
// the line the command fell on.
//
// The fix is a HANDOFF, and the seam it needs is drawn here, in the package that
// owns the exec.Cmd frame and the timeout, because those are the two things a
// handoff has to move. What it moves them TO is nobody's business here: this
// package has no registry, no notion of a session, and no opinion about what a
// promoted process should be called afterwards. It offers a running call and
// takes back one sentence.
//
// Three rules hold the whole thing up:
//
//   - ONE Wait PER PROCESS. Go's exec package permits exactly one call to
//     cmd.Wait, and bash's is already in flight when a promotion happens. So the
//     wait is not moved — its RESULT is. [BashCall.Exit] is fed by the same
//     goroutine that was always waiting, and whoever adopts the call reads the
//     exit code off it and settles the process however it settles processes.
//     The alternative — handing the *exec.Cmd over and letting the new owner
//     wait on it — is the one shape the standard library will not allow.
//
//   - THE ADOPTION IS ONE ATOMIC CLAIM. A call ends exactly once, and four
//     things race to end it: the process exiting, the timeout firing, the turn's
//     context being cancelled, and somebody adopting it. [BashCall.Adopt] runs
//     the adopter's own work INSIDE the claim, so there is no window between
//     "nothing else may end this call" and "this is the sentence it answers".
//
//   - INTERRUPT MEANS STOP. A call whose context is already cancelled may not be
//     adopted, at any price. A person who pressed esc asked for the work to end,
//     and promoting it into something that outlives the turn they just ended
//     would be the harness overruling them.

import (
	"context"
	"io"
	"os/exec"
	"sync"
)

// bashOutcome is who owns the ending of one foreground bash call.
type bashOutcome int

const (
	// bashOpen is the ordinary state: the process is running and nothing has
	// claimed how the call ends.
	bashOpen bashOutcome = iota
	// bashAdopted means a promoter took the process and supplied the sentence
	// the call answers with.
	bashAdopted
	// bashEnded means bare itself owns the ending — the process exited, the
	// timeout killed it, or the turn was interrupted.
	bashEnded
)

// BashCall is one running foreground bash process, offered to whoever put a
// [BashPromoter] in the call's context.
//
// It is a HANDLE and not a copy: the process, its output and its exit code are
// still bare's until somebody adopts it, and after that they are the adopter's
// and bare stops speaking about them entirely.
type BashCall struct {
	command string
	cmd     *exec.Cmd
	acc     *outputAccumulator
	// ctx is the turn's, kept only to answer one question: has the person
	// already asked for this work to stop? See [BashCall.Adopt].
	ctx context.Context
	// exit carries the process's exit code to an adopter. It is buffered so the
	// goroutine that waits never blocks on a call nobody adopted.
	exit chan int
	// promoted is closed once the call has been adopted, which is how the
	// waiting Execute learns to stop waiting.
	promoted chan struct{}

	mu       sync.Mutex
	outcome  bashOutcome
	answer   string
	answerIs bool
	// timedOut records that the ending bare is about to describe was the
	// timeout's. It is set under the lock the timer already takes, so the
	// sentence and the kill that produced it cannot race.
	timedOut bool
}

func newBashCall(command string, cmd *exec.Cmd, acc *outputAccumulator, ctx context.Context) *BashCall {
	return &BashCall{
		command:  command,
		cmd:      cmd,
		acc:      acc,
		ctx:      ctx,
		exit:     make(chan int, 1),
		promoted: make(chan struct{}),
	}
}

// Command is the shell command this call is running, verbatim.
func (c *BashCall) Command() string { return c.command }

// Process is the running command's frame. An adopter needs it for exactly one
// thing — signalling the process GROUP, which bash set up with Setpgid — and a
// promoted process keeps that group, so a kill still reaches the whole tree.
//
// It must not be waited on. See this file's header: the wait is already in
// flight, and its result comes back on [BashCall.Exit].
func (c *BashCall) Process() *exec.Cmd { return c.cmd }

// Exit delivers the process's exit code, exactly once, from the goroutine that
// was already waiting on it. An adopter reads this instead of waiting.
func (c *BashCall) Exit() <-chan int { return c.exit }

// Attach points the running command's output at w: everything bare has kept so
// far is replayed into it, and everything the command writes from here on goes
// there and nowhere else.
//
// The replay is bare's ROLLING TAIL and not necessarily the whole output — a
// foreground call keeps the last few hundred kilobytes and nothing before that
// — so a job promoted out of a very loud command begins where that tail begins.
// Saying so is the honest thing; pretending otherwise would put a hole in the
// middle of a log file somebody is about to read.
func (c *BashCall) Attach(w io.Writer) { c.acc.mirrorTo(w) }

// Kill ends the process group the way the timeout would have. It is here for an
// adopter whose own adoption failed halfway and who must not leave the process
// behind.
func (c *BashCall) Kill() { killProcessGroup(c.cmd) }

// Adopt hands the running process to a caller that will keep it alive.
//
// `take` runs INSIDE the claim, which is the whole design: it is the one moment
// where nothing else can end the call, so whatever the adopter has to do to make
// the process its own — open a log, register it, start reading [BashCall.Exit] —
// happens with the ending already spoken for. It returns the sentence the tool
// call answers with, whether that sentence is an error, and whether the adoption
// happened at all; a false gives the claim back and leaves the call exactly as
// it was.
//
// Adopt refuses a call that has already ended, and it refuses a call whose
// context is cancelled — see this file's header on why an interrupt is not
// negotiable.
func (c *BashCall) Adopt(take func() (answer string, isError bool, ok bool)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outcome != bashOpen || c.ctx.Err() != nil {
		return false
	}
	answer, isError, ok := take()
	if !ok {
		return false
	}
	c.outcome, c.answer, c.answerIs = bashAdopted, answer, isError
	close(c.promoted)
	return true
}

// close claims the ending for bare's own path — the process exited, or the turn
// was cancelled. It answers with the adoption's sentence when somebody got here
// first, which is the one case where bare's own result is not the answer.
func (c *BashCall) close() (answer string, isError bool, adopted bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outcome == bashAdopted {
		return c.answer, c.answerIs, true
	}
	c.outcome = bashEnded
	return "", false, false
}

// closeTimedOut claims the ending for the timeout and records that it was the
// timeout's, both under one lock. It reports false when the call was adopted or
// had already ended, which is the timer's instruction not to kill anything.
func (c *BashCall) closeTimedOut() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outcome != bashOpen {
		return false
	}
	c.outcome, c.timedOut = bashEnded, true
	return true
}

// wasTimedOut reports whether the timeout is what ended this call.
func (c *BashCall) wasTimedOut() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timedOut
}

// BashPromoter is the door a caller puts in a bash call's context to be offered
// the running process.
//
// Both methods may be called from goroutines other than the one running the
// tool, and both may be called concurrently with the process exiting — every
// decision they make goes through [BashCall.Adopt], which is where the race is
// settled.
type BashPromoter interface {
	// Started is called once, as soon as the process is running. The function
	// it returns — which may be nil — is called when the call is over, adopted
	// or not, so the promoter can forget it.
	Started(call *BashCall) (finished func())
	// TimedOut is called when the timeout fires, INSTEAD of the kill. Returning
	// true means the promoter took the process and the kill must not happen;
	// returning false leaves the timeout to do what it has always done.
	TimedOut(call *BashCall) bool
}

// promoterKey is the context key the promoter travels under. It is a private
// type so nothing outside this package can collide with it.
type promoterKey struct{}

// WithBashPromoter returns ctx carrying the door a foreground bash call is
// offered through. A context without one runs bash exactly as it always ran:
// the timeout kills the process group and the call answers `Command timed out
// after N seconds`.
func WithBashPromoter(ctx context.Context, promoter BashPromoter) context.Context {
	if promoter == nil {
		return ctx
	}
	return context.WithValue(ctx, promoterKey{}, promoter)
}

func bashPromoterFrom(ctx context.Context) BashPromoter {
	promoter, _ := ctx.Value(promoterKey{}).(BashPromoter)
	return promoter
}

// watchCancel kills the process group when the turn's context is cancelled, and
// stops watching the moment the call is over.
//
// It is here rather than in exec.CommandContext for the reason the whole file
// exists: a CONTEXT-BOUND COMMAND CANNOT BE PROMOTED. exec's own watcher kills
// the group on cancel and then force-closes the output pipes after WaitDelay,
// so an adopted process would either be killed by the turn ending or have its
// stdout pulled out from under it — and the turn ending is precisely the moment
// a promoted job is supposed to survive.
//
// The semantics are the ones exec.CommandContext gave and that bash's own
// cmd.Cancel spelled out: SIGKILL to the whole group, because a command that
// left a background child sharing its stdout must still cost its timeout and
// nothing more. WaitDelay stays set on the command itself, which is what bounds
// the wait when a grandchild is still holding the pipe open after its parent
// died.
func watchCancel(ctx context.Context, call *BashCall, done <-chan struct{}) {
	go func() {
		select {
		case <-ctx.Done():
			if _, _, adopted := call.close(); !adopted {
				killProcessGroup(call.cmd)
			}
		case <-done:
		}
	}()
}
