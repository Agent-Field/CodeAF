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
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── retry constants (pi spec §4, verbatim from internal/exec/bare) ──────────

// maxRetries is pi's default auto-retry count: 3. The schedule is 2s, 4s, 8s
// (baseDelayMs=2000 * 2**(attempt-1)).
const maxRetries = 3
const retryBaseDelay = 2 * time.Second

// truncationContinuations gives a cut-off answer two chances to finish in
// smaller pieces. The bound matters because a model that ignores the note can
// otherwise turn one bad output ceiling into an unbounded, silent spend.
const truncationContinuations = 2

// ── what a cut stream is worth asking again ─────────────────────────────────
//
// A stream the guard cut (internal/provider's streamguard.go) is a different
// kind of failure from a torn connection, and it gets its own budget rather than
// spending the transport one above: the request never failed, so there is
// nothing here to back off from, and the same three attempts that make sense for
// a socket would keep a model that has lost the thread going four times over a
// context that is only getting worse.
//
// SILENCE IS WORTH ASKING TWICE. The endpoint is very often simply a bad draw
// out of a router's pool, and the second try lands on a different one.
//
// DEGENERATION IS WORTH ASKING ONCE. If the same transcript produces soup twice,
// the transcript is the problem and asking a third time spends the whole prompt
// to be told so again — which is the point at which the person is told instead.
//
// AND A THIRD BUDGET, for the case where asking again reaches the same endpoint
// every time. Two attempts that could only land in the same place are one
// attempt with a wait in front of it, so the step stops asking sooner and moves
// to another model instead. The whole rule is stated at [cutBudget].
const (
	silentRetries = 2
	babbleRetries = 1
	blindRetries  = 1
)

const truncationContinuationNote = "Your last reply was cut off at the output limit. " +
	"Continue the work in smaller parts. Use tool calls to save any large deliverable " +
	"when writing is in scope, and keep the final report short."

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
// It is 32k rather than a paragraph's worth because of what a surface derives
// from this field. An edit call's "+3 −1" and its unified diff are computed
// from the old/new strings the call carried (docs/CHAT-V3.md D11, rendered in
// internal/tui3) — the tool's own result is one sentence saying it worked, so
// the arguments are the ONLY record of the change that reaches a screen. A cap
// that cut them at 400 bytes did not shorten the diff; it produced the wrong
// number and a diff that stopped mid-line.
//
// 32k is roughly eight hundred lines of source. It clears internal/tui3's whole
// ladder with room to spare — a write's expansion keeps 20 rows and an edit's
// diff 40, and the "… N more lines" foot that lifts those caps has to have
// something to lift — and it clears the 600-line ceiling the exact diff runs
// under (its diffCeiling), so a replacement block that a person can be shown a
// real diff of is a block that arrives whole.
const argsLimit = 32768

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
func (a *Agent) runTurn(ctx context.Context, hub *eventHub, user userMessage) bool {
	started := time.Now()
	var turn Usage

	// BEFORE ANYTHING IS SENT ANYWHERE: is this turn one of the things this
	// build already knows how to do properly? A sub-harness has no slash command,
	// so the turn itself is how one is reached: a strong match against the
	// registry raises one line asking whether that is what was meant
	// (harness.go).
	//
	// It is not a routing: the no is free and leaves the ordinary turn below
	// untouched, and a build with no registry — every caller today — never
	// reaches past the first nil check. COMMISSIONING a harness is not read here
	// at all; it is a tool the model reaches for (tools_harness.go), because
	// whether a sentence asked for a saved procedure is a judgement and not a
	// lookup.
	if answered, completed := a.routeHarness(ctx, hub, user, started); answered {
		return completed
	}

	// And the third thing a turn can be instead of a request to the model: a
	// request for an ADAPTIVE RUN — work whose shape nobody knows yet, planned
	// and executed against a fuel cap while the conversation carries on
	// (orchestrate.go). It is the same bargain the two above keep: an anchored
	// cue and nothing else enters it, the turn ends the moment the run starts,
	// and a build with no runner never reaches past one nil check.
	if answered, completed := a.routeOrchestrate(ctx, hub, user, started); answered {
		return completed
	}

	// AND THE LAST THING BEFORE THE FIRST REQUEST: which of the things this
	// person has had aforge remember bear on what they just said (memory.go).
	// It is one small call on the reflex tier against an index of titles, it
	// happens here rather than in [Agent.startTurnLocked] because that runs with
	// a.mu held, and everything about it fails open — an empty block is a turn
	// exactly as it would have been.
	a.refreshMemory(ctx, hub, user.text())

	// AND BESIDE IT, WHAT THE OTHER WINDOWS ON THIS PROJECT HAVE BEEN DOING
	// (taskdelta.go). It sits here for the line above's reason — it reads a
	// shared directory and [Agent.startTurnLocked] holds a.mu — and it fails
	// open the same way: no folder, no index and no other window each answer an
	// empty block, and a turn with an empty block is a turn as it always was.
	a.refreshElsewhere()

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

	// toolCtx is the turn's context WITHOUT the observer installed below. EVERY
	// TOOL RUNS ON IT — the batch below as well as the early start — because a
	// tool must run under the turn's cancellation and nothing else. Giving an
	// early tool a context pointed back at the stream it was started from would
	// be a loop, and reading the reassigned ctx from inside the closure would be
	// a second reader of a variable the loop writes.
	//
	// The batch was handed the observed context for a long time, and it cost two
	// things at once. A tool that asks a model something — view_image, sense,
	// read_document — took the STREAMING path without meaning to, which on this
	// adapter carries no total deadline by design (internal/provider's
	// transport.go), so a provider that went quiet mid-answer hung the tool and
	// with it the batch, and the call was journaled with no result forever. And
	// its answer, which is a tool result and not the room's reply, was typed
	// into the transcript in the chat model's voice — the very thing
	// [provider.WithoutStream] exists to prevent.
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
			warm.announce(hub, a, event.Delta)
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

	// usedTools says this turn touched the belt at all. It is the one fact the
	// route judge cannot see from outside the loop (route_judge.go): a turn that
	// called tools was already work of some size, and asking whether work should
	// have been work is a question with no useful answer.
	usedTools := false

	// overflowCompacted bounds the compact-and-retry answer to a context
	// overflow at one pass per turn. A second overflow after a successful
	// compaction is not a context problem this loop can fix by shrinking
	// further, and retrying it forever would burn a summary call per attempt.
	overflowCompacted := false

	// A text-only answer normally closes the turn. A length stop is not an
	// answer, though: it is the provider saying that the answer did not fit, so
	// this count keeps that exceptional continuation both useful and bounded.
	truncations := 0

	for {
		// The cancel check comes BEFORE the drain: steering typed in the
		// instant before an interrupt must not be spliced into a transcript
		// this turn is abandoning unanswered. Nothing is lost — the turn's end
		// drains the queue under the same lock that clears running (agent.go),
		// so a leftover lands ahead of the next Submit's message.
		if ctx.Err() != nil {
			a.keepPartial(partial)
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
			return false
		}

		// Steering lands here, between batches: the transcript tail is a tool
		// result or an assistant answer, both legal places for a user message.
		a.drainSteering()

		// NOTHING TOO BIG TO FIT IS SENT AND HOPED OVER. The last thing before
		// the wire, after steering has landed, because steering is part of the
		// request being measured.
		a.guardOversizeRequest(ctx, hub)

		response, answered, err := a.completeWithRetry(ctx, hub, model, effort, partial, warm, forming)
		// THE MODEL THIS TURN IS ON CAN CHANGE UNDER IT. A step whose budget of
		// cut streams ran out moves to the next model in the chain and says so,
		// and everything the rest of the turn attributes — the usage rows, the
		// sealed turn's model, the level the next step asks for — has to name the
		// model that actually answered rather than the one that stopped.
		if answered != "" && answered != model {
			model = answered
			a.mu.Lock()
			effort = a.reasoningLocked(model)
			a.mu.Unlock()
		}
		if err != nil {
			// Interrupt (or the caller's own deadline). Whatever was streamed
			// before the cut is real work the person watched arrive, so it
			// stays in the transcript and the turn ends normally.
			if ctx.Err() != nil {
				a.keepPartial(partial)
				hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
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
			hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(turn, started, model)})
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
			if store.ClassifyEnd(provider.FinishReason(response), true) == store.EndLength {
				truncations++
				if truncations <= truncationContinuations {
					a.record(ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: truncationContinuationNote}}})
					continue
				}
				a.markTurnTruncated()
			}
			// `pre-decision` (hooks.go): the last chance to shape what the model
			// will be sent next. Its one citizen today is the stubbing pass, which
			// runs BEFORE the compaction check, and the order is the whole economy
			// of it (stub.go): a transcript whose old heavy results have just
			// become one-line pointers may no longer be over the threshold at all,
			// so the check that follows is made against what the next request will
			// actually weigh.
			episode.preDecision(ctx)
			a.maybeCompact(ctx, hub)
			// AND THE LAST QUESTION OF THE TURN, asked only of a turn that answered
			// in words alone: should that have been WORK? A second small model reads
			// what was asked and the shape of what came back, and a yes raises one
			// card offering to start it (route_judge.go). It launches nothing on its
			// own, it is silent when it cannot work, and it is asked before the turn
			// is sealed so that the card lives exactly as long as the turn does —
			// which is how every other question this loop can raise behaves.
			a.routeJudge(ctx, hub, user, usedTools, response.Text())
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
			// The name comes after the turn is done and before the hub closes:
			// the person is not kept waiting on a title, and the event still has
			// a stream to land on (title.go).
			a.maybeTitle(ctx, hub)
			// AND THE EXCHANGE IS READ FOR ANYTHING WORTH KEEPING, off this
			// goroutine entirely and on the session's own lifetime rather than
			// the turn's (memory.go). Nobody is waiting for it, nothing it finds
			// reaches this turn, and it says nothing whatever happens to it.
			a.learnFromTurn(user.text(), response.Text())
			return true
		}

		a.record(ai.Message{Role: "assistant", Content: assistantContent(response), ToolCalls: calls})
		partial.reset()
		usedTools = true

		results := a.runToolsWarm(toolCtx, episode, calls, hub, warm)

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

// sealTurn stamps the turn's wall duration, folds it into the session total,
// and writes the turn down.
//
// THIS IS THE ONE PLACE A TURN'S COST REACHES THE JOURNAL, and it is here
// because every turn shape in the package ends through it: the loop above, the
// harness turn, the orchestrated turn and the image turn, on the answering path
// and on both failing ones. A write anywhere else would be a turn shape that
// silently kept no record.
//
// The model is the CALLER'S rather than a.model, for the reason runTurn latches
// it: a mid-turn /model swap must not be attributed backwards to work another
// model did. The write happens with a.mu released — the file takes its own lock
// (see [sessionFile.writeLine]) — and a turn that spent nothing writes no line
// at all (see [sessionFile.appendUsage]).
func (a *Agent) sealTurn(turn Usage, started time.Time, model string) Usage {
	turn.Duration = time.Since(started)
	a.mu.Lock()
	a.usage.Duration += turn.Duration
	a.mu.Unlock()
	a.file.appendUsage(turn, model, false, "")
	// AND THE SESSION'S RUNNING TOTAL IS STAMPED BESIDE IT, for the reason this
	// function is the one place the journal is written: what a conversation has
	// cost is a fact every reader of the machine wants and only the transcript
	// holds, and a surface listing every session on disk cannot open every
	// transcript to find it (placemeta.go's [Agent.stampSpend]).
	a.stampSpend()
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
func (a *Agent) completeWithRetry(ctx context.Context, hub *eventHub, model string, effort provider.Effort, partial *partialBuffer, warm *warmBatch, forming *formingBatch) (*ai.Response, string, error) {
	var lastErr error
	// cuts counts the attempts the STREAM GUARD ended — a stall, or a reply that
	// stopped being language. They are counted apart from the transport attempts
	// below for the reason the constants say, and the loop's own attempt number
	// does not advance for one: a cut is not evidence that the endpoint is
	// failing, so it must not shorten the patience a real fault gets.
	cuts := 0
	// rerouted says at least one of this step's cuts took an endpoint out of the
	// ledger, so the attempts since then were genuinely served by somebody else.
	// It is what [cutBudget] narrows on; the law is stated there.
	rerouted := false
	// hopped is the models this step has already moved to, in order, and its
	// length is where the chain is read from next. It is what the failure
	// sentence names when even the fallbacks could not answer. `origin` is kept
	// beside it because the chain is always the chain of the model the step
	// started on, however far along it the step has walked.
	origin := model
	var hopped []string
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

		// The level is stamped PER ATTEMPT rather than once outside the loop,
		// because the model can change inside it. Reasoning strength is a choice
		// about a model and is held per model id (agent.go), so a step that has
		// moved to a fallback asks that model for the level somebody set on IT —
		// never for the level they dialled onto the model that stopped answering.
		attemptCtx := ctx
		if effort != provider.EffortNone {
			attemptCtx = provider.WithConfiguredReasoningEffort(ctx, effort)
		}
		messages := a.snapshot()
		response, err := a.client.CompleteWithMessages(attemptCtx, messages,
			ai.WithModel(model), ai.WithTools(a.beltDefinitions()))
		if err == nil {
			return response, model, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return nil, model, ctx.Err()
		}
		// THE GUARD'S CUT, ANSWERED HERE. The three resets at the top of this
		// loop are exactly what a cut needs — the soup that was streamed, the
		// reads it started, the calls it was half-way through asking for — so a
		// cut re-enters the loop through the same door a fault does, and the junk
		// is gone before the next request is assembled.
		if cut, isCut := provider.CutFrom(err); isCut {
			if cut.Rerouted {
				rerouted = true
			}
			if cuts >= cutBudget(cut, rerouted) {
				// THE BUDGET IS SPENT, SO THE MODEL MOVES. Asking the same
				// weights a fourth time is the one thing already known not to
				// work; the chain is the same one an endpoint refusal walks
				// (internal/provider's endpoints.go), and the hop is SAID rather
				// than done quietly, because the rest of this reply arrives in a
				// different voice and the person is watching it happen.
				if next, moved := a.nextFallback(origin, hopped); moved {
					hopped = append(hopped, next)
					hub.send(Event{Kind: EventRetrying, Text: hopNotice(cut, next)})
					model = next
					a.mu.Lock()
					effort = a.reasoningLocked(model)
					a.mu.Unlock()
					// A new model gets a whole budget of its own: what the last
					// one did says nothing about this one, and a fallback that
					// inherited a spent budget would be given up on before it had
					// answered once.
					cuts, rerouted = 0, false
					attempt--
					continue
				}
				return nil, model, cutFailure(cut, cuts+1, hopped)
			}
			cuts++
			hub.send(Event{Kind: EventRetrying, Text: cutNotice(cut)})
			attempt--
			continue
		}
		errMsg := err.Error()
		if isContextOverflow(errMsg) || !isRetryable(errMsg) {
			return nil, model, err
		}
		if attempt < maxRetries {
			delay := retryBaseDelay * (1 << attempt) // 2s, 4s, 8s
			if waitErr := backoffWait(ctx, delay); waitErr != nil {
				return nil, model, waitErr
			}
		}
	}
	return nil, model, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// ── THE ENDPOINT-DIVERSITY GATE ─────────────────────────────────────────────
//
// A model hop is a big move — it changes whose weights finish a reply somebody
// is reading — and it is only honest AFTER the cheaper explanation has been
// ruled out. The cheaper explanation is almost always the endpoint: a router
// serves one model id from a pool, and a single bad member of that pool can
// swallow three attempts in a row.
//
// That is exactly what the layer underneath now prevents. A cut whose stream
// named its provider strikes that (model, endpoint) lane out of the velocity
// ledger, so the next attempt is encoded away from it — endpoint diversity, got
// for free, before this ever asks about models. By the time a full budget of
// cuts is spent, several different endpoints have failed and the model itself is
// the remaining suspect.
//
// UNLESS NOTHING WAS STRUCK, which is [provider.StreamCut.Rerouted] being false
// and has two causes that look identical from here and want the same answer:
//
//   - `routing = off`: the person has told the adapter not to steer, so the
//     ledger is silent by their own instruction. Nothing is being routed around.
//   - an ANONYMOUS cut: the stream died before any chunk named the endpoint that
//     served it, so there was no lane to strike.
//
// In both, the next attempt is drawn from the same pool by the same rules and
// lands on the same lane deterministically — so the extra attempts buy nothing,
// and spending a person's wait on them to look thorough is dishonest. The
// budget narrows to [blindRetries] and the hop comes sooner.
//
// `routing = off` therefore does NOT switch model hops off. It switches ENDPOINT
// steering off, which is a different promise; the two knobs that do switch hops
// off are an empty chain and `--one-model`, and both make the hop ABSENT rather
// than broken.

// cutBudget is how many times a cut of this kind is worth asking again, given
// what is known about whether the last attempts reached different endpoints.
//
// Degeneration is the one reason the gate says nothing about: soup is a claim
// about the transcript and the weights reading it, never about which endpoint
// delivered it, so it keeps its own single retry either way.
func cutBudget(cut *provider.StreamCut, rerouted bool) int {
	if cut.Reason == provider.CutBabble {
		return babbleRetries
	}
	if !rerouted {
		return blindRetries
	}
	return silentRetries
}

// nextFallback is the model this step moves to next, and false when there is
// none left — an empty chain, a completer with no chain to offer, or a chain
// already walked to its end.
//
// The order and the cap are NOT decided here. They are the adapter's, read
// through [modelChain], so the models a refusal falls back to and the models a
// stall falls back to are the same models in the same order.
// It is asked about `origin`, the model the STEP STARTED ON, so a second hop
// walks the same list rather than deriving a fresh chain from the fallback —
// which is how a bounded chain of two becomes an unbounded walk.
func (a *Agent) nextFallback(origin string, hopped []string) (string, bool) {
	chain, ok := a.client.(modelChain)
	if !ok {
		return "", false
	}
	options := chain.FallbackModels(origin)
	if len(hopped) >= len(options) {
		return "", false
	}
	return options[len(hopped)], true
}

// cutNotice is the dim line the person sees while the question is asked again.
//
// It says what happened and that something is being done about it, and nothing
// else: the turn is still going, nobody has to decide anything, and a note that
// asked for a decision here would be interrupting a wait it cannot shorten.
func cutNotice(cut *provider.StreamCut) string {
	switch cut.Reason {
	case provider.CutBabble:
		return "the reply lost its thread — that text was dropped, asking again"
	case provider.CutStalled:
		return "the model went quiet mid-reply — asking again"
	default:
		return "nothing came back from the model — asking again"
	}
}

// hopNotice is the line the person reads when the step gives up on one model
// and finishes the reply on another.
//
// It is [cutNotice]'s register — what happened, then what is being done — with
// the one difference that matters: it NAMES THE MODEL. The rest of the answer
// will arrive in a different voice, at a different price, and somebody watching
// text appear is owed the reason before it does.
func hopNotice(cut *provider.StreamCut, next string) string {
	switch cut.Reason {
	case provider.CutBabble:
		return "the reply kept losing its thread — finishing this one on " + next
	case provider.CutStalled:
		return "the model kept going quiet mid-reply — finishing this one on " + next
	default:
		return "nothing kept coming back from the model — finishing this one on " + next
	}
}

// cutFailure is the sentence the turn ends on when asking again did not help.
//
// It names what happened in the person's own terms and then names the DOORS
// that actually open, and which those are depends on what has already been
// tried. When a chain was configured and walked, "try a different model" has
// already happened and saying it again would be advice the surface knows to be
// spent — so the sentence names the models that also failed and stops there.
// When no chain was walked, both doors are real and both are one keystroke: a
// different model is a different set of weights on the same conversation, and
// compaction is the same weights on a shorter one — and a long conversation is
// exactly the condition a reply loses its thread in, which is why the second
// door is offered at all rather than being general advice.
func cutFailure(cut *provider.StreamCut, attempts int, hopped []string) error {
	said := ""
	switch {
	case cut.Reason == provider.CutBabble && len(hopped) > 0:
		said = "the reply lost its thread " + timesWord(attempts) +
			" — it came back as repetition and jumbled text, so none of it was kept. " +
			alsoTried(hopped) + ", so /compact to lighten the conversation"
	case cut.Reason == provider.CutBabble:
		said = "the reply lost its thread " + timesWord(attempts) +
			" — it came back as repetition and jumbled text, so none of it was kept. " +
			"a different model may hold it (/model), or /compact to lighten the conversation"
	case len(hopped) > 0:
		said = fmt.Sprintf("%s, %s. %s — /model to pick another one yourself",
			cut.Error(), timesWord(attempts), alsoTried(hopped))
	default:
		said = fmt.Sprintf("%s, %s. a different model may answer — /model, "+
			"or set models.fallbacks so this can move on its own",
			cut.Error(), timesWord(attempts))
	}
	return &cutGaveUp{cut: cut, said: said}
}

// cutGaveUp is the sentence WITH the cut still reachable under it.
//
// The words are what a person reads; the cut is what a layer further out reads,
// and it has one question this is the only honest answer to: has the fallback
// chain already been walked for this failure? It has — every road to this
// function has spent a budget of cuts and offered the chain first — so a task
// node that met this error must not spend a whole second worker discovering the
// same thing (task_run.go's [terminalProviderFailure]). A decision made by
// matching substrings of a sentence is a decision that breaks the next time
// somebody rewords it, which is the same reason [provider.StreamCut] is a type.
type cutGaveUp struct {
	cut  *provider.StreamCut
	said string
}

func (e *cutGaveUp) Error() string { return e.said }

func (e *cutGaveUp) Unwrap() error { return e.cut }

// alsoTried names the models a step actually moved to. It replaces the advice
// to try another model, because "try another model" said to somebody who has
// just watched two of them fail is the surface not knowing what it did.
func alsoTried(hopped []string) string {
	return strings.Join(hopped, " and ") + " could not finish it either"
}

// timesWord counts the way a person counts. Small numbers have words.
func timesWord(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	case 3:
		return "three times"
	}
	return fmt.Sprintf("%d times", n)
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
// The agent is here for the gloss and nothing else: an account's own tool reads
// as a line only the session can write, and the announced row and the row that
// follows it must say the same thing about the same call ([Agent.gloss]).
func (b *warmBatch) announce(hub *eventHub, agent *Agent, payload string) {
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
		Hint:   agent.gloss(call),
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
//
// ── A TOOL CALL THAT STARTS ALWAYS ENDS JOURNALED ──
//
// One way (a result) or another (an error), because the alternative is not a
// failure the person can read: runTurn records one tool message per call after
// this returns, so a call that never comes back leaves the assistant's
// instruction on the record with nothing answering it, and the surface draws
// that — honestly — as a row still running, for the rest of the session
// (internal/tui3's room.go).
//
// Three things hold the law here, and each of them is load-bearing. Every slot
// is SEEDED with a panic result before the goroutine that fills it, so a fault
// cannot leave the zero value, which reads as an empty success. Every goroutine
// carries guard.Recover, so a panicking tool unwinds into its seeded slot
// instead of the process. And wg.Done is deferred OUTERMOST, so a faulted tool
// still releases the batch.
//
// What none of them can hold is a tool that simply never returns: this function
// waits for its batch, and it must, because the transcript's next step cannot
// be assembled with a hole in it. SO EVERY TOOL THAT WAITS ON SOMETHING OUTSIDE
// THIS PROCESS BOUNDS ITS OWN WAITING — the shell tools by their timeout
// argument (tools_jobs.go), a watch tick by watchMaxTickTimeout
// (tools_watch.go), a connect question by connectAskTimeout (connect.go), a
// look at a picture by viewLookWindow (tools_view.go). A new tool that blocks
// without a bound of its own is the one way left to break this.
func (a *Agent) runToolsWarm(ctx context.Context, ep *episode, calls []ai.ToolCall, hub *eventHub, warm *warmBatch) []toolResult {
	for _, call := range calls {
		hub.send(Event{
			Kind: EventToolBegin,
			Tool: call.Function.Name,
			Hint: a.gloss(call),
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
		started := time.Now()
		// THE CALL'S OWN ID TRAVELS WITH IT. It is what lets a tool still
		// running be addressed from outside the turn — the surface's key that
		// sends a foreground bash call to the background has to find the process
		// somehow, and the id on the row is the only handle it has (promote.go).
		// It is set at this chokepoint rather than per tool, so the early warm
		// start carries it exactly as the batch does.
		text, isError, err := tool.Execute(withCallID(ctx, call.ID), args)
		// THE ROW'S CLOCK IS THIS CALL'S OWN CLOCK. The result cannot be sent
		// yet — it goes out with the batch, in call order, because that is the
		// order the transcript is written in — but the fact that this call is
		// OVER, and what it cost, is known here and is stale by the time the
		// slowest sibling returns. Sent from inside the execution so the early
		// start (warmBatch.consider) is measured the same way the batch is.
		if hub != nil {
			hub.send(Event{
				Kind:   EventToolFinished,
				Tool:   call.Function.Name,
				Args:   argsText(call),
				CallID: call.ID,
				Took:   time.Since(started),
			})
		}
		if err != nil {
			// Harness-level failure: the model sees the Go error as the tool
			// result, matching pi's thrown-Error semantics.
			return ep.noteToolOutcome(call, toolResult{text: err.Error(), isError: true})
		}
		// AND THE LAST THING THAT HAPPENS TO A RESULT IS THE ERROR→FIX SIDECAR
		// (fixrecall.go). A failure this machine has seen before leaves with one
		// line saying what made it go away last time; a success that follows one
		// records the command that did it. It is here, at the chokepoint, for the
		// reason the pre-action gate is: the batch and the early start both pass
		// through this function and nothing else does, so a call cannot be
		// executed without being learned from.
		//
		// It must happen HERE and not at post-feedback, which is where an
		// observer of results would otherwise belong: runTurn writes each result
		// into the transcript BEFORE that seam runs, so a line added there would
		// be a line no model was ever sent.
		return ep.noteToolOutcome(call, toolResult{text: text, isError: isError})
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
	// The settings read is the row it went to look at, and a call with no key
	// at all is the whole sheet, which reads honestly as its bare name.
	"settings": "key",
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
	// A settings change needs BOTH, and this is the one entry here where the
	// second field is not a nicety. change_setting goes to the person through
	// the approval gate, and this line is the headline of the card they answer
	// with one key: "change_setting" alone would be a question about nothing,
	// and the whole of what they are agreeing to is which row and what it
	// becomes.
	"change_setting": {"key", "value"},
}

// gloss renders one call as a person-readable line: the tool name and the one
// argument that identifies the work. Unparseable arguments degrade to the bare
// name rather than to the raw JSON — a malformed call is still a call the
// person should see happening.
// gloss on the AGENT is the same line for a tool whose name and arguments were
// never written down here: an account's own tool reads as the account, the name
// that account calls it, and what the call is about (served.go). Every surface
// takes this one rather than the free function below, so the row a person
// watches and the question they are asked say the same thing.
func (a *Agent) gloss(call ai.ToolCall) string {
	if record, served := a.servedRecord(call.Function.Name); served {
		return scrubbed(servedGloss(record, call.Function.Arguments))
	}
	return gloss(call)
}

// gloss is the free function, and it is the one that SCRUBS — every path that
// builds a gloss out of a model's arguments goes through here or through the
// method above, and both leave with plain text.
//
// A GLOSS IS MODEL-CONTROLLED TEXT ON THE ONE LINE THAT MUST NOT LIE. It is the
// headline of the consent card, and the card's next line is the offer a person
// answers with one key; an argument carrying escape bytes can move the cursor up
// and repaint that offer, so the question on screen says one thing and the call
// underneath it is another. Cutting the newline was never enough — a terminal
// takes its orders in escapes, and internal/tui3's fitter measures those as zero
// cells and lets them through whole. So they are dropped here, at the only place
// a gloss is made, exactly as an OSC payload's are (internal/tui3's notify.go).
func gloss(call ai.ToolCall) string {
	return scrubbed(glossOf(call))
}

// scrubbed drops the bytes a terminal reads as instructions rather than as
// text: the escape that opens a control sequence, and every other control byte
// with it. It DROPS rather than escapes, on notify.go's reasoning — a row with a
// stray backslash in it says less than a row with a missing byte, and the byte
// was never anything a person was going to read.
func scrubbed(text string) string {
	if strings.IndexFunc(text, control) < 0 {
		return text
	}
	return strings.Map(func(r rune) rune {
		if control(r) {
			return -1
		}
		return r
	}, text)
}

func control(r rune) bool { return r < ' ' || r == 0x7f }

func glossOf(call ai.ToolCall) string {
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
//
// ARGS STAY PARSEABLE WHENEVER THE WIRE ARGS WERE, and that is this function's
// whole contract to a surface. Cutting the compacted JSON at a byte offset ends
// it in the middle of a string literal, and every reader downstream then answers
// "this is not JSON" about a call that was perfectly well formed: internal/tui3
// read an empty content field out of a 20k write and drew the em dash it draws
// for an expansion with nothing in it, so opening the largest writes — the ones
// worth opening — showed a dash. The cut therefore happens INSIDE the oversized
// string values (see [capArgsValues]) and the object is written back out whole.
func argsText(call ai.ToolCall) string {
	raw := strings.TrimSpace(call.Function.Arguments)
	if raw == "" {
		return ""
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(raw)); err == nil {
		raw = compacted.String()
		if len(raw) > argsLimit {
			if capped, ok := capArgsValues(raw, argsLimit); ok {
				raw = capped
			}
		}
	}
	// SCRUBBED FOR THE GLOSS'S REASON. This is the other half of the same card —
	// the phone sheet lays the command out of these arguments rather than out of
	// the headline — and arguments that did not parse pass through as their own
	// text, raw control bytes and all. An escape spelled the JSON way is six
	// ordinary characters here and becomes a control byte only when a surface
	// unmarshals it, which is why internal/tui3's card scrubs what it reads back
	// out of the arguments too.
	//
	// The clip is the LAST RESORT, for the one input the branch above cannot
	// help: arguments that are not JSON at all. Those have no string values to
	// cut inside of, and a byte cut of a malformed payload breaks nothing that
	// was not already broken.
	return scrubbed(clip(raw, argsLimit))
}

// capArgsValues brings one compacted JSON payload under limit by shortening its
// oversized STRING values rather than by cutting the text, and reports whether
// it managed it.
//
// THE RULE IS ONE THRESHOLD FOR THE WHOLE PAYLOAD: every string longer than it
// is cut to it and marked, every string shorter is untouched, and the threshold
// is the largest one under which the re-marshaled object fits. That is what
// shares the budget honestly across a call carrying several long strings — an
// edit sending four replacement blocks gets four comparable windows onto them
// rather than a whole diff spent on whichever block the model sent first, and a
// write sending one body gets all of it.
//
// It walks NESTED values, unlike the forming scanner's deliberate top-level-only
// rule (toolhint.go): bare spells an edit as {path, edits:[{oldText,newText}]},
// so the two strings a diff is computed from live inside an array, and a capper
// that only knew about top-level fields would leave the exact call this exists
// for untouched.
//
// The one thing the round trip does not preserve is the ORDER of an object's
// keys, which comes back alphabetical. Nothing downstream reads arguments
// positionally — every reader in the tree asks for a field by name — and this
// runs only for a payload past the cap, which in practice is a write or an edit
// whose fields a surface has a table row for.
func capArgsValues(compacted string, limit int) (string, bool) {
	// UseNumber, because the round trip has to give the numbers back as the model
	// spelled them. Decoded into a bare any they become float64 and a timeout of
	// 1200000 comes back out as 1.2e+06 — a payload nobody sent, in a field a
	// surface reads.
	decoder := json.NewDecoder(strings.NewReader(compacted))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return "", false
	}
	// The walk collects a setter per string rather than the strings themselves,
	// because the same threshold is applied several times over — once per probe
	// below — and each probe writes the ORIGINAL value back through the setter
	// it came with.
	type slot struct {
		text string
		set  func(string)
	}
	var slots []slot
	var walk func(value any, set func(string))
	walk = func(value any, set func(string)) {
		switch typed := value.(type) {
		case string:
			slots = append(slots, slot{text: typed, set: set})
		case map[string]any:
			for key, child := range typed {
				key := key
				walk(child, func(text string) { typed[key] = text })
			}
		case []any:
			for index, child := range typed {
				index := index
				walk(child, func(text string) { typed[index] = text })
			}
		}
	}
	walk(payload, func(string) {})
	if len(slots) == 0 {
		return "", false
	}

	// fits applies one threshold and answers with the payload it produced. A
	// threshold of zero is legal and means "every long string is nothing but its
	// marker", which is the floor this search stops at.
	fits := func(threshold int) (string, bool) {
		for _, s := range slots {
			s.set(capBytes(s.text, threshold))
		}
		// SetEscapeHTML(false), because this is not going into a web page and the
		// default would spell every `<` in a written Go file as < — six bytes
		// of the budget for one character, and a payload that no longer looks like
		// the one the model sent to anybody reading it as text.
		var out bytes.Buffer
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(payload); err != nil {
			return "", false
		}
		// Encode ends every value with a newline, which json.Compact would not
		// have left there and [scrubbed] would drop anyway.
		text := strings.TrimRight(out.String(), "\n")
		return text, len(text) <= limit
	}

	// The largest threshold that fits, by bisection on the byte budget itself.
	// Marshaled size does not move one-for-one with the threshold — a marker
	// costs about twenty bytes and an escaped rune costs six — so the size is
	// measured rather than predicted, and the search is over the one quantity
	// that is actually monotonic in it.
	low, high := 0, limit
	best, found := "", false
	for low <= high {
		mid := (low + high) / 2
		out, ok := fits(mid)
		if ok {
			best, found = out, true
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	return best, found
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
	// Calls counts THIS request, and every other one the session makes. It is
	// the honest denominator Turns cannot be: Turns is the conversation's own
	// steps by law, and an auxiliary call is not one of them (see
	// [Agent.addAuxiliaryUsage]).
	turn.Calls++
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
	a.usage.Calls++
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

	// maxTrustedWindow is the largest window the compaction machinery will
	// BELIEVE, whatever a catalog row claims, and it is a hard ceiling rather
	// than a preference.
	//
	// The threshold is derived from a number a provider publishes about itself,
	// and a published number can be enormous. The catalog row for
	// ~deepseek/deepseek-v4-flash-latest claims 1,310,720 tokens, which put this
	// session's trigger at 1,114,112 — so a real conversation grew to 386,309
	// tokens with compaction never once firing, and what came back at that size
	// was not an answer but the model's own template turned inside out. Nothing
	// was broken; the law simply believed the row.
	//
	// Twice [defaultContextWindow] is the ceiling because that constant is what
	// this codebase already believes about windows: 128k is the smallest window
	// this surface routes to, and no model is asked to carry more than double
	// the smallest into a single request. A model whose real window is 200k or
	// 400k is untouched — only a claim beyond 256k is clamped, and a claim
	// beyond 256k is exactly the kind that let 386k out.
	maxTrustedWindow = 2 * defaultContextWindow
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

// TrustedWindow is a claimed context window with the ceiling applied — the
// figure the compaction machinery works from, as opposed to [Agent.window],
// which stays the model's own claim because the status meter is describing the
// model rather than this law.
//
// It is exported for the same reason [CompactThreshold] is: a surface that
// needs to know how much room the guard leaves must read the guard, not a
// second copy of it.
func TrustedWindow(window int) int {
	if window <= 0 {
		return 0
	}
	if window > maxTrustedWindow {
		return maxTrustedWindow
	}
	return window
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
//
// A window larger than [maxTrustedWindow] is clamped to it BEFORE the reserve is
// taken, so an absurd or unpublished claim can never move the trigger past the
// ceiling. That is the guard, and it lives here rather than at the door because
// the door is not the only way a window arrives.
func CompactThreshold(window int) int {
	window = TrustedWindow(window)
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

// guardOversizeRequest is the check made with the request already assembled and
// about to go out, and it is the one pass Config's CompactEnabled does not
// govern.
//
// [Agent.maybeCompact] is the ordinary pass and a person may switch it off: it
// is about headroom, and headroom is a preference. This is not that. A
// transcript that has already grown past the whole window is a request the
// endpoint cannot serve, and until this existed the loop found that out by
// SENDING IT and reading the refusal — the overflow branch in the step loop.
// A blind send costs the whole prompt in latency, costs money on an endpoint
// that bills the attempt, and on an endpoint that neither refuses nor serves it
// costs the turn: 386,309 tokens went out against a row claiming 1.3M and came
// back as corrupted template text rather than an error anything could catch.
//
// The bar is [TrustedWindow], not the model's own claim, for exactly that
// reason — the claim is what was wrong.
func (a *Agent) guardOversizeRequest(ctx context.Context, hub *eventHub) {
	ceiling := TrustedWindow(a.window())
	if ceiling <= 0 {
		return
	}
	a.mu.Lock()
	estimate := a.estimateTokensLocked()
	a.mu.Unlock()
	if estimate <= ceiling {
		return
	}
	// A pass that finds nothing is not an error here: the transcript is then
	// the person's own words and the recent tail, and the step goes out because
	// there is nothing left to take out of it.
	_, _ = a.compact(ctx, hub)
}

// maybeCompact is the automatic pass, checked after every step. Config's
// CompactEnabled gates only this one — Compact, the oversize guard and the
// overflow recovery run regardless.
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

// compactionPass is what one pass did, and it is the ONLY thing a pass produces:
// two counts and, when something was folded, the marker line that stands in its
// place. There is no summary because there is no summarizer — a pass is a
// rearrangement of text this session already has (see the file header comment on
// [Agent.compact]).
type compactionPass struct {
	stubbed int
	folded  int
	marker  string
	// stored says the full record went somewhere a later session can still read
	// it — the store's thread (chatlog.go). It is what makes the difference
	// between the two announce lines honest.
	stored bool
}

func (p compactionPass) empty() bool { return p.stubbed == 0 && p.folded == 0 }

// compact runs one pass, and IT MAKES NO MODEL CALL AT ALL.
//
// The old pass paid a summarizer to write prose about the prefix it was about to
// throw away. It was expensive at the worst moment, it was lossy by
// construction, and the loss was unrecoverable because the transcript the prose
// was written from went with it. What replaces it is two mechanical passes over
// the same messages, in order of how cheap the content is to give up:
//
//  1. THE STUB PASS. A tool result the model has already used is a pointer to
//     its own bytes (stub.go). Nothing is described, nothing is decided, and the
//     bytes stay readable — in the store's thread, or in this session's logs/.
//
//  2. THE FOLD. If the transcript is still over threshold, the oldest ASSISTANT
//     work is replaced by one marker line naming how much went and where it can
//     be read. User messages are never folded: a person's own words are the one
//     thing in a transcript that nothing else can reconstruct.
//
// What the model is handed instead of a summary is the STATE CARD, which rides
// in the system prompt on every turn and is maintained incrementally by the
// post-turn extractor (card.go). So the cost of knowing what the conversation is
// about is amortized across the turns that produced it, and the compaction
// itself is free.
//
// The lock is held across the WHOLE pass, which the old one could not do because
// it was waiting on a provider. That is not a cost, it is the removal of one:
// the mid-batch race the old pass had to repair — a tool result landing after
// the cut while the summary was being written — cannot happen when nothing is
// awaited.
func (a *Agent) compact(_ context.Context, hub *eventHub) (bool, error) {
	a.mu.Lock()
	if a.compacting {
		a.mu.Unlock()
		return false, ErrCompactionInFlight
	}
	a.compacting = true
	tokensBefore := a.estimateTokensLocked()

	// THE CONVERSATION IS SHAPED FOR THE SCROLLBACK BEFORE IT IS EDITED. This is
	// the same region a resume recovers from the journal ([replayedSession.earlier]),
	// taken from memory because that is where it is: the pass is about to stub
	// results and fold assistant work in place, and afterwards the original text
	// exists only in the file. Shaping it now is what lets a person scroll back
	// through a pass that fired under them and read what was there.
	//
	// It is shaped rather than copied for [Agent.earlier]'s reason — a copy of
	// the messages would hold this session's pictures alive after the pass let go
	// of them — and it is assigned only once the pass is known to have DONE
	// something, below, so a refused pass leaves the region it replaced alone.
	// It is [Agent.Transcript]'s own shaping, over [Agent.Transcript]'s own
	// messages, and that exactness is the point: a surface holding a position in
	// the transcript it drew can carry that position straight over into the
	// region, because the two lists are the same list (internal/tui3's replay.go).
	// The system message needs no removing — shapeEntries drops it.
	earlier := shapeEntries(a.messages, a.file)

	pass := compactionPass{stored: a.chatlog != nil}
	pass.stubbed = a.stubOldOutputsLocked()
	if a.estimateTokensLocked() > a.compactThreshold() {
		pass.folded, pass.marker = a.foldLocked(pass.stored)
	}
	a.compacting = false

	if pass.empty() {
		// Nothing was old enough to stub and nothing was foldable: the whole
		// transcript is the person's own words and the recent tail, which is
		// what [ErrNothingToCompact] has always meant.
		a.mu.Unlock()
		return false, ErrNothingToCompact
	}

	// The pass really edited the transcript, so the region above it is now
	// history and this is the record of it. The region a PREVIOUS pass left is
	// replaced rather than prepended to, which is the same one-hop reading the
	// journal is given ([replayedSession.earlier]): the window this pass just
	// rewrote already contains everything the older marker was about.
	//
	// AND THE FLOOR IS THE WHOLE TRANSCRIPT, because at this instant the whole
	// transcript IS the rewritten copy — every entry of it is a stub, a fold line
	// or a kept line standing in for something in the region above. Everything
	// appended after this point is new conversation and sits below the floor,
	// which is why the floor is an index from the START and never moves again
	// ([EarlierHistory]).
	a.earlier = earlier
	a.earlierFloor = len(shapeEntries(a.messages, a.file))

	// The provider's context figure described the request that is now gone.
	// Zero sends the estimator back to the content until the next response.
	a.contextTokens = 0
	tokensAfter := a.estimateTokensLocked()
	// The whole rebuilt window is re-journaled behind the marker, not just the
	// tail: a stub and a fold are edits to messages the file already holds ABOVE
	// the marker, and replay discards everything above it. Writing the window is
	// what makes [replaySessionFile] rebuild the identical transcript instead of
	// a plausible one (sessionfile.go).
	if a.file != nil {
		window := make([]ai.Message, len(a.messages)-1)
		copy(window, a.messages[1:])
		a.file.appendCompaction(pass, tokensBefore, window)
	}
	a.mu.Unlock()

	if hub != nil {
		hub.send(Event{Kind: EventCompacted, Hint: compactionHint(pass, tokensBefore, tokensAfter)})
	}
	return true, nil
}

// compactionHint is the one dim line the turn after a pass shows, and every
// clause in it is a real count:
//
//	compacted · stubbed 14 tool results · folded 31 messages · nothing lost — full record in the store
//
// A clause whose count is zero is not printed at all — the emptiness law. The
// last clause tells the truth about which floor this session actually has: the
// store's thread when there is one, and otherwise the session journal, which
// keeps every original line above the marker and is a smaller promise honestly
// made.
func compactionHint(pass compactionPass, before, after int) string {
	clauses := []string{"compacted"}
	if pass.stubbed > 0 {
		clauses = append(clauses, fmt.Sprintf("stubbed %d tool result%s", pass.stubbed, plural(pass.stubbed)))
	}
	if pass.folded > 0 {
		clauses = append(clauses, fmt.Sprintf("folded %d message%s", pass.folded, plural(pass.folded)))
	}
	if before > after {
		clauses = append(clauses, fmt.Sprintf("%s → %s tokens", approxTokens(before), approxTokens(after)))
	}
	if pass.stored {
		clauses = append(clauses, "nothing lost — full record in the store")
	} else {
		clauses = append(clauses, "full record in the session journal")
	}
	return strings.Join(clauses, " · ")
}

// foldLocked replaces the oldest assistant work with one marker line, and
// reports how many messages went and what the marker says.
//
// THREE THINGS ARE NEVER FOLDED, and each for its own reason:
//
//   - message[0], the system prompt, which carries the memory block and the
//     state card and is rebuilt per turn anyway;
//   - USER MESSAGES, anywhere, because a person's words are the one part of a
//     transcript that cannot be reconstructed from anything else — a question
//     they asked and never got answered has to still be in front of the model;
//   - the verbatim tail below [Agent.cutPointLocked], which is the work in hand.
//
// An assistant message and the tool results answering it go TOGETHER, always. A
// tool result whose call was folded away is an orphan every provider rejects
// with a 400 — on this request and on every request after it, because the
// transcript is append-only — so the walk moves in whole batches and stops on a
// batch boundary.
func (a *Agent) foldLocked(stored bool) (int, string) {
	limit := a.cutPointLocked()
	target := a.compactThreshold() * bytesPerToken
	total := 0
	for _, message := range a.messages {
		total += messageBytes(message)
	}

	folded := make(map[int]bool, 16)
	first, last := -1, -1
	for index := 1; index < limit && total > target; {
		if a.messages[index].Role == "user" {
			index++
			continue
		}
		batch := index + 1
		for batch < limit && a.messages[batch].Role == "tool" {
			batch++
		}
		// A stub is useful only while its tool call remains in the window. Keep
		// tool batches intact: folding the assistant call would either orphan the
		// stub or fold the stub too, defeating the required stubs-plus-folds shape.
		if len(a.messages[index].ToolCalls) > 0 {
			keepsStub := false
			for cursor := index + 1; cursor < batch; cursor++ {
				if strings.HasPrefix(strings.TrimSpace(messageContentText(a.messages[cursor])), stubMarker) {
					keepsStub = true
					break
				}
			}
			if keepsStub {
				index = batch
				continue
			}
		}
		for cursor := index; cursor < batch; cursor++ {
			folded[cursor] = true
			total -= messageBytes(a.messages[cursor])
			if first < 0 {
				first = cursor
			}
			last = cursor
		}
		index = batch
	}
	if len(folded) == 0 {
		return 0, ""
	}

	from, to := a.chatlog.ref(a.messages[first]), a.chatlog.ref(a.messages[last])
	if from == "" && a.file != nil {
		from = a.file.messageRef(a.messages[first])
	}
	if to == "" && a.file != nil {
		to = a.file.messageRef(a.messages[last])
	}
	marker := foldMarker(len(folded), from, to, stored)
	rebuilt := make([]ai.Message, 0, len(a.messages)-len(folded)+1)
	rebuilt = append(rebuilt, a.messages[0])
	for index := 1; index < len(a.messages); index++ {
		if index == first {
			// The marker sits where the run it replaces sat, so the order the
			// conversation happened in survives the fold.
			rebuilt = append(rebuilt, textMessage("user", marker))
		}
		if folded[index] {
			continue
		}
		rebuilt = append(rebuilt, a.messages[index])
	}
	a.messages = rebuilt
	return len(folded), marker
}

// foldMarkerPrefix opens every fold marker. It is how [isCompactionNote]
// recognizes a line this package injected rather than something anybody said —
// a string this package controls, never a guess at wording.
const foldMarkerPrefix = "[folded "

// foldMarker is the line that stands in for what went. It names the count and
// the range, because a person reading their own transcript back has to be able
// to find the part that is not there any more.
//
//	[folded 31 messages · store:104..store:189]
//	[folded 31 messages · full record in the session journal]
func foldMarker(count int, from, to string, stored bool) string {
	where := "full record in the session journal"
	switch {
	case from != "" && to != "" && from != to:
		where = from + ".." + to
	case from != "":
		where = from
	case stored:
		// The store is on but these particular lines never reached it — a post
		// that failed, or a message recorded before the log opened. Say the
		// weaker true thing rather than the stronger one.
		where = "full record in the session journal"
	}
	return fmt.Sprintf("%s%d message%s · %s]", foldMarkerPrefix, count, plural(count), where)
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

// addAuxiliaryUsage folds one auxiliary call — the session title (title.go), a
// memory reflex (memory.go) — into the SESSION total only, and writes it down.
//
// The person pays for it, so it cannot be free; but no turn asked for it, and
// charging it to the turn that happened to cross the threshold would make one
// ordinary question read as three times the cost of its neighbours. Turns is
// left alone for the same reason — this is bookkeeping, not a step of the
// conversation — and contextTokens too: an auxiliary call runs against its own
// two-message context, which says nothing about this session's.
//
// The model is the caller's because only the caller knows it: every one of
// these runs on a model of its own — the namer, the guardian, the reflex, the
// seer, a whole child agent — and a.model is the model the CONVERSATION is on,
// which is precisely the one that did not do this work. calls is how many
// provider requests the figures cover: one for an ordinary auxiliary call, and
// a whole child's tally when a task node is folded in ([Agent.foldTaskUsage]).
//
// The line is journaled with aux set, so a replay can add it to the session's
// spend without counting it as a step of the conversation ([journalUsage]). A
// call that reports no usage at all still folds — into nothing — and writes no
// line, by the same emptiness law the turn seal keeps.
func (a *Agent) addAuxiliaryUsage(response *ai.Response, model string, calls int) {
	a.addAuxiliaryUsageAs(response, model, calls, "")
}

// The roles an auxiliary line can name. A line is journaled with the role that
// made the call so a bad answer can be traced to the model that gave it: the
// session's name and a piece of work's name are the two that a person SEES, and
// the two whose failure ("name this session in ≤8 words, lowercase, no quotes"
// as a session's name) is otherwise unattributable — the aux mark says a turn
// did not ask for the call, and the model says which model answered, but
// neither says what was being asked for.
const (
	auxRoleTitle    = "title"
	auxRoleTaskName = "taskname"
	// auxRoleIntake is the third for the same reason the first two are: the form
	// a subharness is launched on is something a person SEES, and a field filled
	// wrongly is unattributable without the name of the model that filled it
	// (subharness_intake.go).
	auxRoleIntake = "intake"
)

// addAuxiliaryUsageAs is [Agent.addAuxiliaryUsage] with the role named. It is a
// second door rather than a fourth argument on the first because thirty callers
// fold auxiliary usage and only the two namers have anything to say here; an
// empty role journals no field at all, by the emptiness law the rest of the
// line keeps.
func (a *Agent) addAuxiliaryUsageAs(response *ai.Response, model string, calls int, role string) {
	if response == nil || response.Usage == nil {
		return
	}
	usage := response.Usage
	aux := Usage{
		Input:      usage.PromptTokens,
		Output:     usage.CompletionTokens,
		CacheRead:  usage.CacheReadTokens(),
		CacheWrite: usage.CacheCreationTokens(),
		Calls:      calls,
	}
	if usage.Cost != nil {
		aux.CostUSD = *usage.Cost
	}
	a.mu.Lock()
	a.usage.Input += aux.Input
	a.usage.Output += aux.Output
	// The cache figures follow the tokens they belong to. An auxiliary call is
	// paid for out of the same pocket, so leaving them out would make the
	// session's cached share a fraction of only part of its input.
	a.usage.CacheRead += aux.CacheRead
	a.usage.CacheWrite += aux.CacheWrite
	a.usage.CostUSD += aux.CostUSD
	a.usage.Calls += aux.Calls
	a.mu.Unlock()
	// The write is outside the lock for the reason [Agent.sealTurn]'s is: the
	// file has its own, and holding the agent's across a disk write would put
	// every reader of the session's totals behind it.
	a.file.appendUsage(aux, model, true, role)
}
