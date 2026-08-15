// Package session is the v3 conversational agent: a pi-shaped working loop
// you talk to, not a dispatcher. It owns one conversation against one
// workspace: the person submits messages, the agent works (read, bash, edit,
// write, grep, find, ls, todo) and streams what it does as events.
//
// The seams are deliberate and narrow. The agent talks to a provider through
// Completer (one method), and to the person through a channel of Events. The
// tasker does not exist here yet: when it attaches, it arrives as extra tools
// (task/change/stop → store.RequestCommand) registered beside the working
// ones, and nothing in this file changes.
//
// The loop's wire behavior — message assembly, stop condition, retry
// schedule, tool parallelism — follows internal/exec/bare (pi 0.82.1), with
// three deliberate differences: it is interactive (Submit between turns, not
// one task to the end), interruptible (Interrupt cancels the in-flight turn
// and keeps the partial), and its compaction follows docs/CHAT-V3.md
// Decision 9 (omp's architecture: threshold = window − max(15%, 16k), keep
// 20k tokens verbatim, one LLM summary with omp's section contract, the pass
// journaled as a transcript marker).
package session

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/search"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Completer is the narrow slice of provider.Client the loop needs. It is an
// interface so tests substitute a scripted completer.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// EventKind names one thing the person can see happening.
type EventKind int

const (
	// EventTextDelta carries one streamed chunk of the assistant's reply in Text.
	EventTextDelta EventKind = iota
	// EventThinking says the model is reasoning; it carries no text.
	EventThinking
	// EventToolBegin carries the tool name in Tool and a person-readable gloss
	// in Hint — "read internal/session/session.go", "bash go build ./…". Args
	// carries the call's arguments for a surface that expands the row; Output
	// is empty, the call has not run yet.
	EventToolBegin
	// EventToolEnd carries the tool name, a short result hint (often empty),
	// and the call's Args and Output for expansion.
	EventToolEnd
	// EventToolFailed carries the tool name and why, with the same Args and
	// Output as EventToolEnd — a failure is the one result worth reading in
	// full, and the surface has it here without asking.
	EventToolFailed
	// EventTurnDone ends one Submit's stream; Usage is the turn's total.
	EventTurnDone
	// EventError ends the turn abnormally; Err says why.
	EventError
	// EventCompacting says a compaction pass has started — the cut is made
	// and the summarizer is running, which is seconds a surface should show
	// as work, not silence. Hint sizes the pass ("compacting ~84k tokens").
	// EventCompacted always follows it, success or failure: on failure the
	// pass changed nothing and the turn keeps going.
	EventCompacting
	// EventCompacted marks a compaction pass; Hint summarizes
	// ("compacted from ~84k tokens, kept last ~20k").
	EventCompacted
	// EventReasoning carries one streamed chunk of the model's REASONING in
	// Text, for the models that put their working on the wire (OpenRouter's
	// "reasoning", the DeepSeek family's "reasoning_content").
	//
	// It follows the EventThinking that opened the run rather than replacing it:
	// a surface that only draws "thinking…" ignores this kind and is unchanged,
	// and a surface that shows the thought has the words and the boundary both.
	// The text is NOT part of the answer — it is never accumulated into the
	// partial reply and never recorded in the transcript, because a later step
	// re-sending it would be sending the model its own working as if it had said
	// it out loud.
	EventReasoning
	// EventConsentRequest asks the person whether one tool call may run
	// (consent.go). It carries the call's ID, Tool, Args and gloss in Hint, and
	// the policy's own phrasing of why it is asking in Rule.
	//
	// It is a QUESTION, not a report: the call is blocked inside the tool batch
	// until [Agent.ResolveConsent] answers it or the turn's context dies, and a
	// surface that ignores this kind leaves the turn waiting until the person
	// interrupts. It arrives AFTER the batch's EventToolBegin rows, so a surface
	// attaches the question to the row it already drew for that call.
	EventConsentRequest
	// EventTitleChanged carries the session's name in Text (title.go). It fires
	// at most once per session — after the first completed turn, when the
	// session had no name yet.
	EventTitleChanged
	// EventToolAnnounced says one tool call has finished ARRIVING — the model
	// has sent the whole instruction — while the response it rides on is still
	// streaming. It carries the same Tool, Hint and Args EventToolBegin will,
	// and no Output: nothing has run.
	//
	// EventTaskProposal asks the person whether one groomed piece of work may
	// become a task node (task.go). It carries the proposal in Task: title,
	// the two-or-three-line summary, the full brief, and the auto-approve
	// deadline.
	//
	// It is a QUESTION with a CLOCK, not a report: the propose_task call is
	// blocked until [Agent.ResolveTask] answers it or the deadline passes, and
	// the deadline passing means APPROVED — the surface is the person's chance
	// to redirect, never a gate the work waits on forever. A surface with no
	// answer box for this kind still works: the countdown approves.
	EventTaskProposal
	// EventTaskUpdate reports one task node's progress (task.go): Task carries
	// the state (running, done, failed), the elapsed time, and on completion
	// the report, the changed files, and the merge outcome. It is a report,
	// never a question; the first update (running) arrives as the proposal
	// resolves.
	EventTaskUpdate
	// EventToolAnnounced says one tool call has finished ARRIVING — the model
	// has sent the whole instruction — while the response it rides on is still
	// streaming. It carries the same Tool, Hint and Args EventToolBegin will,
	// and no Output: nothing has run.
	//
	// It is the difference between "asked for" and "started", and it exists
	// because those two moments can be seconds apart. A mutating call is
	// announced here and does not begin until the response completes and the
	// batch starts (loop.go's safety law), so a surface that only had
	// EventToolBegin had to choose between drawing nothing for that gap or
	// drawing a spinner for work that had not started. Both are lies; this is
	// the third option.
	//
	// EventToolBegin keeps its exact meaning: EXECUTION STARTED. Every call that
	// is announced is also begun, in the same order, so a surface that ignores
	// this kind is unchanged — and a provider that never announces (a
	// non-streaming endpoint) simply sends no event of this kind.
	EventToolAnnounced
	// EventGuardianAllowed says a call the policy would have ASKED about ran
	// because the guardian model vouched for it (guardian.go). It carries the
	// Tool, the call's gloss in Hint and Args, and the rule that would have
	// prompted in Rule.
	//
	// It is an ANNOTATION, not a question and not a result: the row it belongs to
	// is the ordinary tool row, and this is the dim line beside it saying who
	// answered instead of the person. A surface that ignores this kind shows a
	// call that simply ran, which is what it did — but a gate that answers on
	// somebody's behalf and says nothing about it is a gate nobody can audit, so
	// the event exists whether or not a given surface draws it.
	EventGuardianAllowed
	// EventNudge says the turn has been caught going in circles and has been
	// nudged (looped.go): Tool is the call that repeated, Count is how many times.
	// A surface renders it as "stuck? nudged · <tool> ×N".
	//
	// The nudge itself is a note in the transcript, not an error and not a
	// refusal — the model keeps working, having been told what it has been doing.
	// This event is only how a person gets to SEE that happen.
	EventNudge
)

// Event is one observable thing in a turn. A Submit returns a channel of
// them, closed after EventTurnDone or EventError.
type Event struct {
	Kind  EventKind
	Text  string
	Tool  string
	Hint  string
	Err   error
	Usage Usage

	// Args is the tool call's arguments rendered for display: the JSON the
	// model sent, compacted to one line and capped. It is set on
	// EventToolBegin, EventToolEnd and EventToolFailed. Arguments that do not
	// parse as JSON pass through as the raw text — a malformed call is still a
	// call the person should be able to look at.
	Args string

	// Output is the tool's result text on EventToolEnd and EventToolFailed,
	// verbatim up to a cap and then marked "… (N more bytes)".
	//
	// CONTRACT: Output is FOR DISPLAY EXPANSION ONLY. It is not the result.
	// The wire result — what the model reads, what the transcript records — is
	// unchanged and complete; this field is a capped copy for a surface that
	// wants to show more than Hint. A surface must never treat it as the tool's
	// output for any purpose other than showing it to a person.
	Output string

	// ID names one EventConsentRequest, and is the token a surface hands back
	// to [Agent.ResolveConsent]. It is zero on every other kind.
	ID uint64

	// Task carries one EventTaskProposal or EventTaskUpdate's payload
	// (task_contract.go). It is nil on every other kind, and the ID inside it
	// is the token a surface hands back to [Agent.ResolveTask].
	Task *TaskNotice

	// Rule is the approval policy's own phrasing of why a call is being asked
	// about — `bash pattern "rm -rf *"`, `tool "edit"`, `default`. It is set on
	// EventConsentRequest and empty elsewhere. The wording is the policy's
	// (internal/approval) so that every surface says the same sentence about the
	// same rule instead of deriving one.
	Rule string

	// Count is how many times the thing this event is about has happened. It is
	// set on EventNudge — the number of repetitions that earned the nudge — and
	// zero everywhere else, which is why it is a plain int rather than a pointer:
	// no other kind has a count, and "0" is not a count any kind reports.
	Count int
}

// Usage is token and cost accounting for one turn or the session total.
type Usage struct {
	Input    int
	Output   int
	CostUSD  float64
	Duration time.Duration
	Turns    int

	// CacheRead and CacheWrite are the provider's prompt-cache accounting:
	// tokens served from a warm prefix, and tokens written into one. Both are
	// zero when the provider says nothing, which is a different fact from a
	// cache that missed — but not one a surface can tell apart, so a surface
	// shows nothing rather than "0% cached" (design-law-v2 §16 EMPTINESS).
	//
	// They are read off ai.Usage, which tolerates both spellings the endpoints
	// use: Anthropic-native cache_read_input_tokens/cache_creation_input_tokens
	// and OpenAI-style prompt_tokens_details.cached_tokens.
	CacheRead  int
	CacheWrite int
}

// CachedShare is the fraction of this session's INPUT that came off a warm
// prefix, and false when there is nothing to divide.
//
// The denominator is where the two provider dialects have to be reconciled, and
// they disagree about a fact rather than a name. OpenAI-style endpoints count
// cached tokens INSIDE prompt_tokens — cached_tokens is a subset, so the total
// is already Input. Anthropic-native ones count them BESIDE input_tokens —
// disjoint, so the total is Input + CacheRead. Nothing on the wire says which
// convention a given row used, so the shape does: cache reads that exceed the
// input count cannot be a subset of it, and only then are the two added.
//
// Being wrong in the OpenAI direction would report every warm turn as ~50%
// cached forever; being wrong in the Anthropic direction would report >100%.
// The test for this is in agent_test.go, one case per dialect.
func (u Usage) CachedShare() (float64, bool) {
	if u.CacheRead <= 0 {
		return 0, false
	}
	total := u.Input
	if u.CacheRead > u.Input {
		total = u.Input + u.CacheRead
	}
	if total <= 0 {
		return 0, false
	}
	return float64(u.CacheRead) / float64(total), true
}

// Config builds one agent. The zero value is invalid: Workspace, Model,
// APIKey and BaseURL are required.
type Config struct {
	Workspace string // tools root here; all relative paths resolve inside it
	Model     string
	APIKey    string
	BaseURL   string

	// System is the rendered system prompt. Empty renders the package's
	// embedded default (prompts/system.md + the project footer) for
	// Workspace and Model.
	System string

	// ContextWindow is the model's window in tokens; compaction fires at
	// window − max(15% of window, 16384). Zero selects a conservative default.
	ContextWindow int

	// CompactEnabled gates automatic compaction. Manual compaction via the
	// surface's /compact is a surface concern and always available through
	// Compact.
	CompactEnabled bool

	// SessionFile is the JSONL transcript: header line, then one line per
	// journaled message and compaction marker. Empty keeps the conversation
	// in memory only. If the file exists it is loaded on New and the
	// conversation resumes after the latest compaction marker.
	SessionFile string

	// MemoryFile is the durable memory: one file of lines the model keeps with
	// the note tool and drops with forget (memory.go), rendered into the system
	// prompt as a <memory> block and re-read at the start of every turn. Empty
	// is memory OFF — no file, and no note or forget on the belt.
	//
	// It is the caller's path rather than a path this package derives, for the
	// reason SessionFile is: where a person's state lives is the surface's
	// decision, and a package that picked ~/.aforge/v3/memory.md itself would
	// write there from a test, a subharness leaf, and a second window of the
	// same session alike.
	MemoryFile string

	// ApprovalPolicy decides whether a tool call runs, asks, or is refused
	// (internal/approval, gated in consent.go). NIL ALLOWS EVERYTHING, which is
	// the behavior every caller had before the gate existed: a headless --once
	// and the tests run exactly as they did, and a surface opts into the policy
	// by handing one over.
	ApprovalPolicy *approval.Policy

	// AskConsent says somebody is watching this agent's events and will answer
	// an EventConsentRequest with [Agent.ResolveConsent].
	//
	// It is the difference between a question and a hang. Left false — a
	// headless caller, a cron run, --once — a policy's "prompt" decision denies
	// the call with a result the model can act on, instead of blocking the turn
	// on a question that will never reach a person.
	AskConsent bool

	// Guardian turns on the small model that answers a "prompt" decision before
	// the person is asked at all (guardian.go). FALSE IS THE DEFAULT AND THE
	// ONLY SAFE ONE: this is a gate that answers on somebody's behalf, and a
	// caller that has not said so must never get one. Nothing about the gate
	// changes when it is off — not one extra call, not one extra branch a person
	// can observe.
	Guardian bool

	// RolesSource reads one auxiliary-model setting for internal/roles: the
	// keys are roles.PinKey and roles.TierKey. Nil is a fresh install with no
	// settings file, and every auxiliary call then rides the session's own
	// model — roles.Resolve's floor, not a failure.
	RolesSource func(key string) (string, bool)

	// SupportsImages reports whether a model can read image content parts. It
	// gates [Agent.SubmitImage] and NIL IS FALSE — the opposite of every other
	// nil-is-permissive hook here, and deliberately so: a model that cannot see
	// answers a message full of image parts with a 400 or, worse, with a
	// confident description of nothing. "I don't know whether this model has
	// vision" and "this model has vision" must not be spelled the same way, so a
	// caller that holds no catalog gets a refusal it can read instead of a turn
	// that fails on the wire.
	//
	// It is a function of the model rather than a bool because the model moves:
	// /model swaps it mid-session (see [Agent.SetModel]), and the answer has to
	// follow the model the next turn will actually ride.
	SupportsImages func(model string) bool

	// ImageGenModel and ImageGenClient are the image-generation pair the belt's
	// generate_image tool calls through (tools_image.go): the model that paints,
	// and the client that carries the request to it.
	//
	// They follow the SAME LAW as the search pair below — NIL CLIENT MEANS THE
	// TOOL IS NOT ON THE BELT, not that it is on the belt and refuses — and for
	// the same reason: a model told it can make pictures will keep planning
	// around that capability long after the first refusal. An empty model with a
	// live client is the same absence, unless the person pinned one in settings
	// (roles.PinKey(roles.RoleImageGen), read through RolesSource).
	//
	// ImageGenerator is provider.MediaClient's own signature, so the wiring wave
	// assigns the media client here with no adapter in between.
	ImageGenModel  string
	ImageGenClient ImageGenerator

	// SearchProvider and SearchFetcher are the web-search pair the belt's
	// web_search and web_fetch tools call through (tools_search.go). They are
	// [search.Provider] and [search.Fetcher] rather than a resolved
	// configuration because WHICH back end answers is not this package's
	// question: internal/search owns the resolution law, the surface runs it
	// against the person's settings, and what arrives here is the answer.
	//
	// NIL IS THE DEFAULT AND MEANS THE TOOL IS NOT ON THE BELT — not that it
	// is on the belt and fails. A model told about a tool it cannot reach is
	// strictly worse off than a model never told: it will spend a call, read a
	// refusal, and often try again in different words, and the whole time it
	// is planning around a capability that does not exist. The two are
	// separate fields for the same reason [search.Resolve] returns two: a
	// binary that can search but not fetch is a real configuration, and it
	// should get exactly the one tool it can honour.
	SearchProvider search.Provider
	SearchFetcher  search.Fetcher

	// TaskAutoApproveSeconds is how long a task proposal waits before the clock
	// approves it (task.go, config.KeyTaskAutoApprove). 0 IS A CLOCK THAT IS
	// OFF — the proposal waits for [Agent.ResolveTask] and nothing else — which
	// is only a sentence a WATCHED session can honour: with nobody subscribed
	// to the events (AskConsent false, or no turn hub), the deadline approves
	// whatever this says, because a headless run has no one to wait for.
	//
	// It is seconds rather than a Duration because it is one settings row read
	// straight off the sheet, and a surface counting it down draws the same
	// number the person typed.
	TaskAutoApproveSeconds int

	// InTask marks this agent as ONE TASK NODE'S RUNNER (task_run.go) rather
	// than the conversation. It changes exactly two things, and both are
	// consequences of the same fact — there is nobody to talk to:
	//
	//   - the belt leaves off propose_task and watch (tools.go): a node does
	//     the work it was briefed with, and a watch's news has no conversation
	//     to arrive in.
	//   - a call the policy would ask about is REFUSED in the node's own words
	//     (consent.go) instead of hanging or borrowing the session's wording
	//     about a resolver that was never going to be attached.
	//
	// It is false for every conversation, and no surface sets it: the executor
	// sets it on the config it builds for a node and nowhere else.
	InTask bool

	// SpendRailUSD stops a session that has spent this much. 0 is off. The
	// check happens BEFORE a turn starts (rail.go) and reads the session's own
	// journaled usage, so the rail is exact rather than an estimate, and a turn
	// already in flight is never cut in half by it.
	SpendRailUSD float64
}

// Agent is one conversation. It is safe for concurrent use, but Submit
// serializes: a second Submit while a turn is in flight queues the message as
// an injected user message (omp's steering model), so the surface never needs
// a queue of its own, and hands back its own live channel onto that turn's
// events — every Submit streams, whether it started the turn or steered it.
// The methods live in agent.go; the loop they drive lives in loop.go.
type Agent struct {
	config Config
	client Completer
	// system is message[0] of every request: the rendered prompt, held once
	// because it is the same bytes on every step of every turn.
	system string
	tools  []bare.Tool
	// defs is the wire form of tools, built once — the belt does not change
	// during a session, and rebuilding it per step would re-marshal seven
	// schemas on the hot path.
	definitions []ai.ToolDefinition
	file        *sessionFile
	// cacheKey is this session's prompt-cache lineage, stamped on every request
	// by [sessionCompleter]. It is fixed at construction — derived from the
	// session file's id, or from a fresh one when the conversation lives only in
	// memory — and is never written after, so it needs no lock.
	cacheKey string

	// jobs is the background-command registry (jobs.go): the processes bash
	// started with background:true, alive across turns until Close.
	//
	// It is outside mu and holds its own locks. A job's lifetime is the
	// session's, not a turn's, and the goroutines watching them must never
	// contend for the lock Interrupt has to be able to take at any moment.
	jobs *jobRegistry

	// memory is the durable-notes file (memory.go), nil when Config.MemoryFile
	// is empty. Like jobs it sits outside mu and holds its own lock: its writers
	// are tool calls running in parallel inside a batch, and its one reader is
	// the per-turn prompt refresh.
	memory *memoryStore

	// mu guards everything below it. The lock is held for state transitions
	// only, never across a provider call or a tool execution: a turn that
	// holds it while waiting on the network would deadlock Interrupt, which is
	// the one call that must always be answerable.
	mu    sync.Mutex
	model string
	// reasoning is how hard each model is asked to think, by model id, and it
	// is a MAP rather than a field for the reason agent.go's block states: the
	// level is a choice about a model, and a /model switch must not carry one
	// model's answer onto another. Absent means "send nothing"; it holds no
	// EffortNone entries. Nil until somebody sets a level, which is most
	// sessions.
	reasoning map[string]provider.Effort
	messages  []ai.Message
	usage     Usage
	running   bool
	cancel    context.CancelFunc
	steering  []userMessage
	closed    bool
	// done is closed when the in-flight turn has recorded its last message,
	// non-nil exactly while running. Close waits on it so a cancelled turn's
	// tail reaches the journal before the file does.
	done chan struct{}
	// hub is the in-flight turn's fan-out, non-nil exactly while running. Every
	// Submit that lands on the turn subscribes to it, so a steering caller gets
	// a live channel of its own instead of a closed one.
	hub *eventHub
	// contextWindow is the window learned after construction — the catalog's
	// figure for a model chosen with /model, which Config.ContextWindow cannot
	// carry because the model was picked long after New. Zero means nobody has
	// said and the config's answer stands (see [Agent.window]).
	//
	// It is atomic rather than guarded by mu because the threshold is read from
	// cutPointLocked, which already holds the lock: a second acquisition there
	// would deadlock the one call — Interrupt — that must always be answerable.
	contextWindow atomic.Int64

	// compacting serializes compaction passes. One pass reads the transcript,
	// releases the lock to summarize, then rebuilds; a second pass entering
	// that window would summarize a prefix the first one is about to drop.
	compacting bool
	// contextTokens is the last provider-reported context size, the honest
	// figure when there is one. Zero means "estimate from content".
	contextTokens int

	// followups is the second injection queue (agent.go). Steering drains at a
	// step boundary INTO the running turn; a follow-up waits for the turn to
	// end and then starts one of its own.
	followups []followUp

	// consent is the questions a person owes an answer to, keyed by the id the
	// EventConsentRequest carried, and consentSeq is what names them. Both are
	// ephemeral: a request lives exactly as long as the tool call blocked on it
	// (consent.go).
	consentSeq uint64
	consent    map[uint64]chan consentAnswer
	// consentMemo is the "don't ask me again for this tool" answer, for this
	// agent's life only. It is never persisted — a session-scoped answer that
	// outlived the session would be a settings change nobody made.
	consentMemo map[string]bool

	// tasks is the work this conversation has handed off: the graph of nodes,
	// their dependency edges, and the frontier executor that runs them
	// (task_run.go). It is nil until the first proposal is admitted — most
	// conversations never groom one — and is built under mu by [Agent.graph].
	//
	// Like jobs it holds its own lock and its nodes outlive the turn that
	// proposed them. Nothing here is ever read with mu held: the graph's own
	// lock is taken by goroutines that finish minutes later, and a session lock
	// held across one of those is the lock Interrupt could not take.
	tasks *TaskGraph
	// taskAnswers is the proposals a person owes an answer to, keyed by the id
	// the EventTaskProposal carried. It is consent's pending-id machinery for a
	// question with a CLOCK: the wait ends on an answer, on the deadline, or
	// with the turn (task.go). The ids are the GRAPH's — a proposal is a node
	// that has not been admitted yet, not a second numbering.
	taskAnswers map[uint64]chan TaskAnswer
	// taskWatchers are the standing subscriptions to task updates
	// ([Agent.TaskUpdates]). They are not the turn's hub and do not close with
	// it: a node's most important event lands minutes after the turn that
	// proposed it ended, when there is no hub to send it to.
	taskWatchers []*eventStream

	// title is the session's name and titleTried marks the one attempt at
	// generating it (title.go). A resumed session loads its name from the
	// journal, so it never re-names itself.
	title      string
	titleTried bool
}
