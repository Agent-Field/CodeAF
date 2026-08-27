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
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/search"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Completer is the narrow slice of provider.Client the loop needs. It is an
// interface so tests substitute a scripted completer.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// modelChain is the OPTIONAL half of a [Completer]: which models it would move
// to when the one in hand can no longer answer, in order.
//
// It is a second interface rather than a second method on [Completer] because a
// chain is a thing only the real adapter has (internal/provider's
// FallbackModels). A completer that does not offer one — a test double, a build
// wired to no catalog and no `models.fallbacks` row — makes the hop ABSENT: the
// turn ends on the sentence it has always ended on, rather than on a capability
// that is present and fails.
type modelChain interface {
	FallbackModels(model string) []string
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
	// the CallID its forming events carried, and no Output: nothing has run.
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
	// EventToolForming says one tool call is still ARRIVING — the model is
	// spelling it out and has not finished. It is the phase BEFORE
	// EventToolAnnounced, and it exists because that gap is not instant: a long
	// write or a groomed propose_task takes seconds to stream, and a surface
	// with only the announcement draws nothing at all for them.
	//
	// It carries CallID (the call's id once the wire has said one), Tool (the
	// name once its delta has landed), Hint (a best-effort gloss built from the
	// argument fields that have CLOSED so far — "write internal/foo.go" while the
	// body of the file is still arriving), ArgsText (the raw partial arguments)
	// and Bytes (how much of them has arrived).
	//
	// NOTHING HERE IS AN INSTRUCTION. ArgsText is half-sent JSON and is never
	// parsed into Args; Hint is a scan, not an unmarshal; and forming NEVER
	// implies execution — a formed call has not been announced, let alone begun,
	// let alone consented to.
	//
	// ORDERING: forming (zero or more, per call) → EventToolAnnounced →
	// EventToolBegin, keyed by CallID. Every call that forms is announced and
	// begun in that order; calls in a parallel batch interleave with each other,
	// but each call's own sequence holds. A non-streaming provider forms nothing,
	// so a surface that ignores this kind is exactly what it was.
	EventToolForming
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
	// EventNotice carries one line in Text about what the turn's own machinery is
	// doing to make the request land — not the model's words, and not a failure.
	//
	// Its one source today is the provider's endpoint-refusal chain
	// (internal/provider's endpoints.go): "Retry 1/3: removed max_tokens",
	// "Retry 3/3: Falling back to <model>". Those retries change the shape of the
	// request a person asked for, so a surface that drew nothing for them would
	// be showing an answer without showing what it cost to get one.
	//
	// It is a NOTE, like EventNudge: dim, one line, never an interruption. It can
	// arrive before any text on the turn, and a turn may end in EventError with
	// several of these already on screen — that sequence is the chain trying
	// everything it had and saying so.
	EventNotice
	// EventConnectAsk asks the person whether one of their accounts may be
	// connected (connect.go). It carries the id the answer is handed back with in
	// ConnectID, and the account in Service and ServiceName — "google" and
	// "Google", the word the tools use and the word a person reads.
	//
	// It is a QUESTION, and the same kind of question a consent prompt is: the
	// use_service call is blocked inside the tool batch until
	// [Agent.ResolveConnect] answers it, the five-minute clock runs out, or the
	// turn's context dies. A surface that ignores this kind leaves the call
	// waiting until one of those three happens, and a clock that runs out is a NO.
	EventConnectAsk
	// EventConnectAuth carries the page the person opens to say yes to the
	// service named in Service: the address is in AuthURL.
	//
	// It is an INSTRUCTION to the surface — open this — and it arrives only after
	// the person has already agreed to connect the account. It is followed by
	// exactly one EventConnectDone, whatever happens next.
	EventConnectAuth
	// EventConnectDone ends one connect attempt for the service in Service:
	// Account is the address it connected as, and Failed says it did not connect
	// at all. The two are exclusive — a failure carries no account — and a
	// person who simply walked away shows up here as a failure, because from
	// this side an attempt nobody finished and an attempt that broke are the same
	// fact: nothing is connected.
	EventConnectDone
	// EventHarnessOffer asks the person whether one sub-harness should take this
	// turn (harness.go). It carries the id the answer is handed back with in ID,
	// the harness's name in Text, and its one-sentence description in Hint.
	//
	// Model is the model the turn NAMED — "research this with opus" — resolved
	// to an id this install has, and empty when nobody said. ModelNote is the
	// other half of that: a word that named no model here, said in words a
	// surface prints as it stands. Neither is a refusal; the offer is the same
	// offer either way.
	//
	// It is a QUESTION, and the quietest kind on this list: the turn is held
	// before its first request until [Agent.ResolveHarness] answers it or the
	// turn's context dies, and NO is free — the turn the person typed runs
	// exactly as it would have. A surface that ignores this kind would leave the
	// turn waiting, which is why the offer is never raised unless somebody has
	// said they are watching (Config.AskConsent).
	EventHarnessOffer
	// EventHarnessRun says the person said yes and the harness named in Text has
	// the turn. Hint is its description, and Model is what it is running on when
	// the turn named one.
	//
	// It is a REPORT, not a question, and it is what a surface draws instead of
	// a model thinking: what follows is the harness's report as ordinary text
	// and then EventTurnDone, or EventError if the run failed.
	EventHarnessRun
	// EventHarnessStep is one step of a running sub-harness, the instant it
	// lands: Step is the walk's own trail entry (subharness.RunWatched) and ID is
	// the run it belongs to — the id EventHarnessRun carried.
	//
	// It is a REPORT and it is DISPLAY-ONLY. A run takes minutes, and between the
	// announcement and the report there was nothing on screen saying which part of
	// it was happening. Nothing here is recorded: the report that follows carries
	// the whole trail (subharness.RunCard), so a step kept in the transcript would
	// be the same news written down twice.
	EventHarnessStep
	// EventHarnessDesign says a turn asked for a sub-harness to be BUILT — "make
	// a harness for triaging flaky tests" — and the design has started
	// (harness_build.go). Text is the goal, less the words that asked for it;
	// Hint is "designing"; Model is what the design is thinking with.
	//
	// TASK NAMES THE NODE IT RUNS AS, and it is the only field on this kind a
	// surface can act on. A design is a task now (harness_task.go): it has an id
	// a person can say out loud, a room they can walk into, and a stop. So the
	// one line this event draws names it — "harness · designing X — task 4" —
	// and everything else about the design's life arrives on the task lane, not
	// this one. Only ID is filled.
	//
	// It is a REPORT and it does not hold the turn: the turn is already over when
	// it arrives, because designing takes a minute and a conversation held on one
	// is a conversation nobody can use. Exactly one of EventHarnessDesignDone or
	// an EventNotice saying why not follows it, on the standing lane
	// ([Agent.HarnessDesigns]) as well as on the turn's stream.
	EventHarnessDesign
	// EventHarnessProgress reports one live snapshot of a harness design call.
	// It is display-only: partial JSON and reasoning never enter the transcript.
	// Goal names the request; Phase is designing or reviewing; Attempt and
	// Attempts size the retry ladder. ThoughtTail is the recent reasoning, Hint
	// is the best meaning recovered from partial JSON, Bytes is content received,
	// and Stalled says no delta has arrived for ten seconds.
	EventHarnessProgress
	// EventHarnessDesignDone carries a finished design in Harness, with the id
	// the answer goes back through in ID, the name in Text and the description in
	// Hint.
	//
	// It is a QUESTION — the only one on this list that outlives the turn that
	// raised it. A surface draws the page (subharness.CardLines is the renderer
	// every surface shares) and answers through [Agent.ResolveHarness], the same
	// method an offer is answered with: TRUE SAVES IT into the registry, false
	// drops it. Nothing is written before that answer, and a surface that ignores
	// this kind saves nothing — which is the same posture EventHarnessOffer
	// keeps, one lane over.
	EventHarnessDesignDone
	// EventHarnessDesignRevising WITHDRAWS a design card the person asked to have
	// changed. ID is the design it is about and Text is the change, in the
	// person's own words as the design's thread passed them on (harness_task.go's
	// revise_design).
	//
	// IT EXISTS BECAUSE A QUESTION CAN BE OVERTAKEN BY A THIRD ANSWER. A design
	// card is one decision behind two doors — the card in the conversation and the
	// approval row in the design's own room — and there is a third thing a person
	// can do with a page, which is to say what is wrong with it. When they do, the
	// page that card is about stops existing, so the card has to come down: left
	// standing it would be a save key over a draft that has been replaced, and the
	// answer it took would save the wrong page.
	//
	// A surface takes the card back to the LIVE form it wore while the page was
	// first being written, because that is what is happening again — the designer
	// is at work, EventHarnessProgress starts arriving, and exactly one
	// EventHarnessDesignDone follows it with the rewritten page. The design's own
	// ROW needs nothing from this kind: the node moves back to the "designing"
	// phase on the task lane, and the approval row is drawn off that phase.
	EventHarnessDesignRevising
	// EventOrchestrateNote carries one planner note from an adaptive run
	// (internal/orchestrate): Text is the note, ID the run. A REPORT; the room
	// draws it as the thin thinking-row between completions.
	EventOrchestrateNote
	// EventOrchestrateFuel is the gauge and its early warning: Text is the
	// spend summary ("$1.60 of $2.00"), Hint holds the cap. A REPORT at the
	// 80% mark and whenever a surface asks; it never blocks anything.
	EventOrchestrateFuel
	// EventOrchestratePause says the run hit its fuel cap: in-flight nodes
	// finished, nothing new launched, the frontier is frozen mid-shape. Text
	// is the spend summary. It is a QUESTION answered through
	// [Agent.ResolveOrchestrate] — top up, finish with what we have, or stop —
	// and until that answer the run sits in its Paused state, resumable.
	EventOrchestratePause
	// EventToolFinished says ONE call's own work is over, the instant it is
	// over, and carries how long that call took in Took.
	//
	// It is a CLOCK EVENT and nothing else: the result is not in it, and the row
	// is not closed by it. The result still arrives as EventToolEnd or
	// EventToolFailed, after the whole batch has finished, in call order — the
	// order the transcript is written in.
	//
	// It exists because those two moments are not the same moment. A batch's
	// calls run together and finish in any order, so a `cd` that took five
	// milliseconds sat under a spinner and a climbing clock until the slowest
	// call beside it returned, and then claimed that whole span as its own
	// duration. The row was reading the BATCH's clock. This is the call's own,
	// measured where it ran (loop.go's executeTool), so a surface can stop the
	// row's clock and state the figure the call actually cost.
	//
	// A surface that ignores this kind is exactly what it was.
	EventToolFinished
	// EventRetrying says THIS STEP IS BEING ASKED AGAIN, and that whatever the
	// dead attempt streamed is void. Text carries the one line explaining why —
	// "nothing came back from the model — asking again", "the reply lost its
	// thread — that text was dropped, asking again".
	//
	// It fires when the stream guard cut a request (internal/provider's
	// streamguard.go): the endpoint went quiet, or the reply stopped being
	// language. The turn loop has already thrown away that attempt's partial
	// text, its early reads and its half-arrived calls, so A SURFACE MUST THROW
	// AWAY WHAT IT DREW FOR THEM TOO — everything after the last thing the person
	// typed belongs to a response that will never exist, and leaving it on screen
	// would show half a dead answer above the live one.
	//
	// It is also the one place a surface learns that a wait is a RETRY rather
	// than a first attempt, which is the difference between "waiting for" and
	// "trying again". It never ends a turn: either the next attempt streams, or
	// EventError arrives with the sentence about giving up.
	EventRetrying
	// EventStandingProposal asks the person whether one standing item — a
	// reminder, a watch, a rule, an overnight job — may stand (standing_contract.go).
	// Standing carries the card; the ID inside it is the token a surface hands back
	// to [Agent.ResolveStanding]. Nothing stands until the answer is yes.
	EventStandingProposal
	// EventStandingUpdate reports a standing item changing under a live window: it
	// was ratified, it fired, it was paused, retired, or it needs the person. It is
	// a report, never a question.
	EventStandingUpdate
	// EventSubharnessAsk is a running subharness putting one question to the
	// person (subharness_env.go, the Env's ask() door). ID is the run's task
	// node, Text is the question in the program's own words, and Args carries
	// the answers it offers as a JSON array when it offers a set.
	//
	// It is a QUESTION and it arrives IN THE RUN'S ROOM, which is where the run
	// lives: its journal, its progress and its ✕ are all there already, and a
	// question about the work belongs beside the work. A person who is not in the
	// room learns about it from the ROSTER, because the node moves to the
	// "awaiting your look" phase for exactly as long as the question stands —
	// the same phase a design waiting on its card wears, for the same reason.
	//
	// It is answered through [Agent.AnswerSubharness], which takes the run's id,
	// what they said, and whether they are TAKING OVER. A surface that ignores
	// this kind leaves the run waiting until the node is stopped or the session
	// closes, which is why the question is only ever put where somebody is
	// watching (Config.AskConsent) — an unattended run answers from what the gate
	// declared or stops incomplete, and never guesses.
	EventSubharnessAsk
	// EventSubharnessStep is one host call a running subharness just made
	// (internal/exec's JournalEntry): ID is the run's node and Step carries the
	// entry whole — which door, what it was about, what it cost.
	//
	// It is a REPORT and it is DISPLAY-ONLY, on EventHarnessStep's terms: the
	// journal is the permanent record, this is how a person watches it being
	// written. Nothing here is recorded in any transcript.
	EventSubharnessStep
	// EventSubharnessProposal asks whether one saved program should take this
	// piece of work (tools_subharness.go). ID is the token an answer goes back
	// through, Text is the program's name, Hint is what it is for, and
	// Subharness carries the INTAKE CARD — every input field, what this
	// conversation already answers, and which required blanks are left.
	//
	// It is a QUESTION and it is the one on this list with NO CLOCK THAT
	// APPROVES. A task proposal's countdown ends in a yes because it is a window
	// to redirect ordinary work; this one may not, because a program that ran
	// because nobody answered would be exactly the silent auto-execution the
	// whole path is built to prevent (docs/SUBHARNESS-PRD.md §9). It is answered
	// through [Agent.ResolveSubharness] — true runs it, with the form as the
	// person left it — and a surface that ignores this kind runs nothing at all,
	// which is the correct behaviour rather than a degradation.
	EventSubharnessProposal
	// EventSubharnessProposalOff takes the card named by ID back down. Nothing
	// ran, and nothing about the person's own intentions is being reported: the
	// tool call that raised the card has let the turn go, either because its
	// window expired or because the turn it belonged to was interrupted
	// (tools_subharness.go).
	//
	// IT EXISTS SO THAT A CARD CANNOT OUTLIVE ITS LISTENER. The window is a
	// bound on the TOOL CALL and not a deadline on a person, so it fires while
	// the card is still on somebody's screen — and a card left standing after it
	// would be a `run it` that resolves nothing, silently, which is the one
	// ending a question is never allowed to have. A surface that ignores this
	// kind leaves that dead card up; a surface that draws it takes the card down
	// and says so.
	EventSubharnessProposalOff
	// EventTaskReplyTags names the finished tasks whose notes the next words
	// answer. TaskReplyTags carries them in note order.
	EventTaskReplyTags
	// EventSteerAccepted says one sentence the person typed INTO the running
	// turn is on the queue and will reach the model at the next step boundary
	// (steer.go). Steer carries its identity, its words and the instant it was
	// sent; nothing is in the transcript yet.
	//
	// It is a PROMISE AND NOT AN OUTCOME, which is why exactly one of the two
	// kinds below always follows it on the same stream: the boundary it is
	// waiting for may never come.
	EventSteerAccepted
	// EventSteerConsumed says the model HAS BEEN GIVEN that sentence: it is in
	// the transcript as user content of the turn it was typed into, and the
	// request carrying it is the next thing that goes out. Steer names which
	// steer landed.
	EventSteerConsumed
	// EventSteerFellThrough says the turn ENDED FIRST — it answered, it faulted,
	// or somebody stopped it — with that sentence still waiting, so no request of
	// that turn ever carried it. The words are not lost and they did not steer
	// anything: they move to the queue that holds a message waiting for a turn of
	// its own, and the stream the steer was sent on carries that turn when it
	// starts (steer.go states the whole law).
	EventSteerFellThrough
)

// TaskReplyTag is the task identity a surface places beside the answer its
// completion prompted. Request is the person's original text, not the brief.
type TaskReplyTag struct {
	ID      uint64 `json:"id"`
	Title   string `json:"title"`
	Request string `json:"request,omitempty"`
}

// Event is one observable thing in a turn. A Submit returns a channel of
// them, closed after EventTurnDone or EventError.
//
// A STEER'S CHANNEL IS THE ONE EXCEPTION, and it is exact: [Agent.Steer] hands
// back a stream that outlives the turn it was sent into when the steer falls
// through, so EventSteerFellThrough arrives AFTER that turn's EventTurnDone or
// EventError, and the turn the words then start speaks on the same channel
// (steer.go says why). A caller that reads to close — which is every caller
// today — sees all of it in order and needs no second rule; a caller that stops
// at the terminal event stops at the terminal event of the FIRST turn.
type Event struct {
	Kind          EventKind
	Text          string
	Tool          string
	Hint          string
	Err           error
	Usage         Usage
	TaskReplyTags []TaskReplyTag

	// Args is the tool call's arguments rendered for display: the JSON the
	// model sent, compacted to one line and capped. It is set on
	// EventToolBegin, EventToolEnd and EventToolFailed. Arguments that do not
	// parse as JSON pass through as the raw text — a malformed call is still a
	// call the person should be able to look at.
	//
	// THE CONTRACT IS THAT THIS STAYS PARSEABLE WHENEVER THE WIRE ARGUMENTS
	// WERE, at every size. The cap ([argsLimit]) is spent INSIDE the oversized
	// string values, each of which then ends in the marker [capBytes] writes —
	// `… (12345 more bytes)` — rather than by cutting the text, which would end
	// a 20k write's payload in the middle of a string literal and leave every
	// reader downstream calling a well-formed call malformed.
	//
	// So a field a surface reads back out of this may be SHORTER than the one
	// the model sent, and says so in its own last bytes. Anything derived from
	// one is a floor rather than a figure: a capped write's line count is "at
	// least this many", and internal/tui3 spells that with a trailing `+`.
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

	// HarnessMade says this step's failure was written by the HARNESS and not by
	// the world the model reached for: a hand that was withdrawn (withdrawn.go),
	// a door that refused the call (consent.go and the rest of the pre-action
	// chain). It is set on EventToolFailed and on nothing else.
	//
	// IT EXISTS FOR THE COUNTERS. A stuck detector's whole claim is that a step
	// which taught nothing was a step the model had no business taking, and that
	// claim is false when the harness wrote the answer itself — measured in
	// SWE-Marathon s4, where the harness withdrew `bash`, answered eight retries
	// with "Unknown tool", and then injected three [stuck] notes blaming the
	// model for the retries (withdrawn.go states the whole failure). A surface
	// may show it or ignore it; the runner reads it to keep the harness's own
	// steps out of the model's ledger ([runTaskChild]).
	HarnessMade bool

	// ID names one EventConsentRequest, and is the token a surface hands back
	// to [Agent.ResolveConsent]. It is zero on every other kind but
	// EventHarnessOffer, whose own id goes back through
	// [Agent.ResolveHarness] — two lanes, two counters, and one field, because
	// "which question" is the same question for both of them.
	ID uint64

	// CallID is the PROVIDER's id for the tool call an EventToolForming, an
	// EventToolAnnounced or an EventConsentRequest is about — the same string
	// the tool result carries — and is empty on every other kind. It is empty on
	// a forming event too until the wire has sent one, which is the first
	// fragment in practice and nothing the consumer may assume.
	//
	// ON A CONSENT REQUEST IT IS WHICH CALL IS BEING ASKED ABOUT. A surface pairs
	// the question to the row it draws the question under, and the card reads the
	// command it is about to remember off that row — so a question paired by tool
	// name alone can, with two bash calls in flight, show one command and bank a
	// standing rule for the other (internal/session's consent.go).
	//
	// It is on BOTH ends of that pair on purpose: forming and announced are two
	// states of one call, and the id is what lets a surface say so. Without it
	// the announcement can only be paired by tool name, and a batch of parallel
	// calls of the same tool has no name to tell its rows apart by.
	//
	// It is not [Event.ID] because that field is the consent lane's own token, a
	// uint64 this session mints; these are two different names for two different
	// things and folding them would make "which call" and "which question"
	// the same field with two answers.
	CallID string

	// ArgsText is the RAW, PARTIAL arguments text of a forming call: exactly what
	// the provider has streamed so far, uncompacted and unparsed. It is set on
	// EventToolForming and empty everywhere else — Args is the display JSON of a
	// WHOLE call, and half of a JSON object is not that.
	//
	// It is CUMULATIVE: every fragment carries the whole text that has arrived so
	// far, not the piece that just landed, so a surface keeping it replaces what
	// it held rather than appending to it. It is capped at [formingArgsLimit]
	// from the FRONT, and Bytes beside it is the honest size of the whole.
	//
	// A surface may show it, cut it, or ignore it. NOTHING MAY UNMARSHAL IT — and
	// nothing needs to: [PartialString] is the tolerant read of one field's
	// streamed text, and it is one scanner in one place rather than a second
	// parser per surface.
	ArgsText string

	// Took is how long ONE tool call's own work took, on EventToolFinished and
	// zero on every other kind. It is measured around the tool's execution and
	// around nothing else: not the wait for a consent question, and not the wait
	// for the rest of the batch.
	Took time.Duration

	// Bytes is how much of a forming call's arguments has arrived. It is the
	// length of ArgsText, carried as its own field so a surface can show progress
	// ("write · 4.2 KB") without measuring text it may have chosen not to keep.
	Bytes int

	// Harness progress fields ride on EventHarnessProgress alone. They are flat
	// because the event is already the transport envelope and every field is a
	// short fact a surface may independently omit.
	Goal        string
	Phase       string
	Attempt     int
	Attempts    int
	ThoughtTail string
	Stalled     bool

	// Step is one finished step of a RUNNING sub-harness, on EventHarnessStep
	// alone and nil on every other kind. It is the walk's own trail entry rather
	// than a copy of the parts of it a surface might want, so the row drawn while
	// the run happens and the row on the card read back afterwards are rendered
	// from one fact (subharness.StepLine).
	Step *subharness.Trail

	// Entry is one host call a running SUBHARNESS just made, on
	// EventSubharnessStep alone and nil on every other kind
	// (internal/exec's JournalEntry). It is the journal's own entry rather than a
	// copy of the parts of it a surface might want, for the reason Step above is
	// the trail's: the row drawn while the run happens and the row read back out
	// of the journal afterwards are one fact rendered twice.
	Entry *exec.JournalEntry

	// Task carries one EventTaskProposal or EventTaskUpdate's payload
	// (task_contract.go). It is nil on every other kind, and the ID inside it
	// is the token a surface hands back to [Agent.ResolveTask].
	Task *TaskNotice

	// Standing carries one EventStandingProposal or EventStandingUpdate's payload
	// (standing_contract.go). It is nil on every other kind.
	Standing *StandingNotice

	// Subharness carries one EventSubharnessProposal's intake card
	// (subharness_contract.go). It is nil on every other kind, and the ID beside
	// it is the token a surface hands back to [Agent.ResolveSubharness].
	Subharness *SubharnessCard

	// Steer carries one sentence spliced into a running turn, on
	// EventSteerAccepted, EventSteerConsumed and EventSteerFellThrough alone; it
	// is nil on every other kind (steer.go). The same [SteerNote] value rides
	// all three, so a surface pairs the outcome with the row it drew on the
	// acceptance by [SteerNote.ID] and never by matching the words.
	Steer *SteerNote

	// Rule is the approval policy's own phrasing of why a call is being asked
	// about — `bash pattern "rm -rf *"`, `tool "edit"`, `default`. It is set on
	// EventConsentRequest and empty elsewhere. The wording is the policy's
	// (internal/approval) so that every surface says the same sentence about the
	// same rule instead of deriving one.
	Rule string

	// Memo says whether a ConsentToolSession answer to this question WOULD DO
	// ANYTHING. It is set on EventConsentRequest and false everywhere else.
	//
	// It exists because the consent lane carries two different questions. The
	// gate's question is about a TOOL, so "and stop asking me about this tool"
	// is a real answer and this is true. The stuck question (recovery.go)
	// borrows the same lane to ask about a TURN, and a tool-session scope on it
	// is dropped on the floor — which, without this field, a surface could not
	// know, and so offered an option that silently did nothing. An offer that
	// is inert must not be on screen: it is worse than a missing key, because a
	// person who presses it believes they have changed something.
	Memo bool

	// Count is how many times the thing this event is about has happened. It is
	// set on EventNudge — the number of repetitions that earned the nudge — and
	// zero everywhere else, which is why it is a plain int rather than a pointer:
	// no other kind has a count, and "0" is not a count any kind reports.
	Count int

	// The five fields of the three connect kinds (connect.go). They are flat
	// rather than a payload struct because the three events between them carry
	// five short strings and a bool, and a surface drawing the sequence reads
	// them one after another off the same event.
	//
	// ConnectID names one EventConnectAsk and is the token handed back to
	// [Agent.ResolveConnect]. Service is the account's id — "google" — on all
	// three kinds; ServiceName is the word a person reads — "Google" — on the
	// ask. AuthURL is the page to open, on EventConnectAuth only. Account and
	// Failed are the outcome, on EventConnectDone only.
	ConnectID   string
	Service     string
	ServiceName string
	AuthURL     string
	Account     string
	Failed      bool
	// NeedsKey rides on EventConnectAsk alone and says that this account is
	// not connected in a browser but with a key the person already holds. A
	// surface that sees it asks for the key and hands it back through
	// [Agent.ResolveConnectKey]; a plain yes means nothing here, because
	// there is nothing to open and no page to say yes on.
	//
	// No EventConnectAuth follows a NeedsKey ask, ever: the next thing is
	// the EventConnectDone that says whether the key was good.
	NeedsKey bool

	// Model is which model a harness offer would run on, and the one it did run
	// on: set on EventHarnessOffer and EventHarnessRun, empty everywhere else
	// and empty on both of those when the turn named no model (harness.go).
	//
	// It is a RESOLVED ID and never the person's word — "opus" arrives here as
	// anthropic/claude-opus-5 — so a surface draws what will actually be sent
	// rather than what somebody typed.
	Model string

	// Harness is the page one EventHarnessDesignDone is asking about, and nil on
	// every other kind. It is the whole harness rather than a rendering of one
	// because the rendering is shared (subharness.CardLines): a surface draws the
	// same card the tool prints and the panel lists, and a session that shipped
	// pre-rendered lines would have made itself the second renderer.
	//
	// It is a POINTER so that "no design here" is spelled once, and the value it
	// points at is this event's own copy — nothing else holds it, and answering
	// the question is what decides whether it is ever written down.
	Harness *subharness.Harness

	// ModelNote is why a model the turn NAMED is not in Model: a word no model
	// here answers to, a word too many of them answer to. It is set on
	// EventHarnessOffer alone.
	//
	// The words are this package's, on the same terms Rule's are: a note about
	// a model this session could not find should read the same on every
	// surface, and a surface that phrased it itself would be writing a sentence
	// about a catalog it did not consult.
	ModelNote string
}

// Usage is token and cost accounting for one turn or the session total.
type Usage struct {
	Input    int
	Output   int
	CostUSD  float64
	Duration time.Duration
	Turns    int

	// Calls is EVERY request this session made to a provider — the turn's own
	// steps and the auxiliary calls beside them: the namer, the guardian, a
	// memory reflex, a look at a picture, a whole child agent folded in.
	//
	// It is a second counter rather than a wider Turns because Turns has a law
	// of its own that other code is written against: it counts steps of the
	// CONVERSATION, so a turn that used three tools reads as one turn with
	// three steps and the title call that followed it reads as nothing. Calls is
	// the honest denominator for "how many requests did this cost me", which is
	// a different question and the one a person asking about the bill is asking.
	Calls int

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

// Config builds one agent. The zero value is invalid: Workspace, Model and
// BaseURL are required. APIKey may be empty for a session opened before the
// person has handed one over — the first-run setup's case — and every request
// refuses until [Agent.SetAPIKey] lands it.
type Config struct {
	Workspace string // tools root here; all relative paths resolve inside it
	Model     string
	APIKey    string
	BaseURL   string

	// There is no app-attribution field here any more. The three that used to
	// be forwarded to the provider client — a referer, a title, a category
	// list — were three fields every new construction site had to remember to
	// copy, and the ones that forgot spent their tokens under no app at all.
	// The values are constants the provider stamps for itself
	// (provider.ApplyAttribution), so nothing above it carries them.

	// System is the rendered system prompt. Empty renders the package's
	// embedded default (prompts/system.md + the project footer) for
	// Workspace and Model.
	System string

	// ContextWindow is the model's window in tokens; compaction fires at
	// window − max(15% of window, 16384). Zero selects a conservative default.
	ContextWindow int
	// ContextWindowFor answers from the catalog owned by the machine running
	// the session. A model switch consults it there so a remote surface's
	// different catalog cannot move this engine's compaction point.
	ContextWindowFor func(model string) int

	// Routing is how this session asks the router to choose among the endpoints
	// serving its model, and whether it times them at all (internal/provider's
	// velocity.go). EMPTY IS LATENCY, the default the settings row carries, so a
	// caller that says nothing still gets the fastest endpoint the router can
	// find and still measures what it actually got.
	Routing provider.RoutingStrategy

	// CompactEnabled gates automatic compaction. Manual compaction via the
	// surface's /compact is a surface concern and always available through
	// Compact.
	CompactEnabled bool

	// SessionFile is the JSONL transcript: header line, then one line per
	// journaled message and compaction marker. Empty keeps the conversation
	// in memory only. If the file exists it is loaded on New and the
	// conversation resumes after the latest compaction marker.
	SessionFile string

	// Place is the session folder and everything inside it (place.go,
	// Decision 26). The zero Place is the legacy flat layout: sidecar paths
	// keep deriving from SessionFile, droppings keep landing in the
	// workspace's .aforge-v3, and nothing changes for a caller that has not
	// adopted the folder. When set, SessionFile and Place.Transcript() name
	// the same file.
	Place Place

	// ArtifactsIndex is the global deliverables index (artifacts.go): the file
	// one row is appended to whenever this session produces something a person
	// might want to find again — a generated picture, an exported conversation.
	// Empty records nothing, which is what a test and a headless --once both
	// want.
	//
	// It is the caller's path rather than one this package derives, for the
	// reason SessionFile is: where a person's state lives is the surface's
	// decision. The surface's answer is ~/.aforge/v3/artifacts.jsonl, resolved
	// through internal/home so AFORGE_HOME moves it with everything else.
	ArtifactsIndex string

	// Memory is the brain this session remembers into (memory.go): the store's
	// event-sourced memories, routed into the prompt before a turn and written
	// after one. NIL IS MEMORY OFF — no <memory> block, no reflex call, and no
	// `remember` on the belt, so the model does not have the verb.
	//
	// It is the caller's store rather than one this package opens, for the
	// reason SessionFile is a path rather than a directory: where a person's
	// state lives is the surface's decision, and the door is also where the
	// memory.enabled row is read. A door that turns memory off hands nothing
	// here, which is what makes "no calls" structural.
	Memory *store.Store

	// MemoryImport is the legacy memory.md this session carries into the store
	// on its first turn, once, before it is renamed to memory.md.imported
	// (memory.go). Empty imports nothing, which is every caller but the v3 door
	// and every machine that has already been through it.
	MemoryImport string

	// ApprovalPolicy decides whether a tool call runs, asks, or is refused
	// (internal/approval, gated in consent.go). NIL ALLOWS EVERYTHING, which is
	// the behavior every caller had before the gate existed: a headless --once
	// and the tests run exactly as they did, and a surface opts into the policy
	// by handing one over.
	//
	// It is the policy this session STARTS on and not the one it is stuck with:
	// a surface that banks a rule mid-conversation replaces it with
	// [Agent.SetApprovalPolicy] (approvalgate.go). This field itself is never
	// written after New, which is what lets task_run.go copy the whole config
	// without a lock.
	ApprovalPolicy *approval.Policy

	// AskConsent says somebody is watching this agent's events and will answer
	// an EventConsentRequest with [Agent.ResolveConsent].
	//
	// It is the difference between a question and a hang. Left false — a
	// headless caller, a cron run, --once — a policy's "prompt" decision denies
	// the call with a result the model can act on, instead of blocking the turn
	// on a question that will never reach a person.
	AskConsent bool

	// HarnessCards says a surface in THIS PROCESS holds the harness lane — the
	// standing subscription every card raised on it is drawn from
	// ([Agent.WatchHarnessDesigns]) — and will answer what arrives there.
	//
	// IT IS NOT AskConsent SAID TWICE, and the difference is a road rather than
	// a mood. AskConsent is about the TURN'S OWN STREAM: an approval, a connect
	// offer, a task proposal, all of which cross a connection as ordinary events,
	// which is why `aforge --host` sets it (cmd/aforge's engine.go). The harness
	// lane does not cross — the surface on the far end of that wire holds a
	// remote handle with no WatchHarnessDesigns on it — so a card raised there
	// would be raised into an empty room and expire unseen. engine.go already
	// says exactly this in prose about the DESIGN card, which it switches off by
	// nilling HarnessStore; this field is that same fact with a name, for the
	// lane's other card ([Agent.canProposeSubharness]).
	//
	// LEFT FALSE IT TAKES THE VERB AWAY RATHER THAN BREAKING IT, which is this
	// belt's law (tools.go): a model told it can offer a saved program plans
	// around that ability for the rest of the conversation, long after the first
	// offer nobody could answer.
	HarnessCards bool

	// Guardian turns on the small model that answers a "prompt" decision before
	// the person is asked at all (guardian.go). FALSE IS THE DEFAULT AND THE
	// ONLY SAFE ONE: this is a gate that answers on somebody's behalf, and a
	// caller that has not said so must never get one. Nothing about the gate
	// changes when it is off — not one extra call, not one extra branch a person
	// can observe.
	// TaskAudit gates the verified frontier (task_audit.go): when false, a
	// finished node merges on its own report — faster and cheaper, and
	// 'done' stops meaning 'proven'. The config row (task.audit) defaults on.
	TaskAudit bool
	Guardian  bool

	// ReplyGuardOff turns off the watch on replies that stop being language
	// (internal/provider's streamguard.go). The config row (reply.guard)
	// defaults ON, and this field is spelled as the OFF state so that a Config
	// nobody filled in keeps the guard rather than silently losing it.
	//
	// It says nothing about the silence watchdog beside it, which has no switch.
	ReplyGuardOff bool

	// TaskSettle is who decides a task that landed needing a look — the
	// `task.settle` row, as the person set it ([TaskSettle]). Empty is
	// [TaskSettleAsk], which is the default and the only value a caller that has
	// said nothing may get: a session must not start settling work on somebody's
	// behalf because a field was left blank.
	//
	// It changes exactly one string — the landing note a settled node writes to
	// whoever asked for the work (task_run.go's [taskNote]). Nothing about the
	// three answers changes: the tool takes the same verbs and the surface offers
	// the same choices whichever way this is set.
	TaskSettle string

	// Standing is the ambient side (standing_contract.go, internal/standing).
	// Nil is off: no belt tool, no card, no ticking from this process.
	Standing *Standing

	// standingItems overrides where [Standing.Store] would be read, and it is
	// unexported because it exists for THIS PACKAGE'S TESTS and for nothing
	// else: the store is a concrete *standing.Store on the seam a door fills,
	// and a test that wants to watch what a ratified card actually writes needs
	// a fake behind the same three methods (tools_standing.go's standingStore).
	standingItems standingStore

	// ProfileDir is the person's profile directory — the one holding the
	// config.json that /settings writes (internal/config's settings registry).
	// It is what the settings and change_setting tools are a door onto
	// (tools_settings.go): the model can read the person's settings back and
	// change one permanently, by the row's own registry key and through the
	// row's own validated write.
	//
	// EMPTY KEEPS BOTH TOOLS OFF THE BELT, on the absence law every conditional
	// family here states: a settings tool with no profile behind it would answer
	// every call with the same refusal, and a model told it can change a setting
	// will plan a whole reply around one. A headless --once, a task node and
	// every test get exactly what they had before this field existed.
	//
	// It is the caller's path rather than one this package derives, for the
	// reason SessionFile is: where a person's state lives is the surface's
	// decision, and a package that resolved ~/.aforge itself would write there
	// from a test.
	ProfileDir string

	// failures is the tally the piece of work this agent belongs to keeps of its
	// own failures (internal/taxonomy). A worker built for a task node carries
	// its node's; a conversation carries none, and every method on the type
	// tolerates the nil.
	failures *taxonomy.Tally

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

	// SupportsParameter answers whether a model accepts a request field, and
	// whether anybody knows (internal/catalog's SupportsParameter states the two
	// bools). The adapter asks it before it lets an optional knob travel, so a
	// reasoning level set on a model that publishes no reasoning parameter is
	// simply not sent instead of narrowing the endpoint set to nothing.
	//
	// NIL IS "NOBODY KNOWS", which is not the same as "no": an unwired seam
	// leaves the adapter's own explicit-only rule in force, which is exactly the
	// behaviour every caller had before this field existed.
	SupportsParameter func(model, parameter string) (bool, bool)

	// ModelPrice is a model's own published list price, per token in US dollars,
	// and whether anybody published one (internal/catalog's PriceNow). The
	// adapter bounds a latency-sorted request against it, so this session asks
	// for the fastest endpoint that is not also charging several times what the
	// model itself costs.
	//
	// NIL IS "NO PRICE IS KNOWN", which sends no ceiling and routes exactly as an
	// unwired session always did.
	ModelPrice func(model string) (prompt, completion float64, known bool)

	// TaskProgressCheck is the test seam for leash checkpoints. Production uses
	// the node's ordinary read-only checker; a test may answer deterministically.
	TaskProgressCheck func(brief string, evidence []string) (working bool, reason string)
	// TaskDeadline overrides one checkpoint interval. Zero keeps the one-hour
	// production interval and lets deadline behavior be tested without an hour.
	TaskDeadline time.Duration
	// HarnessDesignWindow overrides how long a sub-harness design is given to
	// WRITE ITS PAGE (harness_build.go's harnessDesignWindow). Zero keeps the
	// half-hour production window. It bounds the writing only — the card that
	// follows waits on the person for as long as they take — and it is settable
	// for TaskDeadline's reason: what happens at the end of the window is worth a
	// test, and half an hour is not a thing a test can wait for.
	HarnessDesignWindow time.Duration

	// ModelFallbacks are the models a turn moves to, in order, when no endpoint
	// serving this session's model will accept the request's shape at all
	// (internal/provider's endpoints.go). It is the person's own models.fallbacks
	// row; empty means the catalog is asked for the nearest same-class model
	// instead, through NearestModels.
	ModelFallbacks []string

	// NearestModels names the models closest to one that just refused
	// everything. It is consulted ONLY when ModelFallbacks is empty, and it never
	// waits: a catalog that has not resolved answers nil, and a chain with no
	// fallback simply ends in the diagnosis instead of on another model.
	NearestModels func(model string) []string

	// Harnesses is this build's sub-harness registry, in the fields a turn is
	// matched against: name, description, and the cue list the designer froze at
	// build time (internal/subharness). EMPTY IS DETECTION OFF, which is every
	// caller that has not loaded a registry, and it is off at the cost of one
	// length check per turn.
	//
	// The whole registry is handed over rather than a path to it for the reason
	// SessionFile is a path and not a directory this package picks: where the
	// entries come from is the surface's business, and a package that read
	// ~/.aforge/harnesses itself would read it from a test and from a task
	// node's own agent too.
	Harnesses []subharness.Entry

	// RunHarness runs one harness for one turn and returns its report. The name
	// is an entry's own Name; the text is the person's words, verbatim — less
	// the clause that chose the model, when they wrote one.
	//
	// The model is what the turn asked the run to ride, resolved against
	// TaskModels (harness.go). EMPTY IS THE ORDINARY CASE and means nobody
	// said: the runner uses whatever model it was built on, which is what every
	// run did before a turn could name one.
	//
	// NIL IS DETECTION OFF, whatever Harnesses holds, and it is the seam that
	// keeps the engine out of this package: the conversation decides WHETHER a
	// harness runs — it is the half a person answers — and the engine decides
	// what running one means.
	//
	// step is where the engine reports each step as it lands, and it is what
	// makes a run something a person can WATCH rather than wait out: the report
	// only exists when the whole thing is over. It is never nil, so a runner
	// calls it without checking; a runner with nothing to report simply never
	// does. Calling it BLOCKS the run for as long as the send takes, which is
	// why what is behind it is one hub send and nothing else.
	//
	// The [subharness.Usage] is WHAT THE RUN COST, summed over every model call
	// it made, and it is returned rather than left to the engine because the
	// person paying for it is sitting in this conversation: a run bills through
	// the auxiliary door and lands in /cost, on the status line and against the
	// spend rail (harness.go). A runner that cannot account for its calls
	// returns the zero value, which is a run this session does not claim was
	// free — it is a run nobody reported a price for, and the emptiness law
	// says to show nothing rather than a zero.
	RunHarness func(ctx context.Context, name, text, model string, step func(subharness.Trail)) (string, subharness.Usage, error)

	// HarnessStore is where a harness this conversation DESIGNS is written, and
	// it is the same registry Harnesses was read out of (harness_build.go). The
	// model's build_harness hand reaches the designer through it (tools_harness.go);
	// a page nobody approved never touches it.
	//
	// NIL IS BUILDING OFF, on exactly the terms RunHarness is detection off — and
	// the two are checked together, because a harness this session can write and
	// cannot run would be a page saved into a registry with no engine under it.
	//
	// It is the STORE and not a path for the reason Harnesses is a slice: where
	// the registry lives is the surface's decision, and a package that opened
	// ~/.aforge/harnesses itself would open it from a test and from a task node's
	// own agent too.
	HarnessStore *subharness.Store

	// OrchestrateRunner launches one adaptive run (internal/orchestrate): the
	// goal, the model the turn named (empty is the session's), and the fuel
	// cap in dollars. It returns the run's id; events stream on the standing
	// lanes as EventOrchestrateNote/Fuel/Pause.
	//
	// NIL IS ORCHESTRATION OFF, the same posture RunHarness keeps: a surface
	// that was not handed a runner never offers an adaptive run, at the cost
	// of one nil check per turn.
	OrchestrateRunner func(ctx context.Context, goal, model string, capDollars float64) (string, error)

	// Subharnesses is this surface's subharness registry: the compiled-in Go
	// programs it built, and the stores it put in front of them
	// (docs/SUBHARNESS-CONTRACT.md). It is the registry itself rather than a
	// path for the same reason HarnessStore is a store — where the bundles live
	// is the surface's decision, and a package that opened
	// ~/.aforge/subharnesses itself would open it from a test and from a task
	// node's own agent too.
	//
	// NIL IS SUBHARNESSES OFF, on exactly the terms RunHarness is detection off.
	// The three doors in subharness_contract.go answer nothing, calmly, and a
	// surface built against them draws nothing rather than an error — which is
	// the "absent, not broken" law arriving at a door that was never wired.
	Subharnesses *exec.Registry

	// SubharnessMemory is where a running subharness keeps what it has learned
	// about its OWN domain — its file in its own bundle, never this
	// conversation's memory (subharness_env.go's [SubharnessMemory] says why the
	// two must not share a page).
	//
	// NIL IS A BUILD WITH NO BUNDLE MEMORY, and the remember/recall doors then
	// answer [exec.NotWired] for their own names, which is the contract's own
	// answer for a door with nothing behind it. It is a SEAM the store lane
	// fills, on the terms Subharnesses is one: where a bundle's memory lives is
	// the surface's decision, and a package that opened
	// ~/.aforge/subharnesses itself would open it from a test too.
	SubharnessMemory SubharnessMemory

	// SubharnessLastRun is the dim note under one row of the `/subharness` list:
	// when that program last ran here and how it went, in a person's words
	// ([SubharnessRow.LastRun]). It is a closure rather than a table because the
	// answer is about the moment the list is drawn, and a snapshot taken at
	// launch would be silent about the run that finished five minutes ago.
	//
	// NIL IS NO HISTORY, and every row then draws nothing there — never "0 runs",
	// never "never run" (the emptiness law). It is the STORE LANE's seam: the run
	// journals it keeps beside each bundle are the only thing that can answer.
	SubharnessLastRun func(name string) string

	// SubharnessRecordRun is told how one run went, the moment it lands
	// (subharness_run.go). It is the write half of [Config.SubharnessLastRun] and
	// it is the STORE LANE's seam too — the note goes beside the bundle, which is
	// the only place a later session can read it back from.
	//
	// NIL IS A BUILD THAT KEEPS NO HISTORY, and a run then simply leaves none. It
	// is not an error and nothing is drawn about it: a list with no notes is what
	// a machine that has run nothing looks like, and the two are the same picture
	// on purpose.
	SubharnessRecordRun func(name string, note SubharnessRunNote)

	// WorktreeRoot is where isolated worktrees for a run's write-capable
	// nodes live. The session-id wave owns what fills it; this is the
	// ABSTRACT SEAM — a path per job id, nothing more. EMPTY means worktree
	// nodes share the workspace instead, which is the safe degradation.
	WorktreeRoot string

	// Media and MediaModel are the v3-revision media pair (docs/MULTIMODAL.md
	// Decisions 5-8): the one client that reaches every generation endpoint —
	// /images, /audio/speech, /videos — and the ONE USE-TIME RESOLVER that
	// answers which model serves a modality. MediaModel takes exactly one of
	// "image", "speech", "video", "vision" and answers a slug the resolver has
	// already capability-checked against the catalog, or "" when that modality
	// has no capable model; the ladder behind it (settings slot → role pin →
	// best catalog candidate → curated fallback) is the surface's business,
	// which is why this is a closure and not a table.
	//
	// The absence law is per-verb: a nil Media keeps every generation tool off
	// the belt; a nil MediaModel (or one answering "") keeps that MODALITY's
	// tools off ([Agent.mediaHand]). They REPLACED a pre-revision pair of this
	// config's own — an image client and an image slug, with a pin ladder the
	// tool walked itself — and nothing of that pair survives: one client and one
	// resolver serve every verb, so a machine cannot paint and be unable to
	// speak for reasons nobody can find.
	Media      MediaGenerator
	MediaModel func(modality string) string

	// MediaPick is the just-in-time half of the pair above: where MediaModel
	// answers "the default for this modality", MediaPick answers "the model
	// asked for THIS name, for this one call". It takes the same modality word
	// and the model's own word for what it wants — a slug, a fragment like
	// "seedream", or "best" — and answers the resolved slug, or an error in
	// words the model can act on ("no image model matches", "X makes speech,
	// not image"). An empty word answers ("", nil), which the belt reads as
	// "keep the default".
	//
	// NIL MEANS THE CHOICE DOES NOT EXIST: the making verbs advertise no
	// `model` argument at all, by the same absence law as the verbs themselves
	// — a knob with nothing behind it is left off the schema rather than
	// present and refused. The surface that wires it (cmd/aforge's
	// chatv3_media.go) answers from the same catalog the defaults ladder
	// reads, so a picked model is capability-checked exactly as a default is.
	MediaPick func(modality, word string) (string, error)

	// DocumentEngine is the rung read_document climbs to (tools_doc.go): the
	// person's document_engine row, one of auto, local, free or ocr
	// (config.DocumentEngines), resolved by the surface exactly as the search
	// pair below is and handed over as the answer.
	//
	// EMPTY IS AUTO, not "off". Unlike the two pairs around it, this is a
	// preference and not a back end: the rungs ride this session's own API key
	// and base URL, so there is nothing a nil here could mean except "nobody
	// chose", and config.DefaultDocumentEngine is what nobody-chose resolves to
	// everywhere else in the binary. The tool is on the belt either way, because
	// read's own scanned-PDF refusal names it by name and a named way out that
	// resolves to nothing is worse than a rung that says why it cannot run.
	DocumentEngine string

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

	// Connect is the person's connected accounts (internal/connect): which
	// services this build can offer, which of them are connected on this
	// machine, and an authorized client for each one that is.
	//
	// NIL IS THE DEFAULT AND MEANS THE FEATURE IS ABSENT — no services tool, no
	// use_service, and nothing on the belt that mentions an account. It is the
	// same law the search pair above states and it is stated again because the
	// cost of breaking it is larger here: a model told it can read a mailbox
	// will plan a whole answer around one, and a refusal at the end of that plan
	// is a turn spent on a capability that never existed. A build with no
	// registration for any service hands over nil and the conversation is exactly
	// what it was before this field.
	Connect *connect.Manager

	// connectHub is the seam the belt actually calls through, and the one place
	// this package touches an account at all. It is unexported because it is not
	// a caller's choice: a real caller hands over Connect and this is derived
	// from it (connect.go's newConnectHub). What it buys is the tests, which
	// drive the ask, the arming and the failure paths against a hub of their own
	// without a Google account and without a network.
	connectHub connectHub

	// writeScope bounds which repo paths this agent's edit and write calls may
	// touch (orchestrate.go's writeGuard). It is unexported for connectHub's
	// reason — it is not a caller's choice but a bound the machinery puts on an
	// agent it built — and it is set in exactly one place: the executor that
	// runs one node of an adaptive run, from that node's own declared scope.
	//
	// EMPTY IS NO BOUND, which is every agent in this build but a scoped node
	// and a fork's hand (fork.go), which is the second citizen this bound got and
	// the reason it is stated in the agent's own voice rather than a node's.
	writeScope []string

	// inHand says this agent IS one of a fork's hands (fork.go), and it exists to
	// take one verb away: a hand may not fork again. It is a flag rather than a
	// belt decision made at the fork because a belt is assembled once, inside
	// [newAgent], so a verb withheld afterwards would be a verb the model was
	// already told it had.
	//
	// It is unexported for writeScope's reason: it is not a caller's choice but a
	// fact about an agent this package built.
	inHand bool

	// handLeash is a hand's round budget, as a citizen of the control plane
	// (fork.go, hooks.go). It is a pointer because the budget is state that the
	// running turn writes and the fork reads afterwards, and it is nil for every
	// agent that is not a hand — which is what leaves the plane exactly as it was
	// for everybody else.
	handLeash *handLeash

	// pacing is how a node hears that its own calls have parked on the
	// provider's rate limiting, and it is unexported for connectHub's reason: it
	// is not a caller's choice. The executor sets it on the config it builds for
	// a node's agent — and for the auditor and the repair workers that stand in
	// for the same node — and nothing else in this build sets it at all.
	//
	// It is a callback rather than a field to poll because the fact it carries
	// is an EDGE: a call started waiting, a call stopped waiting. Polling it
	// would mean a clock, and the whole point of the signal is that it is free.
	pacing func(bool)

	// ── the effort ladder's two posture fields ──────────────────────────────
	//
	// Between them they say what this session IS, so its every model call can be
	// answered by one resolver instead of by each spawn site's own judgment
	// (effort.go, internal/effort).

	// Effort is the rung this session was HANDED — the work's own rung, filling
	// the ladder's task scope. It is set on a child: a task worker gets the
	// task's rung, a standing firing gets the item's. EMPTY IS THE HONEST
	// DEFAULT and means nobody set one for this piece of work, which is every
	// conversation a person opens themselves.
	Effort effort.Rung

	// EffortRole is what this session is FOR, and it is the rung of last resort
	// before the install's default: a standing firing and its checks stay cheap
	// however deep the install is dialled, and an errand asks for nothing at
	// all. THE ZERO VALUE IS NOT A ROLE and falls through to DefaultEffort,
	// which is the right answer for a caller that has not thought about it — a
	// headless --once, a test — because it is the same answer a person's own
	// conversation gets.
	EffortRole effort.Role

	// DefaultEffort is the install's `effort` row, read by the door
	// (config.DefaultEffortAt). EMPTY ASKS FOR NOTHING, which is what a session
	// built without a door has always sent: config.Ship is the shipped answer to
	// the settings row and never a default this package invents, so a caller
	// that wires no profile is not silently opted into paying for depth.
	DefaultEffort effort.Rung

	// beat is the node's heartbeat on disk, and nil for every agent that is not
	// standing in for a task node (task_beat.go). It is unexported for pacing's
	// reason — it is not a caller's choice but a fact about an agent this package
	// built — and the loop takes its two edges either side of the wire.
	beat *taskBeat

	// TaskModel is the model a task runs on when its proposal names none — the
	// person's task.model row. EMPTY IS THE CONVERSATION'S OWN MODEL, which is
	// the behaviour every task had before this field existed: a node is the same
	// worker doing the same job somewhere quieter, so the same model is the
	// honest default. It is resolved through the same matcher a proposal's word
	// is (taskmodel.go), so a row written "opus-5" reaches the same id.
	TaskModel string

	// TaskModels lists the models a task may be sent to — the surface's catalog,
	// as ids. It is the seam a `model` argument is validated and resolved
	// against, and it is a function for the reason SupportsImages is one: the
	// list arrives from a lazily loaded catalog and is not the same list at boot
	// as it is a minute later.
	//
	// NIL IS "NOBODY CAN SAY", not "there are none". A caller that hands over no
	// list gets every named model taken as written and the provider's own error
	// if it is wrong — exactly what every caller had before the argument existed
	// — because a package with no catalog refusing a model id would be inventing
	// a catalog to refuse from.
	TaskModels func() []string

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

	// TaskRepairRounds is how many times a node whose work came back with gaps
	// is handed back to a fresh worker in the SAME worktree before it lands as
	// incomplete (task_audit.go, config.KeyTaskRepairRounds). 0 IS THE LOOP
	// TURNED OFF: the first gap ends the node, which is how the frontier worked
	// before the loop existed.
	//
	// Zero is also the zero value, and that is deliberate rather than a defect —
	// it is [TaskAutoApproveSeconds]'s arrangement, for the same reason. A caller
	// that builds a Config and says nothing about repair gets the behaviour that
	// spends nothing extra, and the DEFAULT of one round is the door's answer
	// (config.DefaultTaskRepairRounds), read from the person's own settings.
	TaskRepairRounds int

	// TaskParallel is how many task nodes may RUN AT ONCE, and 0 IS NO LIMIT
	// (task_run.go's frontier, config.KeyTaskParallel). It is the person's own
	// number and it is off by default, because the count of nodes was never
	// what runs out: what runs out is this machine's cores and memory — see
	// TaskMaxLoad and TaskMinFreeMB below — and the provider's rate limit,
	// which the adapter already adapts to on its own.
	//
	// Zero being both "no limit" and the zero value is deliberate, in
	// [TaskRepairRounds]'s arrangement: a caller that builds a Config and says
	// nothing about parallelism gets the ceilings that are really there rather
	// than a number this package invented for it.
	TaskParallel int

	// TaskMaxLoad is the one-minute load average PER CORE at or above which the
	// frontier stops starting new nodes (task_pressure.go,
	// config.KeyTaskMaxLoad). 0 turns the load check off.
	//
	// Per core rather than raw, because the same reading means opposite things
	// on a two-core laptop and a thirty-two-core workstation, and a person's
	// setting has to mean one thing on both.
	TaskMaxLoad float64

	// TaskMinFreeMB is the floor of AVAILABLE memory — the kernel's
	// MemAvailable, what a new process could actually get — below which the
	// frontier stops starting new nodes (task_pressure.go,
	// config.KeyTaskMinFreeMB). 0 turns the memory check off.
	//
	// Both of these gate ADMISSION and nothing else. A node that is already
	// running keeps its worktree and its child agent however loaded the machine
	// gets, which is what lets pressure drain instead of having to be relieved.
	TaskMinFreeMB int

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

	// Errand marks this agent as the short exchange behind home's `ask here`
	// (cmd/aforge's chatv3_exchange.go) rather than a conversation somebody
	// sits in. It is a conversation in every other way — a real model, a real
	// transcript, a card it can answer — so InTask would be a lie about it.
	//
	// IT CHANGES EXACTLY ONE THING: an errand is never registered as a live
	// delivery target (standing_run.go). A firing steered into an exchange is
	// news typed into a forty-cell pane that closes with home, and the person
	// sitting in an ordinary conversation in the same window is never told —
	// which is what happened the first time a reminder made from home ever
	// fired.
	//
	// Ratifying the exchange's OWN card is untouched by this, and the two are
	// separate lanes on purpose: a card is answered through the agent the
	// surface is holding ([Agent.ResolveStanding]), never through the registry,
	// so an exchange still proposes and still hears yes.
	Errand bool

	// roomThread says this agent is a node somebody TALKS TO rather than a
	// worker a runner drives, and it is set on exactly one kind of node: the
	// thread a sub-harness is designed in (harness_task.go).
	//
	// It changes one thing. An InTask agent never starts a turn of its own —
	// its turns belong to the runner, and a worker waking inside a worktree
	// would be a second conversation nobody asked for ([Agent.wakeLocked]) — and
	// that is exactly wrong for a thread whose whole life is somebody arriving
	// and saying something. A design thread spends most of its time with no turn
	// running: the page is written, the card is up, and the person is reading
	// it. So a line steered into it here STARTS one, and everything else InTask
	// means — no propose_task, a refusal instead of a question, patience with a
	// provider that is pacing it — is left exactly as it is.
	//
	// It is private for InTask's reason: no surface sets it, the executor does.
	roomThread bool

	// reviseDesign is the one extra hand a design thread has, and the whole of
	// what puts revise_design on its belt (tools_harness.go). It carries the
	// change, in the person's own words, to the design loop parked on the
	// approval card — the only thing in this process that can act on it — and it
	// answers with the sentence the model is told when the page is not in a
	// state to be changed (harness_task.go's reviseDoor).
	//
	// IT IS NIL EVERYWHERE ELSE, and that nil is the gate rather than a check
	// inside the tool: this codebase's law is that a capability with nothing
	// behind it is ABSENT and not broken, so an agent with no design behind it is
	// never given the verb at all. It is private for roomThread's reason — no
	// surface sets it, the executor wires it from the node.
	reviseDesign func(string) error
	// memoryBrief is the <memory> block a task node OPENS WITH: the parent
	// routed it against this node's brief at the spawn seam, because a node has
	// no turn of its own to route against and no store of its own to route into
	// (memory.go, task_run.go's newTaskAgent).
	//
	// It is private for roomThread's reason: no surface sets it, the executor
	// does — and a node is handed the WORDS rather than the store, so a family
	// of eight nodes cannot become eight writers on one brain.
	memoryBrief string
	// fixesDir is the project bucket the error→fix sidecar keeps its file in
	// (fixstore.go), and it is set only when this agent is a NODE. A node's
	// session file is a journal inside its parent's place rather than a place of
	// its own, so it cannot derive the bucket for itself; handed one, a family of
	// eight workers and the conversation that spawned them all learn from the
	// same file. It is private for memoryBrief's reason: no surface sets it, the
	// executor does (task_run.go, orchestrate.go).
	fixesDir string
	// droppings is THE FAMILY'S SESSION FOLDER, carried by an agent that has no
	// folder of its own: a task node's worker, a part's worker under that one, a
	// fork's hand, an adaptive run's child, an auditor, a standing probe. It is
	// read in exactly one place ([Config.droppingsPlace]) and answers exactly one
	// question — where a job log or a stubbed tool result lands (landing.go
	// states the law and the failure that wrote it).
	//
	// IT IS NOT Place UNDER A SECOND NAME, and the distinction is the whole point.
	// Setting Place on a worker would make the worker a SESSION: it would stamp
	// the conversation's meta.json with the worker's own spend and title
	// (placemeta.go), file its journal under the conversation's id (agent.go's
	// openSessionFile), and paint its pictures into the conversation's work/
	// instead of the worktree it is about to merge back (landing.go's
	// deliverablesDir). This row carries the ONE fact a worker needs — where the
	// harness keeps its own litter — and nothing else.
	//
	// It is private for memoryBrief's reason: no surface sets it, the constructor
	// that builds the worker does.
	droppings Place
	// The three rows below are the TASK FAMILY'S, and like InTask the executor
	// is the only writer: they are what lets a node hand PART of its own work
	// further out (task.go's fan-out law).
	//
	// tasker is THE CONVERSATION'S GRAPH, handed down rather than copied. A node
	// that proposes work adds a node to the graph the person is already
	// watching — one id space, one roster, one cap, one checkpoint — which is
	// what "decomposition is edges added to this graph" was always going to mean
	// (task_contract.go). It is nil in every conversation, which builds its own,
	// and nil in every OTHER agent this package runs inside a node: an auditor
	// and an adaptive run's worker are handed none, so neither has the verb.
	tasker *TaskGraph
	// taskID is the id of the node this agent IS, and 0 in a conversation. A
	// proposal made here is registered under it ([TaskNotice.Parent]), which is
	// what draws the family on the roster and what scopes the `tasks` tool to
	// this node's own children.
	taskID uint64
	// taskDepth is how many tasks deep this agent sits: 0 in the conversation, 1
	// in a task the conversation proposed, 2 in a sub-task of that one.
	// taskDepthLimit is the floor, and an agent standing on it is handed no
	// propose_task at all (tools.go) — absent, not refusing.
	taskDepth int

	// Divide arms the division road for the tasks this session admits
	// (task_divide.go). ON is what the v3 door wires (cmd/aforge's chatv3.go,
	// from internal/config's Swarm, default true); the zero value is off, which
	// is what keeps every scripted agent in this package's tests exactly as it
	// was.
	//
	// IT IS THE ROAD AND NOT THE DECISION. A task is armed one at a time and
	// only when something says its work might be wide ([Agent.armDivision]), and
	// a worker that IS armed still has to get a division past the evidence and
	// the free hands before anything is born. This row only says the road
	// exists.
	Divide bool

	// usageLedger points this agent's spending records at a file OTHER than the
	// machine's own (usage_ledger.go's [UsageLedgerPath]). Empty — which is every
	// door in the product — means the machine's.
	//
	// It is private for [Config.fixesDir]'s reason inverted: no surface sets it
	// and no surface should, because the whole value of the ledger is that there
	// is exactly one of it. What it is for is a test that wants to read back what
	// a turn recorded without depending on where this machine keeps its state.
	usageLedger string

	// standingItemID is the id of the standing item whose firing this agent IS
	// (standing_run.go), and empty in every conversation and every ordinary task.
	// It rides on the config for [Config.taskID]'s reason: the money a firing
	// spends has to be attributable to the promise the person made, and the only
	// thing that knows which promise is the runner that built this config.
	standingItemID string

	// SpendRailUSD stops a session that has spent this much. 0 is off. The
	// check happens BEFORE a turn starts (rail.go) and reads the session's own
	// journaled usage, so the rail is exact rather than an estimate, and a turn
	// already in flight is never cut in half by it.
	SpendRailUSD float64

	// Unattended says NOBODY IS SITTING IN FRONT OF THIS SESSION — the door's
	// `--yolo`, which until now reached this package only as an approval default
	// (cmd/aforge's v3Policy) and said nothing about who was watching.
	//
	// IT IS NOT THE ARMING BIT ON ITS OWN. Together with a Budget it makes this
	// session's principal a [Steward] (principal.go); alone it changes nothing
	// whatever, because a flag that quietly started carrying a conversation on
	// for hours would be the harness spending somebody's money on a sentence
	// they did not write. The door says so in one line at launch.
	Unattended bool

	// Budget is the ceiling an unattended session runs under: hours, dollars, or
	// both. THE ZERO BUDGET IS NO CEILING, which is what every session has always
	// had, and it is what leaves `--yolo` alone exactly as it was.
	//
	// It is separate from SpendRailUSD above and they are different rails for
	// different questions. The rail REFUSES THE NEXT TURN once a conversation has
	// spent its ceiling, whoever is driving it; this is what the Steward is
	// allowed to spend CARRYING ON BY ITSELF, and reaching it ends the run with a
	// report rather than with a refusal nobody reads.
	Budget Budget

	// newerBuild is the cheap process-local reading that says this running
	// aforge has been replaced on disk. It is private because the session owns
	// when the reading reaches a turn; tests replace only the reading itself.
	newerBuild func() string
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
	// limits are the response boundary's three numbers — how many times the wire
	// is forgiven, how many measured failures buy a stronger tier, and what that
	// tier may cost one piece of work (taxonomy_boundary.go). They are resolved
	// ONCE, from the person's profile, because resolving them reads a file and
	// the boundary is asked on the failure path of every request.
	limitsOnce sync.Once
	limits     taxonomy.Limits
	// tallies is what each task node this agent owns remembers about its own
	// failures, keyed by node id and guarded by mu. It lives here rather than on
	// the node so the graph's own struct stays what it is — the person's work —
	// and so a node that nothing classified simply has no entry.
	tallies map[uint64]*taxonomy.Tally
	// system is message[0] of every request: the rendered prompt, held once
	// because it is the same bytes on every step of every turn.
	system string
	// systemAt is when [Agent.system] was rendered, and systemOwn says this
	// agent rendered it rather than being handed one. Together they are what
	// lets a turn move the prompt's `Now` line forward when it has gone stale
	// ([Agent.refreshClockLocked]) — and what stops it doing that to a prompt
	// somebody else wrote, where there may be no `Now` line to move and
	// rendering our own would throw theirs away. Both sit under mu with
	// [Agent.system].
	systemAt  time.Time
	systemOwn bool
	// tools is the belt and definitions is its wire form, built once at
	// construction — rebuilding them per step would re-marshal every schema on
	// the hot path — and thereafter APPEND-ONLY, under armMu (connect.go).
	//
	// Both are COPY-ON-WRITE: arming allocates a new array and swaps the header,
	// so a reader that took a snapshot under armMu may walk it without the lock
	// and can never see a half-written slice. Nothing already in either is ever
	// moved, rewritten or removed, because the definition block rides at the
	// front of every request and a definition that shifts re-bills the whole
	// prompt behind it (internal/exec's tools.go states the law).
	tools       []bare.Tool
	definitions []ai.ToolDefinition
	// served is what the belt cannot say about the tools an ACCOUNT named
	// rather than this build (served.go): whose account each one is, what the
	// account calls it, and which capability governs it. Keyed by the name the
	// tool is armed under.
	//
	// It is under armMu with the belt, and written at the same door, because a
	// tool on the belt without its record would be a tool judged by nothing.
	served map[string]servedTool
	// withdrawn is the record of a belt narrowed ON PURPOSE (withdrawn.go): the
	// hands the harness took, why, and what is left. Nil whenever the belt is
	// whole, which is nearly always.
	//
	// It is under armMu WITH the belt because it is the belt's other half: a
	// dispatcher that found a name missing needs to know whether it was taken or
	// never existed, and the two answers must not be able to disagree.
	withdrawn *toolWithdrawal
	// armMu guards those headers, that map, and nothing else. It is not mu:
	// arming happens inside a tool call, and a tool call must never take the
	// lock Interrupt has to be able to take.
	armMu sync.Mutex
	// connect is the accounts seam, nil when the feature is absent (connect.go).
	// It is written once at construction and read without a lock.
	connect connectHub
	file    *sessionFile
	// id is this session's identity: the journal header's id when there is a
	// file, and a fresh one when the conversation lives only in memory. It is
	// fixed at construction and never written after, so it needs no lock, and
	// it is what the chat log posts its thread under (chatlog.go).
	id string

	// cacheKey is this session's prompt-cache lineage, stamped on every request
	// by [sessionCompleter]. It is derived from [Agent.id] — a hash, so nothing
	// about the session's own id reaches a router's logs — fixed at construction
	// and never written after, so it needs no lock either.
	cacheKey string

	// presence is this session's liveness file (taskpresence.go): the small
	// crash-safe claim, refreshed on a heartbeat, that lets ANOTHER window say
	// this session is running right now and whether it needs its person. It is
	// nil for every agent that keeps none — a memory-only conversation, the
	// legacy flat layout, and every task node's agent.
	//
	// It is written once by [Agent.startPresence] inside the constructor, before
	// the agent is reachable, and never again — so it is read without a lock, on
	// the terms [Agent.id] and [Agent.cacheKey] are. That matters: its nudge is
	// called from seams that are already holding mu.
	presence *presenceDesk

	// jobs is the background-command registry (jobs.go): the processes bash
	// started with background:true, alive across turns until Close.
	//
	// It is outside mu and holds its own locks. A job's lifetime is the
	// session's, not a turn's, and the goroutines watching them must never
	// contend for the lock Interrupt has to be able to take at any moment.
	jobs *jobRegistry

	// inFlightBash is every foreground bash call that could be sent to the
	// background right now, keyed by the provider's id for the call
	// (promote.go). It sits outside mu and holds its own lock for the reason
	// jobs does, and for one more: the surface reaches into it from the input
	// goroutine at the exact moment a turn is holding mu.
	inFlightBash promotableCalls

	// memory is the brain (memory.go), nil when Config.Memory is. Like jobs it
	// sits outside mu and holds its own lock: its writer is a post-turn goroutine
	// that outlives the turn that started it, and its reader is the pre-turn
	// router.
	memory *memoryBrain

	// memoryCtx is the lifetime of every background memory pass, and memoryJobs
	// counts the ones still running. They are the [jobRegistry]'s bargain in
	// miniature: Close cancels the context so nothing waits on a provider, and
	// waits on the group so a write already in flight reaches the store.
	//
	// The context is written once at construction and the cancel is called once
	// by Close; both are read under mu, because the one thing that must be
	// atomic is "closed, therefore no new job" (see [Agent.startMemoryJob]).
	memoryCtx  context.Context
	memoryStop context.CancelFunc
	memoryJobs sync.WaitGroup

	// chatlog is the LOSSLESS FLOOR under compaction (chatlog.go): every message
	// of this conversation posted into the store's thread as it lands, so that a
	// stub and a fold point at text somebody can still read. It is nil when there
	// is no store, which is memory off, and it sits outside mu holding its own
	// lock for the reason memory does — its writer outlives the turn.
	chatlog *chatJournal

	// stateStore is the BPE working state (state.go): the beliefs and progress
	// records that live OUTSIDE the transcript so a compaction cannot lose them.
	// It is built on first use through [Agent.state] — the belt closes over the
	// agent, so the tools reach a store that construction need not have made yet
	// — and stateOnce is what makes that exactly one rehydration from disk.
	//
	// Like memory and jobs it sits outside mu and holds its own lock: its writers
	// are tool calls running in parallel inside one batch, and its reader is a
	// compaction pass that must not need the session lock to render a block.
	stateOnce  sync.Once
	stateStore *stateStore

	// fixShelf is the error→fix sidecar's pair of files — this project's and
	// this machine's (fixstore.go). It is built on first use through
	// [Agent.fixShelfFor] for stateStore's reason: a conversation in which
	// nothing ever fails should open no file at all. Like the stores above it
	// sits outside mu and holds its own lock, because its writers are the tool
	// calls of one batch running in parallel.
	fixOnce  sync.Once
	fixShelf *fixShelf

	// cardOnce / cardStore are the STATE CARD (card.go): what the work is for
	// and where it stands, folded in by the post-turn extractor and rendered
	// into every system prompt. It is built on first use for stateStore's
	// reason, and holds its own lock for the same one — its writer is the
	// post-turn goroutine, which outlives the turn that started it.
	cardOnce  sync.Once
	cardStore *cardStore

	// docs is the OCR rung: the document parser read_document calls through and
	// the per-document memo that makes paging a scan free (tools_doc.go). It is
	// built on first use through [Agent.documentParser] for the reason
	// stateStore is, and holds its own once and its own lock for the same
	// reason: its callers are tool calls running in parallel inside one batch.
	docs documentRung

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
	// absent entries. Nil until somebody sets a level, which is most
	// sessions.
	//
	// It is the TURN scope of the effort ladder (effort.go): the most specific
	// thing anything can say about how hard to think, and the one a person's own
	// hand is on.
	reasoning map[string]effort.Rung
	// effort is the rung this whole conversation was set to, kept in the session
	// folder's meta.json so it survives a restart (placemeta.go). It is one
	// field and not a map because it is a choice about THIS CONVERSATION rather
	// than about a model: a person dialling their session deeper means the
	// session, whatever they switch the model to inside it.
	effort   effort.Rung
	messages []ai.Message
	// earlier is the conversation ABOVE the latest compaction marker, shaped for
	// a surface's scrollback and held for no other reason: nothing here ever
	// sends it, and the model does not carry it (see [Agent.EarlierTranscript]).
	//
	// It is DISPLAY ENTRIES rather than messages, and that is the whole of why it
	// can be held at all. A run of ai.Message keeps every picture in it alive as
	// the multi-megabyte data URL it was rebuilt into — the exact thing
	// [sessionFile.images] is fingerprinted to avoid — while a display entry
	// keeps the words, the path a picture came from, and a CAPPED copy of each
	// tool result. So the memory is bounded by the text of the journal's earlier
	// region and is smaller than the file that holds it.
	earlier []DisplayEntry
	// earlierFloor is how many entries at the START of [Agent.Transcript] are the
	// latest pass's own rewritten copy of earlier — the stubs and fold lines it
	// put in place of the conversation above. A surface draws the region instead
	// of them, so the conversation is told once ([EarlierHistory]).
	earlierFloor int
	// personAsk is the last thing THE PERSON typed, kept apart from the
	// transcript because the transcript cannot answer the question. Every user
	// message in a.messages is user-role, including the ones the session wrote
	// itself — a task landing, a job exiting — and the bit that says who spoke
	// (userMessage.wake, .authored) does not survive the append. So the answer is
	// recorded where the message is recorded, by [Agent.rememberAskLocked].
	//
	// It is what work handed out of this conversation carries as the person's own
	// words (task_brief.go), and it is deliberately the WHOLE message rather than
	// a summary of it.
	personAsk string
	// replyTags are finished-task identities placed in the transcript but not
	// yet handed to the surface. They persist across the turn-end seam.
	replyTags []TaskReplyTag
	// divisibleAsk is the text the sizing judge last answered YES about
	// (task_person.go's [Agent.judgeDecomposable]) — the one signal that arms
	// the division road for a task somebody then starts as a single worker
	// ([Agent.armDivision]).
	//
	// It is ONE entry rather than a map because the judge is asked immediately
	// before the work is started, by one command, and a bank that grew for the
	// life of the session would be remembering answers about work that was
	// never begun.
	divisibleAsk string
	// lastTurnTruncated is the honest handoff from the model loop to headless
	// node reporters. The finish reason is response metadata and is not part of
	// the transcript, so without this bit a digest can only repeat the cut-off
	// prose and falsely make the node look complete.
	lastTurnTruncated bool
	// memoryText is the <memory> block message[0] currently carries: what the
	// router asked for at the start of this turn, or the block a task node was
	// opened with (memory.go). It is under mu because it is rendered into the
	// transcript's first message, and it is REPLACED per turn rather than
	// appended to — a turn's memories are that turn's.
	memoryText string
	// cardText is the <state> block (card.go): what this conversation is doing,
	// as the post-turn pass has folded it. It sits under mu because it is
	// rendered into the transcript — at the TAIL, in the volatile note
	// ([Agent.landVolatileLocked]), and no longer in message[0], because it
	// moves every time a delta lands and message[0] is in front of everything.
	cardText string
	// standingText is the <standing> block message[0] currently carries
	// (standing_world.go): the orders the person holds over this conversation,
	// which the model must work within. It sits under mu beside the three blocks
	// below for their reason, and it is re-rendered at the start of every turn —
	// an unchanged set renders the same bytes, so a conversation whose orders
	// have not moved leaves message[0] exactly as the provider cached it.
	standingText string
	// elsewhereText is the <elsewhere> block (taskdelta.go): what the OTHER
	// windows on this project landed and are running. It sits under mu beside
	// cardText and rides where cardText rides, at the tail of the transcript —
	// an unchanged block lands no second note, which is what keeps the whole
	// conversation in front of it cached.
	elsewhereText string
	// elsewhereTold is the short memory of landings this session's model has
	// already been handed, newest first and capped at [deltaLandedRows]. The
	// stamp on disk advances the moment the block goes out, so without this the
	// block would name a landing on one turn and forget it on the next —
	// see [deltaRemember].
	elsewhereTold []deltaLanding
	usage         Usage
	// principal is WHO THIS SESSION IS WORKING FOR (principal.go), and it is
	// never nil: a session built with no posture at all gets a [Person], which
	// answers every question the way this package answered it before the
	// interface existed. It is set once in [newAgent] and never written after,
	// so every road may read it without the lock.
	principal Principal
	// startedAt is when this process opened the session, and it is the only
	// wall clock this package keeps. Usage.Duration is the SUM OF TURN
	// DURATIONS, which is a different number and the wrong one for a budget: a
	// session idle for an hour between two ten-second turns has spent an hour of
	// somebody's evening and twenty seconds of that figure.
	//
	// It is THIS LAUNCH and not the session's birth. Place.Created is on disk and
	// is days old on a resumed conversation, and a budget measured from it would
	// stop a resumed session before its first turn.
	startedAt time.Time
	// createdFiles is EVERYTHING THIS SESSION MADE THAT WAS NOT THERE BEFORE, in
	// first-touch order (principal_audit.go). It is folded in from the per-turn
	// ledger recovery.go already keeps — one source of truth for "did this exist
	// before the call" — because that ledger is dropped at the end of every turn
	// and the question this answers is asked once, at the end of the session.
	createdFiles []fileChange
	running      bool
	// turnFloor is where the running turn's WORK begins in a.messages: the
	// index just past the message that opened the turn, stamped by
	// [Agent.startTurnLocked] and meaningful only while running is true. It is
	// what lets [Agent.AttachReplay] hand a surface the conversation once and
	// whole — the journal's record up to the floor, the hub's backlog from it —
	// with nothing drawn twice. A compaction pass that rebuilds a.messages
	// mid-turn moves the floor with the rebuild ([Agent.foldLocked]); a rewind
	// never has to, because a cut is refused while a turn is in flight.
	turnFloor int
	cancel    context.CancelFunc
	steering  []userMessage
	closed    bool
	// steerSeq names the sentences the person has spliced into a running turn
	// (steer.go). It is an atomic rather than a field under mu because minting an
	// identity is not a fact about the transcript, and an id that could only be
	// taken while holding this lock would be an id nothing outside a locked
	// section could ask for.
	steerSeq atomic.Uint64
	// taskNotes counts the reports this agent's OWN sub-tasks have handed over
	// that no request has carried yet, and taskNews is the generation channel
	// closed each time one lands. They exist for one reader — the runner holding
	// a task node open while its children work (task_run.go's [runTaskChild]) —
	// and they are zero and nil in every conversation, which has no runner and
	// wakes for itself ([Agent.postTaskNews]).
	taskNotes int
	taskNews  chan struct{}
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

	// wakeLanes are the standing subscriptions to turns the session started by
	// itself ([Agent.Wakes]) — a task landing on an idle conversation, which is
	// the one turn no Submit is holding a channel for. Each carries the woken
	// turn's own event stream, handed over before the turn's first event.
	//
	// They are not the turn's hub and not taskWatchers: the hub belongs to one
	// turn and does not exist yet when a wake is decided, and the task lane
	// carries updates about work rather than a conversation. Nil for every
	// surface that does not draw woken turns, which costs that surface nothing.
	wakeLanes []chan (<-chan Event)

	// opened says the session has been handed to whoever asked for it. It is
	// false for the whole of New — including the task recovery that runs at the
	// end of it — and true forever after, and the one thing it gates is the wake:
	// a turn started before any surface exists is a turn nobody can read.
	opened bool

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

	// connectAsks is the connect questions a person owes an answer to, keyed by
	// the id the EventConnectAsk carried, and connectSeq is what names them
	// (connect.go). They are consent's pending-id machinery for a question about
	// an ACCOUNT, and they are ephemeral in exactly the same way: a question
	// lives as long as the use_service call blocked on it.
	connectSeq  uint64
	connectAsks map[string]connectAsk

	// harnessAsks is the sub-harness offers a person owes an answer to, keyed by
	// the id the EventHarnessOffer carried, and harnessSeq is what names them
	// (harness.go). Same machinery as consent's, one lane over: an offer lives
	// exactly as long as the turn held on it, which is at most one per turn.
	//
	// It is its own counter rather than consent's because the two lanes are
	// answered by two methods and neither may be able to answer the other's
	// question by guessing a number.
	harnessSeq  uint64
	harnessAsks map[uint64]harnessAsk

	// subharnessAsks is the questions a RUNNING SUBHARNESS owes an answer to,
	// keyed by the task node the run is (subharness_env.go). It is the same
	// pending-id machinery consent and the harness offer keep, one lane over,
	// with one difference worth stating: the key is not a counter of its own.
	//
	// A RUN IS A NODE AND A NODE ALREADY HAS AN ID, and a run puts at most one
	// question at a time — it is one program on one goroutine, and a second
	// question would mean a second thing to answer about work that has not moved.
	// So the node's number is the token, which is also the number on the roster
	// row, the number in the ✕, and the number a person says out loud.
	subharnessAsks map[uint64]chan subharnessReply

	// subharnessOffers is the intake cards chat has raised and nobody has
	// answered yet, keyed by the id the EventSubharnessProposal carried, and
	// subharnessSeq is what names them (tools_subharness.go).
	//
	// It is its own counter for [Agent.harnessAsks]' reason: the two lanes are
	// answered by two methods, and neither may be able to answer the other's
	// question by guessing a number.
	//
	// EACH ENTRY CARRIES ITS OWN CARD, for the reason [Agent.harnessAsks] keeps
	// a design's: a surface that subscribes while the question stands — the
	// ordinary case for a conversation somebody left behind home and came back
	// to — would otherwise wait forever on a card that is already up.
	subharnessSeq    uint64
	subharnessOffers map[uint64]*subharnessOffer

	// harnessPick is a harness the PERSON chose rather than one a matcher
	// offered, left here by [Agent.RunHarnessRequest] for the turn it just
	// started to collect (harness.go). It is a hand-off between two halves of
	// one call and never state: the turn takes it, clears it, and runs it.
	harnessPick *harnessRoute

	// routeTurns counts the turns this session has begun and routeOffered is the
	// one the route judge last started work on (route_judge.go). They are the
	// whole of that feature's memory: the judge starts at most one task every
	// few turns, and "a few turns ago" is a number that only means anything if
	// something is counting. Both are zero for the life of a session nothing is
	// ever started in, which is most of them.
	//
	// ONE PAIR SERVES BOTH MOMENTS THE JUDGE LOOKS AT — before a message is
	// answered and after a words-only answer — because the limit is about how
	// often WORK may begin over the top of a conversation, which is one question
	// however it was noticed. The count is stepped at the front of a turn, where
	// every turn passes.
	routeTurns   uint64
	routeOffered uint64

	// harnessWatchers are the standing subscriptions to the design lane
	// ([Agent.HarnessDesigns]), and harnessAdded is what this session has
	// designed and saved since it opened (harness_build.go).
	//
	// The watchers exist for taskWatchers' reason, one lane over: a design starts
	// on a turn and finishes after it, so the card asking whether to keep it has
	// no hub left to arrive on. The entries exist because Config.Harnesses is a
	// SNAPSHOT the surface took at launch — a harness saved five minutes ago is
	// in the store and not in that slice, and detection reads this list beside it
	// so that a harness this conversation built is reachable from the next
	// sentence rather than from the next process.
	harnessWatchers []*eventStream
	harnessAdded    []subharness.Entry
	// harnessThreads is the task each harness this session designed was designed
	// IN, keyed by the harness's name (harness_task.go). It is what lets the
	// build tool point a later sentence about that harness at a room rather than
	// at nothing.
	//
	// IT IS THIS PROCESS'S MEMORY AND NOT THE REGISTRY'S. A harness somebody
	// designed last week has a thread on disk and this session has never heard of
	// it, so the honest answer for one of those is no number at all — the
	// alternative is a surface offering a door onto a room that is not there.
	harnessThreads map[string]uint64
	// orchestrations are the adaptive runs this session is driving, keyed by
	// the run id, and orchestrateSeq is what names them (orchestrate.go). The
	// watchers are the standing subscription those runs report on
	// ([Agent.Orchestrations]).
	//
	// A run outlives the turn that asked for it, so the gate it raises when the
	// fuel runs out has no hub to arrive on — and [Agent.Close] is the only
	// thing that can tell a run in flight that the session has left.
	orchestrateSeq      uint64
	orchestrations      map[string]*orchestration
	orchestrateWatchers []*eventStream

	// harnessRuns is the sub-harness RUNS in flight, keyed by the id their
	// EventHarnessRun carried, and the value is how each one is ended
	// (cancel.go's beginHarnessRun).
	//
	// It is the register a run would otherwise not have. A run happens INSIDE a
	// turn, on the turn's own context, so nothing outside that turn has a handle
	// on it — and [Agent.Cancel] is asked to stop work by name from a surface
	// that is not in the turn. The entry lives for exactly the length of the run.
	harnessRuns map[uint64]context.CancelFunc

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
	// standingAnswers is the same wait, for standing cards (standing_contract.go).
	standingAnswers map[uint64]chan StandingAnswer
	// standingSeq numbers those cards. It is the agent's own sequence and not
	// the task graph's, because a standing proposal is not a node: nothing is
	// reserved, nothing is admitted, and the only thing the number has to do is
	// name one outstanding question until it is answered (tools_standing.go).
	standingSeq uint64
	// taskWatchers are the standing subscriptions to task updates
	// ([Agent.TaskUpdates]). They are not the turn's hub and do not close with
	// it: a node's most important event lands minutes after the turn that
	// proposed it ended, when there is no hub to send it to.
	taskWatchers []*eventStream
	// standingNews is what fired while this window was SHUT, waiting for a
	// reader ([Agent.drainStandingInbox]). It is a queue and not a send because
	// the fold is built inside New — before the caller holds the agent, before
	// any surface has subscribed to anything — so a send there would go to an
	// empty list of watchers and the person would open a conversation with news
	// in it and see nothing. The first [Agent.TaskUpdates] takes it.
	standingNews []Event
	// jobRows is the roster id minted for each background job, keyed by the
	// registry's own number for it. The two numberings are separate counters and
	// a row keyed on the registry's would collide with a task's, which is why
	// there is a map here at all (jobrow.go).
	jobRows map[int]uint64

	// title is the session's name and titleTried marks the one attempt at
	// generating it (title.go). A resumed session loads its name from the
	// journal, so it never re-names itself.
	title      string
	titleTried bool

	// approvalPolicy is the gate as it stands NOW, when a surface has replaced
	// the one this session launched on ([Agent.SetApprovalPolicy], and the prose
	// in approvalgate.go for why that is a thing a surface may do). Nil is the
	// ordinary case — nobody has replaced anything — and Config.ApprovalPolicy
	// still answers.
	//
	// It is under mu with everything else here, and it is the ONLY approval
	// state that is: Config.ApprovalPolicy is written once before New returns
	// and never again, which is what lets task_run.go copy the whole config
	// without a lock and still be right.
	approvalPolicy *approval.Policy
}
