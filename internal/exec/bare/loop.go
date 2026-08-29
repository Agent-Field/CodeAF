package bare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── retry constants (pi spec §4) ────────────────────────────────────────────

// maxRetries is pi's default auto-retry count: 3 (settings-manager.js L553-559).
// The schedule is 2s, 4s, 8s (baseDelayMs=2000 * 2**(attempt-1)).
const maxRetries = 3
const retryBaseDelay = 2 * time.Second

// ── what a cut stream is worth asking again ─────────────────────────────────
//
// A stream the guard cut (internal/provider's streamguard.go) is a different
// kind of failure from a torn connection: the request never failed, so there is
// nothing to back off from, and the transport's three attempts are the wrong
// budget for it. The figures are internal/session's, SPELLED THE SAME WAY here
// rather than shared, because that package imports this one and the reverse
// would be a cycle — the reason every constant in this file is a copy of one.
//
// A cut used to end a bare run outright: its text matches no retryable pattern,
// so it fell through to the "this will never work" branch and the whole run died
// on an endpoint that had simply gone quiet.
//
// WHAT IS DELIBERATELY NOT HERE is the model hop internal/session makes when
// this budget is spent. A hop is only honest if it is ANNOUNCED — the rest of
// the answer arrives in a different voice, at a different price — and this loop
// has no lane to announce anything on. An unannounced model change is the one
// thing that layer's law forbids, so a spent budget here is the error it was.
const (
	silentRetries = 2
	babbleRetries = 1
	// blindRetries is the budget when nothing was routed away from the endpoint
	// that went quiet: `routing off`, or a stream that died before naming its
	// provider. Two attempts that could only land in the same place are one
	// attempt with a wait in front of it.
	blindRetries = 1
)

// ── compaction constants (pi spec §5) ────────────────────────────────────────

const (
	compactionReserveTokens    = 16384
	compactionKeepRecentTokens = 20000
	// contextWindow is the model's context window. pi reads it from
	// model.contextWindow; we use a conservative default since bare does not
	// carry a catalog. This is the threshold against which compaction fires:
	// shouldCompact = contextTokens > contextWindow - reserveTokens.
	defaultContextWindow = 128_000
)

// ── retry classification (pi-ai compat) ─────────────────────────────────────

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
// these — it compacts instead. We match a subset of the common patterns.
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

// ── loop state ───────────────────────────────────────────────────────────────

// loopState holds the mutable state of one bare run.
type loopState struct {
	client providerClient
	tools  []Tool
	// sweep opens the world's own account of the workspace immediately before a
	// tool call and hands back the function that closes it, so the files the
	// call left behind can be filed under this leaf. pi's tools know a directory
	// and nothing of a workspace, and this is the seam that keeps them from
	// having to.
	//
	// It is a pair rather than a single call with a timestamp because the
	// evidence is a BEFORE-AND-AFTER READ OF THE TREE and not a clock reading —
	// see internal/exec/produced.go and FAILSAFE.md rule 2. Nil is a loop with
	// nobody to tell, which is what some of the tests run.
	sweep    func() (file func())
	system   string
	user     string
	cwd      string
	deadline time.Duration

	// messages is the append-only transcript. Turn N>=2: system, user(task),
	// assistant(turn1), toolResult(turn1), ..., assistant(turnN).
	messages []ai.Message

	// requestLog records the messages slice sent on each request, for the
	// golden equivalence test to assert append-only extension.
	requestLog [][]ai.Message

	// turn is the model request this loop is on, numbered from one. It exists
	// as state rather than as a parameter because everything that records a
	// transcript entry — the turn itself, the tools it spawned, a compaction
	// that fired underneath it — has to file the entry against the same number,
	// and threading it through four call sites is how they drift apart.
	turn int

	// transcript is where this run's turns are written down, or nil when
	// nobody is listening. It is read once, from the context, at the top of
	// run: the sink belongs to one attempt at one node and the context is what
	// carries per-attempt facts here (see exec.WithTranscript).
	//
	// THE REASON IT EXISTS AT ALL is that this loop used to be unobservable.
	// messages and requestLog above are in-memory only; they go away with the
	// process. A leaf that ran this loop billed 1.29M prompt tokens and fifty
	// cents against one node, wrote no file, and left a record consisting
	// entirely of the harness's own progress lines — so nobody could say what it
	// had done, or learn anything from what it cost.
	transcript exec.TranscriptSink

	// faulted carries one interruption of this loop out to the surface that
	// owns the journal, so a person watching a headless run is told about it
	// while it is happening (exec.Task.Fault, and internal/store/fault.go for
	// the law).
	//
	// THE TRANSCRIPT IS NOT ENOUGH ON ITS OWN. A note recorded under the node is
	// durable and readable afterwards, which answers the autopsy; it reaches
	// nobody who is watching the run now, and a stalled provider call that costs
	// fifteen minutes is exactly the fact an operator has to see while there is
	// still a run to save. FAILSAFE.md's third clause: a fail-safe propagates to
	// the verdict the person reads. Nil is a loop with nowhere to write, which
	// is what the tests and the bench harness run.
	faulted func(error)

	// measured is what this leaf has learned about the machine it is running
	// on: how long its own model calls and its own transcript writes actually
	// take. It is what the tool bound and the landing decision are derived
	// from, so neither of them is a number somebody picked. See room.go.
	measured pace
}

// tell reports one interruption of this loop to whoever can write it down, and
// reports nothing when nobody can. The nil check lives here rather than at the
// call sites for the reason exec.Task.progress gives about its own.
//
// It is the sibling of [loopState.fault], and the pair is deliberate: fault
// writes the RECORD, under the node, for whoever reads the run afterwards; this
// writes the LINE, now, for whoever is watching it. The two answer different
// questions and a cut that only did one of them was the defect.
func (l *loopState) tell(err error) {
	if l.faulted == nil || err == nil {
		return
	}
	l.faulted(err)
}

// providerClient is the narrow slice of provider.Client the loop needs.
// It is an interface so the golden test can substitute a scripted completer.
type providerClient interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// run executes the loop. It returns a fully populated exec.Outcome.
// run drives the loop with nothing to close: the shape every test and every
// caller that has no workspace to photograph gets.
func (l *loopState) run(ctx context.Context) *exec.Outcome {
	return l.runClosing(ctx, nil)
}

// runClosing is run with the leaf's own closing armed.
//
// close is asked ONCE PER ANSWER, at the one exit where this loop finishes under
// its own power — the turn that came back with no tool call. It is handed the
// outcome with the leaf's own after-photograph already taken and answers with
// the note to carry on from, or the empty string to land on the reading it just
// took. That is the whole seam: the decision itself, the bound on it and the
// journal are exec.SelfCloser's, shared with the generalist belt, because a
// mechanism only one worker has is one the run does not (see
// internal/exec/selfclose.go).
func (l *loopState) runClosing(ctx context.Context, close func(*exec.Outcome) string) *exec.Outcome {
	outcome := &exec.Outcome{Stop: exec.StopDone}
	started := time.Now()

	// The sink, and the promise that whatever this loop records survives the
	// way it ends. The flush is deferred rather than written at each return
	// because there are five ways out of this function and one of them is a
	// panic unwinding through it — and a panic is precisely the ending whose
	// record a reader needs most. The runner flushes again on its own side
	// (cmd/aforge's runLeafWithWatchdog); both are no-ops on an empty buffer.
	l.transcript = exec.TranscriptFrom(ctx)
	defer l.flushTranscript()

	// Apply the deadline. bare sends NO cache key — no
	// provider.WithLeafCacheKey, no provider.WithCall. The provider client
	// adds cache headers only when CacheKeyFrom(ctx) is set, and bare leaves
	// it unset.
	if l.deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.deadline)
		defer cancel()
	}

	// Assemble the initial messages: system + user(task text).
	l.messages = []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: l.system}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: l.user}}},
	}

	// Build the tool definitions that go on the wire. These are the four
	// active tools (read, bash, edit, write) with verbatim pi schemas and
	// descriptions.
	definitions := l.toolDefinitions()

	for {
		// Check context before each turn.
		if ctx.Err() != nil {
			outcome.Stop = exec.StopDeadline
			outcome.Text = l.lastAssistantText()
			outcome.Elapsed = time.Since(started)
			l.fault("the leaf's time ran out before turn " + strconv.Itoa(l.turn+1) + ": " + ctx.Err().Error())
			return outcome
		}
		// THE ROOM TO LAND IS NOT THE WORK'S TO SPEND. Asked before the call
		// rather than after it, because a turn started inside the reserve is a
		// turn whose answer arrives after the deadline and is thrown away —
		// which is how a leaf that had been working for seventeen minutes was
		// recorded as one that never came back. See room.go.
		if left, bounded := room(ctx); bounded && left <= l.measured.reserve() {
			return l.landOnTheWall(ctx, outcome, started)
		}
		l.turn++

		// Snapshot the messages for the append-only test assertion.
		l.requestLog = append(l.requestLog, copyMessages(l.messages))

		// One turn = one provider request. Retry on retryable errors.
		response, err := l.completeWithRetry(ctx, definitions)
		if err != nil {
			if ctx.Err() != nil {
				outcome.Stop = exec.StopDeadline
			} else {
				outcome.Stop = exec.StopError
			}
			outcome.Text = l.lastAssistantText()
			outcome.Elapsed = time.Since(started)
			l.fault(err.Error())
			return outcome
		}

		outcome.Turns++
		addUsage(&outcome.Usage, response)

		calls := response.ToolCalls()

		// Stop condition (pi spec §2): the loop ends when the assistant
		// response has NO tool call.
		if len(calls) == 0 {
			outcome.Text = strings.TrimSpace(response.Text())
			outcome.Elapsed = time.Since(started)
			l.say(store.TranscriptAssistant, outcome.Text)
			// AND THE LEAF READS ITS OWN WORK BEFORE ANYBODY ELSE DOES. A
			// finding this leaf's own closing photograph raises against this
			// leaf — a public name it deleted, a name it reads that nothing
			// binds, a check it turned red — used to reach nobody until the
			// leaf had landed and a gate had bought a cold repair round. The
			// worker holding the transcript that produced the fault is still
			// standing right here, so it is asked. A close reopens the loop the
			// way any other unanswered turn does: the answer goes in as the
			// draft it now is, the finding follows it, and the next turns
			// settle it.
			if close != nil {
				if note := close(outcome); note != "" {
					l.messages = append(l.messages,
						ai.Message{Role: "assistant", Content: assistantContent(response)},
						ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: note}}})
					l.note(note)
					continue
				}
			}
			return outcome
		}

		// Append the assistant message with its tool calls.
		assistantMsg := ai.Message{
			Role:      "assistant",
			Content:   assistantContent(response),
			ToolCalls: calls,
		}
		l.messages = append(l.messages, assistantMsg)

		// The turn goes on the record before the tools run, in the order it
		// happened: what the model said, then what it asked for. A turn whose
		// tools hang would otherwise be a turn nobody could see the reasoning
		// for, and a hung tool is one of the two endings this record is for.
		l.say(store.TranscriptAssistant, strings.TrimSpace(response.Text()))
		for _, call := range calls {
			l.record(store.TranscriptEntry{
				Kind:   store.TranscriptToolCall,
				Tool:   call.Function.Name,
				CallID: call.ID,
				Text:   call.Function.Arguments,
			})
		}

		// THE BATCH RUNS INSIDE WHAT IS LEFT, LESS WHAT IT TAKES TO LAND. A
		// command handed the leaf's whole remaining envelope can spend all of
		// it, and then there is no leaf left to read what it said. Cutting the
		// command instead costs the command; cutting the leaf costs everything
		// the leaf had done. See room.go.
		toolCtx, release, affordable := l.toolRoom(ctx)
		if !affordable {
			// Nothing left but the landing. The calls the model just made are
			// already on the record as calls that were never run, which is the
			// honest account of them.
			return l.landOnTheWall(ctx, outcome, started)
		}

		// Execute the tool calls in parallel (pi spec §2: "run in parallel").
		results := l.executeTools(toolCtx, calls)
		// Released here rather than deferred: this is a loop that runs two
		// hundred times, and two hundred parked cancels is a leak wearing a
		// keyword.
		release()

		// Append tool result messages in the order the calls were issued.
		for i, call := range calls {
			outcome.ToolCalls++
			recordCall(outcome, call, results[i].isError)
			l.messages = append(l.messages, ai.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    []ai.ContentPart{{Type: "text", Text: results[i].text}},
			})
			l.record(store.TranscriptEntry{
				Kind:   store.TranscriptToolResult,
				Tool:   call.Function.Name,
				CallID: call.ID,
				Text:   results[i].text,
				Millis: results[i].elapsed.Milliseconds(),
				Failed: results[i].isError,
			})
		}

		// Compaction check (pi spec §5). Must exist; will not fire on small runs.
		l.maybeCompact(ctx, response, definitions)
	}
}

// ── the record ───────────────────────────────────────────────────────────────
//
// Four one-line helpers rather than four call sites building an entry by hand.
// Every one of them is a no-op when nothing is listening, and the nil check
// lives HERE rather than at each call site so that adding a fifth thing worth
// recording cannot forget it.

// record files one entry under the turn it belongs to.
func (l *loopState) record(entry store.TranscriptEntry) {
	if l.transcript == nil {
		return
	}
	entry.Turn = l.turn
	l.transcript.Record(entry)
}

// say records the model's own words. Empty is skipped: a turn that was nothing
// but tool calls said nothing, and an empty row claiming otherwise is a row a
// reader has to decide to ignore.
func (l *loopState) say(kind store.TranscriptKind, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	l.record(store.TranscriptEntry{Kind: kind, Text: text})
}

// fault records how the loop stopped being able to continue. It is the entry
// an autopsy reads first, and the one an in-memory transcript could never
// leave behind — the process that could have printed it is the process that
// went away.
func (l *loopState) fault(reason string) {
	l.record(store.TranscriptEntry{Kind: store.TranscriptFault, Text: reason, Failed: true})
}

// note records the harness talking about its own machinery, marked so it is not
// read as something the model said.
func (l *loopState) note(text string) {
	l.record(store.TranscriptEntry{Kind: store.TranscriptNote, Text: text})
}

// flushTranscript makes the record durable. It is called from a defer in run,
// so it must tolerate a nil sink and a second call from the runner.
func (l *loopState) flushTranscript() {
	if l.transcript == nil {
		return
	}
	// The write times itself, because the landing reserve is partly this: a
	// leaf that lands has to get its record on disk, and how long that takes is
	// a fact about the store under it rather than a number this file may pick.
	began := time.Now()
	l.transcript.Flush()
	l.measured.noteFlush(time.Since(began))
}

// toolResult is the output of executing one tool call.
type toolResult struct {
	text    string
	isError bool
	// elapsed is how long the tool took. It is measured here rather than
	// derived from the record's timestamps because the batch runs in parallel:
	// six tools that started together have one journal time between them, and
	// the one that took four minutes is the only interesting thing about them.
	elapsed time.Duration
}

// executeTools runs all tool calls in the batch concurrently and returns
// results in the same order as the calls.
func (l *loopState) executeTools(ctx context.Context, calls []ai.ToolCall) []toolResult {
	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c ai.ToolCall) {
			// wg.Done outermost, so a faulted tool still releases the batch:
			// guard.Recover turns the panic into a logged fault, and the slot
			// this goroutine owns stays the zero result rather than hanging
			// every sibling behind a Wait that never returns.
			defer wg.Done()
			defer guard.Recover("exec/bare tool " + c.Function.Name)
			started := time.Now()
			result := l.executeTool(ctx, c)
			result.elapsed = time.Since(started)
			results[idx] = result
		}(i, call)
	}
	wg.Wait()
	return results
}

// executeTool dispatches one tool call to the matching bare tool.
func (l *loopState) executeTool(ctx context.Context, call ai.ToolCall) toolResult {
	name := call.Function.Name
	args := json.RawMessage(call.Function.Arguments)
	for _, tool := range l.tools {
		if tool.Name == name {
			var file func()
			if l.sweep != nil {
				file = l.sweep()
			}
			// A COMMAND THAT IS RUNNING IS A WORKER THAT IS ALIVE. This span is
			// the only thing the claim reaper can read about a leaf spending
			// seven minutes inside one `go test`: the record is batched and
			// nothing reaches the store until the batch fills. See
			// exec.Working.
			done := exec.Working(ctx)
			text, isError, err := tool.Execute(ctx, args)
			done()
			if file != nil {
				file()
			}
			if err != nil {
				// Harness-level failure: treat as a tool error with the Go
				// error message as the model-visible text. This mirrors pi's
				// behavior: a thrown Error becomes isError=true with
				// error.message as the toolResult content.
				return toolResult{text: err.Error(), isError: true}
			}
			return toolResult{text: text, isError: isError}
		}
	}
	// Unknown tool: pi does not have this path, but a model could emit one.
	// Return an error so the model sees the failure and can correct.
	return toolResult{
		text:    fmt.Sprintf("Unknown tool: %s", name),
		isError: true,
	}
}

// toolDefinitions builds the ai.ToolDefinition slice from the bare tools,
// carrying the verbatim pi schemas and descriptions on the wire.
func (l *loopState) toolDefinitions() []ai.ToolDefinition {
	defs := make([]ai.ToolDefinition, len(l.tools))
	for i, tool := range l.tools {
		var params map[string]interface{}
		_ = json.Unmarshal(tool.Schema, &params)
		defs[i] = ai.ToolDefinition{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		}
	}
	return defs
}

// completeWithRetry sends one provider request, retrying on retryable errors
// with pi's exact schedule: 2s, 4s, 8s, max 3 retries. On each retry the
// failed assistant message is popped from the transcript (pi spec §4:
// _prepareRetry pops the failing assistant message from agent.state.messages).
func (l *loopState) completeWithRetry(ctx context.Context, defs []ai.ToolDefinition) (*ai.Response, error) {
	var lastErr error
	// cuts counts the attempts the STREAM GUARD ended, apart from the transport
	// attempts: a cut is not evidence that the endpoint is failing, so it must
	// not shorten the patience a real fault gets. rerouted says at least one of
	// them took an endpoint out of the ledger, which is what decides the budget.
	cuts, rerouted := 0, false
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// The same span around the other thing a leaf waits on. The stream
		// guard already cuts a call that has gone silent, so this is not a
		// safety net — it is the honest reading of a long reply that is still
		// arriving, which is a worker at work and not a claim to reap.
		callDone := exec.Working(ctx)
		began := time.Now()
		response, err := l.client.CompleteWithMessages(ctx, l.messages, ai.WithTools(defs))
		callDone()
		// Measured whether it answered or failed: a call that took four minutes
		// to time out is four minutes this leaf must expect to need again, and
		// a reserve that only counted successes would be blind to exactly the
		// machine that is running slow.
		l.measured.noteCall(time.Since(began))
		if err == nil {
			return response, nil
		}
		lastErr = err

		// Context cancelled — stop immediately.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// THE GUARD'S CUT, ANSWERED HERE. The transcript is append-only and the
		// failing response was never appended, so a cut re-enters this loop
		// through the same door a fault does with nothing of the dead attempt
		// left behind.
		if cut, isCut := provider.CutFrom(err); isCut {
			if cut.Rerouted {
				rerouted = true
			}
			if cuts >= cutBudget(cut, rerouted) {
				l.tell(fmt.Errorf("%w — this leaf has no attempts left for it", err))
				return nil, err
			}
			cuts++
			attempt--
			// A cut, on the record. Without it a leaf that spent four minutes
			// asking the same question of an endpoint that had gone quiet reads
			// back as one turn that simply took four minutes, and the endpoint —
			// the thing that was actually wrong — is nowhere in the account.
			l.note("the stream was cut and the turn was asked again: " + err.Error())
			// AND ON THE STREAM SOMEBODY IS WATCHING. The note above is the
			// record; this is the line. What it says is the whole of what a
			// person needs while it is happening — what stalled, and that THE
			// CALL was retried rather than the leaf restarted, because a run
			// whose leaf keeps starting over is the shape this was mistaken for
			// on 2026-08-28.
			l.tell(errors.New(cutWords(cut, rerouted)))
			continue
		}

		// Classify the error.
		errMsg := err.Error()
		if isContextOverflow(errMsg) {
			// Context overflow is NOT retried; it triggers compaction.
			// The caller (run loop) will see this as an error. In pi,
			// overflow triggers auto-compaction and then a retry of the
			// turn. We handle that in maybeCompact's caller path.
			return nil, err
		}
		if !isRetryable(errMsg) {
			return nil, err
		}

		// Retryable: wait and retry. The last attempt's delay is not waited.
		if attempt < maxRetries {
			delay := retryBaseDelay * (1 << attempt) // 2s, 4s, 8s
			l.note("retrying after a provider error, waiting " + delay.String() + ": " + errMsg)
			if waitErr := backoffWait(ctx, delay); waitErr != nil {
				return nil, waitErr
			}
			// Pop the failed assistant message if one was appended. In pi,
			// the failing assistant message is the one that was just added
			// (the error response). Here we have not appended it yet, so
			// there is nothing to pop — the retry re-sends the same
			// messages. But if a previous turn's assistant message is the
			// one that triggered the error (e.g. the model's response was
			// an error message), we pop it. The pi code pops the last
			// assistant message from agent.state.messages (L2160-2163).
			// Our messages slice has the assistant message we appended
			// before the tool results; if the error came from the provider
			// call (not from tool execution), the last message in our
			// slice is the one before the call — there is no assistant
			// message to pop because we have not appended the response.
			// So this is a no-op in our architecture, matching pi's
			// intent: the retry sends the same messages.
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// maybeCompact checks whether the context has grown past the compaction
// threshold and, if so, summarizes the oldest messages. Pi spec §5:
// threshold = contextWindow - reserveTokens; keep ~keepRecentTokens of the
// most recent messages; summarize the rest via SUMMARIZATION_SYSTEM_PROMPT.
//
// This path exists for completeness. On a small bench run (< 30 turns) it
// will not fire, matching the oracle. The compaction itself requires a model
// call to summarize; if the context has not crossed the threshold, nothing
// happens.
func (l *loopState) maybeCompact(ctx context.Context, response *ai.Response, defs []ai.ToolDefinition) {
	if response == nil || response.Usage == nil {
		return
	}
	// contextTokens = usage.totalTokens (or input+output+cacheRead+cacheWrite).
	contextTokens := response.Usage.TotalTokens
	if contextTokens == 0 {
		contextTokens = response.Usage.PromptTokens + response.Usage.CompletionTokens +
			response.Usage.CacheReadTokens()
	}
	threshold := defaultContextWindow - compactionReserveTokens
	if contextTokens <= threshold {
		return
	}

	// Find the cut point: keep ~keepRecentTokens worth of the most recent
	// messages. We estimate tokens as 4 bytes per token (a rough heuristic;
	// pi uses estimateTokens which is a character-based estimate).
	keepBytes := compactionKeepRecentTokens * 4
	cut := len(l.messages)
	for cut > 0 {
		msg := l.messages[cut-1]
		msgBytes := estimateMessageBytes(msg)
		if keepBytes-msgBytes < 0 {
			break
		}
		keepBytes -= msgBytes
		cut--
	}

	// The messages to summarize are everything from index 1 (after the system
	// message) up to the cut point. The system message and the kept tail stay.
	if cut <= 1 {
		return // nothing to summarize
	}

	discarded := l.messages[1:cut]
	summary := l.summarize(ctx, discarded, defs)
	if summary == "" {
		return
	}

	// Rebuild: system, summary (as a user message), kept tail.
	//
	// THE SYSTEM MESSAGE AND THE TAIL ARE READ BEFORE l.messages IS REPLACED,
	// and that ordering is the whole of this block's correctness. The rebuild
	// used to assign the fresh slice first and then reach for l.messages[0] to
	// copy the system message across — but by then l.messages WAS the fresh
	// slice, empty, and the read panicked with "index out of range [0] with
	// length 0". It took a leaf long enough to actually compact before anything
	// ran this line, which is why a crash on the first line of the rebuild
	// survived: the short runs never got here.
	system := l.messages[0]
	kept := append([]ai.Message(nil), l.messages[cut:]...)
	l.messages = make([]ai.Message, 0, 2+len(kept))
	l.messages = append(l.messages, system)
	l.messages = append(l.messages, ai.Message{
		Role:    "user",
		Content: []ai.ContentPart{{Type: "text", Text: summary}},
	})
	l.messages = append(l.messages, kept...)
	// Compaction is the single largest thing that can happen to a leaf without
	// the leaf saying anything, and it is the reason a long run's later turns
	// stop referring to what its early turns found. A reader who cannot see it
	// happen reads amnesia as incompetence.
	l.note("compacted the context: " + strconv.Itoa(len(discarded)) +
		" messages summarised into one, " + strconv.Itoa(len(kept)) + " kept")
}

// summarize asks the model to produce a structured summary of the discarded
// messages, using pi's verbatim SUMMARIZATION_SYSTEM_PROMPT. This is a
// separate model call; on a small run it never fires.
func (l *loopState) summarize(ctx context.Context, discarded []ai.Message, defs []ai.ToolDefinition) string {
	summarizeMessages := []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: summarizationSystemPrompt}}},
	}
	// Flatten the discarded messages into a single user message.
	var sb strings.Builder
	for _, m := range discarded {
		sb.WriteString(fmt.Sprintf("[role: %s]\n", m.Role))
		for _, p := range m.Content {
			if p.Type == "text" && p.Text != "" {
				sb.WriteString(p.Text)
				sb.WriteString("\n")
			}
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				sb.WriteString(fmt.Sprintf("[tool call: %s(%s)]\n", tc.Function.Name, tc.Function.Arguments))
			}
		}
		sb.WriteString("\n")
	}
	summarizeMessages = append(summarizeMessages, ai.Message{
		Role:    "user",
		Content: []ai.ContentPart{{Type: "text", Text: sb.String()}},
	})

	// No tools for the summarization call.
	response, err := l.client.CompleteWithMessages(ctx, summarizeMessages)
	if err != nil || response == nil {
		return ""
	}
	return strings.TrimSpace(response.Text())
}

// ── helpers ──────────────────────────────────────────────────────────────────

// assistantContent builds the content parts for an assistant message from the
// response. The ai.Response carries text content in Choices[0].Message.Content;
// we pass it through. (Thinking/reasoning is not part of the OpenAI wire format
// the provider sends — it arrives as a content type in the response, and we
// preserve whatever the response carries.)
func assistantContent(response *ai.Response) []ai.ContentPart {
	if len(response.Choices) == 0 {
		return []ai.ContentPart{{Type: "text", Text: ""}}
	}
	content := response.Choices[0].Message.Content
	if len(content) == 0 {
		return []ai.ContentPart{{Type: "text", Text: ""}}
	}
	return content
}

// lastAssistantText extracts the text from the last assistant message in the
// transcript, used as the outcome's Text when the loop ends on an error.
func (l *loopState) lastAssistantText() string {
	for i := len(l.messages) - 1; i >= 0; i-- {
		if l.messages[i].Role == "assistant" {
			var sb strings.Builder
			for _, p := range l.messages[i].Content {
				if p.Type == "text" {
					sb.WriteString(p.Text)
				}
			}
			return sb.String()
		}
	}
	return ""
}

// addUsage accumulates token usage from one response into the outcome, mirroring
// internal/exec/linear.go's addUsage. It parses OpenRouter usage including
// cache_read_input_tokens / cache_creation_input_tokens.
func addUsage(usage *exec.Usage, response *ai.Response) {
	usage.Calls++
	if response == nil || response.Usage == nil {
		return
	}
	usage.PromptTokens += response.Usage.PromptTokens
	usage.CompletionTokens += response.Usage.CompletionTokens
	usage.CachedTokens += response.Usage.CacheReadTokens()
	if response.Usage.Cost != nil {
		usage.Cost += *response.Usage.Cost
	}
}

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
// error. These are NOT retried — pi compacts instead.
func isContextOverflow(errMsg string) bool {
	return contextOverflowPattern.MatchString(errMsg)
}

// backoffWait sleeps for the delay, returning early on context cancellation.
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

// copyMessages makes a shallow copy of the messages slice for the request log.
func copyMessages(msgs []ai.Message) []ai.Message {
	cp := make([]ai.Message, len(msgs))
	copy(cp, msgs)
	return cp
}

// estimateMessageBytes gives a rough byte size of a message for the compaction
// cut-point heuristic.
func estimateMessageBytes(msg ai.Message) int {
	total := 0
	for _, p := range msg.Content {
		total += len(p.Text)
	}
	for _, tc := range msg.ToolCalls {
		total += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return total
}

// recordCall appends one executed call to the outcome's bounded tail,
// mirroring exec.Outcome.record (which is unexported). The Ran tail keeps the
// last 40 calls with arguments clipped to 200 bytes — the same bounds the
// exec package uses — so a reader of a finished job can see what was run.
func recordCall(outcome *exec.Outcome, call ai.ToolCall, failed bool) {
	const ranLimit = 40
	const ranArgumentBytes = 200
	line := strings.TrimSpace(call.Function.Name + " " + snip(strings.TrimSpace(call.Function.Arguments), ranArgumentBytes))
	if failed {
		line += "  → error"
	}
	outcome.Ran = append(outcome.Ran, line)
	if len(outcome.Ran) > ranLimit {
		outcome.Ran = outcome.Ran[len(outcome.Ran)-ranLimit:]
	}
}

// snip clips a string to at most n bytes, appending an ellipsis when it cut.
func snip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// cutBudget is how many times a cut of this kind is worth asking again, given
// whether anything was routed away from the endpoint that failed. It is
// internal/session's rule, spelled the same way for the reason the constants
// above are: this package cannot import that one.
//
// Degeneration is the one reason the endpoint question says nothing about: soup
// is a claim about the transcript and the weights reading it, never about which
// endpoint delivered it.
func cutBudget(cut *provider.StreamCut, rerouted bool) int {
	if cut.Reason == provider.CutBabble {
		return babbleRetries
	}
	if !rerouted {
		return blindRetries
	}
	return silentRetries
}

// cutWords is the one line a person watching a headless run is given when the
// guard cuts a call.
//
// It says three things and no more: WHAT went wrong, WHO it was routed away
// from when the ledger could act, and — the part the reader most needs — that
// what was retried is THE CALL. A run whose leaves restart reads, from outside,
// exactly like a run whose calls retry, and the difference between them is
// whether the work already paid for still exists. Naming it here is what stops
// the two being confused again.
//
// The provider's name is included only when the wire actually said it: a cut
// stream that never named its endpoint has nothing to name, and inventing one
// would be the phrase-matching mistake FAILSAFE.md's first rule is about.
func cutWords(cut *provider.StreamCut, rerouted bool) string {
	words := cut.Error() + " → the call was retried"
	if rerouted && strings.TrimSpace(cut.Provider) != "" {
		words += ", routed away from " + strings.TrimSpace(cut.Provider)
	}
	return words
}
