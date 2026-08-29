package bare

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A TOOL CALL RUNS INSIDE THE LEAF'S REMAINING ROOM, NEVER ACROSS IT.
//
// THE DEFECT THIS ANSWERS. The leaf's whole envelope is one context deadline
// (loop.go applies exec.SubharnessInfo.Deadline), and the tool call inherited it
// unchanged: pi's bash tool takes an optional timeout from the model and has no
// default, so a command the model did not think to bound could run until the
// leaf's clock ran out. When it did, three things happened at once and all of
// them were wrong. The command was SIGKILLed and reported its truncated output
// as a clean success, because the cut was tested for cancellation and a deadline
// is not a cancellation. The loop's next turn-boundary check saw a dead context
// and ended the run, so the output the command had actually produced never
// reached the model that asked for it. And the node watchdog two minutes above
// was already counting, so a leaf that was working right up to its last second
// was recorded as one that never came back.
//
// The repair is not a per-command timeout constant. A number picked here is
// wrong on every machine it was not picked on — these leaves run in amd64
// containers under qemu where everything is five to ten times slower than the
// wall-derived arithmetic assumes (PERF.md) — and a command that fits the
// number is not the question anyway. The question is whether the leaf can
// AFFORD this command and still land, and the leaf is the only thing that knows.
//
// So: THE BOUND ON A TOOL CALL IS THE ROOM THE LEAF HAS LEFT, LESS WHAT IT
// TAKES THIS LEAF TO LAND. Both halves are read rather than chosen. The room is
// the context's own deadline minus now. The landing cost is measured from this
// leaf's own work — the slowest model call it has actually made, plus the
// slowest transcript flush it has actually taken — because landing is exactly
// those two things happening once more: one call in which the leaf writes its
// answer, and one write that puts the record on disk. A leaf on a slow machine
// measures a slow machine and reserves accordingly, with nothing to configure.

// pace is one leaf's measurement of itself: how long its own work actually
// takes on the machine it is actually running on.
//
// It holds the slowest observation of each rather than an average, because the
// reserve is an answer to "will there be enough time" and the honest answer to
// that is the worst this leaf has seen, not the middle of what it has seen. A
// mean would reserve too little precisely on the run where the machine is
// getting slower, which is the run this exists for.
//
// It is only ever written from the loop's own goroutine — the model call and
// the flush are both serial in run — so it needs no lock.
type pace struct {
	// call is the longest completed model round-trip.
	call time.Duration
	// flush is the longest completed transcript write.
	flush time.Duration
}

// noteCall records one completed model round-trip.
func (p *pace) noteCall(took time.Duration) {
	if took > p.call {
		p.call = took
	}
}

// noteFlush records one completed transcript write.
func (p *pace) noteFlush(took time.Duration) {
	if took > p.flush {
		p.flush = took
	}
}

// reserve is what this leaf must keep back to land: one more model call, in
// which it says what it did, and one more flush, which puts that on disk.
//
// It is zero until the leaf has made a call and taken a flush, and that is
// correct rather than a gap: a tool call can only be asked for by a model that
// has already answered once, so by the time anything consults this there is
// always at least one measurement of each. A leaf that has measured nothing has
// also not yet spent anything, so it has nothing to protect.
func (p *pace) reserve() time.Duration {
	return p.call + p.flush
}

// room is how long until the leaf's own deadline, and whether it has one at
// all. A context with no deadline is the ordinary case for a test and for a
// bare loop run outside a scheduler; it has infinite room and every bound below
// declines to narrow it.
func room(ctx context.Context) (time.Duration, bool) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	return time.Until(deadline), true
}

// affordable reports what this leaf can spend on the next tool batch, and
// whether the leaf still has room to spend anything at all.
//
// A leaf whose remaining room is inside its own landing cost has no room for
// work: false says so, and the loop lands rather than starting something it
// cannot finish. This is the whole of the mechanism — the reserve is not a
// grace period the tool is allowed to eat into, it is the part of the envelope
// that was never the tool's to spend.
func affordable(ctx context.Context, measured pace) (time.Duration, bool) {
	left, bounded := room(ctx)
	if !bounded {
		return 0, true
	}
	budget := left - measured.reserve()
	if budget <= 0 {
		return 0, false
	}
	return budget, true
}

// toolRoom hands the next tool batch a context that cannot outlive the leaf's
// ability to land, and says when there is no such context left to hand.
//
// The returned release must be called as soon as the batch is done. It is
// deliberately not a defer at the call site: run is a loop that goes round two
// hundred times, and a cancel parked on each pass is a leak wearing a keyword.
func (l *loopState) toolRoom(ctx context.Context) (context.Context, func(), bool) {
	budget, canWork := affordable(ctx, l.measured)
	if !canWork {
		return ctx, func() {}, false
	}
	if budget <= 0 {
		// No deadline on the leaf at all, so nothing to narrow. The tool still
		// runs under the leaf's own context and still dies with it.
		return ctx, func() {}, true
	}
	bounded, cancel := context.WithTimeout(ctx, budget)
	return bounded, cancel, true
}

// landingWords is what the record says when the loop stops working and starts
// finishing. It is a note rather than a fault: nothing went wrong, the leaf
// reached the end of the room it was given, and the difference between those
// two is the difference between a run somebody investigates and a run somebody
// reads.
const landingWords = "the room to work is gone; spending what is left on an answer"

// landingAsk is what the model is told when the loop lands. It offers no tools
// and asks for no more work, because there is provably no time for any: the
// only thing left that is worth buying is the leaf's own account of what it
// did, which is what the next claim resumes from and what the delivery gate
// reads.
const landingAsk = "Your time for this task has run out. Do not start anything else and do not call any tool. " +
	"Reply now with what you did, what is finished, what is not, and exactly where you had got to — " +
	"another agent will continue from your answer."

// landOnTheWall spends the reserve the loop kept back.
//
// A LEAF THAT RAN OUT OF ROOM LANDS; IT IS NOT KILLED. The difference is a
// whole run's worth of work: killed, the leaf's last words are whatever it
// happened to have said before its final tool call, which on a debugging run is
// a sentence about a print statement. Landed, it says what it did and where it
// got to, and that answer is what the next claim resumes from.
//
// The outcome is marked Exhausted rather than failed, because running out of
// the room it was granted is what the growth governor buys more room against —
// it is the input to a decision, not a fault (see cmd/aforge's
// journalLeafExhaustion and store.EventLeafExhausted).
func (l *loopState) landOnTheWall(ctx context.Context, outcome *exec.Outcome, started time.Time) *exec.Outcome {
	l.note(landingWords)
	outcome.Exhausted = exec.StopDeadline
	// The bound names itself here as it does on the generalist belt, so an
	// autopsy of either reads the same way. See exec.Meter.
	if left, bounded := room(ctx); bounded {
		outcome.Meter = exec.Meter{Name: "deadline", Unit: "seconds",
			Reached: int(time.Since(started).Seconds()),
			Allowed: int((time.Since(started) + left).Seconds())}
	}
	outcome.Text = l.lastAssistantText()
	outcome.Elapsed = time.Since(started)
	// The landing turn itself. It is one call with no tools on the wire, which
	// is the same shape the reserve was measured as, and it is allowed to fail:
	// a leaf whose provider has stopped answering still lands, with whatever it
	// had already said.
	l.turn++
	l.messages = append(l.messages, ai.Message{
		Role: "user", Content: []ai.ContentPart{{Type: "text", Text: landingAsk}},
	})
	callDone := exec.Working(ctx)
	began := time.Now()
	response, err := l.client.CompleteWithMessages(ctx, l.messages)
	callDone()
	l.measured.noteCall(time.Since(began))
	if err != nil || response == nil {
		outcome.Elapsed = time.Since(started)
		return outcome
	}
	outcome.Turns++
	addUsage(&outcome.Usage, response)
	if text := strings.TrimSpace(response.Text()); text != "" {
		outcome.Text = text
		l.say(store.TranscriptAssistant, text)
	}
	outcome.Elapsed = time.Since(started)
	return outcome
}
