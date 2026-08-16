package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// defaultContextWindow is the window assumed when Config.ContextWindow is
// zero. 128k is the smallest window among the models this surface routes to,
// so assuming it compacts a little early on a larger model and never
// overflows a smaller one — the safe direction for a guess to be wrong in.
const defaultContextWindow = 128_000

// providerTimeout bounds one non-streaming completion. Only the compaction
// summary takes that path (every turn request streams, and the adapter's
// stream client carries no total deadline by design), so this is a backstop
// against a wedged summarizer rather than a limit on how long a turn may run:
// the turn's authority is its context, which Interrupt cancels.
const providerTimeout = 10 * time.Minute

// closeGrace is how long Close waits for a cancelled turn to land its last
// messages. Two seconds is generous for the journal appends a cancelled turn
// still owes and short enough that a quit still feels like one.
const closeGrace = 2 * time.Second

// jobShutdownGrace is how long Close gives every running background job,
// together, to answer a SIGTERM before it is killed. It matches closeGrace for
// the same reason: a quit that takes four seconds already feels like a hang,
// and a dev server that has not shut down in two is not going to.
const jobShutdownGrace = 2 * time.Second

// New builds one session agent against a live provider. It performs no
// network request: the client is constructed, the prompt rendered, and the
// session file — if configured and present — replayed into the transcript.
func New(config Config) (*Agent, error) {
	client, err := provider.NewClient(provider.Config{
		APIKey:  config.APIKey,
		BaseURL: config.BaseURL,
		Model:   config.Model,
		Timeout: providerTimeout,
		// The routing row, already resolved. It is handed down as a source
		// rather than as a path so that nothing under here ever reads a settings
		// file to decide how a request is routed.
		Routing: provider.StaticRouting(config.Routing),
		// The catalog gate on optional knobs, and the two answers to "what
		// else could serve this?" when no endpoint will take the request at all
		// (internal/provider's endpoints.go). All three are seams the surface
		// resolves; a caller that hands over none of them keeps today's
		// behaviour exactly — knobs travel only when explicit, and a refusal
		// ends in the diagnosis rather than on another model.
		SupportsParameter: config.SupportsParameter,
		Fallbacks:         config.ModelFallbacks,
		NearestModels:     config.NearestModels,
	})
	if err != nil {
		return nil, err
	}
	return newAgent(config, client)
}

// newAgent is the seam New and the tests share: everything except which
// Completer the turns run against.
func newAgent(config Config, client Completer) (*Agent, error) {
	if strings.TrimSpace(config.Workspace) == "" {
		return nil, errors.New("session: workspace is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("session: model is required")
	}
	if client == nil {
		return nil, errors.New("session: completer is required")
	}
	system := config.System
	if strings.TrimSpace(system) == "" {
		system = renderSystem(config.Workspace)
	}
	agent := &Agent{
		config: config,
		system: system,
		model:  config.Model,
		// A memory-only session still has ONE lineage; it just has no name on
		// disk to derive it from. The file-backed case overwrites this below
		// with the header's id, which survives every resume.
		cacheKey: sessionCacheKey(newSessionID()),
	}
	// Memory is built before the belt for the same reason the registry is: the
	// belt carries note and forget only when there is a file to write, so the
	// store has to exist before the tools are assembled (memory.go).
	if strings.TrimSpace(config.MemoryFile) != "" {
		agent.memory = newMemoryStore(config.MemoryFile)
	}
	// The registry is built before the belt because the belt closes over it:
	// bash's background path and the jobs tool are both views onto this one
	// object, and it is the agent's own steering queue they report into.
	agent.jobs = newJobRegistry(config.Workspace, agent.enqueueSteering)
	agent.tools = agent.belt()
	definitions, err := toolDefinitions(agent.tools)
	if err != nil {
		return nil, err
	}
	agent.definitions = definitions
	agent.messages = []ai.Message{textMessage("system", system)}
	agent.refreshSystemLocked()

	if strings.TrimSpace(config.SessionFile) != "" {
		file, restored, err := openSessionFile(config.SessionFile, config.Workspace, config.Model)
		if err != nil {
			return nil, err
		}
		agent.file = file
		// A resumed session keeps the name it was given: the title is a fact
		// about the conversation in the file, and re-deriving it from the same
		// opening exchange would pay for an answer we already have.
		agent.title = file.Title()
		// And it keeps its cache lineage for the same reason, which matters
		// more: a session resumed tomorrow re-sends the transcript it built
		// today, and a key that changed with the process would ask the router
		// for a fresh replica and pay to write that whole prefix again.
		if id := file.ID(); id != "" {
			agent.cacheKey = sessionCacheKey(id)
		}
		// The system message is rendered fresh rather than replayed: the date
		// and AGENTS.md in the footer are facts about now, not about the
		// session that wrote the file.
		agent.messages = append(agent.messages, restored...)
	}
	// The client is wrapped LAST, once the lineage is known: the wrapper is the
	// one place every request this agent makes passes through, so it is where
	// /compact's extra instruction is spliced in (see [Agent.CompactWithFocus])
	// and where the prompt-cache key is stamped. Wrapping earlier would have to
	// read the key through the agent, which is a pointer cycle to save a line.
	agent.client = sessionCompleter{inner: client, cacheKey: agent.cacheKey}
	// AND THE WORK IS RECOVERED LAST, once this agent can actually run one. A
	// resumed journal may have a task graph beside it — nodes that landed, a node
	// that was still running when the process died, nodes waiting on them — and
	// recovery is load, reconcile with the disk, continue the frontier
	// (task_store.go). A fresh session has no checkpoint and this is a stat.
	agent.recoverTasks()
	return agent, nil
}

// sessionCacheKey names one session's prompt-cache lineage. It is derived
// through [provider.RunCacheKey] so the key is the same shape every other
// aforge lineage uses — an "aforge-" prefix an operator can recognize in a
// router's logs — and so nothing about the session's own id reaches the wire.
func sessionCacheKey(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return provider.RunCacheKey("session/"+id, "")
}

// Usage returns the session's accumulated usage.
func (a *Agent) Usage() Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage
}

// ContextTokens is what the conversation now weighs, in tokens: the same figure
// the compaction threshold is checked against, under the same lock.
//
// It is THE honest number, and honest means two different things depending on
// what has happened. When a response has come back, it is the provider's own
// count of the request it just served — the system prompt, the tool schemas,
// every tool result, the arguments of every call, all the bytes a surface
// counting words cannot see. When the transcript has grown since (a 300KB file
// read that has not been sent yet), the content estimate is larger and wins. See
// [Agent.estimateTokensLocked] for why it is the max of the two.
//
// Zero is a session that has neither sent nor recorded anything, which is the
// only case where "nothing" is true.
func (a *Agent) ContextTokens() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.estimateTokensLocked()
}

// Model returns the model the next request will use.
func (a *Agent) Model() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// SetModel swaps the model for subsequent turns. A turn in flight finishes on
// the model it started on: runTurn latches the model once at the start and
// every step and retry of that turn rides the latched value, so a swap made
// while the agent is working lands at the next Submit.
func (a *Agent) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	a.mu.Lock()
	a.model = model
	a.mu.Unlock()
}

// ── reasoning strength ──────────────────────────────────────────────────────
//
// How hard the model is asked to think is a CHOICE ABOUT A MODEL, not about a
// session, so it is kept per model id and not as one field. A person who dials
// a reasoning model to high, switches to a cheap one for a quick question and
// switches back finds the high still there: the second model never had a level,
// and setting one on it would have been a decision nobody made.
//
// The levels are provider.Effort's, minus one. "off" here means SEND NOTHING —
// no reasoning field on the wire, the model's own default — and NOT
// provider.EffortOff, which sends {"reasoning":{"enabled":false}} to suppress
// the thinking pass outright. The distinction matters at exactly this seam: a
// person turning a knob back to off is saying "stop asking for extra thinking",
// which is the model's default, while EffortOff is a harness economy that some
// endpoints refuse with a 400 (provider's quirks.go). The harness may spend a
// round-trip discovering that; a person changing their mind must not.

// Reasoning is the level the model now in use will be asked for, "" when none
// is set.
func (a *Agent) Reasoning() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return string(a.reasoningLocked(a.model))
}

// ReasoningFor is the level held for one model id, whichever model is in use.
// It is what a picker asks while drawing a row for a model nobody has switched
// to yet.
func (a *Agent) ReasoningFor(model string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return string(a.reasoningLocked(model))
}

// SetReasoning sets the level for the model now in use, for subsequent turns.
// A turn in flight finishes on the level it started with, exactly as it
// finishes on the model it started on: runTurn latches both once (loop.go), so
// a change made while the agent is working lands at the next Submit.
//
// An unrecognized level is ignored rather than cleared. The two callers are a
// picker that can only produce the four it draws and a flag the door has
// already validated with [ParseReasoning]; between them, a value this does not
// know is a bug upstream, and answering it by silently dropping a level the
// person did set would hide it.
func (a *Agent) SetReasoning(level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setReasoningLocked(a.model, level)
}

// SetReasoningFor sets the level for one model id without switching to it.
func (a *Agent) SetReasoningFor(model, level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setReasoningLocked(model, level)
}

func (a *Agent) setReasoningLocked(model, level string) {
	key := reasoningKey(model)
	if key == "" {
		return
	}
	effort, ok := parseReasoning(level)
	if !ok {
		return
	}
	// None is absence and is stored as absence: a map that held EffortNone
	// entries would answer "this model has a level" for every model anybody
	// ever cycled back to off.
	if effort == provider.EffortNone {
		delete(a.reasoning, key)
		return
	}
	if a.reasoning == nil {
		a.reasoning = make(map[string]provider.Effort, 2)
	}
	a.reasoning[key] = effort
}

func (a *Agent) reasoningLocked(model string) provider.Effort {
	key := reasoningKey(model)
	if key == "" {
		return provider.EffortNone
	}
	return a.reasoning[key]
}

// reasoningKey folds a model id the way every other lookup on this surface
// does — trimmed and case-insensitive — so a level set from a picker row is
// found again by a /model <slug> typed in another case.
func reasoningKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// ParseReasoning normalizes one operator-supplied level and reports whether it
// is one. It answers in the surface's own words rather than provider.Effort's
// so a door can validate `--reasoning` without importing the adapter, and "off"
// and "" both normalize to "" — see the block comment above for why off is
// silence and not provider.EffortOff.
func ParseReasoning(level string) (string, bool) {
	effort, ok := parseReasoning(level)
	return string(effort), ok
}

func parseReasoning(level string) (provider.Effort, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "off":
		return provider.EffortNone, true
	case string(provider.EffortLow):
		return provider.EffortLow, true
	case string(provider.EffortMedium):
		return provider.EffortMedium, true
	case string(provider.EffortHigh):
		return provider.EffortHigh, true
	default:
		return provider.EffortNone, false
	}
}

// SetContextWindow tells the agent how many tokens the model it is now running
// actually accepts, and the compaction threshold follows it from the next check
// onward.
//
// It exists because [Agent.SetModel] does. Config.ContextWindow is the window of
// the model the session STARTED on; a person who switches to a 1M-token model
// mid-conversation would otherwise keep compacting at the old model's threshold
// — reading 128k of a window eight times that size — and one who switches the
// other way would overflow. A surface that knows the new model's window (the
// catalog's figure) sets it here alongside SetModel; one that does not, does
// not call this, and the configured window stands.
//
// Zero and negative are ignored rather than clearing the window: "I don't know
// this model's size" must not be spelled the same way as "this model has no
// context", and a caller passing an unknown figure through means the first.
func (a *Agent) SetContextWindow(tokens int) {
	if tokens <= 0 {
		return
	}
	a.contextWindow.Store(int64(tokens))
}

// Submit appends text as a user message and runs one turn: provider requests
// interleaved with tool execution until the assistant answers without a tool
// call. The returned channel streams the turn's events and is closed after
// EventTurnDone or EventError. Submitting while a turn runs injects the
// message into that turn (steering) rather than starting a second one.
//
// EVERY call returns a live channel, steering included. The in-flight turn
// owns an event hub, and a steering Submit subscribes a fresh channel to it:
// the caller sees the turn's events from its own subscribe point onward and
// the close at EventTurnDone. There is no replay — a surface that submits per
// message is watching the turn from where it spoke, not re-reading it — and a
// surface holding both channels sees each event on each, which is what a
// per-message caller wants and what a whole-session reader must de-duplicate.
func (a *Agent) Submit(ctx context.Context, text string) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errors.New("session: agent is closed")
	}
	if a.running {
		// A steering message means the person is here, so the idle pass stands
		// down (memory_consolidate.go). Nothing can be armed while a turn is
		// running — the arming happens at a turn's end — so this is a no-op
		// today; it is written because the law is "anything the person says
		// disarms it", not "a turn start disarms it".
		a.disarmIdle()
		// Steering. The message is queued rather than appended here because
		// the transcript's tail is mid-tool-batch: a user message spliced
		// between an assistant's tool_calls and their results is a shape every
		// provider rejects. The loop appends it at the next step boundary,
		// where it is journaled like any other user message.
		a.steering = append(a.steering, userText(text))
		// Subscribing under a.mu — not after releasing it — is what makes the
		// returned channel live rather than a coin flip: the turn's goroutine
		// clears running under this same lock BEFORE it closes the hub, so
		// running == true here means the hub cannot already be closed.
		events := a.hub.subscribe()
		a.mu.Unlock()
		return events, nil
	}
	// The spend rail is checked here, before anything is recorded: a refused
	// turn must do NO work, so the person's text is not journaled either — the
	// message is theirs to send again once the rail moves (rail.go).
	if err := a.railBlockLocked(); err != nil {
		a.mu.Unlock()
		return refusedStream(err), nil
	}
	events := a.startTurnLocked(ctx, userText(text), nil)
	a.mu.Unlock()
	return events, nil
}

// userMessage is a person's message on its way into the transcript: the message
// the model reads, and the durable references the JOURNAL writes in place of
// the parts it must not hold.
//
// The two travel together because they are written together — recordUserLocked
// appends the message and journals the line under one lock — and because the
// live part cannot be recovered from later. A data URL is bytes with no
// provenance: nothing in it says which file it came from or whether that file
// still holds the same picture, which is exactly what the journal has to write
// (see [journalPart]). Carrying the references beside the message is what keeps
// a queued image message — steering, a follow-up — journalable at the moment it
// finally lands, minutes after it was assembled.
//
// Everything the person types is one of these. A text-only message has no
// references and journals exactly as it always did.
type userMessage struct {
	message ai.Message
	refs    []journalPart
}

// userText is the ordinary case: a message that is only words.
func userText(text string) userMessage {
	return userMessage{message: textMessage("user", text)}
}

// text is the message's words — what a queued message says, with its parts left
// out. It is what a reader of the queue wants: the pictures are not a line of
// the conversation, and a data URL rendered into one would be unreadable.
func (u userMessage) text() string { return messageContentText(u.message) }

// startTurnLocked begins one turn on a transcript the caller has already
// checked, with a.mu held. It is the ONE place a turn starts: Submit reaches it
// with the person's message, and a drained follow-up reaches it with a message
// that was typed while the last turn was still running.
//
// watcher is a stream built before the turn existed — a queued follow-up's —
// and is adopted onto the new hub before the loop can emit anything. Nil means
// the caller takes its own subscription, and it is the returned channel.
func (a *Agent) startTurnLocked(ctx context.Context, user userMessage, watcher *eventStream) <-chan Event {
	a.running = true
	// A turn is the person being back, so the idle consolidation timer stands
	// down before anything else happens (memory_consolidate.go). It is disarmed
	// BEFORE the memory block is re-read below, so a turn cannot open on a file
	// a pass is about to be armed against.
	a.disarmIdle()
	// The memory block is re-read here, at the start of every turn, so a note
	// written by the last turn is in front of the model for this one and a
	// person who edited the file by hand is obeyed without a restart
	// (memory.go). It is one 4KiB read per turn, not per step.
	a.refreshSystemLocked()
	hub := newEventHub()
	a.hub = hub
	turnCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	// done is how Close waits for this turn: closed under a.mu by the cleanup
	// below, after the turn's last message is journaled.
	done := make(chan struct{})
	a.done = done
	a.recordUserLocked(user)
	var events <-chan Event
	if watcher != nil {
		hub.adopt(watcher)
		events = watcher.out
	} else {
		events = hub.subscribe()
	}

	go func() {
		// completed is the turn's outcome: true only when the model answered
		// without a tool call and nobody interrupted. It is what decides
		// whether a queued follow-up may start (see [Agent.FollowUp]).
		completed := false
		defer cancel()
		defer hub.close()
		defer func() {
			a.mu.Lock()
			// Whatever is still queued was typed at this turn and belongs to
			// the transcript. Draining it here — under the same lock that
			// clears running, so no Submit can slip between — is what keeps a
			// leftover from landing AFTER the next Submit's message, answering
			// a question the person asked before the one they just typed.
			a.drainSteeringLocked()
			a.running = false
			a.cancel = nil
			a.hub = nil
			a.done = nil
			close(done)
			// The follow-up drain happens under the SAME lock that cleared
			// running, for the reason the steering drain does: between the two
			// there must be no window in which a Submit could start a turn and
			// have the follow-up land behind it, out of the order the person
			// typed them in.
			if next, ok := a.nextFollowUpLocked(completed); ok {
				// context.Background rather than the finished turn's: the
				// Submit that would have carried a context never happened, and
				// a follow-up inheriting a cancelled one would end before it
				// started. Interrupt and Close still reach it — both go through
				// a.cancel, which this call replaces.
				a.startTurnLocked(context.Background(), next.message, next.stream)
			} else {
				// THE TURN HAS SETTLED, and nothing is queued behind it: this is
				// the one moment a session is idle. Arm the consolidation
				// countdown (memory_consolidate.go). Under the same lock as the
				// drain, so a Submit cannot slip between the two and find a
				// timer armed against a session that is working again — and in
				// the else branch, because a follow-up starting is a session
				// that was never idle at all.
				a.armIdleLocked()
			}
			a.mu.Unlock()
		}()
		// A faulted turn must end its streams with a reason rather than take
		// the process down: the person is holding a live channel.
		defer func() {
			if recovered := recover(); recovered != nil {
				completed = false
				hub.send(Event{Kind: EventError, Err: guard.Note("session/turn", recovered)})
			}
		}()
		completed = a.runTurn(turnCtx, hub)
	}()
	return events
}

// FollowUp queues a message to be asked AFTER the current turn ends, and hands
// back the channel that turn will stream on.
//
// It is the second queue beside steering, and the difference between them is
// what each drain does. Steering lands INSIDE the running turn at its next step
// boundary: the model reads it as one more thing the person said mid-work.
// A follow-up waits for the work to finish and then starts a turn of its own —
// the same machinery, the same events, a normal new turn as far as any surface
// is concerned. That is the right shape for "and after that, do this", which as
// steering would arrive as an interruption of the thing it is meant to follow.
//
// ONE AT A TIME is the whole steering mode here: a turn's end dequeues exactly
// one message, and the rest wait for the end of the turn that one starts. A
// drain that started several turns at once, or spliced the whole queue into one
// message, would be a decision the person did not make.
//
// A follow-up queued while nothing is running starts immediately: there is no
// turn end coming to drain it, and a message that sat in a queue until the
// person happened to ask something else would be a silent hold.
func (a *Agent) FollowUp(text string) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("session: agent is closed")
	}
	stream := newEventStream()
	if a.running {
		a.followups = append(a.followups, followUp{message: userText(text), stream: stream})
		return stream.out, nil
	}
	if err := a.railBlockLocked(); err != nil {
		return refuseOn(stream, err), nil
	}
	return a.startTurnLocked(context.Background(), userText(text), stream), nil
}

// followUp is one queued message and the stream its turn will speak on. The
// stream exists from the moment the message is queued so the caller has
// something to hold while it waits.
type followUp struct {
	message userMessage
	stream  *eventStream
}

// nextFollowUpLocked takes the next queued message, if a turn may start for it.
//
// completed is the finished turn's outcome, and this is where the drain law
// lives: A DRAIN MUST NEVER RESURRECT A STOPPED TURN. A turn that was
// interrupted or that died on an error is a turn the person stopped or that
// stopped itself, and starting the next queued message off the back of it would
// take a session that was just halted and set it working again. Interrupt
// clears the queue outright for the same reason; this is the second lock on the
// same door, because a turn can also end badly without anybody interrupting it.
//
// The spend rail is checked here too: a follow-up is a turn, and a turn that
// starts must be one the session can pay for.
func (a *Agent) nextFollowUpLocked(completed bool) (followUp, bool) {
	if len(a.followups) == 0 {
		return followUp{}, false
	}
	if !completed || a.closed {
		a.dropFollowUpsLocked()
		return followUp{}, false
	}
	if err := a.railBlockLocked(); err != nil {
		for _, queued := range a.followups {
			refuseOn(queued.stream, err)
		}
		a.followups = nil
		return followUp{}, false
	}
	next := a.followups[0]
	a.followups = a.followups[1:]
	return next, true
}

// dropFollowUpsLocked forgets every queued follow-up and ends its stream. The
// channel closing with no events is what a caller reads as "this never ran" —
// the alternative, leaving it open, is a surface waiting on a turn that will
// never come.
func (a *Agent) dropFollowUpsLocked() {
	queued := a.followups
	a.followups = nil
	for _, item := range queued {
		item.stream.close()
	}
}

// Interrupt cancels the in-flight turn, if any. The partial reply is kept in
// the transcript; the turn's stream ends with EventTurnDone. A tool call
// blocked on a consent request (consent.go) is released by the same
// cancellation and refuses, so the turn ends rather than waiting on a question
// nobody is going to answer now.
//
// It empties BOTH queues, and they empty differently because their drains do
// different things. The follow-up queue is DROPPED here: draining it would
// start a new turn, and a stop that was followed by the session working again
// is not a stop. The steering queue keeps its own law — the turn's end drains
// it into the transcript (the person typed it, so it is part of the record) —
// and that drain starts nothing, so it cannot resurrect anything. Both queues
// are empty once the interrupted turn has finished.
func (a *Agent) Interrupt() {
	a.mu.Lock()
	cancel := a.cancel
	a.dropFollowUpsLocked()
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Title is the session's name, empty until it has one (title.go).
func (a *Agent) Title() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

// Compact runs a compaction pass now (the surface's /compact). It is a no-op
// when the transcript is smaller than the keep-recent floor.
func (a *Agent) Compact(ctx context.Context) error {
	return a.CompactWithFocus(ctx, "")
}

// CompactWithFocus is Compact with one extra instruction for the summarizer:
// what this person wants the summary to be careful about, in their words. It is
// the surface's `/compact keep the API decisions and the failing test` — the
// summary is lossy by construction, and this is where somebody who knows what
// matters says which part must survive.
//
// The focus is appended to the summarization prompt as a final line rather than
// replacing any of it: the section contract (loop.go) is what makes a summary
// resumable, and a focus that could overwrite it would let one careless phrase
// cost the next session its file paths. Empty focus is exactly Compact.
func (a *Agent) CompactWithFocus(ctx context.Context, focus string) error {
	_, err := a.compact(withCompactFocus(ctx, focus), nil)
	return err
}

// compactFocusKey names the focus on the context. It travels on the context
// rather than on the Agent because it belongs to ONE call: a field would have to
// be set before the pass and cleared after it, and a second pass entering that
// window — the automatic one, which nobody focused — would summarize under an
// instruction the person gave to a different pass.
type compactFocusKey struct{}

func withCompactFocus(ctx context.Context, focus string) context.Context {
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return ctx
	}
	return context.WithValue(ctx, compactFocusKey{}, focus)
}

// sessionCompleter is the Completer every request goes through, and it does two
// things to them.
//
// ── THE FOCUS ──
//
// It changes exactly one request: the summarization call made under a context
// carrying a focus, whose system prompt gains a final "Additional focus:" line.
// It is a wrapper rather than an argument threaded down to the summarizer
// because the compaction pass is a closed piece of machinery — threshold, cut,
// summary, rebuild — and the focus is one sentence of prompt, not a change to
// how any of that works. A turn's request never carries the key, so a turn is
// byte-for-byte what it was.
//
// ── THE CACHE LINEAGE, AND WHY IT DEVIATES FROM bare ──
//
// It stamps the session's prompt-cache key on every request. internal/exec/bare
// sends NO key at all, and that is right for what bare is: a leaf is one task,
// run once, whose prefix nothing will ever ask for again — a key there buys a
// replica pin and pays for a cache write nobody reads.
//
// A SESSION IS A LINEAGE, and the arithmetic inverts. Its transcript is re-sent
// whole on every step of every turn, grows all day, and is picked up again
// tomorrow by [Agent] resuming the same file. Without a key each request is free
// to land on whichever replica the router likes, so a warm prefix is a
// coincidence; with one, every step of a days-long conversation asks for the
// same instance and the growing head stays hot. The key is derived from the
// SESSION ID rather than from the model, the task, or the process, because that
// is the identity that survives a /model swap, a restart, and a resume — the
// three things that would otherwise split one conversation into three lineages.
//
// The key is set once, at construction, so nothing at request time can split it.
// An empty key is left off entirely rather than sent blank (provider.WithCacheKey
// ignores it), which keeps a session with no identity unkeyed instead of sharing
// one lineage with every other unkeyed session.
type sessionCompleter struct {
	inner    Completer
	cacheKey string
}

func (f sessionCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if focus, ok := ctx.Value(compactFocusKey{}).(string); ok && focus != "" {
		messages = withFocusAppended(messages, focus)
	}
	ctx = provider.WithCacheKey(ctx, f.cacheKey)
	return f.inner.CompleteWithMessages(ctx, messages, options...)
}

// withFocusAppended returns a copy of the request whose system message ends with
// the focus line. The copy is deep enough to matter: the messages are the
// agent's own, shared with the transcript, and appending in place would edit the
// session's system prompt from inside one request.
func withFocusAppended(messages []ai.Message, focus string) []ai.Message {
	for index, message := range messages {
		if message.Role != "system" {
			continue
		}
		copied := make([]ai.Message, len(messages))
		copy(copied, messages)
		copied[index] = textMessage("system", messageContentText(message)+"\n\nAdditional focus: "+focus)
		return copied
	}
	return messages
}

// Close ends the session: an in-flight turn is cancelled and waited for,
// background jobs are terminated, then the session file is flushed and closed.
//
// The three steps are sequential, not concurrent. The turn's wait is first
// because a running turn is what still owes the journal messages; the jobs come
// after because a turn cancelled mid-tool-call may still be the thing that
// started the job being killed; the file closes last because both of the above
// can still write to it. Nothing here races the existing grace — the job round
// EXTENDS it, adding at most one more jobShutdownGrace to a quit.
//
// The wait is the point. A turn cancelled at Close still has messages to
// journal — the partial reply it kept, the steering it drained — and closing
// the file first would drop exactly the tail a resume needs. The wait is
// bounded because Close is what the person's quit reaches: a turn wedged
// inside a tool must not hold the process open, and past the grace the journal
// simply stops accepting writes rather than writing to a closed descriptor.
func (a *Agent) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	// Nothing dreams after the lights go out: a timer that fired after Close
	// would consolidate against a journal that is already shut, and the
	// process is leaving anyway (memory_consolidate.go).
	a.disarmIdle()
	file := a.file
	cancel := a.cancel
	done := a.done
	// Nothing queued will ever run now, and a caller holding one of those
	// channels is owed the close rather than a wait that never ends.
	a.dropFollowUpsLocked()
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		timer := time.NewTimer(closeGrace)
		select {
		case <-done:
		case <-timer.C:
		}
		timer.Stop()
	}

	// A background job's lifetime is the session's: cancelling the turn above
	// says nothing to it (its process was deliberately never bound to a turn's
	// context — jobs.go), so quitting the session is where it ends. Every kill
	// here is a requested one, so no job reports its own death onto a steering
	// queue nobody will drain.
	if a.jobs != nil {
		a.jobs.shutdown(jobShutdownGrace)
	}

	if file == nil {
		return nil
	}
	return file.Close()
}

// ── transcript ──────────────────────────────────────────────────────────────

// recordLocked appends one COMPLETED message to the transcript and journals
// it. The journal write happens under the same lock as the append so the file
// order is the transcript order by construction; it is one buffered append to
// an already-open file, not a place a turn waits.
func (a *Agent) recordLocked(message ai.Message) {
	a.messages = append(a.messages, message)
	if a.file != nil {
		a.file.appendMessage(message)
	}
}

// recordUserLocked is recordLocked for a message the PERSON sent: the same
// append, and a journal line that carries the message's durable references
// beside its text.
//
// The split exists because only a person's message can hold a part the journal
// must not write. Everything the model and the tools produce is text and goes
// through recordLocked exactly as before.
func (a *Agent) recordUserLocked(user userMessage) {
	a.messages = append(a.messages, user.message)
	if a.file != nil {
		a.file.appendMessage(user.message, user.refs...)
	}
}

func (a *Agent) record(message ai.Message) {
	a.mu.Lock()
	a.recordLocked(message)
	a.mu.Unlock()
}

// snapshot copies the messages slice for one provider request. The copy is
// shallow and the elements are immutable once recorded, so this costs one
// slice header per step and buys a request that cannot be mutated underneath
// the client by a concurrent compaction. The model is not read here: it is the
// turn's, latched once by runTurn.
func (a *Agent) snapshot() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	messages := make([]ai.Message, len(a.messages))
	copy(messages, a.messages)
	return messages
}

// drainSteering moves queued steering messages into the transcript at a step
// boundary and reports how many landed.
func (a *Agent) drainSteering() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.drainSteeringLocked()
}

// drainSteeringLocked is the drain itself, for callers already holding a.mu —
// the turn's end, which must drain and clear running without a gap.
func (a *Agent) drainSteeringLocked() int {
	queued := a.steering
	a.steering = nil
	for _, message := range queued {
		a.recordUserLocked(message)
	}
	return len(queued)
}

// enqueueSteering puts one line the SESSION authored — a background job's
// completion note (jobs.go) — onto the same queue the person's steering rides.
//
// The lane is shared on purpose. Both are news that arrives while the model is
// busy, both must land at a step boundary rather than inside a tool batch, and
// both belong in the transcript as plain user-role text. Giving completions
// their own channel would mean a second drain, a second ordering rule, and a
// second way for a message to arrive at a moment the provider rejects — for a
// message that is, from the model's side, exactly a line somebody typed.
//
// A closed agent drops the note rather than queueing it: after Close nothing
// drains, and the journal it would be written to is already shut.
func (a *Agent) enqueueSteering(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	// News arriving is the session being in use, even when the news is the
	// session's own: the turn that drains this queue will read the memory file,
	// and a consolidation pass firing into that moment would swap it underneath
	// a turn that is about to start (memory_consolidate.go).
	a.disarmIdle()
	a.steering = append(a.steering, userText(text))
}

func textMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}

// ── event hub ───────────────────────────────────────────────────────────────

// eventHub is one turn's fan-out: every Submit that lands on the turn — the
// one that started it and any steering Submit after — gets its own eventStream,
// and the turn's events go to all of them.
//
// Subscribers are independent queues rather than one queue with many readers
// because the events are a narrative, not work: a slow surface must fall
// behind on its own channel, not steal deltas from the surface beside it. A
// late subscriber starts empty — the events before it spoke belong to a stream
// somebody was already reading, and replaying them would make the second
// caller re-draw the first caller's turn.
type eventHub struct {
	mu          sync.Mutex
	subscribers []*eventStream
	closed      bool
}

func newEventHub() *eventHub { return &eventHub{} }

// subscribe returns a fresh channel carrying the turn's events from now on. A
// subscription to a finished hub is an already-closed channel: the turn whose
// events it would carry is over, and a channel that never closes would hang
// the caller instead of telling it so.
func (h *eventHub) subscribe() <-chan Event {
	stream := newEventStream()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		stream.close()
		return stream.out
	}
	h.subscribers = append(h.subscribers, stream)
	h.mu.Unlock()
	return stream.out
}

// adopt hands an ALREADY-BUILT stream to this hub. It is subscribe for a
// caller that had to hold its channel before the turn it belongs to existed: a
// queued follow-up is handed a stream the moment it is queued, and that stream
// becomes a subscriber of whichever turn eventually runs it. A hub that has
// already closed closes the stream instead, so the caller's channel ends rather
// than waiting for a turn that is over.
func (h *eventHub) adopt(stream *eventStream) {
	if stream == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		stream.close()
		return
	}
	h.subscribers = append(h.subscribers, stream)
	h.mu.Unlock()
}

// send fans one event out to every current subscriber. The lock is held across
// the fan-out — an eventStream send is an append and a signal, never a wait —
// so every subscriber sees the same events in the same order, and a subscriber
// arriving mid-fan-out lands cleanly before or after this event rather than
// inside it.
func (h *eventHub) send(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for _, stream := range h.subscribers {
		stream.send(event)
	}
}

// close ends every subscriber's channel. It runs after the turn's last event,
// so each channel closes once its queue has drained.
func (h *eventHub) close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subscribers := h.subscribers
	h.subscribers = nil
	h.mu.Unlock()
	for _, stream := range subscribers {
		stream.close()
	}
}

// ── event stream ────────────────────────────────────────────────────────────

// eventStream is an unbounded queue in front of one subscriber's channel.
//
// The two obvious alternatives are both wrong here. A blocking send stalls the
// loop — mid-tool-batch, with a provider connection open — behind a surface
// that is redrawing; a fixed buffer with a dropping send loses text deltas,
// which are the one event whose loss is visible as corruption rather than as
// latency. So the producer never waits and never drops, and the cost is one
// goroutine per turn.
type eventStream struct {
	out chan Event

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []Event
	closed bool
}

// refusedStream is one event and a closed channel: a turn that was refused
// before it started still answers on a stream, because every caller of Submit
// reads its answer the same way — from the channel. An error return would make
// a refusal the one outcome a surface has to handle twice.
func refusedStream(err error) <-chan Event {
	return refuseOn(newEventStream(), err)
}

// refuseOn is refusedStream onto a stream somebody already holds — a queued
// follow-up's.
func refuseOn(stream *eventStream, err error) <-chan Event {
	stream.send(Event{Kind: EventError, Err: err})
	stream.close()
	return stream.out
}

func newEventStream() *eventStream {
	stream := &eventStream{out: make(chan Event)}
	stream.cond = sync.NewCond(&stream.mu)
	go stream.pump()
	return stream
}

func (s *eventStream) send(event Event) {
	s.mu.Lock()
	if !s.closed {
		s.queue = append(s.queue, event)
		s.cond.Signal()
	}
	s.mu.Unlock()
}

func (s *eventStream) close() {
	s.mu.Lock()
	s.closed = true
	s.cond.Signal()
	s.mu.Unlock()
}

// pump drains the queue into the channel and closes it when the turn is over
// and nothing is left. A consumer that abandons the channel parks this
// goroutine on its last send: the Event contract gives a reader no way to say
// "I am gone", and parking one goroutine is the cheap end of that tradeoff —
// the turn itself has already finished.
func (s *eventStream) pump() {
	defer close(s.out)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		event := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		s.out <- event
	}
}

// DisplayEntry is one journaled message shaped for surface replay: who spoke
// and what they said, with tool calls flattened to their gloss and carrying the
// payload the journal kept for them. No reasoning — that is never recorded — and
// nothing about the wire itself.
type DisplayEntry struct {
	Role string // "user" | "assistant" | "tool" | "note" (system-injected, e.g. compaction summary)
	Text string
	Tool string // set when the entry is one call in a batch
	Hint string // the call's gloss, as the tool cluster rendered it

	// Args and Output are a TOOL entry's payload, in exactly the two shapes a
	// live surface already holds them in ([Event.Args] and [Event.Output]): the
	// arguments the model sent, compacted onto one line and capped, and the text
	// of the result that answered them, capped rune-safe for display.
	//
	// They are populated for a tool entry whenever the journal carries them,
	// which is every session file this build writes — the arguments ride the
	// assistant message's tool_calls and the result is the tool message keyed by
	// the same id. Both are "" otherwise: for every entry that is not a call, for
	// a call whose result never reached the file (a session killed mid-batch), and
	// for a file written before either was journaled.
	//
	// EMPTY MEANS NO PAYLOAD, and a surface must read it that way rather than as
	// an empty result: a replayed row with nothing behind it has nothing to
	// expand, and offering an expansion that opens on a blank is the defect this
	// field exists to end.
	//
	// CONTRACT, inherited from [Event.Output]: Output is FOR DISPLAY ONLY. It is a
	// capped copy, never the result the model read.
	Args   string
	Output string

	// ImageRefs are the paths of the pictures a person's message carried, in the
	// order they sit in it — what the journal wrote where the bytes would have
	// been (see [journalPart]). It is what lets a replayed message mark its
	// attachments the way the live surface does, "[photo.png]", instead of
	// showing the words alone as though nothing had been attached.
	//
	// Nil for every message that carried none, and for a session with no file:
	// the paths are the JOURNAL's record, and a conversation that lives only in
	// memory never wrote one.
	ImageRefs []string
}

// Transcript returns the conversation so far as display entries, oldest
// first. It exists for replay-on-resume: the surface renders the tail instead
// of opening on an empty screen. The system prompt is never included; a
// compaction summary note is.
func (a *Agent) Transcript() []DisplayEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return shapeEntries(a.messages, a.file)
}

// displayEntries is the shaping without a journal behind it: [Agent.Rewind]
// hands it the turn it just dropped, so a surface un-draws exactly the rows it
// drew. The dropped turn's rows are being REMOVED — nothing is about to expand
// one — so the picture paths a file would have answered are not asked for.
func displayEntries(messages []ai.Message) []DisplayEntry {
	return shapeEntries(messages, nil)
}

// shapeEntries is the shaping itself, over any run of messages.
//
// The journal is consulted for ONE thing — the paths of a message's pictures,
// which cannot be recovered from the message itself (a data URL is bytes with no
// provenance) — and nil is a session that has no file to ask.
//
// Everything else comes from the messages, and the tool payload is the reason
// the results are indexed first: a call's arguments ride the assistant message
// that made it, and its result is a SEPARATE message further down, keyed by the
// call's id. One pass to index, one pass to shape, so a batch of ten calls costs
// one walk rather than ten.
func shapeEntries(messages []ai.Message, journal *sessionFile) []DisplayEntry {
	results := toolResults(messages)
	entries := make([]DisplayEntry, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		entries = append(entries, DisplayEntry{
			Role:      msg.Role,
			Text:      messageContentText(msg),
			ImageRefs: journal.imageRefs(msg),
		})
		for _, call := range msg.ToolCalls {
			entries = append(entries, DisplayEntry{
				Role: "tool",
				Tool: call.Function.Name,
				Hint: gloss(call),
				// The same two renderings a live row is drawn from (loop.go),
				// applied to the same fields the journal kept: a replayed row and
				// the row it replaces are the same row, or replay is a second
				// rendering of one conversation.
				Args:   argsText(call),
				Output: capOutput(results[call.ID]),
			})
		}
	}
	return entries
}

// toolResults indexes a run of messages by the call each one answered. A result
// with no id is skipped rather than kept under "": it is a message no call can
// claim, and a call with no id would otherwise pick it up.
func toolResults(messages []ai.Message) map[string]string {
	var results map[string]string
	for _, msg := range messages {
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}
		if results == nil {
			results = make(map[string]string, 8)
		}
		results[msg.ToolCallID] = messageContentText(msg)
	}
	return results
}
