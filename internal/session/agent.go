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
		APIKey:         config.APIKey,
		BaseURL:        config.BaseURL,
		Model:          config.Model,
		Timeout:        providerTimeout,
		SiteURL:        config.SiteURL,
		SiteName:       config.SiteName,
		SiteCategories: config.SiteCategories,
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
		system = renderSystem(config)
	}
	agent := &Agent{
		config: config,
		system: system,
		model:  config.Model,
		// A memory-only session still has ONE lineage; it just has no name on
		// disk to derive it from. The file-backed case overwrites this below
		// with the header's id, which survives every resume.
		id: NewSessionID(),
	}
	agent.cacheKey = sessionCacheKey(agent.id)
	// Memory is built before the belt for the same reason the registry is: the
	// belt carries `remember` only when there is a brain to write into, so the
	// store has to exist before the tools are assembled (memory.go). The
	// background lifetime is minted with it, because a pass started by the first
	// turn has to have somewhere to be cancelled from.
	if config.Memory != nil {
		agent.memory = newMemoryBrain(config.Memory)
		agent.memoryCtx, agent.memoryStop = context.WithCancel(context.Background())
	}
	// And the block a task node was OPENED with, if it was opened with one: the
	// parent routed it at the spawn seam and handed it down here, because a node
	// has no turn of its own to route against (task_run.go).
	agent.memoryText = config.memoryBrief
	// The registry is built before the belt because the belt closes over it:
	// bash's background path and the jobs tool are both views onto this one
	// object, and it is the agent's own steering queue they report into.
	// The registry gets the WAKING lane, the same one a task node's completion
	// rides (see [Agent.enqueueSteering]): a job that exits and a watch with news
	// are both work the person asked the harness to do FOR THEM, and the answer
	// they are owed is a sentence, not a line in a transcript nobody is reading.
	agent.jobs = newJobRegistry(config.Workspace, config.Place, agent.enqueueSteering)
	// And the accounts seam before the belt for the belt's own reason: the two
	// connect tools are on it only when there is something behind them, so the
	// hub has to exist before the tools are assembled (connect.go).
	agent.connect = newConnectHub(config)
	agent.tools = agent.belt()
	definitions, err := toolDefinitions(agent.tools)
	if err != nil {
		return nil, err
	}
	agent.definitions = definitions
	agent.messages = []ai.Message{textMessage("system", system)}
	agent.refreshSystemLocked()

	if strings.TrimSpace(config.SessionFile) != "" {
		// The folder's name is the id a fresh journal's header takes (place.go):
		// the session was named before the directory that holds it existed, and
		// the header repeats that name rather than minting a second one. A
		// legacy flat session hands "" and the file names itself, exactly as it
		// always did.
		file, restored, err := openSessionFile(config.SessionFile, config.Workspace, config.Model, config.Place.ID())
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
			agent.id = id
			agent.cacheKey = sessionCacheKey(id)
		}
		// The system message is rendered fresh rather than replayed: the date
		// and AGENTS.md in the footer are facts about now, not about the
		// session that wrote the file.
		agent.messages = append(agent.messages, restored...)
		// AND THE PICTURES ARE MADE SAFE HERE RATHER THAN IN THE REPLAY. A
		// session resumed onto a model without vision — the journal remembers
		// the model it was written on, the person can start it on another — would
		// otherwise re-send yesterday's base64 to a model that cannot read it,
		// which is [Agent.SetModel]'s hole through the other door. The scrub is
		// the same function both doors call, and it is done HERE because
		// [replaySessionFile] is a pure function of the file: which model this
		// session will ride, and whether it can see, are facts about the agent,
		// and threading a capability closure into a file parser would put the
		// question in the one layer that cannot answer it. No lock is taken for
		// the reason nothing else in this constructor takes one — the agent is
		// not reachable yet.
		agent.scrubBlindImagePartsLocked(agent.model)
	}
	// THE THREAD IS THE SESSION'S OWN ID, and it is minted nowhere: the journal
	// header already carries one that survives every resume, the folder is named
	// by the same string, and a memory-only session has the one this constructor
	// minted for its cache lineage. Deriving a second identity here would give
	// one conversation two threads the day somebody resumed it (chatlog.go).
	agent.chatlog = newChatJournal(config.Memory, agent.threadID(), config.Workspace, config.Place)
	// And the state card is rendered into the first request before any turn has
	// run: a resumed conversation's card is what it knew yesterday, and a model
	// that had to wait for the first post-turn pass to be told would answer one
	// question in the dark (card.go).
	agent.cardText = agent.stateCardText()
	agent.refreshSystemLocked()
	// The client is wrapped LAST, once the lineage is known: the wrapper is the
	// one place every request this agent makes passes through, so it is where
	// the prompt-cache key is stamped. Wrapping earlier would have to read the
	// key through the agent, which is a pointer cycle to save a line.
	agent.client = sessionCompleter{
		inner:    client,
		cacheKey: agent.cacheKey,
		// AND WHERE THE NODE'S PATIENCE IS STAMPED, for the reason the cache key
		// is stamped here: this is the one place every request this agent makes
		// passes through, and the adapter underneath is shared with the
		// conversation that spawned the node (see [Agent.newTaskAgent], which
		// hands the parent's own client down). A flag on the client would make
		// the conversation patient too; a flag on the wrapper is a flag on this
		// agent's calls and nobody else's.
		patient: config.InTask,
		pacing:  config.pacing,
	}
	// AND THE WORK IS RECOVERED LAST, once this agent can actually run one. A
	// resumed journal may have a task graph beside it — nodes that landed, a node
	// that was still running when the process died, nodes waiting on them — and
	// recovery is load, reconcile with the disk, continue the frontier
	// (task_store.go). A fresh session has no checkpoint and this is a stat.
	agent.recoverTasks()
	// AND THE PROJECT'S RECORD IS RECONCILED BESIDE IT. The checkpoint above is
	// one conversation's graph; the project index is every window's record of
	// what this directory ever ran, and it holds rows that say "running" — a run
	// takes one the moment it starts (orchestrate.go). A process that died owes
	// those rows a closing one, and this is the moment anybody can know it is
	// owed. It runs AFTER recovery so that a node the graph took back is not
	// closed out from under it.
	agent.closeInflightTaskIndexRows()
	// AND ONLY NOW MAY IT SPEAK UNPROMPTED. Recovery turns the frontier, and a
	// cascade over the dependents of an interrupted node settles them right here,
	// inside New — before the caller holds the agent, before any surface has
	// subscribed to anything. A turn started at that moment would be answered
	// into a room that does not exist yet: journaled, paid for, and never drawn.
	// Everything recovery has to say is queued instead, and the first turn reads
	// it (see [Agent.wakeLocked]).
	agent.mu.Lock()
	agent.opened = true
	agent.mu.Unlock()
	return agent, nil
}

// threadID is the identity this session posts its transcript under — the
// journal header's id when there is a file, the folder's name when there is a
// folder, and the one minted for the cache lineage otherwise.
//
// The cache key cannot answer it — that is a hash, deliberately, so nothing
// about a session's id reaches a router's logs — so the id itself is held.
// No lock: this runs inside the constructor, before the agent is reachable.
func (a *Agent) threadID() string {
	if id := strings.TrimSpace(a.id); id != "" {
		return id
	}
	return a.config.Place.ID()
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
//
// AND IT IS WHERE THE PICTURES ARE MADE SAFE. A conversation carrying attached
// images carries them as base64 in the live transcript, re-sent on every step
// of every turn after they arrived; swapping onto a model that cannot see would
// send them to it with no gate in the way, because no image is being attached
// this turn. [Agent.scrubBlindImagePartsLocked] states the whole rule and its
// three deliberate limits — the journal is untouched, the swap is one-way, and a
// model that CAN see is handed everything unchanged.
func (a *Agent) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	a.mu.Lock()
	a.model = model
	a.scrubBlindImagePartsLocked(model)
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

	// wake marks a note the model OWES AN ANSWER FOR: a task's completion
	// (task_run.go's reportTaskNode), a background job's exit or a watch's delta
	// (jobs.go's reap and tools_watch.go). It is the difference between the two kinds
	// of news this queue carries — see [Agent.enqueueSteering] — and it is read
	// at exactly two moments: when the note is queued, and when the turn that
	// was running drains what is left of the queue at its end. Both are places
	// where "does anybody have to say something about this" is the question.
	wake bool

	// authored marks a line the SESSION wrote rather than the person: every note
	// that goes through [Agent.enqueueNote], whether or not anybody owes it an
	// answer. It is WHO SAID IT, where wake is WHAT IS OWED, and the two are
	// separate because an ambient note nobody must answer is still not the
	// person's words.
	//
	// It is read at exactly one place — the journal write in
	// [Agent.recordUserLocked] — and what it buys is a replay that draws the
	// harness's own line in the harness's own lane (sessionfile.go's
	// [sessionEntry.Note]).
	authored bool
}

// userText is the ordinary case: a message that is only words.
func userText(text string) userMessage {
	return userMessage{message: textMessage("user", text)}
}

// wakeNote is a line the SESSION authored that the model owes the person an
// answer for. It is userText with the mark on it, and it is what makes a
// finished task produce a sentence instead of a card nobody replies to.
func wakeNote(text string) userMessage {
	return userMessage{message: textMessage("user", text), wake: true}
}

// empty reports whether there is nothing here to record. It is the shape a
// WOKEN turn opens with: the note it is about is on the steering queue and the
// turn's first drain lands it, so there is no second message to write (see
// [Agent.wakeLocked]).
func (u userMessage) empty() bool {
	return len(u.message.Content) == 0 && len(u.refs) == 0
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
// the caller takes its own subscription, and it is the returned channel. The
// extra streams are adopted the same way and for the same reason a follow-up's
// is: a turn NOBODY ASKED FOR has to hand its events to the standing wake
// subscriptions ([Agent.Wakes]) before its first byte, or a surface would start
// reading it half way through its own answer.
//
// An EMPTY user message records nothing. That is the woken turn's opening: what
// it is about is already on the steering queue, and the loop's first drain
// writes it (see [Agent.wakeLocked]).
func (a *Agent) startTurnLocked(ctx context.Context, user userMessage, watcher *eventStream, extra ...*eventStream) <-chan Event {
	a.running = true
	a.lastTurnTruncated = false
	// The system message is rebuilt here so a turn never opens carrying the
	// memories of the one before it. WHAT THIS TURN NEEDS is routed inside the
	// turn goroutine instead ([Agent.refreshMemory], called from the loop): that
	// is a provider call, and this runs with a.mu held.
	//
	// ONLY AN AGENT THAT CAN ROUTE CLEARS THE BLOCK. A task node was handed its
	// memories once, at the spawn seam, by the conversation that had the store
	// (memory.go's memoryBrief); it has nothing to replace them with, and
	// clearing them on its first turn would take away the one thing it was given.
	if a.remembers() {
		a.memoryText = ""
	}
	a.refreshSystemLocked()
	hub := newEventHub()
	a.hub = hub
	turnCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	// done is how Close waits for this turn: closed under a.mu by the cleanup
	// below, after the turn's last message is journaled.
	done := make(chan struct{})
	a.done = done
	if !user.empty() {
		a.recordUserLocked(user)
	}
	// AND THE PERSON'S OWN WORDS ARE KEPT, so that work this turn hands off can
	// carry the sentence that asked for it rather than a paraphrase of it
	// (task_brief.go). A woken turn opens with nothing and changes nothing here.
	a.rememberAskLocked(user)
	var events <-chan Event
	if watcher != nil {
		hub.adopt(watcher)
		events = watcher.out
	} else {
		events = hub.subscribe()
	}
	for _, stream := range extra {
		hub.adopt(stream)
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
			//
			// unanswered is the half of that drain nobody has replied to: a note
			// the SESSION authored — a task landing — that arrived after this
			// turn's last request went out, so the model never saw it. It is in
			// the transcript now and nothing is going to speak about it, which is
			// exactly the silence the wake below exists to end.
			_, unanswered := a.drainSteeringLocked()
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
			} else if completed && unanswered && a.wakeLocked() {
				// A TASK LANDED IN THE LAST SECONDS OF THIS TURN. Its note is in
				// the transcript, unread by any request, so the answer the person
				// is owed needs one more turn — started here, with no new message,
				// because the thing to answer is already recorded.
				//
				// It is gated on `completed` for the reason the follow-up drain is
				// (see [Agent.nextFollowUpLocked]): a drain must never resurrect a
				// turn somebody stopped. A person who interrupted gets the note in
				// their transcript and silence, which is what they asked for.
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
		// The turn's own opening message travels with it, for the one thing that
		// has to know whether a PERSON started this turn (harness.go): a woken
		// turn opens empty and reads its note off the steering queue, and a
		// matcher that went looking in the transcript would score the last thing
		// somebody typed against a turn they did not start.
		completed = a.runTurn(turnCtx, hub, user)
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

// CompactWithFocus is Compact, and THE FOCUS IS NOW IGNORED.
//
// It was one extra instruction for the summarizer — `/compact keep the API
// decisions and the failing test`, a person saying which part of a lossy summary
// had to survive. There is no summarizer any more (loop.go): a pass stubs tool
// results and folds assistant work, and neither of those is a judgement anybody
// can steer. The kept content is the same whatever is typed after /compact —
// every user message, the recent tail, and the state card — so there is nothing
// for a focus to protect that is not already protected.
//
// The door stays open with its signature unchanged because the surface calls it
// (internal/tui3), and a person who types the old form gets the pass they asked
// for rather than an error about a machine that used to exist.
func (a *Agent) CompactWithFocus(ctx context.Context, _ string) error {
	_, err := a.compact(ctx, nil)
	return err
}

// sessionCompleter is the Completer every request goes through, and it does two
// things to them.
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
// ── AND THE PATIENCE ──
//
// It marks a TASK CHILD'S calls as ones that wait a provider's pacing out
// instead of giving up on it (internal/provider's patience.go). A conversation
// keeps the bounded patience it always had, and the difference between the two
// is entirely whether anybody is watching: a person in front of a cursor is owed
// an error long before they are owed a ten-minute silence, while a node with a
// worktree and nobody watching loses an hour of real work to a 429 that was
// always going to clear. The wait is still the caller's context's to end, so an
// interrupt or a stop cuts through a parked node's call at once.
type sessionCompleter struct {
	inner    Completer
	cacheKey string
	// patient marks this agent's calls as a task child's: see above.
	patient bool
	// pacing is who to tell while one of those calls is parked, and nil for
	// every conversation and every agent nobody is drawing a card for.
	pacing func(bool)
}

func (f sessionCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	ctx = provider.WithCacheKey(ctx, f.cacheKey)
	if f.patient {
		ctx = provider.WithPatientRateLimits(ctx)
	}
	if f.pacing != nil {
		ctx = provider.WithPacingNotice(ctx, f.pacing)
	}
	return f.inner.CompleteWithMessages(ctx, messages, options...)
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
	// No memory pass outlives the session. The cancel is what stops one waiting
	// on a provider; the wait below is what lets one that is already writing
	// reach the store (memory.go).
	memoryStop := a.memoryStop
	file := a.file
	cancel := a.cancel
	done := a.done
	// Nothing queued will ever run now, and a caller holding one of those
	// channels is owed the close rather than a wait that never ends.
	a.dropFollowUpsLocked()
	// And so is a surface waiting for the next woken turn: no more will come,
	// and a lane left open is a pump waiting on a session that has left.
	for _, lane := range a.wakeLanes {
		close(lane)
	}
	a.wakeLanes = nil
	// And an adaptive run: it holds a context of its own precisely because its
	// turn ended, so this is the only thing that can reach it (orchestrate.go).
	// A harness being designed needs nothing here — it is a task now, and the
	// job round below cuts it with every other node (harness_task.go).
	a.cancelOrchestrationsLocked()
	a.mu.Unlock()

	if memoryStop != nil {
		a.waitForMemory(memoryStop)
	}
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

	// The store's copy of the transcript is drained LAST of the writers and
	// before the file is closed, for the reason the turn is waited for: a
	// cancelled turn's tail is queued by the wait above, and closing the log
	// first would drop exactly the lines a compacted resume has nowhere else to
	// read (chatlog.go).
	a.chatlog.close()

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
	// AND THE STORE GETS IT TOO, when there is one. The journal is what a resume
	// replays; the store is what survives a compaction, which discards the
	// journal's pre-cut lines by definition. The call queues and returns
	// (chatlog.go), so this stays one append under the lock.
	a.chatlog.post(message)
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
	// The store's copy is taken before the journal's early return: a session
	// with no file still has a conversation worth keeping, and the person's own
	// words are the last thing that should depend on which layout they opened in.
	a.chatlog.post(user.message)
	if a.file == nil {
		return
	}
	if user.authored {
		// THE SESSION'S OWN LINE IS MARKED AS ONE. The transcript keeps it
		// user-role, which is what the model has to read it as; the journal keeps
		// the one bit that says nobody typed it, so a resume can draw it where the
		// live surface drew it (sessionfile.go's [sessionEntry.Note]). A note
		// carries no pictures, which is why this door takes none.
		a.file.appendNote(user.message)
		return
	}
	a.file.appendMessage(user.message, user.refs...)
	// AND THE FOLDER LEARNS THE PERSON WAS HERE. Resume order is on when the
	// person last spoke and not on file mtime (place.go's [Meta.LastUserAt]),
	// and this line — the one place the person's own words reach the journal —
	// is the only honest witness to that. A session with no folder stamps
	// nothing (placemeta.go).
	a.stampUserLocked(messageContentText(user.message))
}

func (a *Agent) record(message ai.Message) {
	a.mu.Lock()
	a.recordLocked(message)
	a.mu.Unlock()
}

// markTurnTruncated preserves the provider's stop reason after the response
// itself has gone. Node reports need this fact, while ordinary transcript
// messages deliberately contain only what the participants said.
func (a *Agent) markTurnTruncated() {
	a.mu.Lock()
	a.lastTurnTruncated = true
	a.mu.Unlock()
}

// turnTruncated is the reporting side of [Agent.markTurnTruncated].
func (a *Agent) turnTruncated() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastTurnTruncated
}

// journalOnly writes one message into this agent's journal WITHOUT putting it in
// front of the model.
//
// It is the door for something a person has to be able to READ BACK and the
// model must not be asked to reason from. Today that is one thing: the replies a
// harness designer streams into its own node's room, so that reopening a design
// tomorrow shows the writing of the page and not only the page (harness_task.go's
// designSeat). Two separate reasons keep those lines out of the transcript, and
// either alone would be enough.
//
//   - THE TRANSCRIPT IS THE THREAD'S BRIEF. The design thread already holds the
//     page that was actually written; a superseded draft beside it is a model
//     being invited to answer "what does step three do" out of the version that
//     was thrown away.
//   - AND A MESSAGE APPENDED MID-TURN IS AN ILLEGAL TRANSCRIPT. These lines land
//     while somebody may be talking to this same agent in the room, and an
//     assistant message that arrives between a tool call and its result is a
//     request the provider refuses. Nothing rebuilds a conversation out of the
//     journal here — a node's journal path is minted fresh for each life of the
//     node (task_run.go's taskJournalPath) and is never resumed — so the file
//     takes the line with no such ordering to break.
func (a *Agent) journalOnly(message ai.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file != nil {
		a.file.appendMessage(message)
	}
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
	// THIS DRAIN IS THE ONE THAT ANSWERS. It runs immediately before the next
	// request (loop.go), so anything on the queue is in front of the model from
	// here — which is precisely what a task node's runner is waiting to be true
	// of its sub-tasks' reports (see [Agent.postTaskNews]). The turn's END drain
	// deliberately does not clear it: those notes reached the transcript and no
	// request.
	a.taskNotes = 0
	landed, _ := a.drainSteeringLocked()
	return landed
}

// drainSteeringLocked is the drain itself, for callers already holding a.mu —
// the turn's end, which must drain and clear running without a gap.
//
// It reports how many messages landed AND whether any of them was a wake note
// (see [Agent.enqueueSteering]). The second answer only means anything to the
// turn's end: a note drained at a step boundary is one the next request carries,
// so the model answers it as part of the turn it is already in, while a note
// drained after the last request is one nobody has said a word about.
func (a *Agent) drainSteeringLocked() (int, bool) {
	queued := a.steering
	a.steering = nil
	woke := false
	for _, message := range queued {
		a.recordUserLocked(message)
		// A STEERING MESSAGE IS STILL THE PERSON ASKING. It arrives mid-turn and
		// is often the correction the work about to be handed off must carry, so
		// the newest thing they typed is what a proposal made after this drain
		// quotes (task_brief.go).
		a.rememberAskLocked(message)
		woke = woke || message.wake
	}
	return len(queued), woke
}

// enqueueSteering puts one line the SESSION authored — a task node landing
// (task_run.go), a background job exiting (jobs.go), a watch with news
// (tools_watch.go) — onto the same queue the person's steering rides, AND WAKES
// THE SESSION IF NOBODY IS WORKING.
//
// The lane is shared on purpose. Both are news that arrives while the model is
// busy, both must land at a step boundary rather than inside a tool batch, and
// both belong in the transcript as plain user-role text. Giving completions
// their own channel would mean a second drain, a second ordering rule, and a
// second way for a message to arrive at a moment the provider rejects — for a
// message that is, from the model's side, exactly a line somebody typed.
//
// ── WHY IT WAKES, AND WHY THE OTHER LANE DOES NOT ──
//
// A queue alone was the whole defect. A person hands off a task, the work runs
// for eleven minutes, it lands — and the note sat here until the person happened
// to type something else, so what they got for their research was a card and
// silence. The answer they asked for is a SENTENCE from the model ("the
// comparison is at ~/oauth.md; the short version is…"), and a model that is never
// asked never writes one. So a note that lands on an idle session starts a turn.
//
// THAT IS TRUE OF EVERY BACKGROUND ERRAND, NOT ONLY OF TASKS. The wake was
// scoped to settles first and the same silence was left standing beside it: a
// person says "run the build in the background", the build fails four minutes
// later, and the exit note sat on this queue until they happened to type. A
// watch is worse — its whole reason to exist is that the model stopped polling,
// so nothing is ever going to come and look. Both are work the person ASKED THE
// HARNESS FOR and both are owed the same sentence, so both wake.
//
// It is not a turn per event. Everything below coalesces: the notes queue, the
// FIRST one starts a turn, and every note that lands while that turn runs is
// drained into it at a step boundary. A dev server that flaps six times in one
// window is six lines in front of one model, not six paid turns — and the rail,
// InTask, Close and the not-yet-open session all still decline
// ([Agent.wakeLocked]).
//
// [Agent.enqueueAmbientNote] is the same queue with that one difference removed,
// and what is left on it is news NOBODY ASKED FOR: an account of what an
// interrupt left behind that a resume found on disk (task_store.go), an account
// the harness gives of itself (looped.go, recovery.go), an OAuth connection
// completing in a browser tab (connect.go). Context for whatever is said next —
// real, worth carrying, nobody standing there for it.
//
// A closed agent drops the note rather than queueing it: after Close nothing
// drains, and the journal it would be written to is already shut.
func (a *Agent) enqueueSteering(text string) {
	a.enqueueNote(wakeNote(text))
}

// enqueueAmbientNote is enqueueSteering for news nobody is waiting on: it
// queues and never starts a turn. See the block above for which news is which.
func (a *Agent) enqueueAmbientNote(text string) {
	a.enqueueNote(userText(text))
}

func (a *Agent) enqueueNote(note userMessage) {
	text := strings.TrimSpace(note.text())
	if text == "" {
		return
	}
	note.message = textMessage("user", text)
	// Both kinds of note are the SESSION's words. It is set here, at the one door
	// both of them come through, rather than at the two constructors above.
	note.authored = true
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.steering = append(a.steering, note)
	if note.wake {
		// A turn already running is the coalescing case and needs nothing done:
		// wakeLocked declines, and the note lands in that turn at its next step
		// boundary exactly as a person's steering does.
		a.wakeLocked()
	}
}

// takesNotes reports whether this agent can still read anything it is handed. A
// closed one drops every note silently ([Agent.enqueueNote]), which is right —
// nothing drains after Close — so a caller CHOOSING between two readers has to
// ask first, or it will choose the one that is not listening
// ([Agent.deliverTaskNote]).
func (a *Agent) takesNotes() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed
}

// postTaskNews records that one of this agent's OWN sub-tasks has handed over
// its report, and releases whoever is waiting to hear it.
//
// A NODE'S TURNS ARE ITS RUNNER'S TO START, which is why this is not a wake:
// [Agent.wakeLocked] declines inside a task and must, because a node starting
// turns of its own would be a second conversation inside a worktree with nobody
// reading it. So a child's report is COUNTED here, and the runner that is
// already holding the parent open re-enters the model with it (task_run.go's
// [runTaskChild]). The count is cleared by the drain that puts those notes into
// a request ([Agent.drainSteering]), so "outstanding" means what it says.
func (a *Agent) postTaskNews() {
	a.mu.Lock()
	a.taskNotes++
	if a.taskNews != nil {
		close(a.taskNews)
		a.taskNews = nil
	}
	a.mu.Unlock()
}

// taskNewsWait is the generation a runner takes BEFORE it asks whether anything
// is outstanding. Holding the channel first is what closes the gap between the
// question and the wait: a report that lands in it closes this channel, so the
// wait returns immediately instead of missing the news it was waiting for.
func (a *Agent) taskNewsWait() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.taskNews == nil {
		a.taskNews = make(chan struct{})
	}
	return a.taskNews
}

// taskNewsOwed reports how many sub-task reports this agent has been handed
// that no request has carried yet.
func (a *Agent) taskNewsOwed() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.taskNotes
}

// resumeTurn starts one turn on what is ALREADY on the steering queue and hands
// back its stream, for the one caller whose turns are driven from outside: a
// task node's runner, re-entering the model with its sub-tasks' reports.
//
// It is [Agent.wakeLocked] with the wake taken out. Same empty opening message
// — what the turn is about is on the queue and the loop's first drain records
// it — and the same refusals, minus the two that only make sense for a
// conversation: there is no rail on a node (its spend is the session's, and the
// session's rail was checked when the work was proposed) and no wake lane to
// hand a stream to, because the caller is holding it.
//
// A nil answer means there is no turn to have: the agent is closed, or one is
// already running, which for a runner means the child is still working and the
// notes will land in it at a step boundary.
func (a *Agent) resumeTurn(ctx context.Context) <-chan Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.running || !a.opened {
		return nil
	}
	return a.startTurnLocked(ctx, userMessage{}, nil)
}

// wakeLocked starts a turn for what is waiting on the steering queue, with a.mu
// held, and reports whether one began.
//
// It records NOTHING. The turn opens on an empty message and the loop's first
// act is to drain the queue (loop.go), which records and journals every note in
// the order it arrived and does it before the first provider request. That is
// what makes the coalescing free: two tasks landing in the same idle window
// append two notes, the first sets running under this lock, and the second finds
// a turn already going — ONE wake, one request, both outcomes in front of the
// model. A timer would have bought the same behaviour and a window in which a
// settle could be lost.
//
// ── WHAT DECLINES A WAKE ──
//
//   - a turn is already running: the note is steering, not a second turn.
//   - the session is closed: nothing drains after Close.
//   - InTask: this agent is one task node's runner (session.go's Config), whose
//     turns belong to the runner that drives it. A node starting a turn of its
//     own would be a second conversation inside a worktree. THE ONE EXCEPTION
//     is a node that is a ROOM and not a worker — the thread a sub-harness is
//     designed in (harness_task.go) — which is a conversation by construction
//     and whose steering has no turn to land in unless it starts one.
//     own would be a second conversation inside a worktree. A node WITH
//     SUB-TASKS still has to be re-entered when one of them reports, and it is —
//     through [Agent.resumeTurn], by the runner, which is the same turn started
//     by somebody who is reading it.
//   - the session is not open yet: recovery settles nodes inside New, and a turn
//     started there speaks to nobody (see [newAgent]).
//   - the spend rail: a turn that starts must be one the session can pay for,
//     and this is the one turn nobody asked for. The note stays queued and is
//     read by whatever the person says next.
//
// WHAT IT DOES NOT CHECK is that there is anything to answer, and it cannot:
// its two callers know that in two different ways. The enqueue has just put a
// note on the queue; the turn's end has just drained one INTO the transcript, so
// the queue is empty and the thing to answer is the last message. A third caller
// would have to establish the same fact before calling.
func (a *Agent) wakeLocked() bool {
	if a.running || a.closed || (a.config.InTask && !a.config.roomThread) || !a.opened {
		return false
	}
	if err := a.railBlockLocked(); err != nil {
		return false
	}

	// Every standing subscription gets this turn's stream BEFORE it starts, so a
	// surface draws the answer from its first delta. A lane whose reader is that
	// far behind is skipped rather than waited on: this call holds the lock
	// Interrupt needs, and a wake is not worth a session that cannot be stopped.
	watchers := make([]*eventStream, 0, len(a.wakeLanes))
	for _, lane := range a.wakeLanes {
		stream := newEventStream()
		select {
		case lane <- stream.out:
			watchers = append(watchers, stream)
		default:
			stream.close()
		}
	}
	// The turn's own subscription is drained and thrown away. A woken turn has no
	// caller holding a channel — that is what makes it a wake — and an
	// unread stream would park its pump goroutine on the first event forever.
	sink := newEventStream()
	go func() {
		for range sink.out { //nolint:revive // draining is the point
		}
	}()
	a.startTurnLocked(context.Background(), userMessage{}, sink, watchers...)
	return true
}

// Wakes is the standing subscription to turns THE SESSION STARTED ON ITS OWN:
// one channel per woken turn, handed over before that turn's first event, and
// closed when it ends — the same shape [Agent.Submit] returns, because it is the
// same thing.
//
// It exists because a wake has no caller. Every other turn is somebody asking
// for something and reading the answer off the channel they were given; a turn
// started by a task landing is the model speaking to a room nobody is holding a
// microphone into, and without this the answer would reach the journal and never
// the screen. A surface adopts each stream exactly as it adopts a follow-up's
// (internal/tui3's followup.go): draw the turn, pump to close.
//
// The lane is buffered and NEVER BLOCKS the session: a subscriber that has
// stopped reading misses wakes rather than freezing the agent that is trying to
// tell it something. It closes with the session.
func (a *Agent) Wakes() <-chan (<-chan Event) {
	lane := make(chan (<-chan Event), wakeLaneDepth)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		close(lane)
		return lane
	}
	a.wakeLanes = append(a.wakeLanes, lane)
	return lane
}

// wakeLaneDepth is how many woken turns a subscriber may be behind on before it
// starts missing them. Wakes are rare — one per idle window in which work
// landed — so a handful is a surface that has stopped reading, not a busy one.
const wakeLaneDepth = 8

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
	// Role is "user" | "assistant" | "tool" | "note" | "aside".
	//
	// "note" is a system-injected marker a surface draws as a rule of its own — a
	// compaction summary is the one that exists.
	//
	// "aside" is a line the SESSION WROTE and the person did not: a task's
	// completion note, a job's exit, a resume's account of what an interrupt left
	// behind (agent.go's [Agent.enqueueNote]). It rides the user role in the
	// transcript because that is the only role the model can be told something
	// in, and it is separated here because a surface that drew it as a user
	// message would be putting words in somebody's mouth — words that, live, that
	// same surface deliberately never draws. It is answered from the journal's own
	// mark, so a line from a file written before the mark existed still arrives as
	// "user", which is exactly what it always was.
	Role string
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
		role := msg.Role
		if role == "user" && journal.isNote(msg) {
			// A LINE THE SESSION WROTE IS NOT THE PERSON'S. It is user-role in the
			// transcript because that is the only role the model can be told
			// something in, and the journal is the only place that remembers the
			// difference (sessionfile.go's [sessionEntry.Note]). Drawn as a user
			// message it would be this build putting words in somebody's mouth —
			// the exact thing the live surface refuses to do with the same note.
			// It is NOT "note": that role is the compaction marker a surface draws
			// as a rule of its own, and these two are not one shape.
			role = "aside"
		}
		entries = append(entries, DisplayEntry{
			Role:      role,
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
