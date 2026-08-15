package session

// Loop detection: the nudge for a turn going in circles.
//
// A model that is stuck does not stop — it repeats. The same grep with the same
// pattern, four times. The same build, failing the same way, six times. Nothing
// in the loop (loop.go) notices, because every individual step is legal: a tool
// was called, a result came back, the model asked again. The turn ends when the
// model runs out of ideas or when the person interrupts, and the second is what
// actually happens.
//
// ── WHAT IS WATCHED ──
//
// Two signals, per turn, over a sliding window of the last dozen calls:
//
//   - THE SAME CALL THREE TIMES IN A ROW. Consecutive, because a call repeated
//     with other work between the repeats is usually a person's transcript being
//     re-read, not a loop. Identity is the tool name and its arguments — a read
//     of two different files is two different calls.
//   - THE SAME ERROR THREE TIMES IN THE TURN. Total rather than consecutive, and
//     across tools rather than per tool, because this is the shape a real loop
//     takes: the model varies the call, the failure does not move.
//
// ── WHAT A NUDGE IS ──
//
// A note in the transcript, in the lane a person's steering rides (agent.go's
// enqueueSteering): plain user-role text, drained at the next step boundary,
// journaled like anything else the model was told. It is deliberately NOT a
// provider error and not a tool failure — the model is not being punished, it is
// being told what it has been doing, which is the one fact it cannot see.
//
// ONE NUDGE PER SIGNATURE, AND THEN HYSTERESIS. A loop that continues
// immediately after being named does not earn a second identical note; a second,
// different loop in the same turn does. The whole watch resets per turn, because
// a turn is where a person's attention resets too.
//
// ── THE HYSTERESIS LAW ──
//
// Forward progress is accepted IMMEDIATELY; backward movement needs REPEATED
// evidence (PMCoder's phase hysteresis, https://arxiv.org/abs/2608.06811 — the
// mechanism that stops a planner thrashing between phases on one noisy signal).
// Here the two directions are:
//
//   - FORWARD is a successful call this turn has not already been nudged about.
//     One of those kills the backward case outright: every streak resets, and a
//     model that was three repetitions deep starts again from nothing. No
//     confirmation, no decay, no half-credit — the evidence that the turn is
//     working is the turn working.
//   - BACKWARD is a signature that was already named repeating AGAIN. It takes
//     [loopHysteresis] more of them to advance the ladder, not one. The
//     asymmetry is the whole mechanism: the harness is quick to believe the turn
//     is fine and slow to escalate against it, because escalating costs the
//     person's attention and being wrong about progress costs nothing.
//
// ── AND THEN THE PERSON ──
//
// Past two nudges the notes have stopped working, and a third one is the harness
// talking to itself. So when the session is in prompt mode — the person is at
// the keyboard and has already said they want to be asked about things — the
// third nudge is asked as a consent question in the existing lane (consent.go)
// rather than written as a note. By then the person IS the better nudge: they
// can see the loop, and they are the only party in the conversation with new
// information — and the question carries the one move that is not more words,
// the revert (recovery.go).

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// loopWindow is how many recent call signatures are remembered. Twelve is
	// three or four batches: long enough to hold a repetition, short enough that
	// a turn's early work cannot haunt its later work.
	loopWindow = 12

	// loopRepeats is how many identical calls, or identical errors, make a loop.
	// Two is a retry — which is often the right thing to do and sometimes works.
	// Three is a habit.
	loopRepeats = 3

	// loopNudgeCeiling is how many notes a turn gets before the person is asked
	// instead. Two, because a third note would be the third time the same
	// sentence failed to change anything.
	loopNudgeCeiling = 2

	// loopHysteresis is how many MORE repetitions of an already-named signature
	// count as evidence before the ladder advances again.
	//
	// Two, not one. One would mean a nudged model gets a second nudge on its very
	// next step — before the note it was just handed has even reached a request —
	// and a ladder that climbs faster than its own advice can be read is a ladder
	// measuring latency, not stuckness. Two says: the model saw the note, kept
	// going, and did the same thing twice anyway.
	loopHysteresis = 2
)

// nudge is one detected loop, ready to be said out loud.
type nudge struct {
	// call is the repeating call itself, kept whole so the consent escalation
	// can show the person the same row the turn already drew.
	call ai.ToolCall
	// tool is the call's name, and count how many times it repeated (or how many
	// times the error came back).
	tool  string
	count int
	// failing distinguishes the two rules: a repeated CALL, or a repeated ERROR.
	// They read differently to the model, so they are worded differently.
	failing bool
	// nth is which nudge of this turn it is, 1-based. It is what the escalation
	// law reads.
	nth int
}

// loopWatch is one turn's memory of what it has been doing.
//
// It carries a mutex although only the turn goroutine drives it today: it is
// reachable from the Agent's turn, the batch results arrive from goroutines the
// batch owns, and a watcher whose safety depended on nobody ever calling observe
// from a second place is a watcher that breaks silently the first time somebody
// does.
type loopWatch struct {
	mu sync.Mutex
	// recent is the sliding window of call signatures, oldest first.
	recent []string
	// errors counts each distinct error text seen this turn.
	errors map[string]int
	// named is every signature already nudged for, so nothing is said twice in a
	// row for the same reason.
	named map[string]bool
	// streak is the backward evidence: how many times each ALREADY-NAMED
	// signature has repeated since its last nudge. It is what
	// [loopHysteresis] is counted against, and forward progress empties it.
	streak map[string]int
	// nudges is how many nudges this turn has produced.
	nudges int
}

func newLoopWatch() *loopWatch {
	return &loopWatch{
		errors: make(map[string]int, 4),
		named:  make(map[string]bool, 2),
		streak: make(map[string]int, 2),
	}
}

// observe folds one finished batch into the watch and reports a nudge if this
// batch is the one that tipped a rule over.
//
// AT MOST ONE NUDGE PER BATCH, even when both rules fire: two notes about the
// same moment is the harness being noisy about its own cleverness, and the call
// rule is the more specific of the two, so it wins.
//
// FORWARD EVIDENCE IS READ FIRST, before any rule is tested, so a batch that
// both progressed and repeated cannot escalate. That ordering is the hysteresis
// law's "immediately": a model that got something done this step is not a model
// the harness interrupts this step, even if it also re-ran the thing it was
// nudged about.
func (w *loopWatch) observe(calls []ai.ToolCall, results []toolResult) (nudge, bool) {
	if w == nil || len(calls) == 0 {
		return nudge{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.sawProgress(calls, results) {
		clear(w.streak)
	}

	var found nudge
	ok := false
	for index, call := range calls {
		signature := callSignature(call)
		w.recent = append(w.recent, signature)
		if len(w.recent) > loopWindow {
			w.recent = w.recent[len(w.recent)-loopWindow:]
		}
		if run := w.trailingRun(signature); run >= loopRepeats && !ok && w.speakAbout(signature) {
			found, ok = nudge{call: call, tool: call.Function.Name, count: run}, true
		}

		if index >= len(results) || !results[index].isError {
			continue
		}
		failure := errorSignature(results[index].text)
		w.errors[failure]++
		if count := w.errors[failure]; count >= loopRepeats && !ok && w.speakAbout(failure) {
			found, ok = nudge{call: call, tool: call.Function.Name, count: count, failing: true}, true
		}
	}
	if !ok {
		return nudge{}, false
	}
	w.nudges++
	found.nth = w.nudges
	return found, true
}

// speakAbout reports whether a signature that has just tipped a rule over is
// worth saying something about, and books the consequence of the answer.
//
// The first time, always: the loop has a name nobody has said yet. After that it
// is the hysteresis law — the signature has to come back [loopHysteresis] more
// times before the ladder advances, and the streak resets on the escalation so
// the next one costs the same evidence again rather than firing on every step
// from here on.
func (w *loopWatch) speakAbout(signature string) bool {
	if !w.named[signature] {
		w.named[signature] = true
		return true
	}
	w.streak[signature]++
	if w.streak[signature] < loopHysteresis {
		return false
	}
	w.streak[signature] = 0
	return true
}

// sawProgress reports whether this batch contains forward movement: a call that
// SUCCEEDED and that this turn has not already been nudged about.
//
// A named signature repeating is not progress however well it went — the model
// re-running a successful grep for the fourth time is the loop, not the way out
// of it — and a failed call is not progress by definition. Everything else is:
// the turn did something new and it worked.
func (w *loopWatch) sawProgress(calls []ai.ToolCall, results []toolResult) bool {
	for index, call := range calls {
		if index >= len(results) || results[index].isError {
			continue
		}
		if w.named[callSignature(call)] {
			continue
		}
		return true
	}
	return false
}

// trailingRun is how many entries at the END of the window are this signature.
// Only the tail counts: the rule is "three times in a row", and a signature seen
// three times with other calls between them is a model working, not looping.
func (w *loopWatch) trailingRun(signature string) int {
	run := 0
	for index := len(w.recent) - 1; index >= 0; index-- {
		if w.recent[index] != signature {
			break
		}
		run++
	}
	return run
}

// callSignature identifies a call by what it DOES: the tool and its arguments,
// hashed so the window costs bytes rather than kilobytes. The hash is fnv — this
// is a cache key for a nudge, not a claim about anything, and a collision costs
// one note that names the wrong tool.
func callSignature(call ai.ToolCall) string {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(call.Function.Name))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(strings.TrimSpace(call.Function.Arguments)))
	return fmt.Sprintf("call:%x", digest.Sum64())
}

// errorSignature identifies a failure by its text. Whitespace is folded because
// the same failure re-run is the same failure however a shell wrapped its lines.
func errorSignature(text string) string {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(strings.Join(strings.Fields(text), " ")))
	return fmt.Sprintf("error:%x", digest.Sum64())
}

// nudgeNote is what the model reads. It states the fact, then asks the two
// questions a stuck model has stopped asking itself.
func nudgeNote(n nudge) string {
	what := "the same " + n.tool + " call"
	outcome := "with the same result"
	if n.failing {
		what = n.tool
		outcome = "and it has failed the same way each time"
	}
	return fmt.Sprintf("[stuck] You have repeated %s %d times %s. Rethink your approach: "+
		"which assumption is wrong, and what is a different way to get this done? "+
		"If there is no different way, say so and stop rather than trying again.",
		what, n.count, outcome)
}

// loopRule is how the escalated question names itself to the person, in the slot
// the approval policy's own wording usually occupies.
func loopRule(n nudge) string {
	if n.failing {
		return fmt.Sprintf("stuck: %s has failed the same way %d times", n.tool, n.count)
	}
	return fmt.Sprintf("stuck: the same %s call %d times", n.tool, n.count)
}

// ── the turn's side ─────────────────────────────────────────────────────────

// nudgeIfLooping folds one finished batch into the turn's watch and says
// something if it tipped a rule over. It is the loop detector's `post-feedback`
// hook (hooks.go), so it runs at the step boundary, after the batch's results
// are in the transcript and before the next request is assembled — the one
// moment a note can ride into the next request the way a person's steering does.
//
// It NEVER fails the turn. Everything here — the note, the event, the question —
// is an aside about work that is already recorded; a turn that could be ended by
// its own loop detector would be a detector nobody could afford to trust.
func (a *Agent) nudgeIfLooping(ctx context.Context, hub *eventHub, ep *episode, calls []ai.ToolCall, results []toolResult) {
	looping, ok := ep.watch.observe(calls, results)
	if !ok {
		return
	}
	// The event fires for every nudge, escalated or not: what a surface draws is
	// "this turn is going in circles", which is true either way.
	hub.send(Event{
		Kind:  EventNudge,
		Tool:  looping.tool,
		Count: looping.count,
		Hint:  loopRule(looping),
	})

	// Past the ceiling, in prompt mode, with somebody there to answer: the
	// person is asked instead of the model being told again.
	if looping.nth > loopNudgeCeiling && a.promptMode() && a.config.AskConsent && hub != nil {
		// The question, and the recovery move it carries, are recovery.go's: by
		// this point the interesting decision is not "is this a loop" but "what do
		// we do about the mess", and that is a different file's job.
		a.askAboutLoop(ctx, hub, ep, looping)
		return
	}
	a.enqueueSteering(nudgeNote(looping))
}

// promptMode reports whether this session's blanket answer is "ask me".
//
// It reads the policy's DEFAULT rather than what the repeating call itself
// resolved to, because the question being asked is about the person and not
// about the tool: is this a session where somebody is expected to be answering
// questions? A session with no policy at all is not — nil allows everything,
// which is the ungated shape a headless caller and the tests run in — and an
// unset default reads as prompt, exactly as internal/approval reads it.
func (a *Agent) promptMode() bool {
	policy := a.config.ApprovalPolicy
	if policy == nil {
		return false
	}
	return policy.Default == approval.ActionPrompt || policy.Default == ""
}
