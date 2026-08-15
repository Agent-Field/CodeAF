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
		client: client,
		system: system,
		model:  config.Model,
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
		// The system message is rendered fresh rather than replayed: the date
		// and AGENTS.md in the footer are facts about now, not about the
		// session that wrote the file.
		agent.messages = append(agent.messages, restored...)
	}
	return agent, nil
}

// Usage returns the session's accumulated usage.
func (a *Agent) Usage() Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage
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
		// Steering. The message is queued rather than appended here because
		// the transcript's tail is mid-tool-batch: a user message spliced
		// between an assistant's tool_calls and their results is a shape every
		// provider rejects. The loop appends it at the next step boundary,
		// where it is journaled like any other user message.
		a.steering = append(a.steering, text)
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
	events := a.startTurnLocked(ctx, text, nil)
	a.mu.Unlock()
	return events, nil
}

// startTurnLocked begins one turn on a transcript the caller has already
// checked, with a.mu held. It is the ONE place a turn starts: Submit reaches it
// with the person's message, and a drained follow-up reaches it with a message
// that was typed while the last turn was still running.
//
// watcher is a stream built before the turn existed — a queued follow-up's —
// and is adopted onto the new hub before the loop can emit anything. Nil means
// the caller takes its own subscription, and it is the returned channel.
func (a *Agent) startTurnLocked(ctx context.Context, text string, watcher *eventStream) <-chan Event {
	a.running = true
	hub := newEventHub()
	a.hub = hub
	turnCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	// done is how Close waits for this turn: closed under a.mu by the cleanup
	// below, after the turn's last message is journaled.
	done := make(chan struct{})
	a.done = done
	a.recordLocked(textMessage("user", text))
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
				a.startTurnLocked(context.Background(), next.text, next.stream)
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
		a.followups = append(a.followups, followUp{text: text, stream: stream})
		return stream.out, nil
	}
	if err := a.railBlockLocked(); err != nil {
		return refuseOn(stream, err), nil
	}
	return a.startTurnLocked(context.Background(), text, stream), nil
}

// followUp is one queued message and the stream its turn will speak on. The
// stream exists from the moment the message is queued so the caller has
// something to hold while it waits.
type followUp struct {
	text   string
	stream *eventStream
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
	_, err := a.compact(ctx, nil)
	return err
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
	for _, text := range queued {
		a.recordLocked(textMessage("user", text))
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
	a.steering = append(a.steering, text)
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
// and what they said, with tool calls flattened to their gloss. It carries no
// tool RESULTS and no reasoning — replay shows the conversation, not the wire.
type DisplayEntry struct {
	Role string // "user" | "assistant" | "tool" | "note" (system-injected, e.g. compaction summary)
	Text string
	Tool string // set when the entry is one call in a batch
	Hint string // the call's gloss, as the tool cluster rendered it
}

// Transcript returns the conversation so far as display entries, oldest
// first. It exists for replay-on-resume: the surface renders the tail instead
// of opening on an empty screen. The system prompt is never included; a
// compaction summary note is.
func (a *Agent) Transcript() []DisplayEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	entries := make([]DisplayEntry, 0, len(a.messages))
	for _, msg := range a.messages {
		if msg.Role == "system" {
			continue
		}
		text := ""
		for _, part := range msg.Content {
			if part.Type == "text" {
				if text != "" {
					text += "\n"
				}
				text += part.Text
			}
		}
		entries = append(entries, DisplayEntry{Role: msg.Role, Text: text})
		for _, call := range msg.ToolCalls {
			entries = append(entries, DisplayEntry{
				Role: "tool",
				Tool: call.Function.Name,
				Hint: gloss(call),
			})
		}
	}
	return entries
}
