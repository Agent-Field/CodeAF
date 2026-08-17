package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── retry constants (pi spec §4, verbatim from internal/exec/bare) ──────────

// maxRetries is pi's default auto-retry count: 3. The schedule is 2s, 4s, 8s
// (baseDelayMs=2000 * 2**(attempt-1)).
const maxRetries = 3
const retryBaseDelay = 2 * time.Second

// ── retry classification (pi-ai compat, verbatim from internal/exec/bare) ───

// nonRetryablePattern matches provider errors that are permanent (quota,
// billing, account limits). pi's NON_RETRYABLE_PROVIDER_LIMIT_ERROR_PATTERN.
var nonRetryablePattern = regexp.MustCompile(
	`(?i)` + strings.Join([]string{
		"GoUsageLimitError",
		"FreeUsageLimitError",
		"Monthly usage limit reached",
		"available balance",
		"insufficient_quota",
		"out of budget",
		"quota exceeded",
		"billing",
	}, "|"))

// retryablePattern matches transient provider errors. pi's
// RETRYABLE_PROVIDER_ERROR_PATTERN. A message is retryable when it matches
// this AND does not match nonRetryablePattern.
var retryablePattern = regexp.MustCompile(
	`(?i)` + strings.Join([]string{
		"overloaded",
		`rate.?limit`,
		"too many requests",
		"429",
		"500",
		"502",
		"503",
		"504",
		"524",
		`service.?unavailable`,
		`server.?error`,
		`internal.?error`,
		`provider.?returned.?error`,
		`network.?error`,
		`connection.?error`,
		`connection.?refused`,
		`connection.?lost`,
		"other side closed",
		"fetch failed",
		"getaddrinfo",
		"ENOTFOUND",
		"EAI_AGAIN",
		`upstream.?connect`,
		"reset before headers",
		"socket hang up",
		"socket connection was closed",
		`timed? out`,
		"timeout",
		"terminated",
		`websocket.?closed`,
		`websocket.?error`,
	}, "|"))

// contextOverflowPattern matches context-overflow errors. pi does NOT retry
// these — it compacts instead.
var contextOverflowPattern = regexp.MustCompile(
	`(?i)` + strings.Join([]string{
		"context.?length",
		"context.?window",
		"maximum.?context",
		"token.?limit",
		`context.?limit`,
		"prompt.?is.?too.?long",
		"too.?many.?tokens",
	}, "|"))

// isRetryable reports whether a provider error is retryable per pi's
// isRetryableAssistantError: it must match the retryable pattern and NOT match
// the non-retryable (quota/billing) pattern.
func isRetryable(errMsg string) bool {
	if nonRetryablePattern.MatchString(errMsg) {
		return false
	}
	return retryablePattern.MatchString(errMsg)
}

// isContextOverflow reports whether a provider error is a context-overflow
// error. These are NOT retried — the turn compacts and tries again.
func isContextOverflow(errMsg string) bool {
	return contextOverflowPattern.MatchString(errMsg)
}

// hintLimit bounds an Event.Hint. It is a one-line gloss beside a tool name in
// a terminal column, not a result: 80 columns is where it stops being one.
const hintLimit = 80

// argsLimit bounds Event.Args. Like [outputLimit] it is a DISPLAY cap: the
// wire arguments the model sent and the transcript records are untouched.
//
// It is 8k rather than a paragraph's worth because of what a surface derives
// from this field. An edit call's "+3 −1" and its unified diff are computed
// from the old/new strings the call carried (docs/CHAT-V3.md D11, rendered in
// internal/tui3) — the tool's own result is one sentence saying it worked, so
// the arguments are the ONLY record of the change that reaches a screen. A cap
// that cut them at 400 bytes did not shorten the diff; it produced the wrong
// number and a diff that stopped mid-line.
const argsLimit = 8192

// outputLimit bounds Event.Output. It is a display copy, not the result: the
// model still reads the full text off the transcript. 4000 bytes is a screen
// or two of a file or a build log, which is what an expanded row is for.
const outputLimit = 4000

// ── the turn ────────────────────────────────────────────────────────────────

// runTurn executes one Submit: provider requests interleaved with tool
// execution until the assistant answers without a tool call, the person
// interrupts, or the provider fails permanently.
//
// It mirrors internal/exec/bare's loop — same message assembly, same stop
// condition, same retry schedule, same per-batch tool parallelism with results
// appended in call order — with the two interactive differences: steering
// drains at each step boundary, and cancellation keeps the partial reply.
//
// It reports whether the turn COMPLETED: the model answered without a tool
// call, and nobody interrupted and nothing failed. Only that outcome may drain
// a follow-up (agent.go) — an interrupted or faulted turn must not be the thing
// that starts the next one.
func (a *Agent) runTurn(ctx context.Context, hub *eventHub) bool {
	started := time.Now()
	var turn Usage

	// partial accumulates what the model has streamed for the CURRENT step.
	// It is the transcript's answer for an interrupted step, where no response
	// ever comes back.
	partial := &partialBuffer{}

	// warm holds the read-only calls this turn started while their response was
	// still streaming. It belongs to the turn and is emptied per attempt — see
	// [warmBatch] for the law that decides what may start early at all.
	warm := &warmBatch{}

	// forming holds the calls this turn has watched ARRIVE but not yet finish
	// (toolhint.go). It has the warm batch's lifetime and is emptied in the same
	// breath for the same reason: a retry's calls are its own, and half of a dead
	// attempt's arguments describe bytes nobody will ever be sent.
	forming := &formingBatch{}

	// episode is this turn's CONTROL PLANE (hooks.go): the four named seams and
	// the state their citizens keep — the loop detector's window (looped.go), the
	// ledger of what this turn changed (recovery.go). It belongs to the turn and
	// is built here rather than held on the Agent for the reason the warm batch
	// is: both of those are facts about ONE turn's work, and a detector that
	// remembered yesterday's repetitions would nudge a model for a call it is
	// making for the first time today.
	//
	// This is `episode-init`, the first of the four hooks, and it is the only one
	// called by name from this function; the other three are called at the three
	// lines below that used to call a mechanism directly.
	episode := a.newEpisode()

	// toolCtx is the turn's context WITHOUT the observer installed below. An
	// early tool must run under the turn's cancellation and nothing else; giving
	// it a context pointed back at the stream it was started from would be a
	// loop, and reading the reassigned ctx from inside the closure would be a
	// second reader of a variable the loop writes.
	toolCtx := ctx

	ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		switch event.Kind {
		case provider.StreamDelta:
			partial.write(event.Delta)
			hub.send(Event{Kind: EventTextDelta, Text: event.Delta})
		case provider.StreamThinking:
			hub.send(Event{Kind: EventThinking})
		case provider.StreamReasoning:
			// Reasoning is NOT written to partial: it is the model's working,
			// not its answer, and an interrupted step that recorded it would put
			// the thought in the transcript as something the assistant said.
			hub.send(Event{Kind: EventReasoning, Text: event.Delta})
		case provider.StreamNotice:
			// The adapter reshaping the request to get it accepted at all
			// (internal/provider's endpoints.go). It is not the model speaking and
			// not a failure, so it rides as a note rather than as text: nothing of
			// it reaches the transcript, and the person sees which attempt they
			// are on and what was taken off to get there.
			hub.send(Event{Kind: EventNotice, Text: event.Delta})
		case provider.StreamToolCallForming:
			// The seconds BEFORE the announcement, which the person used to
			// watch as silence. Nothing here starts anything and nothing here
			// is parsed as an instruction: the batch decides how often this
			// call may speak and what it can honestly say about arguments that
			// are still arriving, and the answer rides as a row that the
			// announcement below will replace.
			if formed, speak := forming.note(event); speak {
				hub.send(formed)
			}
		case provider.StreamToolCallReady:
			// ANNOUNCE FIRST, then decide whether it may start. The order is the
			// meaning: the person sees every call the moment the model finishes
			// asking for it, and only the calls the law allows actually move.
			warm.announce(hub, event.Delta)
			warm.consider(toolCtx, a, episode, hub, event.Delta)
		}
	})

	// The model is latched for the whole turn. SetModel's contract is that a
	// turn in flight finishes on the model it started on, and reading a.model
	// per step broke it: a swap between two steps would send one model the
	// transcript another model was mid-way through writing.
	//
	// The reasoning level is latched WITH it, in the same breath and for the
	// same reason — and it is latched for THIS model, so a swap mid-turn cannot
	// leave the turn sending one model's level with another model's name.
	model := a.Model()
	a.mu.Lock()
	effort := a.reasoningLocked(model)
	a.mu.Unlock()

	// overflowCompacted bounds the compact-and-retry answer to a context
	// overflow at one pass per turn. A second overflow after a successful
	// compaction is not a context problem this loop can fix by shrinking
	// further, and retrying it forever would burn a summary call per attempt.
	overflowCompacted := false

	for {
		// The cancel check comes BEFORE the drain: steering typed in the
		// instant before an interrupt must not be spliced into a transcript
		// this turn is abandoning unanswered. Nothing is lost — the turn's end
		// drains the queue under the same lock that clears running (agent.go),
		// so a leftover lands ahead of the next Submit's message.
		if ctx.Err() != nil {
			a.keepPartial(partial)
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started)})
			return false
		}

		// Steering lands here, between batches: the transcript tail is a tool
		// result or an assistant answer, both legal places for a user message.
		a.drainSteering()

		response, err := a.completeWithRetry(ctx, model, effort, partial, warm, forming)
		if err != nil {
			// Interrupt (or the caller's own deadline). Whatever was streamed
			// before the cut is real work the person watched arrive, so it
			// stays in the transcript and the turn ends normally.
			if ctx.Err() != nil {
				a.keepPartial(partial)
				hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started)})
				return false
			}
			// Overflow is the one error with an answer other than reporting
			// it: compact and re-send the same step. It fires regardless of
			// CompactEnabled — that flag gates the automatic pass, not the
			// recovery from a request the provider has already refused.
			if isContextOverflow(err.Error()) && !overflowCompacted {
				overflowCompacted = true
				if compacted, compactErr := a.compact(ctx, hub); compacted && compactErr == nil {
					continue
				}
			}
			// A permanent failure mid-stream is still a step the person
			// watched: the streamed text is kept and the turn is sealed, so an
			// error leaves the same record an interrupt does and the surface
			// gets the turn's duration with the reason.
			a.keepPartial(partial)
			hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(turn, started)})
			return false
		}

		turn.Turns++
		a.addUsage(&turn, response)

		calls := response.ToolCalls()

		// Stop condition (pi spec §2): the loop ends when the assistant
		// response has NO tool call.
		if len(calls) == 0 {
			a.record(ai.Message{Role: "assistant", Content: assistantContent(response)})
			// The step's text is in the transcript now. Resetting here rather
			// than at the top of the next iteration is what keeps an interrupt
			// arriving during the tool batch from recording it a second time.
			partial.reset()
			// `pre-decision` (hooks.go): the last chance to shape what the model
			// will be sent next. Its one citizen today is the stubbing pass, which
			// runs BEFORE the compaction check, and the order is the whole economy
			// of it (stub.go): a transcript whose old heavy results have just
			// become one-line pointers may no longer be over the threshold at all,
			// so the check that follows is made against what the next request will
			// actually weigh.
			episode.preDecision(ctx)
			a.maybeCompact(ctx, hub)
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started)})
			// The name comes after the turn is done and before the hub closes:
			// the person is not kept waiting on a title, and the event still has
			// a stream to land on (title.go).
			a.maybeTitle(ctx, hub)
			return true
		}

		a.record(ai.Message{Role: "assistant", Content: assistantContent(response), ToolCalls: calls})
		partial.reset()

		results := a.runToolsWarm(ctx, episode, calls, hub, warm)

		// Results append in the order the calls were issued, never in the
		// order they finished: the pairing with tool_call_id is by id, but the
		// transcript a later step reads is a narrative.
		for index, call := range calls {
			a.record(ai.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    []ai.ContentPart{{Type: "text", Text: results[index].text}},
			})
		}

		// `post-feedback` (hooks.go): the step boundary, where the turn's ledger
		// reads what the batch changed and the detector says whether the turn is
		// going in circles (recovery.go, looped.go). The batch is recorded, the
		// next request has not been assembled, and a note dropped here rides into
		// it exactly as a person's steering does.
		episode.postFeedback(ctx, hub, calls, results)

		a.maybeCompact(ctx, hub)
	}
}

// partialBuffer holds what the model has streamed for the current step. The
// mutex is not decoration: the observer is called by whoever is reading the
// provider connection, while the loop reads and clears it.
type partialBuffer struct {
	mu   sync.Mutex
	text strings.Builder
}

func (p *partialBuffer) write(delta string) {
	p.mu.Lock()
	p.text.WriteString(delta)
	p.mu.Unlock()
}

func (p *partialBuffer) reset() {
	p.mu.Lock()
	p.text.Reset()
	p.mu.Unlock()
}

// take returns the buffered text and empties the buffer, so no text can be
// recorded twice.
func (p *partialBuffer) take() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	text := p.text.String()
	p.text.Reset()
	return text
}

// keepPartial records the interrupted step's streamed text as an assistant
// message. Nothing is recorded when nothing was streamed — an empty assistant
// turn is noise in the transcript and a shape some providers reject.
func (a *Agent) keepPartial(partial *partialBuffer) {
	text := partial.take()
	if strings.TrimSpace(text) == "" {
		return
	}
	a.record(textMessage("assistant", text))
}

// sealTurn stamps the turn's wall duration and folds it into the session
// total.
func (a *Agent) sealTurn(turn Usage, started time.Time) Usage {
	turn.Duration = time.Since(started)
	a.mu.Lock()
	a.usage.Duration += turn.Duration
	a.mu.Unlock()
	return turn
}

// completeWithRetry sends one provider request, retrying on retryable errors
// with pi's exact schedule: 2s, 4s, 8s, max 3 retries. The transcript is
// append-only and the failing response was never appended, so a retry re-sends
// exactly the same messages (pi's _prepareRetry pop is a no-op in this shape —
// see internal/exec/bare/loop.go). The model is the turn's, latched once by
// runTurn.
//
// The reasoning level is stamped HERE, on the request path and nowhere else, so
// it reaches every step and every retry of the turn and reaches nothing else:
// the compaction summary and the title call are the session's own errands, not
// the person's question, and a level they asked for their conversation to be
// thought about would be an odd thing to spend on naming it.
//
// The stamp is [provider.WithConfiguredReasoningEffort] — the OPERATOR-explicit
// setter — because this level is exactly that: a person turned a knob. The other
// setter, WithReasoningEffort, is for harness defaults, and the adapter drops
// those unless a catalog can vouch for the model (provider's requestedEffort).
// This session's client is built without that catalog seam, so a harness-default
// stamp here would be dropped every time and the knob would do nothing. Nothing
// is stamped when no level is set: an unstamped context is the one shape that
// leaves the request byte-for-byte what it was.
func (a *Agent) completeWithRetry(ctx context.Context, model string, effort provider.Effort, partial *partialBuffer, warm *warmBatch, forming *formingBatch) (*ai.Response, error) {
	if effort != provider.EffortNone {
		ctx = provider.WithConfiguredReasoningEffort(ctx, effort)
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Each attempt streams the reply from the beginning, so the buffer
		// starts empty: an attempt that dies half-way through its text and an
		// interrupt during the next one would otherwise record the two halves
		// concatenated as one answer.
		partial.reset()
		// And so does the warm batch. A retry is a NEW response — its calls are
		// its own, ids and all — so nothing the dead attempt started may be
		// paired with it. The reads that already ran are simply thrown away and
		// re-run, which is the whole reason only read-only tools may start early.
		warm.reset()
		// And the calls that were still arriving when the attempt died. Their
		// half-read arguments belong to a response nobody will ever be sent, and
		// a scanner that kept them would gloss the retry's first call with the
		// dead attempt's path.
		forming.reset()

		messages := a.snapshot()
		response, err := a.client.CompleteWithMessages(ctx, messages,
			ai.WithModel(model), ai.WithTools(a.beltDefinitions()))
		if err == nil {
			return response, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errMsg := err.Error()
		if isContextOverflow(errMsg) || !isRetryable(errMsg) {
			return nil, err
		}
		if attempt < maxRetries {
			delay := retryBaseDelay * (1 << attempt) // 2s, 4s, 8s
			if waitErr := backoffWait(ctx, delay); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func backoffWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ── tools ───────────────────────────────────────────────────────────────────

type toolResult struct {
	text    string
	isError bool
}

// ── the early-start law ─────────────────────────────────────────────────────

// earlyTools is the set of calls that may begin while their response is still
// streaming. It is a LIST, not a property, and it is short on purpose.
//
// ── THE SAFETY LAW ──
//
// A MUTATING CALL NEVER STARTS EARLY. The stream it was announced on can still
// fail — a socket that dies at the last chunk, a 502 between two deltas — and
// that failure is RETRYABLE: completeWithRetry re-sends the same transcript and
// the model issues the batch again. A write or a bash that had already run would
// then run a SECOND time, on nobody's instruction, with the first run recorded
// nowhere. There is no bookkeeping that fixes this, because the loop cannot know
// whether the first run's effects are still there.
//
// A READ-ONLY CALL IS IDEMPOTENT, so the same retry costs only the work: reading
// a file twice returns the file twice and changes nothing about the world or the
// transcript. That asymmetry — not speed, not the tool's cost — is the entire
// reason the two sets are treated differently, and it is why membership here is
// enumerated rather than inferred from a flag a tool could set about itself.
//
// The names are the belt's four readers (bare/tools.go). Anything not named
// here — write, edit, bash, a tool the workforce adds later, a tool a test
// appends — waits for the response to complete, exactly as before this existed.
var earlyTools = map[string]bool{
	"read": true,
	"grep": true,
	"find": true,
	"ls":   true,
}

// warmBatch is the results of calls started before their response arrived.
//
// It is a RACE TO WARM RESULTS, never a dispatch: every call in the batch is
// still accounted for by runTools, which consults this and runs whatever is not
// here. Nothing observable moves — no event is emitted early, no message is
// journaled early, results still append in call order after the response
// completes — so what a person watches and what the transcript records are
// byte-for-byte what they were. The only difference is that a read may already
// be finished by the time the batch starts.
// It also remembers which calls have been ANNOUNCED (EventToolAnnounced), which
// is not a warm start and lives here anyway for one reason: the reset boundary
// is identical. A retry is a new response whose calls are its own, so both the
// early results and the announcements of the dead attempt are thrown away
// together, and a second bookkeeper with the same lifetime would be a second
// thing to remember to reset.
type warmBatch struct {
	mu        sync.Mutex
	started   map[string]*warmCall
	announced map[string]bool
}

// announce emits EventToolAnnounced for one provider.StreamToolCallReady
// payload — every call, whatever the early-start law then decides about it.
//
// A call is announced ONCE. The provider sends one ready event per call, so the
// guard is belt and braces rather than a fix for something seen; the cost of
// being wrong the other way is a row drawn twice, which is a row the person
// cannot reconcile with the batch that follows.
func (b *warmBatch) announce(hub *eventHub, payload string) {
	if b == nil {
		return
	}
	var call ai.ToolCall
	if err := json.Unmarshal([]byte(payload), &call); err != nil {
		return
	}
	if call.Function.Name == "" {
		return
	}
	if call.ID != "" {
		b.mu.Lock()
		if b.announced == nil {
			b.announced = make(map[string]bool, 2)
		}
		if b.announced[call.ID] {
			b.mu.Unlock()
			return
		}
		b.announced[call.ID] = true
		b.mu.Unlock()
	}
	// THE ID RIDES WITH IT. A surface that has been drawing this call's forming
	// row since its first fragment adopts that row on this event, and the only
	// thing that says WHICH row is the provider's id: a batch of three parallel
	// writes forms three rows, and an announcement with no id can be paired only
	// by tool name — oldest-of-that-tool, which is a guess that is right by
	// convention and wrong the moment the provider closes them out of order.
	// The id is already in hand here; carrying it costs a field.
	hub.send(Event{
		Kind:   EventToolAnnounced,
		Tool:   call.Function.Name,
		CallID: call.ID,
		Hint:   gloss(call),
		Args:   argsText(call),
	})
}

// warmCall is one early execution: the call it was started for, and a channel
// closed when the result lands.
type warmCall struct {
	call   ai.ToolCall
	done   chan struct{}
	result toolResult
}

// consider takes one provider.StreamToolCallReady payload and starts the call if
// the law allows it. Everything it declines — a payload that will not parse, a
// nameless call, a mutating call, a call already started — is a silent no-op,
// because declining costs nothing: the batch runs it in a moment anyway.
//
// It is called from the provider's read loop, which must not work, so the parse
// is one small unmarshal and the execution is somebody else's goroutine.
func (b *warmBatch) consider(ctx context.Context, a *Agent, ep *episode, hub *eventHub, payload string) {
	var call ai.ToolCall
	if err := json.Unmarshal([]byte(payload), &call); err != nil {
		return
	}
	if call.ID == "" || !earlyTools[call.Function.Name] {
		return
	}
	// A tool the belt does not have would only produce "Unknown tool" early
	// instead of late; refusing here keeps a warm result from ever being an
	// answer the live belt would not have given.
	if !a.hasTool(call.Function.Name) {
		return
	}
	// A CALL THAT MUST ASK DOES NOT START EARLY. This is not a second gate —
	// executeTool still decides, with the same policy, and a call allowed here
	// is allowed there — it is the early-start law meeting the consent one: the
	// question would be about an instruction the model has not finished
	// sending, and a retry (which throws every warm result away) would ask it a
	// second time about a call that never ran. Anything but allow waits for the
	// batch, where it is asked exactly once.
	if decision, governed := a.decide(call); governed && decision.Action != approval.ActionAllow {
		return
	}

	b.mu.Lock()
	if b.started == nil {
		b.started = make(map[string]*warmCall, 2)
	}
	if _, running := b.started[call.ID]; running {
		b.mu.Unlock()
		return
	}
	// The result is seeded BEFORE the goroutine that fills it, for the reason
	// runTools seeds its slots: a tool that panics would otherwise leave the zero
	// value, and an empty SUCCESS is the one story about the fault that is not
	// true. The wording is runTools' own, so a panic reads the same to the model
	// whether the call started early or in the batch.
	warm := &warmCall{
		call: call,
		done: make(chan struct{}),
		result: toolResult{
			text:    "tool panicked: " + call.Function.Name + " did not return a result",
			isError: true,
		},
	}
	b.started[call.ID] = warm
	b.mu.Unlock()

	go func() {
		defer close(warm.done)
		defer guard.Recover("session early tool " + call.Function.Name)
		warm.result = a.executeTool(ctx, ep, hub, call)
	}()
}

// reset empties the batch. Goroutines already running are left to finish and
// their results are dropped: cancelling them would buy nothing — the work is a
// read — and the context they run on is the turn's, which ends when the turn
// does.
func (b *warmBatch) reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.started, b.announced = nil, nil
	b.mu.Unlock()
}

// take claims the early execution of one call, if there is one for it.
//
// The match is by id AND by the call itself. The id alone would be enough for
// every endpoint that exists, but the early sighting is assembled from stream
// fragments and the response's is assembled from all of them: if those two ever
// disagreed about a call's name or arguments, the response is right, and this
// returns nothing rather than pairing a result with an instruction that is not
// the one the model finally sent.
func (b *warmBatch) take(call ai.ToolCall) *warmCall {
	if b == nil || call.ID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	warm, known := b.started[call.ID]
	if !known {
		return nil
	}
	delete(b.started, call.ID)
	if warm.call.Function.Name != call.Function.Name ||
		warm.call.Function.Arguments != call.Function.Arguments {
		return nil
	}
	return warm
}

// hasTool reports whether the belt carries a tool by this name.
func (a *Agent) hasTool(name string) bool {
	for _, tool := range a.beltTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// runTools executes one batch with nothing started early. It is the whole of
// what this was before the streamed sighting existed, and the shape every
// caller outside the turn uses.
//
// It builds a control plane of its own (hooks.go) rather than taking one,
// because a batch run outside a turn is still a batch: the calls in it must
// still pass the approval gate, and an episode is the only thing that carries
// it. What that episode's per-turn state remembers dies with the call, which is
// correct — there is no turn here to be stuck in.
func (a *Agent) runTools(ctx context.Context, calls []ai.ToolCall, hub *eventHub) []toolResult {
	return a.runToolsWarm(ctx, a.newEpisode(), calls, hub, nil)
}

// runToolsWarm executes one batch concurrently and reports it in call order,
// adopting whatever the stream already started (warm may be nil).
//
// The begins are emitted for the whole batch before the first goroutine
// starts, and the ends after the last one finishes, both in call order. The
// alternative — emitting from inside each goroutine — would put the person's
// transcript in scheduler order, which differs run to run for the same work.
// AN EARLY START CHANGES NEITHER: a call that is already running is waited for
// here, in its slot, and its begin is emitted with the rest of the batch. The
// person watches the same turn they always did.
func (a *Agent) runToolsWarm(ctx context.Context, ep *episode, calls []ai.ToolCall, hub *eventHub, warm *warmBatch) []toolResult {
	for _, call := range calls {
		hub.send(Event{
			Kind: EventToolBegin,
			Tool: call.Function.Name,
			Hint: gloss(call),
			Args: argsText(call),
		})
	}

	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup
	for index, call := range calls {
		// The slot is seeded BEFORE the goroutine that fills it. A tool that
		// panics leaves its slot untouched, and the zero result is an empty
		// SUCCESS — the model reads a tool that ran and returned nothing,
		// which is the one story about the fault that is not true.
		results[index] = toolResult{
			text:    "tool panicked: " + call.Function.Name + " did not return a result",
			isError: true,
		}
		// A call the stream already started is not started again — the warm
		// entry is claimed by id, so no call in this batch can run twice — and
		// waiting for it is one more goroutine in the same batch, so a read that
		// is still going does not hold up its siblings.
		if started := warm.take(call); started != nil {
			wg.Add(1)
			go func(idx int, running *warmCall) {
				defer wg.Done()
				// Waited for unconditionally, exactly as an ordinary tool is:
				// the early execution rides the SAME turn context, so an
				// interrupt ends it on the same beat it would end a call started
				// here — and a slot abandoned on a cancelled context would be the
				// one difference an early start was allowed to make.
				<-running.done
				results[idx] = running.result
			}(index, started)
			continue
		}
		wg.Add(1)
		go func(idx int, c ai.ToolCall) {
			// wg.Done outermost, so a faulted tool still releases the batch:
			// the slot this goroutine owns keeps its seeded panic result
			// rather than hanging every sibling behind a Wait that never
			// returns.
			defer wg.Done()
			defer guard.Recover("session tool " + c.Function.Name)
			results[idx] = a.executeTool(ctx, ep, hub, c)
		}(index, call)
	}
	wg.Wait()

	// The end events carry Args as well as Output. Re-rendering the arguments
	// here rather than making the surface remember the begin event costs one
	// compaction of a string already in hand — the call is in scope — and buys
	// an end event that is self-contained, which is what a surface that renders
	// a finished row from one event needs.
	for index, call := range calls {
		if results[index].isError {
			hub.send(Event{
				Kind:   EventToolFailed,
				Tool:   call.Function.Name,
				Hint:   clip(firstLine(results[index].text), hintLimit),
				Args:   argsText(call),
				Output: capOutput(results[index].text),
			})
			continue
		}
		// A successful tool's hint is empty: the result belongs to the model,
		// and the person already read what the call was going to do. Output is
		// there for a person who asks to see it anyway.
		hub.send(Event{
			Kind:   EventToolEnd,
			Tool:   call.Function.Name,
			Args:   argsText(call),
			Output: capOutput(results[index].text),
		})
	}
	return results
}

// executeTool dispatches one call to the matching belt tool.
//
// ── THE PRE-ACTION CHOKEPOINT ──
//
// The `pre-action` hook (hooks.go) is here, and here only — the consent gate
// (consent.go) with the guardian inside it (guardian.go), and the ledger that
// notes what a mutating call is about to change (recovery.go). This is the one
// function every execution passes through: the batch runs its calls through it
// (runToolsWarm), and so does the early start that begins a read while the
// response is still streaming (warmBatch.consider). A gate wrapped around the
// belt's tools instead would be the same check written once per tool — a tool
// appended later, by the workforce or by a test, would carry no gate and
// nothing would say so — and a gate in runToolsWarm alone would leave the early
// path ungoverned, which is exactly the path that runs without the person
// having seen the call yet.
//
// The episode is a required argument for the same reason: a new call site
// cannot reach a tool without one, so it cannot reach a tool without the plane.
//
// It sits INSIDE the dispatch loop rather than above it, after the belt has
// been found to carry the tool: a call for a tool that does not exist is
// answered "Unknown tool", never asked about. A question about a tool nobody
// has is a question with no right answer.
func (a *Agent) executeTool(ctx context.Context, ep *episode, hub *eventHub, call ai.ToolCall) toolResult {
	for _, tool := range a.beltTools() {
		if tool.Name != call.Function.Name {
			continue
		}
		running, refused, allowed := ep.preAction(ctx, hub, call)
		if !allowed {
			return refused
		}
		// The arguments are read from what pre-action handed back — a canonicalizing
		// citizen's rewrite is what runs — and the TOOL is the one dispatch already
		// found, because the name is what got us here.
		args := json.RawMessage(running.Function.Arguments)
		text, isError, err := tool.Execute(ctx, args)
		if err != nil {
			// Harness-level failure: the model sees the Go error as the tool
			// result, matching pi's thrown-Error semantics.
			return toolResult{text: err.Error(), isError: true}
		}
		return toolResult{text: text, isError: isError}
	}
	return toolResult{text: "Unknown tool: " + call.Function.Name, isError: true}
}

// glossField names the argument that says what a call is DOING, per tool. A
// gloss is what the person reads instead of the raw arguments.
var glossField = map[string]string{
	"read":  "path",
	"edit":  "path",
	"write": "path",
	"ls":    "path",
	// find searches BY glob (bare/tools.go: pattern is its required argument;
	// path is the optional directory), so the pattern is what says what the
	// call is doing.
	"find": "pattern",
	"bash": "command",
	"grep": "pattern",
	// jobs says what it is doing in its action word — list, output, kill —
	// which is the whole of what a person needs to read beside the name.
	"jobs": "action",
	// A task proposal is its title: the one-line name of the work is what the
	// person is being asked about, and the row should say it.
	"propose_task": "title",
	// The two web hands read as what they went looking for: the sentence that
	// was searched, the page that was opened. Not the count, and not the
	// scheme — a person watching wants to know what their agent is reading.
	"web_search": "query",
	"web_fetch":  "url",
	// And the accounts, which read the same way: the account being picked up,
	// the mailbox search, the message opened (tools_connect.go). calendar_list
	// has no single argument that says what it is doing — a span is two — so it
	// is left off and reads as its bare name.
	"use_service":  "service",
	"gmail_search": "query",
	"gmail_read":   "id",
}

// glossFields is [glossField] for the calls where ONE argument is not enough to
// say what is about to happen.
//
// It exists for the two hands that act outside this machine, and it is the
// person's whole view of the question they are being asked: a message is who it
// is going to and what it says it is about, and an event is what it is called
// and when. "gmail_send alice@example.com" would be a question about a
// recipient, not about a message.
var glossFields = map[string][]string{
	"gmail_send":      {"to", "subject"},
	"calendar_create": {"title", "start"},
}

// gloss renders one call as a person-readable line: the tool name and the one
// argument that identifies the work. Unparseable arguments degrade to the bare
// name rather than to the raw JSON — a malformed call is still a call the
// person should see happening.
func gloss(call ai.ToolCall) string {
	name := call.Function.Name
	fields, known := glossFields[name]
	if !known {
		field, single := glossField[name]
		if !single {
			return name
		}
		fields = []string{field}
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return name
	}
	said := make([]string, 0, len(fields)+1)
	said = append(said, name)
	for _, field := range fields {
		if value := glossValue(args, field); value != "" {
			said = append(said, value)
		}
	}
	if len(said) == 1 {
		return name
	}
	return clip(strings.Join(said, " "), hintLimit)
}

// glossValue reads one argument as the line a person would read.
func glossValue(args map[string]json.RawMessage, field string) string {
	raw, present := args[field]
	if !present {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		value = strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(firstLine(value))
}

// argsText renders one call's arguments for Event.Args: the JSON the model
// sent, compacted onto one line and capped. Arguments that do not parse pass
// through as their own text — gloss degrades a malformed call to the bare tool
// name because a hint is a claim about what the call does, but the expansion is
// where a person goes to see what actually arrived.
func argsText(call ai.ToolCall) string {
	raw := strings.TrimSpace(call.Function.Arguments)
	if raw == "" {
		return ""
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(raw)); err == nil {
		raw = compacted.String()
	}
	return clip(raw, argsLimit)
}

// capOutput bounds a tool result for Event.Output, marking the cut with the
// number of bytes left behind. The count is explicit — not an ellipsis — so a
// person reading a truncated build log knows whether they are missing a line or
// a megabyte, and so no surface mistakes this copy for the whole result.
func capOutput(text string) string { return capBytes(text, outputLimit) }

// capBytes is capOutput's rule at any budget: keep the first limit bytes on a
// rune boundary, and say how many were left behind.
//
// It is separated from [capOutput] because the auditor's belt bounds a tool
// RESULT — what one call may weigh in a judge's context — on a different budget
// from the one a person's screen is drawn with (task_audit.go's boundedResult),
// and two truncations with two ways of marking the cut would be two answers to
// "is this the whole thing".
func capBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8RuneStart(text[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… (%d more bytes)", text[:cut], len(text)-cut)
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return strings.TrimSpace(text[:index])
	}
	return text
}

// clip bounds a string to n bytes on a rune boundary, marking the cut.
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n - len("…")
	for cut > 0 && !utf8RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ── usage ───────────────────────────────────────────────────────────────────

// addUsage folds one response's accounting into the turn and the session, and
// remembers the provider's own context size — the honest number the compaction
// threshold prefers over an estimate.
func (a *Agent) addUsage(turn *Usage, response *ai.Response) {
	if response == nil || response.Usage == nil {
		return
	}
	usage := response.Usage
	turn.Input += usage.PromptTokens
	turn.Output += usage.CompletionTokens
	// The cache figures are the provider's own, in whichever dialect it speaks
	// them (ai.Usage reconciles the two spellings). They are recorded on the
	// turn AND on the session because they answer two different questions: what
	// this exchange cost against what it would have, and how warm the lineage
	// has been all day.
	turn.CacheRead += usage.CacheReadTokens()
	turn.CacheWrite += usage.CacheCreationTokens()
	if usage.Cost != nil {
		turn.CostUSD += *usage.Cost
	}

	context := usage.TotalTokens
	if context == 0 {
		context = usage.PromptTokens + usage.CompletionTokens + usage.CacheReadTokens()
	}

	a.mu.Lock()
	a.usage.Input += usage.PromptTokens
	a.usage.Output += usage.CompletionTokens
	a.usage.CacheRead += usage.CacheReadTokens()
	a.usage.CacheWrite += usage.CacheCreationTokens()
	a.usage.Turns++
	if usage.Cost != nil {
		a.usage.CostUSD += *usage.Cost
	}
	if context > 0 {
		a.contextTokens = context
	}
	a.mu.Unlock()
}

// assistantContent passes the response's content parts through, falling back
// to one empty text part so an assistant message is never contentless.
func assistantContent(response *ai.Response) []ai.ContentPart {
	if len(response.Choices) == 0 || len(response.Choices[0].Message.Content) == 0 {
		return []ai.ContentPart{{Type: "text", Text: ""}}
	}
	return response.Choices[0].Message.Content
}

// ── compaction (docs/CHAT-V3.md Decision 9, omp's architecture) ─────────────

const (
	// compactReservePercent / compactReserveFloorTokens are omp's threshold:
	// compaction fires at window − max(15% of window, 16k). The percentage
	// scales the headroom with the model; the floor keeps a small window from
	// reserving less than one long tool result plus one reply.
	compactReservePercent     = 15
	compactReserveFloorTokens = 16384

	// compactKeepRecentTokens is how much of the tail survives verbatim. The
	// summary is lossy by construction, so the recent work — the files just
	// read, the error just seen — is kept as itself.
	compactKeepRecentTokens = 20000

	// bytesPerToken is the estimator used when no provider figure is
	// available: ~4 bytes per token for code and English prose. It is only
	// ever compared against a threshold with 16k of slack, so being 30% wrong
	// moves when compaction fires, never whether the request fits.
	bytesPerToken = 4
)

// window is the model's context in tokens, most specific answer first: the one
// a surface set for the model actually in use (see [Agent.SetContextWindow]),
// then the one this session was configured with, then the conservative default.
//
// It takes no lock — cutPointLocked calls it with mu already held.
func (a *Agent) window() int {
	if learned := int(a.contextWindow.Load()); learned > 0 {
		return learned
	}
	if a.config.ContextWindow > 0 {
		return a.config.ContextWindow
	}
	return defaultContextWindow
}

func (a *Agent) compactThreshold() int {
	return CompactThreshold(a.window())
}

// CompactThreshold is the law itself, exported because a surface has to be able
// to say how close a conversation is to being compacted — and a surface that
// re-derived the formula from the same two constants would be a second copy of
// it, free to drift the moment either one moves (internal/tui3 reads this for
// the status meter's accent).
//
// Zero and negative windows answer zero: a threshold against an unknown window
// is a number that means nothing, and a caller must have an answer for that
// rather than treat it as a tiny model.
func CompactThreshold(window int) int {
	if window <= 0 {
		return 0
	}
	reserve := window * compactReservePercent / 100
	if reserve < compactReserveFloorTokens {
		reserve = compactReserveFloorTokens
	}
	// Invariant: CompactThreshold() > keepRecentTokens(). The 16k floor is
	// bigger than a small window, and below ~21.8k it drove the threshold under
	// the verbatim tail — every step over threshold, every pass finding nothing
	// but the tail to summarize, forever. Half the window is the clamp because
	// the tail is at most a quarter of it.
	if half := window / 2; reserve > half {
		reserve = half
	}
	return window - reserve
}

// keepRecentTokens is the verbatim tail budget, capped at a quarter of the
// window. Keeping 20k of a 200k window is a tail; keeping 20k of an 8k window
// is not a compaction at all, and without the cap a small-window session would
// find nothing to summarize and overflow with the pass "succeeding".
func (a *Agent) keepRecentTokens() int {
	keep := compactKeepRecentTokens
	if quarter := a.window() / 4; quarter < keep {
		keep = quarter
	}
	return keep
}

// maybeCompact is the automatic pass, checked after every step. Config's
// CompactEnabled gates only this one — Compact and the overflow recovery run
// regardless.
func (a *Agent) maybeCompact(ctx context.Context, hub *eventHub) {
	if !a.config.CompactEnabled {
		return
	}
	a.mu.Lock()
	estimate := a.estimateTokensLocked()
	a.mu.Unlock()
	if estimate <= a.compactThreshold() {
		return
	}
	// A failed pass is not a failed turn: the loop keeps going and the next
	// step's provider error, if any, says what actually went wrong.
	_, _ = a.compact(ctx, hub)
}

// ErrNothingToCompact says a compaction pass had nothing to do: the whole
// transcript already fits inside the keep-recent tail. It is a sentinel rather
// than a silent no-op so a surface's /compact can say "nothing to compact"
// instead of reporting a success that changed nothing.
var ErrNothingToCompact = errors.New("session: nothing to compact")

// ErrCompactionInFlight says another pass is already running. The second caller
// gets an error for the same reason: it did nothing, and it should say so.
var ErrCompactionInFlight = errors.New("session: a compaction pass is already running")

// compact runs one pass: cut the transcript at a message boundary, summarize
// the discarded prefix in one LLM call, and rebuild as system + summary note +
// kept tail. It reports whether the transcript changed.
func (a *Agent) compact(ctx context.Context, hub *eventHub) (bool, error) {
	a.mu.Lock()
	if a.compacting {
		a.mu.Unlock()
		return false, ErrCompactionInFlight
	}
	cut := a.cutPointLocked()
	if cut <= 1 {
		// Everything fits in the keep-recent tail: there is no prefix to
		// summarize, and summarizing the tail would discard the live context.
		a.mu.Unlock()
		return false, ErrNothingToCompact
	}
	tokensBefore := a.estimateTokensLocked()
	discarded := make([]ai.Message, cut-1)
	copy(discarded, a.messages[1:cut])
	a.compacting = true
	a.mu.Unlock()

	if hub != nil {
		hub.send(Event{
			Kind: EventCompacting,
			Hint: "compacting ~" + approxTokens(tokensBefore) + " tokens",
		})
	}

	summary, err := a.summarize(ctx, discarded)

	a.mu.Lock()
	a.compacting = false
	if err != nil || strings.TrimSpace(summary) == "" {
		a.mu.Unlock()
		if err == nil {
			err = errors.New("session: summarizer returned nothing")
		}
		// The start event promised an end: a failed pass settles its row too,
		// saying nothing changed — silence would leave "compacting" spinning
		// over a turn that has already moved on.
		if hub != nil {
			hub.send(Event{Kind: EventCompacted, Hint: "compaction failed · context unchanged"})
		}
		return false, err
	}
	// The lock was released across the summary call, but the transcript is
	// append-only and no second pass can have run, so anything recorded
	// meanwhile sits AFTER cut and the index still names the same message.
	kept := a.messages[cut:]
	// Those late arrivals are the reason the tail is re-checked here rather
	// than trusted from cutPointLocked. A pass that starts mid-batch — the
	// surface's /compact, or an overflow retry — can cut above an assistant's
	// tool_calls message and have that batch's results land after the cut while
	// the summary is being written. A tool message with no call above it is a
	// 400 on this request and on every request after it, so the kept tail drops
	// its leading orphans; their call is inside the summary now.
	for len(kept) > 0 && kept[0].Role == "tool" {
		kept = kept[1:]
	}
	rebuilt := make([]ai.Message, 0, 3+len(kept))
	rebuilt = append(rebuilt, a.messages[0], textMessage("user", compactionNote(summary)))
	// Working state survives the pass VERBATIM, after the summary and before
	// the kept tail: the summary compresses the trajectory, and the state
	// block is what the trajectory must never have to be re-read for. It is
	// injected here and never routed through the summarizer — retrieval must
	// not re-ingest its own output (state.go's §4 law).
	if block := a.StateBlock(); block != "" {
		rebuilt = append(rebuilt, textMessage("user", block))
	}
	rebuilt = append(rebuilt, kept...)
	a.messages = rebuilt
	// The provider's context figure described the request that is now gone.
	// Zero sends the estimator back to the content until the next response.
	a.contextTokens = 0
	keptTokens := 0
	for _, message := range kept {
		keptTokens += messageBytes(message)
	}
	keptTokens /= bytesPerToken
	if a.file != nil {
		a.file.appendCompaction(summary, tokensBefore, kept)
	}
	a.mu.Unlock()

	if hub != nil {
		hub.send(Event{
			Kind: EventCompacted,
			Hint: fmt.Sprintf("compacted from %s tokens, kept last %s",
				approxTokens(tokensBefore), approxTokens(keptTokens)),
		})
	}
	return true, nil
}

// cutPointLocked walks back from the tail until the keep-recent budget is
// spent and returns the index the kept tail starts at.
func (a *Agent) cutPointLocked() int {
	budget := a.keepRecentTokens() * bytesPerToken
	cut := len(a.messages)
	// index 0 is the system message; it is never summarized and never cut.
	for cut > 1 {
		size := messageBytes(a.messages[cut-1])
		if budget-size < 0 {
			break
		}
		budget -= size
		cut--
	}
	// A tool result whose assistant tool_calls message was summarized away is
	// an orphan every provider rejects, so the cut walks forward off one.
	for cut < len(a.messages) && a.messages[cut].Role == "tool" {
		cut++
	}
	return cut
}

// estimateTokensLocked reports the larger of the provider's last context figure
// and an estimate of the transcript as it stands.
//
// The provider figure is the honest number for the request that was SENT, and
// it knows nothing about what has been appended since — a single 300KB tool
// result would sit invisible behind a pre-batch figure and never trip the
// threshold. Taking the max keeps the honest number as a floor while letting
// the content speak for everything after it.
func (a *Agent) estimateTokensLocked() int {
	total := 0
	for _, message := range a.messages {
		total += messageBytes(message)
	}
	estimate := total / bytesPerToken
	if a.contextTokens > estimate {
		return a.contextTokens
	}
	return estimate
}

// imagePartTokens is what one image content part is charged in the content
// estimate: A FLAT 1,000 TOKENS, whatever the picture.
//
// Neither of the two obvious alternatives is usable. Counting the part's own
// bytes counts the base64 data URL, which is megabytes for a photograph and has
// nothing to do with what the model is billed — it would trip compaction on the
// turn after a screenshot was pasted. Counting nothing is what this did before,
// and an image is then invisible to the meter and to the threshold: a
// conversation of ten screenshots reads as a few hundred tokens right up to the
// provider's overflow error.
//
// 1,000 is the middle of the range the vision endpoints actually charge — a
// tile-based model bills roughly 250 tokens for a thumbnail and 1,500 for a
// full-screen capture — and the figure is only ever compared against a threshold
// with 16k of slack, so being twice wrong about one image moves when compaction
// fires and never whether a request fits. It is expressed in bytes here because
// messageBytes is a byte count that its callers divide by [bytesPerToken].
const imagePartTokens = 1000

func messageBytes(message ai.Message) int {
	total := len(message.Role)
	for _, part := range message.Content {
		total += len(part.Text)
		// Every non-text part is a picture today (image.go is the only thing
		// that builds one), and the check is on the payload rather than on
		// part.Type so a part that arrives spelled differently is still counted.
		if part.ImageURL != nil {
			total += imagePartTokens * bytesPerToken
		}
	}
	for _, call := range message.ToolCalls {
		total += len(call.Function.Name) + len(call.Function.Arguments)
	}
	return total
}

func approxTokens(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("~%dk", tokens/1000)
	}
	return fmt.Sprintf("~%d", tokens)
}

// summarizationPrompt is omp's section contract. The three MUSTs at the end
// are the ones a lossy summary most often breaks and a resumed session most
// needs: a question the person asked and never got answered, an exact path,
// and the text of an error.
const summarizationPrompt = `You are compacting a working session's transcript so it can continue in a smaller context.

Write a summary of the conversation below under exactly these sections:

## Goal
What the person is trying to achieve, in their terms.

## Constraints & Preferences
Rules, conventions, and preferences they stated or corrected.

## Progress
What has been done and what it produced. Cite files by path.

## Key Decisions
Choices made and the reason each was made, including options rejected.

## Next Steps
What remains, in order.

## Critical Context
Anything else without which the work cannot continue.

MUST reproduce any question asked of the person and not yet answered, verbatim.
MUST preserve exact file paths, symbol names, commands, and error text.
MUST NOT invent, infer, or soften anything: this is a record, not a report.`

// summarize makes the one summarization call.
func (a *Agent) summarize(ctx context.Context, discarded []ai.Message) (string, error) {
	var transcript strings.Builder
	for _, message := range discarded {
		fmt.Fprintf(&transcript, "[%s]\n", message.Role)
		for _, part := range message.Content {
			if part.Type == "text" && part.Text != "" {
				transcript.WriteString(part.Text)
				transcript.WriteString("\n")
			}
		}
		for _, call := range message.ToolCalls {
			fmt.Fprintf(&transcript, "[tool call: %s(%s)]\n", call.Function.Name, call.Function.Arguments)
		}
		transcript.WriteString("\n")
	}

	a.mu.Lock()
	model := a.model
	a.mu.Unlock()

	// WithoutStream: the summary is bookkeeping, not something anybody said.
	// Left on the turn's context it would type itself into the room.
	// No tools either — the summarizer's only job is to produce text.
	response, err := a.client.CompleteWithMessages(
		provider.WithoutStream(ctx),
		[]ai.Message{
			textMessage("system", summarizationPrompt),
			textMessage("user", transcript.String()),
		},
		ai.WithModel(model))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("session: summarizer returned no response")
	}
	a.addAuxiliaryUsage(response)
	return strings.TrimSpace(response.Text()), nil
}

// addAuxiliaryUsage folds one auxiliary call — the compaction summary, the
// session title (title.go) — into the SESSION total only.
//
// The person pays for it, so it cannot be free; but no turn asked for it, and
// charging it to the turn that happened to cross the threshold would make one
// ordinary question read as three times the cost of its neighbours. Turns is
// left alone for the same reason — this is bookkeeping, not a step of the
// conversation — and contextTokens too: an auxiliary call runs against its own
// two-message context, which says nothing about this session's.
func (a *Agent) addAuxiliaryUsage(response *ai.Response) {
	if response == nil || response.Usage == nil {
		return
	}
	usage := response.Usage
	a.mu.Lock()
	defer a.mu.Unlock()
	a.usage.Input += usage.PromptTokens
	a.usage.Output += usage.CompletionTokens
	// The cache figures follow the tokens they belong to. An auxiliary call is
	// paid for out of the same pocket, so leaving them out would make the
	// session's cached share a fraction of only part of its input.
	a.usage.CacheRead += usage.CacheReadTokens()
	a.usage.CacheWrite += usage.CacheCreationTokens()
	if usage.Cost != nil {
		a.usage.CostUSD += *usage.Cost
	}
}

// compactionNote wraps the summary as a user-role message. User role because
// it is context handed TO the model rather than something it produced, and
// marked in plain words because a model that mistakes a summary for a
// transcript will answer questions inside it.
func compactionNote(summary string) string {
	return "[context compacted] Everything before this point was summarized to fit the " +
		"context window. This note is the record of that conversation — it is not something " +
		"either of us said, and any question inside it is still open.\n\n" + summary
}
