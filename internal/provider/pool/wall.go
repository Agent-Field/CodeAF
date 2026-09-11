package pool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// DefaultCallWall is how long one structuring completion may take before the
// system stops waiting on it.
//
// FOUR MINUTES IS A MEASURED FIGURE NOW, AND IT IS SHOWN TO THE MODEL. It used to
// be argued as "deliberately far past honest", on production rows where the
// slowest structuring call was under a minute. Issue #927 measured otherwise on
// a reasoning model with a long request: an honest compile of 225 seconds and
// spine passes of 53 to 165, beside a grounding pass that thought for the whole
// four minutes on one machine and answered in two seconds on another. So the
// wall is no longer only a line past which a completion is gone. It is the time
// the completion is TOLD it has: every walled request carries it to the effort
// ladder, which derives the thinking budget the model is given from it
// (provider's [provider.WithThinkingWall]), and a completion that still reaches
// it with thought on the wire is asked for its answer rather than thrown away
// ([walled.CompleteWithMessages]).
//
// The unit matters. This bounds ONE completion, not one command and not one
// agent loop: a head turn that makes nine tool calls gets nine fresh walls, and
// a leaf that thinks for an hour across forty round-trips is never touched by
// it. Bounding the call rather than the caller is what lets the number be small
// enough to catch a hang without ever cutting honest work in half.
//
// The incident it exists for: a contract call on a chat splice never returned.
// There was no deadline anywhere on the path — not on the context, not on the
// client, not on the transport for a streamed request — so the reconciler's
// command queue stopped forever behind one open socket while the process went
// on looking alive.
const DefaultCallWall = 4 * time.Minute

// completionsPerCall is how many completions one walled call may make: the one
// it was asked for, and — when that one reached its wall with thought on the
// wire — the one ask for the answer the thought reached. It is the count
// [LongestCall] multiplies and the count [walled.allowance] divides a short
// caller's time by, so the rail above and the split below are one figure.
const completionsPerCall = 2

// LongestCall is the longest one call through a slot walled at [DefaultCallWall]
// can honestly take: the completion it was asked for, cut at its wall, and the
// one ask for the answer that completion's thought had reached, under a wall of
// its own. It is exported for the reconciler's command rail, which is counted in
// these (internal/resident's commandWall) so that the two figures cannot drift.
const LongestCall = completionsPerCall * DefaultCallWall

// ErrCallWall is what a call that outlived its wall returns. It is an ordinary
// provider failure by design: every caller on the structuring path already has
// an error branch, and this arrives on it rather than inventing a new one. The
// layer above owns the retry — this layer refuses to wait forever and keeps
// what was thought ([walled.CompleteWithMessages]), and a call that returns this
// is one whose answer ask ran out of time as well.
//
// THE SENTENCE IS THE CAUSE. Silence never reaches this wall: a stream that stops
// writing is cut by the stream guard's own silence bounds long before four
// minutes, with a sentence of its own. What reaches it is a model still WORKING
// — in every row issue #927 measured, still thinking — so "stopped answering",
// which this said until then, was the one account of the event that was false.
//
// It wraps context.DeadlineExceeded deliberately. A stall has to be legible to
// the reconciler's watchdog, which decides between striking a command and
// failing it, and the alternative was for internal/resident to import this
// package for one sentinel. Wrapping the standard error instead means the
// watchdog asks the only question it actually has — "did this die of time?" —
// with errors.Is.
var ErrCallWall = fmt.Errorf("%s: %w", RanOutOfTime, context.DeadlineExceeded)

// RanOutOfTime is the cause, spelled ONCE for every sentence that carries it.
//
// Issue #927's receipt said "the model stopped answering", which was false —
// every first token had arrived in under a second. The same falsehood has two
// other spellings on the structuring road, and they are worse because they are
// machinery: a planning stage that ends on a clock reaches the person as
// `context deadline exceeded`, inside "the plan for task-2 was drawn with
// faults (size stage 1: context deadline exceeded)". A person cannot act on
// that sentence and it does not say what happened. [CauseInWords] is the one
// door every such sentence goes through, and this is the one phrase it uses.
const RanOutOfTime = "the model thought past its time"

// CauseInWords is an error as a person should read it: whatever it says about
// where it happened, with a clock's own vocabulary replaced by the cause.
//
// IT KEEPS THE PLACE AND REPLACES THE JARGON. "size stage 1: context deadline
// exceeded" becomes "size stage 1: the model thought past its time", because
// which pass ran out is information the person's next decision uses, while
// `context deadline exceeded` is this program's internals leaking. An error
// that did not die of time is handed back exactly as it came: this composer
// renames one cause and invents nothing.
func CauseInWords(err error) string {
	if err == nil {
		return ""
	}
	said := err.Error()
	if !errors.Is(err, context.DeadlineExceeded) {
		return said
	}
	// The wall's own error already names the cause and then wraps the standard
	// one, so the pair is collapsed first: a sentence that said it twice would
	// be this composer talking over itself.
	said = strings.ReplaceAll(said, RanOutOfTime+": "+context.DeadlineExceeded.Error(), RanOutOfTime)
	return strings.ReplaceAll(said, context.DeadlineExceeded.Error(), RanOutOfTime)
}

// walled bounds one completion at a time. It is a decorator over router.Client
// rather than a change inside the provider adapters because the wall is a
// policy about which calls the surface is willing to wait on, and the adapters
// do not know which call they are serving.
//
// It is created fresh from Snapshot on every read, so a model swap underneath
// is picked up without the wrapper ever holding a stale client.
type walled struct {
	inner router.Client
	wall  time.Duration
}

// wallClient wraps only when there is a wall to apply, so an unwalled slot —
// the work client, whose leaves are agent loops with their own governors —
// keeps handing back exactly the object it always did, type identity included.
func wallClient(inner router.Client, wall time.Duration) router.Client {
	if inner == nil || wall <= 0 {
		return inner
	}
	return walled{inner: inner, wall: wall}
}

func (w walled) Model() string { return w.inner.Model() }

// CompleteWithMessages is the wall's whole contract with the model, in three
// parts.
//
//   - THE WALL IS TOLD. The completion is sent with the wall on it, so the effort
//     ladder can give the thinking pass the budget the wall implies
//     ([provider.WithThinkingWall]).
//   - THE WALL KEEPS WHAT WAS THOUGHT — WHICHEVER CLOCK CUT IT. What streams is
//     kept as it arrives ([kept]). A completion that ends on a clock with thought
//     or answer on the wire is not thrown away: the model is asked ONCE more, on
//     the same lineage, with what it had worked out in front of it and its
//     thinking switched off, for the answer that work reached. The wall is not
//     the only clock over a completion — the dispatcher gives an attempt its own
//     patience and the stream guard bounds a quiet stream, and issue #927's
//     second run lost its size, bind and contract passes to one of those at 51
//     seconds, not to the four-minute wall — so what the salvage asks is whether
//     TIME ended this completion, never which timer did. Only when the answer ask
//     runs out too, or when nothing had arrived at all, does the caller get an
//     error, and it is the one its own cut named ([ErrCallWall] for the wall's).
//   - THE CALLER'S OWN CANCEL IS NEVER A PROVIDER FAILURE. A caller who went away
//     gets their own context error back untouched, and nothing is asked again.
//
// The answer ask is not a retry. A retry is the same bytes to another machine
// and it belongs to the layer above; this is a DIFFERENT question — "answer from
// what you have" — that only the wall can ask, because only the wall knows the
// first completion ended by time rather than by anything about the request.
func (w walled) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	thought := &kept{}
	first := w.allowance(ctx, completionsPerCall)
	response, how, err := w.completion(ctx, first, messages, options, thought)
	if !how.cut() {
		return response, err
	}
	asked, ok := thought.answerAsk(messages)
	if !ok {
		return nil, ranOut(how, first, err)
	}
	thought.tell(ctx)
	// THINKING OFF IS PART OF THIS ASK'S CORRECTNESS, not an economy, so it is
	// the required form: a seat's pinned effort must not put the model back into
	// the deliberation it has just been stopped out of.
	answer, _, askErr := w.completion(provider.WithRequiredReasoningEffort(ctx, provider.EffortOff),
		w.allowance(ctx, 1), asked, options, nil)
	if askErr == nil || ctx.Err() != nil {
		return answer, askErr
	}
	// The call died of time, and that is what the layer above is told whatever
	// the answer ask then ran into: the watchdog's question is whether the work
	// is worth another run, and it is exactly as worth one as it was before.
	return nil, fmt.Errorf("%w; asked for the answer it had reached: %v", ranOut(how, first, err), askErr)
}

// ending is how one completion finished, in the only three kinds the wall acts
// differently on.
type ending int

const (
	// finished is a completion that came back, or failed for a reason of its
	// own — a refusal, a bad request, a torn connection. Neither is the wall's
	// business and both go straight back to the caller.
	finished ending = iota
	// atTheWall is the wall's own deadline: this decorator set it, this
	// decorator told the model about it, and this decorator names it.
	atTheWall
	// onAnotherClock is a bound underneath the wall — the dispatcher's patience
	// for one attempt, the stream guard's silence bounds — reached while the
	// caller was still waiting. The thought is just as lost and just as
	// salvageable, and the account of what happened is already the cut's own.
	onAnotherClock
)

func (e ending) cut() bool { return e != finished }

// ranOut is the error a cut completion leaves behind when its answer could not
// be recovered: the wall names itself, and any other clock's error is handed
// back exactly as it came, because it already says what stopped the call and
// this decorator relabelling it would be a second account of one event.
func ranOut(how ending, wall time.Duration, err error) error {
	if how == atTheWall {
		return fmt.Errorf("%w after %s", ErrCallWall, wall)
	}
	return err
}

// allowance is the wall one completion really runs under, and so the one the
// model is told: the slot's own, or less when the caller's deadline says so.
//
// ONE DEADLINE, ONE DERIVATION. The completion's context is bounded by the
// earlier of the two, so a budget read off the slot's wall alone would tell a
// model four minutes of thinking inside the ninety seconds a late splice round
// has left of its command — and the caller's deadline, arriving first, would be
// a caller's cut, which asks nothing and keeps nothing. So the time a caller has
// left is divided between the completions still to come in this call: the first
// of [completionsPerCall] gets its share and the answer ask keeps the rest, which
// is what makes the wall, and never the caller, the bound that cuts the first
// one. The answer ask, the last completion, is given all that remains.
func (w walled) allowance(ctx context.Context, completions int) time.Duration {
	wall := w.wall
	if deadline, ok := ctx.Deadline(); ok {
		if share := time.Until(deadline) / time.Duration(completions); share < wall {
			wall = share
		}
	}
	return wall
}

// completion runs one completion under the wall and reports how it ended. The
// expiries are told apart here and nowhere else: a caller who cancelled gets
// their own context error back and is never salvaged for, the wall's own
// deadline is [atTheWall], and a completion that ran out of time on any other
// clock while its caller was still waiting is [onAnotherClock].
//
// A CLOCK IS RECOGNISED BY WHAT IT LEAVES, NOT BY WHOSE IT IS. There is no seam
// through which this layer could ask the dispatcher or the stream guard whether
// they gave up, and there should not be: what both leave behind is an error that
// says time ran out, and that is the whole of what the salvage needs to know.
//
// `wall` is the completion's own [walled.allowance], and it is both the deadline
// and what the model is told: the two are one figure by construction.
//
// `into`, when there is one, keeps what the completion streams. It is set on the
// first completion only: the answer ask is the last thing this wall will ask, so
// there is nothing its stream could be kept for.
func (w walled) completion(ctx context.Context, wall time.Duration, messages []ai.Message, options []ai.Option, into *kept) (*ai.Response, ending, error) {
	callCtx, cancel := context.WithTimeout(ctx, wall)
	defer cancel()
	callCtx = provider.WithThinkingWall(callCtx, wall)
	if into != nil {
		callCtx = provider.WithStreamObserver(callCtx, into.listen(ctx))
		defer into.close()
	}
	response, err := w.inner.CompleteWithMessages(callCtx, messages, options...)
	if err == nil || ctx.Err() != nil {
		return response, finished, err
	}
	if callCtx.Err() != nil {
		return nil, atTheWall, err
	}
	if ranOutOfTime(err) {
		return nil, onAnotherClock, err
	}
	return response, finished, err
}

// ranOutOfTime reports whether an error is a clock's and not the request's.
//
// Both shapes are here because both happen on the structuring road and both
// lose a thought. A deadline under the wall — the dispatcher's own patience for
// an attempt — arrives as context.DeadlineExceeded however deeply it is
// wrapped; a stream the guard gave up on is read through the guard's own door
// ([provider.CutFrom]), which exists precisely so that a decision about a cut is
// not made by matching substrings of a sentence.
func ranOutOfTime(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	_, cut := provider.CutFrom(err)
	return cut
}

// kept is what one walled completion has streamed, held as it arrives.
//
// IT IS THE STREAM'S OWN ACCOUNT AND NOT THE RESPONSE'S. A completion the wall
// cuts returns no response — the provider's race hands its caller the deadline
// and nothing else — so what the model wrote before the cut exists in exactly
// one place: the events that reached the observer while it was writing. This
// listens to them as the caller would, forwards every one of them unchanged to
// whoever the caller had listening, and keeps the two things an answer ask
// needs: the thought, and any answer that had begun — words, or tool calls the
// model had started to write.
//
// It is written from the provider's read loop, on the goroutine of whichever arm
// is speaking, and read by the wall once the completion has ended — so it is
// locked, and it stops forwarding when the completion ends ([kept.close]): an
// arm unwinding after the cut is still a stream about a completion the caller
// has already been told is over, and its last words would land in the middle of
// the answer ask's.
//
// Listening changes nothing about the call itself: the adapter streams whether
// or not anybody listens, and what it says only to a listener — a line owed to a
// person who is watching — is said only on a call whose role is one a person
// watches, and those callers bring an observer of their own, which is still the
// one everything reaches.
type kept struct {
	mu      sync.Mutex
	thought strings.Builder
	answer  strings.Builder
	// calls are the tool calls the model had begun, by the index the wire gave
	// them: what it was calling and the arguments as far as they had arrived. A
	// call is answer being written (provider's forming event says so), so it is
	// kept exactly as the words are and travels with them.
	calls []begunCall
	// shown says the caller's surface is holding something of this completion's
	// answer — words, or a call it drew forming — which an answer ask has to
	// void before its own arrives. It is true exactly when something of the
	// answer is kept, so what is on the screen and what is asked from never
	// disagree.
	shown  bool
	closed bool
}

// begunCall is one tool call as far as it had arrived when the wall came.
type begunCall struct {
	index     int
	tool      string
	arguments string
}

// listen is the observer the completion is sent with: it keeps, then forwards
// to the caller's own context, in the order the provider raised the events.
func (k *kept) listen(caller context.Context) provider.StreamObserver {
	return func(event provider.StreamEvent) {
		k.mu.Lock()
		if k.closed {
			k.mu.Unlock()
			return
		}
		k.note(event)
		k.mu.Unlock()
		provider.EmitEvent(caller, event)
	}
}

// note folds one event into what is kept. Callers hold k.mu.
//
// A REPLACEMENT VOIDS WHAT CAME BEFORE IT, here exactly as on a surface: the
// provider raises it when a rescue's answer takes over from the one being
// shown, and the text above it is then the answer of an arm that lost.
func (k *kept) note(event provider.StreamEvent) {
	switch event.Kind {
	case provider.StreamReasoning:
		k.thought.WriteString(event.Delta)
	case provider.StreamDelta:
		k.answer.WriteString(event.Delta)
		k.shown = true
	case provider.StreamToolCallForming:
		k.begin(event)
		k.shown = true
	case provider.StreamReplaced:
		k.thought.Reset()
		k.answer.Reset()
		k.calls = nil
		k.shown = false
	}
}

// begin keeps one forming call's latest account of itself. A forming event's
// Delta is the call's ACCUMULATED arguments, so the newest one for an index
// replaces the one before it rather than adding to it; the name arrives on an
// early fragment and is kept once said. Callers hold k.mu.
func (k *kept) begin(event provider.StreamEvent) {
	for at := range k.calls {
		if k.calls[at].index == event.Index {
			if event.Tool != "" {
				k.calls[at].tool = event.Tool
			}
			k.calls[at].arguments = event.Delta
			return
		}
	}
	k.calls = append(k.calls, begunCall{index: event.Index, tool: event.Tool, arguments: event.Delta})
}

// close ends the completion's stream: nothing arriving after it is kept or
// forwarded.
func (k *kept) close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.closed = true
}

// answerAsk is the request that asks for the answer: the caller's own messages,
// then what the model wrote as the assistant turn it was, then the ask. It
// reports false when nothing was written, which is a completion that reached
// the wall with nothing to keep — there is no work in hand to answer from, and
// asking again would be a retry, which is the layer above's.
//
// The arrangement is the one every provider agrees on and the one shaped's
// continuation uses: an assistant message is what was said, and a user message
// is what is being asked next. The thought travels as the assistant's words
// rather than as a replayed reasoning field because what the second request is
// FOR is that the model reads it, and a chat template is free to drop an earlier
// turn's reasoning field before the model ever sees it. It travels WHOLE: the
// model wrote it inside this same window, so it fits there by construction, and
// the end of a thought is where its conclusions are.
func (k *kept) answerAsk(messages []ai.Message) ([]ai.Message, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	parts := []string{strings.TrimSpace(k.thought.String()), strings.TrimSpace(k.answer.String())}
	for _, call := range k.calls {
		// A half-sent call is NOT an instruction and is never replayed as one:
		// it travels as words saying what had been begun, so the model can
		// finish the thought it was acting on and make the call again whole.
		parts = append(parts, fmt.Sprintf("I had begun calling %s with: %s", call.tool, call.arguments))
	}
	var said []string
	for _, part := range parts {
		if part != "" {
			said = append(said, part)
		}
	}
	worked := strings.Join(said, "\n\n")
	if worked == "" {
		return nil, false
	}
	asked := make([]ai.Message, 0, len(messages)+2)
	asked = append(asked, messages...)
	return append(asked,
		ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: worked}}},
		ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: answerNowPrompt}}}), true
}

// tell says, on the caller's stream, that the answer is being asked for. A
// surface holding a begun answer is told to throw it away, because the answer
// ask's reply is the whole answer and would otherwise be drawn after half of
// one; a surface holding only "thinking" is told what the next wait is.
func (k *kept) tell(caller context.Context) {
	k.mu.Lock()
	kind := provider.StreamNotice
	if k.shown {
		kind = provider.StreamReplaced
	}
	k.mu.Unlock()
	provider.Emit(caller, kind, askingForTheAnswer)
}

// askingForTheAnswer is the line a person watching the call reads when its wall
// is reached with thought in hand: what happened, then what is being done.
const askingForTheAnswer = "the model thought past its time — asking it for the answer it reached"

// answerNowPrompt is the answer ask itself. It says why there is no more time,
// what is above, and what shape to answer in — the request's own, which still
// travels on this send: it asks for the WHOLE answer rather than the rest of
// one, and a whole answer has the shape the first request asked for.
const answerNowPrompt = `Your time to think about this has run out. What you had worked out so far is above, with the start of your answer if you had begun one.

Give your complete, final answer now, working from that. Answer in exactly the form the request above asks for, with nothing before or after it. Do not reason about it any further.`

// WithCallWall sets the per-completion wall for every call served through this
// slot, including the ones a caller makes on a snapshot it took earlier.
//
// It is opt-in per slot, and the split is the whole safety argument. The
// structuring slots — the one that talks and the one that plans — are single
// completions whose only honest duration is short, so they are walled. The work
// slot is not: a leaf is an agent loop bounded by the executor's own deadline,
// and a wall here would be a second, dumber governor over work that is
// legitimately allowed to take hours.
func (l *Client) WithCallWall(wall time.Duration) *Client {
	if l == nil {
		return l
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wall = wall
	return l
}

// CallWall reports the wall in force on this slot; zero means unbounded.
func (l *Client) CallWall() time.Duration {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.wall
}
