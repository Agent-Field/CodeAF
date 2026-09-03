package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/redact"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── retry constants (pi spec §4, verbatim from internal/exec/bare) ──────────

// retryBaseDelay is the first wait of the retry ladder; each attempt doubles it,
// so 2s, 4s, 8s — pi's schedule, which is what this loop has always walked.
//
// IT IS INTERPOLATED AND NOT TYPED OUT, and so is the attempt count that used to
// sit beside it as `maxRetries = 3`. The ladder a request actually walks is now
// the response boundary's, resolved from the person's settings
// (taxonomy_boundary.go's [Agent.failureLimits]); a second spelling of either
// number here would be the version that drifts, and a shipped install with no
// profile behind it walks exactly the ladder it always did because the
// boundary's own defaults ARE these two.
const retryBaseDelay = taxonomy.DefaultTransportBackoff

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
	"when writing is in scope — a long file is written in parts, a first write and then " +
	"write calls with append:true — and keep the final report short."

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
		// A STREAM THAT DIED MID-BODY. The three below are what the Go net stack
		// says when the far end drops a connection the response was still
		// arriving on — "read: connection reset by peer", "write: broken pipe",
		// "unexpected EOF" — and none of them was on this list: a task node
		// twenty-eight minutes into its work, suite green, writing its landing
		// note, died on attempt 1 of a reset that every other line here would
		// have retried. They are the wire and nothing else; a refusal the router
		// made about our bytes carries a status and is answered before this
		// pattern is asked ([provider.RefusalFrom]).
		`connection.?reset`,
		`broken.?pipe`,
		`unexpected.?eof`,
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

	// NOTHING MAY LEAVE A CLOCK RUNNING. This turn posts phases of its own
	// between requests — a tool batch, a gate reading the answer, a tidying
	// pass (phasenews.go) — and a surface draws every one of them until it is
	// told the work is over. There are a dozen ways out of the loop below,
	// including an interrupt and a permanent failure mid-stream, and a stale
	// phase left on the screen after any of them is exactly the defect the
	// phase clock exists for. So the end is DEFERRED rather than written at
	// each exit: a way out added later cannot forget it, and neither can a
	// panic.
	defer a.endPhase()

	// BEFORE ANY OF IT: WHAT IS THIS SESSION WORKING TOWARDS? On an unattended
	// session with a budget the goal owner is a [Steward] (principal.go), and a
	// Steward that carries work on has to be carrying it on towards something.
	// The done-condition for the WHOLE ask is written here, once, at the start of
	// the first turn — before the work has had a chance to argue for a definition
	// of done that suits it — and frozen for the life of the session
	// (principal_acceptance.go).
	//
	// EVERY OTHER SESSION PASSES STRAIGHT THROUGH IT. There is no Steward, so
	// there is nothing to write, and the cost is one nil check per turn.
	a.openAcceptance(ctx, hub)
	// AND WHAT THE TREE WAS ALREADY FAILING, read at the same moment and for the
	// same reason: this is the last instant that is certainly BEFORE the session's
	// own work, and a check that was red before anybody touched anything is the
	// project's and not this run's (principal_audit.go's [Agent.openBaseline]).
	// A watched session passes straight through it too.
	a.openBaseline(ctx)

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

	// AND ONE MORE QUESTION ABOUT THE SAME SENTENCE, ASKED BEFORE THE MODEL IS
	// SENT ANYTHING AND ANSWERED WHILE IT IS THINKING: is what they just typed
	// WORK? A cheap judge reads the REQUEST — not an answer, because there isn't
	// one yet — and a both-yes converts this turn into a task at the next step
	// boundary below (route_judge.go).
	//
	// IT IS ASKED HERE BECAUSE THE MODEL WILL NOT ASK IT LATER. The prompt teaches
	// mid-turn escalation and the belt carries propose_task, and a measured chat
	// message with four independent pieces of work in it was still ground out
	// inline over ninety tool rounds, twice: a model deep in tool momentum does
	// not stop to reach for a verb it rarely uses. So the decision is made at this
	// seam, where it is the harness's to make.
	//
	// AND IT IS ASKED WITHOUT STOPPING ANYTHING, which is the difference between
	// this line and the one above it. It used to hold the turn against a deadline
	// shorter than the judge's own floor latency, so it answered nothing and cost
	// every message the wait; now it RACES the turn and the loop simply carries the
	// handle. A no is free because nobody ever waits for it, and the turn below
	// runs exactly as it did before this existed.
	race := a.routeAhead(ctx, user)
	// AND THE RACE DIES WITH THE TURN. A verdict that lands after the answer has
	// is a verdict nobody may spend — the post-turn judge has already read that
	// turn, and a task starting on top of a finished answer is the surprise this
	// whole road is built to avoid — so the end of the turn, by any of its exits,
	// is the end of the question too.
	defer race.end()

	// AND THOSE TWO ARE THE ONLY QUESTIONS ASKED HERE. A turn used to be able to
	// be a request for an ADAPTIVE RUN — the planned graph of nodes in
	// orchestrate.go — read off an anchored cue at the head of what somebody
	// typed, and that cue was routed from exactly here. IT IS GONE, AND NO CHAT DOOR REACHES THE PLANNED DAG
	// ANY MORE. Somebody who types `orchestrate the migration` gets an ordinary
	// turn: the model answers it, and if it reads as work the route judge starts
	// a task on the one road every other piece of work takes (route_judge.go).
	// That is ABSENCE AND NOT REFUSAL — nothing special-cases those words,
	// nothing says no to them, and there is no phrasing that gets a run instead.
	//
	// THE ENGINE ITSELF IS UNTOUCHED, and its chat-side wiring is kept
	// deliberately rather than ripped out: [Agent.RunOrchestrate], the roster
	// family, the fuel gate, the snapshot and steering seams a run's page is
	// drawn from, and [Config.OrchestrateRunner] all stand. What drives
	// internal/orchestrate today is cmd/harness-design, on a driver of its own.
	// A saved program from /subharness is NOT a run — it is a task node started
	// by its own runner (subharness_contract.go) — so nothing in a conversation
	// enters that package by any road. The pieces of orchestrate.go that no chat
	// door reaches any more say so where they stand.

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
	// Reasoning is accumulated beside, never inside, the partial answer. A
	// completed step keeps it for continuation; an interrupted attempt drops it.
	reasoning := &reasoningBuffer{}

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
	// called by name from this function; the later seams are called where the
	// turn reaches them below.
	episode := a.newEpisode()
	episode.hub = hub

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

	turnObserver := func(event provider.StreamEvent) {
		switch event.Kind {
		case provider.StreamDelta:
			// THE TURN TIMES ITS OWN STREAM. The first chunk closes the
			// first-token wait and every one after it extends the generation
			// window, which are the two figures the ledger row's ttft_ms and tps
			// are made of (usage_ledger.go's [laneWitness]).
			a.turnLane.token(time.Now())
			partial.write(event.Delta)
			hub.send(Event{Kind: EventTextDelta, Text: event.Delta})
		case provider.StreamThinking:
			hub.send(Event{Kind: EventThinking})
		case provider.StreamReasoning:
			// A reasoning delta IS a token for the clock even though it is not one
			// for the transcript: the model has started writing, which is the
			// thing the first-token wait measures and the thing the person stops
			// waiting on. internal/provider's own watch counts them the same way.
			a.turnLane.token(time.Now())
			// Reasoning is NOT written to partial: it is the model's working, not
			// its answer. The sidecar is recorded only after the response completes.
			reasoning.write(event)
			if event.Delta != "" {
				hub.send(Event{Kind: EventReasoning, Text: event.Delta})
			}
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
	}
	ctx = provider.WithStreamObserver(ctx, turnObserver)
	// ONE MECHANISM ANSWERS ONE SILENCE, AND IT IS NOT THIS ONE. This loop used
	// to keep a hedge of its own here: an eight-second timer that, on a request
	// which had said nothing, sent the whole prompt a second time. The transport
	// now has a better answer to the same question (internal/provider's hedge.go
	// over internal/lane's watch.go) — it derives the wait from the serving
	// lane's own posterior instead of a constant, sends the second request to a
	// NAMED alternative lane rather than back into the same pool, spends it out
	// of a process-wide budget, cancels the loser and folds both arms back into
	// the ledger, and it acts on a reasoning stall as well as on silence. Two
	// mechanisms racing one silence is two bills for one answer and two stories
	// on one status line, so the blind one was retired.

	// WHO IS WAITING ON THIS TURN. A conversation's turn is a person watching an
	// answer arrive, and the endpoint that starts soonest is what they are asking
	// for. A TASK NODE's turn is the same machinery with nobody in front of it —
	// a whole conversation, hub and observer and room, running while the person
	// is somewhere else — and speed is worth nothing to it. It routes by price
	// instead (internal/provider's velocity.go), which is the only thing said
	// here: an explicit routing row still wins over both.
	//
	// AND WHAT THAT WAIT IS WORTH, in the unit the lane chooser trades in: λ,
	// seconds per dollar (internal/lane's Lambda). Who is waiting and what their
	// waiting costs are two different facts and the router needs both — the
	// first decides whether to ask for speed at all, the second decides how much
	// a second of it may cost. A turn is worth a person's attention; it is said
	// rather than inferred for the reason WithRoutingIntent is said rather than
	// inferred.
	//
	// A TASK NODE'S λ IS NOT ZERO WHEN SOMEBODY IS SITTING IN FRONT OF THE RUN.
	// It was, on every task turn this build ever ran, and the simulator priced
	// what that bought: a fifth off the money for nearly four times the wait
	// (bench/lanelab/REPORT.md, "the λ = 0 row"). λ = 0 is the statement that a
	// second is worth NOTHING, and it is only true of work whose owner is not
	// there — a run started from a window somebody is watching has an owner
	// reading its cards as they land, and the seconds are theirs.
	//
	// THIS IS THE V1 RULE AND THE SIGNATURE SAYS SO. [lane.Lambda] takes the
	// slack, the expected duration and the deadline the plan DAG will one day
	// supply, and until it does they are zero here and the answer comes off the
	// first argument alone: attended work is worth a person's attention,
	// unattended work is worth nothing.
	//
	// AND THE INTENT NOW FOLLOWS THAT RULE INSTEAD OF SITTING BESIDE IT. It used
	// to be background for every node whatever was on the screen, on the grounds
	// that nobody reads a node's raw stream — which left the two halves of one
	// fact free to disagree, the λ saying a watched node's seconds belong to
	// somebody and the intent saying they belong to nobody. They are one stamp
	// now: the turn names its ROLE and internal/lane's table answers both, so an
	// unwatched node still asks for the cheap endpoint and a node whose run
	// somebody is sitting in front of asks for the soonest one.
	//
	// AND ALL OF IT SAID ONCE, AS A ROLE. internal/lane's roles.go holds the
	// table — what a second of this errand's wait is worth, what quality bar it
	// needs, how many calls it will make, and whether a person is reading THIS
	// stream — and the role is the only thing a call site names. The intent
	// below is kept beside it because `provider.sort` is still built from it and
	// a dozen other packages still set it, but it is now a READING of the role
	// rather than a second opinion about the same fact (internal/provider's
	// roles.go).
	ctx = provider.WithRole(ctx, a.laneRole())
	if a.config.InTask {
		ctx = provider.WithRoutingIntent(ctx, provider.IntentBackground)
	}
	ctx = provider.WithValueOfTime(ctx, a.turnLambda())

	// The slot the adapter writes each answer's endpoint into. It is per turn and
	// per agent, which is the only scope in which the answer is honest: the
	// process-wide ledger's latest sighting belongs to whichever concurrent node
	// finished last. Read beside every response by [Agent.addUsage].
	served := &provider.ServedEndpoint{}
	ctx = provider.WithServedEndpoint(ctx, served)

	// AND THE SLOT THE ADAPTER WRITES THIS TURN'S HEDGING INTO, beside it and
	// for its reason. A hedge is the one thing in this build that can spend
	// money twice (internal/provider's hedge.go), and the report is a per-call
	// slot rather than a field on the response because hedging is the adapter's
	// own bookkeeping and the response type is the OpenAI shape. Read with the
	// endpoint below, folded into the witness, and written on the ledger row
	// this turn seals (usage_ledger.go).
	hedge := &provider.HedgeReport{}
	ctx = provider.WithHedgeReport(ctx, hedge)
	// The witness is emptied here rather than at the end of the turn before, so
	// that a turn which never reaches the wire at all writes no lane figures
	// instead of the previous turn's.
	a.turnLane.reset()

	// The model is latched for the whole turn. SetModel's contract is that a
	// turn in flight finishes on the model it started on, and reading a.model
	// per step broke it: a swap between two steps would send one model the
	// transcript another model was mid-way through writing.
	//
	// The effort rung is latched WITH it, in the same breath and for the same
	// reason — and it is resolved for THIS model, so a swap mid-turn cannot
	// leave the turn sending one model's level with another model's name. It is
	// the LADDER'S answer and not a field ([Agent.effortFor]): a rung set on the
	// conversation, on the work, or on the install reaches this turn through the
	// same call the dialled level does, which is what makes one resolver true.
	model := a.Model()
	rung := a.effortFor(model)
	// AND THE SURFACE HEARS ABOUT A RESCUE WHILE IT IS STILL OUT, under the
	// latched model for the latch's own reason (lanenews.go). The report is read
	// AFTER the answer; this is the one state of a hedge that is over before the
	// answer exists, and it is the only place this build says the word "slow" —
	// while something is already being done about it.
	a.watchLaneRescue(model, hedge)

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

	// And the same stop can fall mid-TOOL-CALL, which is dearer: the arguments
	// that did stream are already paid for. This counts the writes the salvage
	// yard (salvage.go) lands from such cuts, bounded on its own budget there.
	salvages := 0

	// emptyReplies is how many times this turn has been answered with an HTTP 200
	// carrying nothing at all. It is counted for the turn rather than for the
	// step because that is the shape the measured failure had — three of them in
	// fifteen seconds — and because the ladder it is walked against is the
	// transport ladder, which is a budget for a piece of work and not for a line.
	emptyReplies := 0

	// meter is what this turn has COST, in finished tool rounds, priced against
	// what handing it over would cost (checkpoint.go). It belongs to the turn for
	// the reason the loop window and the change ledger do: it is a fact about one
	// answer, and a meter that remembered yesterday's rounds would move work out
	// of a conversation on the strength of a conversation that already ended.
	meter := &checkpointMeter{}

	for {
		// The cancel check comes BEFORE the drain: steering typed in the
		// instant before an interrupt must not be spliced into a transcript
		// this turn is abandoning unanswered. Nothing is lost — the turn's end
		// drains the queue under the same lock that clears running (agent.go),
		// so a leftover lands ahead of the next Submit's message.
		if ctx.Err() != nil {
			a.keepPartial(partial, hub)
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
			return false
		}

		// Steering lands here, between batches: the transcript tail is a tool
		// result or an assistant answer, both legal places for a user message.
		a.drainSteering(hub)
		if tags := a.takeReplyTags(); len(tags) > 0 {
			hub.send(Event{Kind: EventTaskReplyTags, TaskReplyTags: tags})
		}

		// NOTHING TOO BIG TO FIT IS SENT AND HOPED OVER. The last thing before
		// the wire, after steering has landed, because steering is part of the
		// request being measured.
		a.guardOversizeRequest(ctx, hub)
		// The horizon is stamped AFTER an oversize pass may have rebuilt the
		// transcript and immediately before the request takes its snapshot. Results
		// appended after it have not been seen and are never eligible for the
		// intra-turn fold (turnfold.go).
		episode.decisionBegins()

		// THE NODE'S PULSE, EITHER SIDE OF THE WIRE. This is the one line in this
		// package where a request actually goes out, so it is the one place a
		// heartbeat at the cadence of the work can be taken — no ticker, nothing
		// to start, and nothing written at all by a node that is genuinely wedged,
		// which is exactly the news an outside reader wants (task_beat.go). It is
		// nil for a conversation, whose liveness the presence file already carries
		// (taskpresence.go).
		a.config.beat.began()
		// AND THE FIRST-TOKEN CLOCK, on the same line and for a related reason:
		// this is the one place in this package where a request actually goes
		// out, so it is the only place the wait a person feels can be timed from
		// without timing this package's own preparation as well.
		a.turnLane.sent(time.Now())
		response, answered, err := a.completeWithRetryReasoning(ctx, hub, model, rung, partial, reasoning, warm, forming)
		a.config.beat.ended()
		// THE MODEL THIS TURN IS ON CAN CHANGE UNDER IT. A step whose budget of
		// cut streams ran out moves to the next model in the chain and says so,
		// and everything the rest of the turn attributes — the usage rows, the
		// sealed turn's model, the level the next step asks for — has to name the
		// model that actually answered rather than the one that stopped.
		if answered != "" && answered != model {
			model = answered
			rung = a.effortFor(model)
		}
		if err != nil {
			// A STEER CUTS THIS GENERATION AND OPENS THE NEXT BOUNDARY. It is not
			// an interrupt: the turn context is alive, the partial assistant text
			// stays immediately before the person's words, and the loop continues
			// with a fresh request after the ordinary drain at its head.
			if errors.Is(err, errSteerCut) {
				turn.Turns++
				// THE LANE IS READ BEFORE THE MONEY IS BANKED, because the row the
				// money writes is this call's and the witness is what measured it.
				facts := a.turnLane.answered(readHedge(hedge, served.Name()), responseOutput(response))
				a.addUsage(&turn, response, model, served.Name(), facts)
				a.tellLaneNews(model, facts, hedge)
				droppedCall := forming.any() || warm.anyAnnounced()
				a.keepSteeredPartial(partial, reasoning, droppedCall, hub)
				warm.reset()
				forming.reset()
				continue
			}
			// Interrupt (or the caller's own deadline). Whatever was streamed
			// before the cut is real work the person watched arrive, so it
			// stays in the transcript and the turn ends normally.
			if ctx.Err() != nil {
				a.keepPartial(partial, hub)
				hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
				return false
			}
			// Overflow is the one error with an answer other than reporting
			// it: compact and re-send the same step. It fires regardless of
			// CompactEnabled — that flag gates the automatic pass, not the
			// recovery from a request the provider has already refused.
			if isContextOverflow(err.Error()) && !overflowCompacted {
				overflowCompacted = true
				// AND THE REFUSAL IS THE ONE THING THAT TEACHES THE WINDOW. Every
				// other figure in this law is a claim: the catalog's row, the
				// surface's hint, this package's own default. A provider saying
				// "that did not fit" is a measurement, and it is the only one
				// available — so what was in front of it becomes the ceiling on
				// this model's claim, here and in every later process
				// ([Agent.learnServedWindow]). Until this line the loop compacted
				// and re-sent and learned nothing, so the same over-long request
				// was built again on the next long turn.
				a.mu.Lock()
				refused := a.estimateTokensLocked()
				a.mu.Unlock()
				a.learnServedWindow(model, refused)
				if compacted, compactErr := a.compact(ctx, hub); compacted && compactErr == nil {
					continue
				}
			}
			// A permanent failure mid-stream is still a step the person
			// watched: the streamed text is kept and the turn is sealed, so an
			// error leaves the same record an interrupt does and the surface
			// gets the turn's duration with the reason.
			a.keepPartial(partial, hub)
			hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(turn, started, model)})
			return false
		}

		turn.Turns++
		// WHO ANSWERED, AND WHAT THE RESCUE COST, folded onto what was timed
		// above. It is read here, before the money is banked, because it is the
		// same grain — one response — and it is now the very row that response's
		// ledger line carries ([Agent.addUsage]) rather than something held back
		// for the turn's seal to collect.
		facts := a.turnLane.answered(readHedge(hedge, served.Name()), responseOutput(response))
		a.addUsage(&turn, response, model, served.Name(), facts)
		// AND THE SURFACE IS TOLD WHO ANSWERED, on the same line and at the same
		// grain (lanenews.go). It is the push half of a seam whose pull half
		// cannot exist: a status line cannot see a stream, and the arrow between
		// this package and a surface only points one way.
		a.tellLaneNews(model, facts, hedge)

		calls := response.ToolCalls()

		// Stop condition (pi spec §2): the loop ends when the assistant
		// response has NO tool call.
		if len(calls) == 0 {
			// A CALL THAT ANSWERED NOTHING IS A FAILED CALL, AND IT IS WRITTEN
			// DOWN AS ONE. No words, no tool call, nothing the provider counted:
			// that is not a short answer, it is an endpoint that did not answer,
			// and the journal used to record it as an empty assistant message —
			// indistinguishable, to anybody reading the file afterwards, from a
			// model that had simply finished. Three of them in fifteen seconds
			// were the front half of the measured failure ([journalError]).
			//
			// AND THE TURN NO LONGER ENDS ON IT. An empty 200 used to be written
			// down once and the loop stopped, which on one measured run ended the
			// whole thing eighteen minutes in with hours of budget unspent. The
			// response boundary reads it for what it is — the wire, never the
			// model, never the work — and a transport verdict is not allowed to
			// end a turn (taxonomy_boundary.go's [Agent.readEmptyReply]). The
			// transcript is untouched either way, so the retry re-sends exactly
			// the messages the empty attempt was sent.
			if turnBroke(response) {
				a.journalFailedCall(ctx, model, "", errEmptyAnswer, emptyReplies+1, a.requestEstimate())
				emptyReplies++
				if verdict := a.readEmptyReply(model, emptyReplies); verdict.Retries() {
					partial.reset()
					if waitErr := backoffWait(ctx, verdict.Backoff); waitErr == nil {
						continue
					}
				}
			} else {
				a.recordAssistant(ai.Message{Role: "assistant", Content: assistantContent(response)}, reasoning.snapshot())
			}
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
			// will be sent next. Its two citizens are the cross-turn stub and the
			// current-turn fold, which run BEFORE the compaction check. That order is
			// their whole economy: a transcript whose old results have just become
			// one-line pointers may no longer be over the threshold at all, so the
			// check that follows weighs what the next request will actually carry.
			episode.preDecision(ctx)
			a.maybeCompact(ctx, hub)
			// AND BEFORE THE TURN IS ALLOWED TO END: DID THE ASK END WITH IT?
			//
			// A turn stops when the model emits no tool call, and until this line
			// nothing anywhere checked whether that stop meant the work was finished.
			// It very often does not — "I've finished the parser, next I'll wire the
			// handlers" ends a turn exactly as firmly as a finished job does — and on
			// a measured ten-hour run every harness in the comparison, this one
			// included, stopped with hours of the ask unused.
			//
			// So the same reader the marks use is shown the same account of the work
			// and asked the same remains question the ceiling asks (checkpoint.go). A
			// turn whose last words put a question to the PERSON is never re-opened,
			// because it is waiting rather than stopping; everything else that is not
			// finished is re-opened with one line saying what is left, ON THE SAME
			// METER — so the ceiling still bounds it and a re-opened turn that reaches
			// the ceiling hands off exactly as any other does.
			//
			// AND A TURN THAT BROKE IS NOT A TURN THAT STOPPED SHORT. The whole
			// response goes down rather than its text, because the question this
			// answers is about how the step ENDED and not about what it said
			// ([turnBroke]). The other way a turn ends badly — a call that
			// errored — never arrives here at all: the error path above returns
			// before the loop reaches this line, and that is deliberate.
			//
			// AND THE PERSON IS TOLD IT IS HAPPENING. Everything from the
			// model's last word to the end of the turn is a gate reading an
			// answer, and every one of those gates is another model call: a
			// recon watched this gap run for minutes with the surface drawing
			// nothing but a pulse, because the request the phase clock was
			// following had finished and the ones underneath these lines are
			// made without the turn's stream (checkpoint.go, route_judge.go).
			a.tellPhase(provider.PhaseChecking, "whether the work is finished", time.Now())
			again, over := a.checkpointReopen(ctx, hub, user, meter, &turn, started, model, response)
			a.endPhase()
			if over {
				return true
			} else if again {
				continue
			}
			// AND THE LAST QUESTION OF THE TURN, asked only of a turn that answered
			// in words alone: should that have been WORK? It is the same judge the
			// front of this function asked about the request, reading what the
			// request alone could not have told it. A second small model reads
			// what was asked and the shape of what came back, and a yes STARTS it as
			// a task and says so on the transcript (route_judge.go). It is silent
			// when it cannot work, it is rate-limited to one start every few turns,
			// and it is asked before the turn is sealed so that the work it starts is
			// on the rail by the time the person reads the answer.
			a.tellPhase(provider.PhaseChecking, "whether that should be work", time.Now())
			a.routeJudge(ctx, hub, user, usedTools, response.Text())
			a.endPhase()
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

		// A CALL THE LIMIT CUT IN HALF IS SALVAGED BEFORE IT IS RECORDED
		// (salvage.go): a severed write gets its arguments repaired to what
		// verifiably arrived and runs like any other call — gate, events,
		// transcript all see the repaired bytes — and its result is reshaped
		// below into the way to continue. Everything else severed keeps its
		// refusal, reworded from a parser's shrug into cause and remedy.
		severed := a.considerSeverance(calls, store.ClassifyEnd(provider.FinishReason(response), true), &salvages)

		assistant := ai.Message{Role: "assistant", Content: assistantContent(response), ToolCalls: calls}
		a.recordAssistant(assistant, reasoning.snapshot())
		visibleText := strings.TrimSpace(messageContentText(assistant)) != ""
		partial.reset()
		usedTools = true

		// ── THE ENFORCED RUNG OF A PROCESS RULE (processrule.go) ──
		//
		// THIS IS THE ONE BOUNDARY WHERE BOTH FACTS ARE IN HAND: the batch the
		// model wants run, and whether it wrote anything visible beside it. The
		// assistant message is already in the transcript and nothing has reached
		// the world yet, so a rule that has been advised and ignored can answer
		// the submission with its demand INSTEAD of executing it — which is the
		// difference between a rule and a suggestion, and the thing the second
		// silent note had been promising in words it could not keep.
		//
		// A COMPLYING MODEL NEVER REACHES THE CALL. Every rule passes a
		// submission that meets it, and the write-your-notes rule only holds
		// anything after its advisory has been said twice into an unbroken
		// silence — so this is a map lookup on the ordinary path.
		//
		// IT IS A LINE HERE RATHER THAN A PRE-ACTION HOOK for [Agent.checkpointRound]'s
		// reason: it may STOP something, and hooks.go's law reserves that for the
		// loop that owns the turn's usage, request and meter.
		if hold, held := episode.holdSubmission(submission{calls: calls, visibleText: visibleText}); held {
			if hold.stop {
				return a.stopForProcessRule(ctx, hub, calls, hold, &turn, started, model)
			}
			a.withholdSubmission(hub, calls, hold)
			// The reads this response started early are dropped exactly as a
			// provider retry drops them, and for the same reason the early-start
			// law admits read-only calls only: a discarded read costs the work and
			// nothing else, and the model must not be handed the answer to a call
			// the harness has just refused to run.
			warm.reset()
			continue
		}

		results := a.runToolsWarm(toolCtx, episode, calls, hub, warm)
		// AND A TURN THIS SESSION HAS ALREADY LET GO OF STOPS HERE, WRITING
		// NOTHING. [waitBatch] is the one wait in this loop that can return with
		// its work still running, and everything below this line writes: the tool
		// messages go into a.messages, which by now belongs to whatever turn the
		// person started after they stopped this one. A left-behind turn appending
		// its results there would be another conversation's transcript growing
		// tool rows nobody asked for.
		//
		// It is read here and nowhere else because this is the only boundary an
		// abandoned turn can reach — the batch above is what it was parked in, and
		// its own cleanup already knows to do nothing (agent.go's turnSeq). The
		// read is non-blocking and costs an ordinary turn one closed-channel test
		// per tool round.
		if abandoned(ctx) {
			return false
		}
		if severed != nil {
			results[severed.index] = severed.amend(results[severed.index])
		}

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
		episode.postFeedback(ctx, hub, calls, results, visibleText)
		// A THIRD LOOP SIGNAL ENDS THE TURN. Post-feedback can observe the
		// trajectory but does not own the request, usage or checkpoint meter, so it
		// leaves this bit on the episode and the turn spends it here through the
		// same governed hand-off as the ordinary checkpoint ceiling. If that road
		// is unavailable, the helper still seals the turn with an honest line about
		// what was left rather than letting a fourth warning disappear into it.
		if episode.loopHandoff && a.handOverLoopingTurn(ctx, hub, user, meter, &turn, started, model) {
			return true
		}

		// AND THE RACE STARTED AT THE FRONT OF THE TURN IS ASKED WHETHER IT HAS
		// ANSWERED YET (route_judge.go). It is a non-blocking read: a question still
		// in flight costs this boundary nothing and gets asked again at the next one.
		// A both-yes ENDS NOTHING — it tightens the meter below, so the reading of
		// the work happens at this boundary rather than after the full handoff
		// price. It stands above that line because it feeds it, and because a
		// verdict read after the meter had already counted this round would arrive
		// one boundary too late to move the mark it is pulling down.
		a.routeTriage(race, meter)

		// AND THE PRICE OF THE ANSWER IS READ, at the same boundary and against
		// what handing it over would cost instead (checkpoint.go). The two prior
		// answers to a grinding turn both decide BEFORE there is any evidence —
		// the prompt teaches a judgement the model forgets under momentum, and the
		// route judge reads a request nobody has worked on yet — so this is the
		// one reading taken while the cost is a fact. At each geometric mark a
		// sidecar on the tier that thinks is shown the transcript and asked to
		// sketch what is left; a sketch with independent parts in it ends the turn
		// there, and past the last mark the harness stops reading, ends the turn,
		// and moves what is left onto the one road, where the work runs supervised.
		//
		// NOTHING OF THAT REACHES THE RUNNING MODEL. The question is asked beside
		// the turn and never inside it, which is the whole of the wave that measured
		// it: a model deep in tool momentum answers a mid-turn question with a tool
		// call up to half the time.
		//
		// IT IS A LINE HERE RATHER THAN A HOOK because it may STOP something, and
		// the control plane's law is that pre-action is the only hook that may
		// (hooks.go). It is the one seam left in this loop that can end a turn out
		// of a judgement, and a false is the turn carrying on exactly as it would
		// have.
		if a.checkpointRound(ctx, hub, user, meter, &turn, started, model, calls, nil) {
			return true
		}

		// The ordinary stub citizen remains an end-of-turn pass: running it here
		// would rewrite old turns in the middle of this one and change its cache
		// economics. Only the current-turn fold belongs at every step boundary.
		a.foldTurnOutputs(episode.seenThrough, episode.consumedReads, hub)
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

// stoppedSoupNote is what the person reads when the reply they stopped is not
// in the conversation. It says the same thing the reply guard's own retry note
// says, because it is the same judgement about the same text.
const stoppedSoupNote = "the reply you stopped had lost its thread — that text was not kept"

// keepPartial records the interrupted step's streamed text as an assistant
// message. Nothing is recorded when nothing was streamed — an empty assistant
// turn is noise in the transcript and a shape some providers reject.
//
// AND NOTHING IS RECORDED WHEN WHAT STREAMED HAD STOPPED BEING LANGUAGE. The
// reply guard cuts a degenerate stream it wins the race against; a person who
// stopped the stream first used to be handed the soup as their own kept reply
// — written to the transcript, the journal and the store, and replayed on every
// request after, which is the one way a provider's bad minute became a
// conversation's bad afternoon (streamguard.go's header: junk in the
// transcript breeds junk). The judgement is the guard's own
// ([provider.LostItsThread]), and it is not made at all when the guard is off:
// off means the person sees, and keeps, whatever arrives.
func (a *Agent) keepPartial(partial *partialBuffer, hub *eventHub) {
	text := partial.take()
	if strings.TrimSpace(text) == "" {
		return
	}
	if a.stoppedSoup(text, hub) {
		return
	}
	a.record(textMessage("assistant", text))
}

// stoppedSoup is the keep path's one question, and its one word to the person
// when the answer is yes.
func (a *Agent) stoppedSoup(text string, hub *eventHub) bool {
	if a.config.ReplyGuardOff || !provider.LostItsThread(text) {
		return false
	}
	hub.send(Event{Kind: EventNotice, Text: stoppedSoupNote})
	return true
}

// keepSteeredPartial records the legal assistant half of a cut generation.
// Tool calls are deliberately absent: a call whose result can never follow is
// a provider-invalid assistant message. When fragments had arrived, the text
// says why that instruction is not in the record; otherwise only the text and
// continuation metadata actually received are kept.
func (a *Agent) keepSteeredPartial(partial *partialBuffer, reasoning *reasoningBuffer, droppedCall bool, hub *eventHub) {
	text := partial.take()
	// The same law as keepPartial: a steer that cut a stream mid-soup keeps
	// none of it, and the person is told.
	if a.stoppedSoup(text, hub) {
		return
	}
	if droppedCall {
		if strings.TrimSpace(text) != "" {
			text += "\n\n"
		}
		text += "[incomplete tool call dropped when you steered]"
	}
	if strings.TrimSpace(text) == "" {
		// A reasoning-only cut has no legal visible assistant message to anchor.
		// Omitting it also omits the aligned sidecar, so no reasoning from beyond
		// the bytes actually received can appear on the next request.
		return
	}
	a.recordAssistant(textMessage("assistant", text), reasoning.snapshot())
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
// AND IT IS NOT WHERE THE MACHINE'S LEDGER IS WRITTEN, which is the whole of
// issue #269. A seal is the shape of a TURN — its duration, its place in the
// conversation — and every turn that never got to seal was money the meter had
// and the ledger never saw. The ledger's grain is the CALL and its door is
// [Agent.bank]; nothing here.
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
	a.noticeNewerBuild()
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
func (a *Agent) completeWithRetry(ctx context.Context, hub *eventHub, model string, rung effort.Rung, partial *partialBuffer, warm *warmBatch, forming *formingBatch) (*ai.Response, string, error) {
	return a.completeWithRetryReasoning(ctx, hub, model, rung, partial, &reasoningBuffer{}, warm, forming)
}

func (a *Agent) completeWithRetryReasoning(ctx context.Context, hub *eventHub, model string, rung effort.Rung, partial *partialBuffer, reasoning *reasoningBuffer, warm *warmBatch, forming *formingBatch) (*ai.Response, string, error) {
	// THIS CALL'S WORDS ARE A REPLY SOMEBODY READS, and it is the one place in
	// this package that can say so: every request that goes out through here is
	// the turn's own, and every gate, judge, title and memo is made from some
	// other function.
	//
	// IT DECIDES WHAT HAPPENS TO A RESPONSE THAT THOUGHT AT LENGTH AND SAID
	// NOTHING (internal/provider's answer.go). For a reply, the working IS the
	// reply and the person gets it rather than a turn that appears to have
	// answered nothing. For a gate — which asks for `{"work": false}` and has
	// its answer read by a parser — a missing object is missing, and a
	// deliberation salvaged into its place would start work off a sentence the
	// model was still arguing with itself about.
	ctx = provider.WithProseAnswer(ctx)
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
	// THE LADDER'S LENGTH IS THE BOUNDARY'S, not this file's constant. It is
	// read once, outside the loop, because a bound that could change between two
	// attempts of one ladder is a ladder nobody can reason about afterwards
	// (taxonomy_boundary.go).
	attempts := a.failureLimits().TransportAttempts
	for attempt := 0; attempt < attempts; attempt++ {
		// Each attempt streams the reply from the beginning, so the buffer
		// starts empty: an attempt that dies half-way through its text and an
		// interrupt during the next one would otherwise record the two halves
		// concatenated as one answer.
		partial.reset()
		reasoning.begin(model)
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

		// The rung is stamped PER ATTEMPT rather than once outside the loop,
		// because the model can change inside it. A dialled level is a choice
		// about a model and is held per model id (agent.go), so a step that has
		// moved to a fallback asks the ladder again for THAT model — never for
		// the level they dialled onto the model that stopped answering.
		attemptCtx := ctx
		if rung != effort.None {
			attemptCtx = provider.WithConfiguredEffortRung(ctx, rung)
		}
		// What this call is FOR, for the model-call log. A conversation's own
		// turn and a task child's turn run the identical loop, and the one thing
		// that tells them apart is whether this agent IS a node — so the word
		// follows that, and the node it is gets named beside it.
		if a.config.taskID != 0 {
			attemptCtx = provider.WithCallNode(provider.WithCallTag(attemptCtx, "task"),
				strconv.FormatUint(a.config.taskID, 10))
		} else {
			attemptCtx = provider.WithCallTag(attemptCtx, "turn")
		}
		messages, carried := a.snapshotWithReasoning()
		attemptCtx = provider.WithMessageReasoning(attemptCtx, carried)
		attemptCtx, generation := a.beginGeneration(attemptCtx)
		response, err := a.client.CompleteWithMessages(attemptCtx, messages,
			ai.WithModel(model), ai.WithTools(a.beltDefinitions()))
		cause := a.endGeneration(generation)
		if errors.Is(cause, errSteerCut) {
			return response, model, errSteerCut
		}
		if err == nil {
			return response, model, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return nil, model, ctx.Err()
		}
		// AND THE FAILURE IS WRITTEN DOWN BEFORE ANYTHING DECIDES WHAT TO DO
		// ABOUT IT. Every other outcome of a request reaches the journal; this
		// one reached nothing at all, and a measured run (see [journalError])
		// left five hours of budget unspent with the whole record of why being a
		// turn that stopped. It is journaled per ATTEMPT, so a ladder of three
		// reads as a ladder.
		a.journalFailedCall(ctx, model, "", err, attempt+1, a.requestEstimate())
		// AND THE BOUNDARY READS IT. The row above says WHAT the provider said;
		// this says what the harness took it to MEAN, which is the only half of
		// the record the money turns on (taxonomy_boundary.go). The verdict is
		// read below rather than acted on here, because the two answers already
		// in this loop — a guard cut and a refusal of our own bytes — are more
		// specific than any class and must keep their own arms.
		verdict := a.readCallFailure(err, model, "", attempt+1)
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
					rung = a.effortFor(model)
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
		// ── WHOSE MISTAKE WAS IT? ────────────────────────────────────────────
		//
		// THE REFUSAL ANSWERS FOR ITSELF, BEFORE ANY PATTERN READS ITS SENTENCE.
		// A 4xx that named no upstream is the router reading OUR OWN BYTES and
		// saying no ([provider.APIError.OurRequest]), and every endpoint alive
		// will say the same thing about the same request — so the ladder stops
		// here rather than spending 2s, 4s and 8s to be told it three times. A
		// 4xx that DID name an upstream is that upstream's refusal, another
		// endpoint may serve it, and the adapter has already taken the refusing
		// lane out of the ledger so the next attempt is routed elsewhere
		// (internal/provider's velocity.go, refuseUpstream).
		//
		// IT IS A SHAPE AND NEVER A STATUS LIST, which is the whole point: the
		// measured failure was a 400 whose text — "Provider returned error" —
		// matched the retryable pattern and was retried three times into the same
		// wall, while a differently-worded 400 from the same upstream would have
		// been given up on at once. The pattern is asked second now, and only
		// about errors that carry no refusal to ask.
		if refusal, ok := provider.RefusalFrom(err); ok && refusal.OurRequest() {
			return nil, model, err
		}
		errMsg := err.Error()
		if isContextOverflow(errMsg) || !isRetryable(errMsg) {
			return nil, model, err
		}
		if verdict.Retries() {
			// AND THE WAIT IS SAID OUT LOUD. This ladder is the longest silence
			// in the whole request path — two seconds, then four, then eight,
			// with a failed request in front of each of them — and until this
			// line it told the surface nothing at all, so a person watching a
			// turn back off for fourteen seconds saw a clock counting a request
			// that had already failed. The rung rides as the detail in the
			// person's own arithmetic, so the line reads `trying again · 2 of 4`
			// (internal/provider's phase.go spells the word).
			a.tellPhase(provider.PhaseRetrying,
				fmt.Sprintf("%d of %d", attempt+2, attempts), time.Now())
			waitErr := backoffWait(ctx, verdict.Backoff)
			a.endPhase()
			if waitErr != nil {
				return nil, model, waitErr
			}
		}
	}
	return nil, model, fmt.Errorf("after %d retries: %w", attempts-1, lastErr)
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
//
// AN OVERRUN IS AN ENDPOINT CLAIM and shares the silence budget deliberately.
// A reply that ran past the wall its own lane earned is that lane failing to
// finish, exactly as a reply that went quiet is — the ledger struck it either
// way (internal/provider's noteCutProvider) — so the question "did anything
// actually move" governs both.
func cutBudget(cut *provider.StreamCut, rerouted bool) int {
	if cut.Reason == provider.CutBabble {
		return babbleRetries
	}
	// A machinery leak gets the babble budget for the babble reason: the answer
	// came back wrong-shaped, and asking the same lane again returns the same
	// shape. The one re-ask exists because the ledger struck the lane on the
	// cut, so the next ask lands on a different endpoint serving the same
	// model — where the same model usually answers in language.
	if cut.Reason == provider.CutMachinery {
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
	case provider.CutOverrun:
		return "the reply kept going and never finished — asking again"
	case provider.CutMachinery:
		return "the model answered in its own internal markup instead of words — that text was dropped, asking again"
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
	case provider.CutOverrun:
		return "the reply kept running on without finishing — finishing this one on " + next
	case provider.CutMachinery:
		return "the model kept answering in its own internal markup — finishing this one on " + next
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
	// harness says the HARNESS wrote this failure, rather than the world the
	// model reached for: a withdrawn hand (withdrawn.go), or a door that refused
	// the call before it ran (the pre-action chain). It rides out to the runner
	// on [Event.HarnessMade], and every counter that judges the model by its
	// steps skips it — the harness's failures are the harness's steps.
	harness bool
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
	// A tool the belt does not have would only produce the dispatcher's miss —
	// "Unknown tool", or a withdrawal for a hand that was taken (withdrawn.go) —
	// early instead of late; refusing here keeps a warm result from ever being an
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
		// Rendered in the goroutine, never in [warmBatch.consider]'s own body:
		// this is called from the provider's read loop, which must not work.
		warm.result = a.executeTool(ctx, ep, hub, call, argsText(call))
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

func (b *warmBatch) anyAnnounced() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.announced) > 0
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
	// THE TURN RIDES INTO EVERY TOOL FROM HERE (tasklook.go). A hand that wants
	// to know what THIS turn has already been told — `tasks`, which answers the
	// same list to a model polling for work it will be woken about — has nowhere
	// else to read it: the episode is a fact about one turn and is deliberately
	// not held on the Agent, and the context is the one thing already threaded
	// from the turn down into every Execute.
	ctx = withEpisode(ctx, ep)

	// THE ARGUMENTS ARE RENDERED ONCE PER CALL, HERE, and the string is carried
	// to every event this batch sends about that call — the begin it sends here,
	// the end or the failure below, and the finished event the execution sends
	// (see [Agent.executeTool]). [argsText] is not free on the calls that matter:
	// a write or an edit past the cap is brought under it by a bisection that
	// decodes and re-encodes the whole payload at every step, so rendering the
	// same 20k write three times is three of those searches for one identical
	// string. The three events must carry IDENTICAL bytes anyway — a surface
	// pairs a finished row with the row it has been drawing — so one rendering is
	// not an optimization of three, it is the honest spelling of them.
	rendered := make([]string, len(calls))
	for index, call := range calls {
		rendered[index] = argsText(call)
		hub.send(Event{
			Kind: EventToolBegin,
			Tool: call.Function.Name,
			Hint: a.gloss(call),
			Args: rendered[index],
		})
	}

	// THE NARRATOR ARMS BESIDE THE WORK, not after a long silence. A half-second
	// dwell skips instant batches; anything that runs longer gets a cheap line
	// while it is still live. The child context is cancelled on return so a late
	// answer cannot rewrite a settled caption.
	if len(calls) > 0 {
		captionCtx, disarmCaption := context.WithCancel(ctx)
		defer disarmCaption()
		go func() {
			timer := time.NewTimer(captionDwell)
			defer timer.Stop()
			select {
			case <-timer.C:
				a.maybeCaption(captionCtx, hub, calls, rendered)
			case <-captionCtx.Done():
			}
		}()
	}

	// AND THE CLOCK MOVES WITH THE BATCH. A tool round is the longest wait in
	// this package by a wide margin — a `go test` runs for minutes where a
	// request runs for seconds — and until this line the phase clock went quiet
	// the moment the stream ended, so the surface counted a request that was
	// already over while a build ran underneath it (phasenews.go).
	//
	// A BATCH IS ONE STORY AND IS NAMED ONCE. Several calls run at once here,
	// they finish in scheduler order, and a clock that renamed itself as each
	// one landed would be two stories about one wait — so the batch takes the
	// name of the FIRST call, which is the one the person watched arrive first
	// and the one the transcript already puts at the top of the round. The name
	// is the tool's own, spelled exactly as the begin event above spells it,
	// because a surface that had to translate it would translate it differently
	// from the next one.
	if len(calls) > 0 {
		a.tellPhase(provider.PhaseRunning, calls[0].Function.Name, time.Now())
		defer a.endPhase()
	}

	slots := newBatchSlots(calls)
	var wg sync.WaitGroup
	for index, call := range calls {
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
				slots.put(idx, running.result)
			}(index, started)
			continue
		}
		wg.Add(1)
		go func(idx int, c ai.ToolCall, args string) {
			// wg.Done outermost, so a faulted tool still releases the batch:
			// the slot this goroutine owns keeps its seeded panic result
			// rather than hanging every sibling behind a Wait that never
			// returns.
			defer wg.Done()
			defer guard.Recover("session tool " + c.Function.Name)
			slots.put(idx, a.executeTool(ctx, ep, hub, c, args))
		}(index, call, rendered[index])
	}
	// THE BATCH IS WAITED FOR, AND THE WAIT HAS A SECOND STAGE. See the header:
	// this wait must not be cut by the turn's own cancellation, because a
	// cancelled tool is entitled to the seconds it takes to hand back what it
	// actually did, and cutting it there would throw that away on every ordinary
	// stop. What it IS cut by is the abandon signal, which is closed only once a
	// person's stop has been given its whole settling window and the turn still
	// has not let go (abandon.go's [waitBatch], and issue #265 for the four
	// minutes that is worth).
	//
	// The goroutines are NOT waited for after that and are not stopped either —
	// there is no way to stop them, which is the entire reason this exists. They
	// write into slots that are safe to write into behind us ([batchSlots]) and
	// they send into a hub that has already closed, which ignores them.
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	results := slots.taken(waitBatch(ctx, finished))

	// The end events carry Args as well as Output. Carrying the arguments rather
	// than making the surface remember the begin event costs nothing — the
	// rendering is the one done above — and buys an end event that is
	// self-contained, which is what a surface that renders a finished row from
	// one event needs.
	for index, call := range calls {
		if results[index].isError {
			hub.send(Event{
				Kind:   EventToolFailed,
				Tool:   call.Function.Name,
				Hint:   clip(firstLine(results[index].text), hintLimit),
				Args:   rendered[index],
				Output: capOutput(results[index].text),
				// WHOSE FAILURE THIS WAS travels with it. Everything counting
				// steps out of band — the runner's no-progress ledger above all —
				// reads events and not results, so a fact kept only on the result
				// is a fact no counter can act on (withdrawn.go).
				HarnessMade: results[index].harness,
			})
			continue
		}
		// A successful tool's hint is empty: the result belongs to the model,
		// and the person already read what the call was going to do. Output is
		// there for a person who asks to see it anyway.
		hub.send(Event{
			Kind:   EventToolEnd,
			Tool:   call.Function.Name,
			Args:   rendered[index],
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
// `rendered` is [argsText] of this same call, HANDED IN RATHER THAN COMPUTED,
// because the caller has already rendered it for the events it sends about the
// call and the three must carry identical bytes (runToolsWarm says why). A call
// site with nothing in hand renders it itself; there is no path that may pass
// something else.
//
// It sits INSIDE the dispatch loop rather than above it, after the belt has
// been found to carry the tool: a call for a tool that does not exist is
// answered "Unknown tool", never asked about. A question about a tool nobody
// has is a question with no right answer.
func (a *Agent) executeTool(ctx context.Context, ep *episode, hub *eventHub, call ai.ToolCall, rendered string) toolResult {
	for _, tool := range a.beltTools() {
		if tool.Name != call.Function.Name {
			continue
		}
		running, refused, allowed := ep.preAction(ctx, hub, call)
		if !allowed {
			// A REFUSED DOOR IS THE HARNESS'S OWN ANSWER. The tool never ran, the
			// world never saw the call, and what came back was written here — by a
			// policy, a scope, a capability that is off, a person saying no. Marked
			// at the ONE place every veto passes through rather than inside each
			// citizen, so a pre-action hook added next month cannot forget to say
			// so and have its refusals counted against the model as spinning.
			refused.harness = true
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
				Args:   rendered,
				CallID: call.ID,
				Took:   time.Since(started),
			})
		}
		if err != nil {
			// Harness-level failure: the model sees the Go error as the tool
			// result, matching pi's thrown-Error semantics.
			return a.finishToolResult(ep, call, toolResult{text: err.Error(), isError: true})
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
		return a.finishToolResult(ep, call, toolResult{text: text, isError: isError})
	}
	// ── TAKEN, OR NEVER HELD ──
	//
	// The belt cannot tell these apart: both are one lookup that missed. The
	// difference is a fact only the code that narrowed the belt has, and it
	// recorded it there (withdrawn.go) so this line can read it back.
	//
	// A WITHDRAWN HAND IS REPORTED AS A WITHDRAWAL. Measured in SWE-Marathon s4:
	// the landing pass took `bash`, `read` and `grep` off a worker's belt and the
	// worker was answered "Unknown tool: bash" — eighteen bytes naming no reason,
	// no surviving set and nothing to do instead. It retried eight times, which
	// was the only rational move left, and the stuck watch then punished it for
	// the retries. The notice below says why the hand is gone, what is still on
	// the belt by name, and what to do with what is left.
	//
	// AND IT IS THE HARNESS'S FAILURE, not the model's, so no counter spends it
	// against the model ([toolResult.harness]).
	if notice, withdrawn := a.withdrawalNotice(call.Function.Name); withdrawn {
		return toolResult{text: notice, isError: true, harness: true}
	}
	// A name nobody ever had keeps the old answer, and keeps it word for word:
	// that one IS a sentence about the model.
	return toolResult{text: "Unknown tool: " + call.Function.Name, isError: true}
}

// finishToolResult is THE ONE PLACE A TOOL RESULT GROWS ANYTHING, and the order
// of the two things it can grow is fixed here so nothing downstream has to
// guess at it.
//
// First the error→fix sidecar (fixrecall.go), which is about THIS call and
// belongs against the text that call produced. Then the outstanding jobs
// (jobfooter.go), which are about the session and belong at the very bottom,
// where the model reads them the way a shell prints its background jobs under
// the prompt — and where [stripJobFooter] can take them off again for anything
// that has to compare two results as bodies.
//
// ── AND A SECRET IS TAKEN OUT BEFORE ANY OF IT ──
//
// The first thing that happens to a result here is that token-shaped spans in
// it are replaced with a marker (internal/redact). It is THE FIRST line rather
// than the last because everything after this function is a copy: the journal
// on disk, the row on the person's screen, the transcript the model reads on
// every later request, and the error→fix store's own signature and patch, which
// outlive the session entirely. One of those copies is written by the line
// below this one, so a redaction added anywhere further out would already be
// too late for it.
//
// It sits at THIS chokepoint for the same reason the pre-action gate sits at
// the one above ([Agent.executeTool]): the batch, the early warm start, a task
// node's turns and a subharness's worker all pass through here and nothing else
// does, so a tool appended later — by the workforce, by a connected account, by
// a test — cannot return a credential without passing this line. A worker that
// ran `gh auth token` put a live OAuth token in two task journals in plain text
// before it existed.
func (a *Agent) finishToolResult(ep *episode, call ai.ToolCall, result toolResult) toolResult {
	result.text = redact.Secrets(result.text)
	return a.withJobState(ep.noteToolOutcome(call, result))
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
	"use_service":         "service",
	"gmail_search":        "query",
	"gmail_read":          "id",
	"slack_search":        "query",
	"slack_read_thread":   "channel",
	"slack_list_channels": "filter",
	// The settings read is the row it went to look at, and a call with no key
	// at all is the whole sheet, which reads honestly as its bare name.
	"settings": "key",
}

// glossFields is [glossField] for the calls where ONE argument is not enough to
// say what is about to happen.
//
// It exists for the three hands that act outside this machine, and it is the
// person's whole view of the question they are being asked: a message is who it
// is going to and what it says it is about, and an event is what it is called
// and when. "gmail_send alice@example.com" would be a question about a
// recipient, not about a message.
var glossFields = map[string][]string{
	"gmail_send":      {"to", "subject"},
	"calendar_create": {"title", "start"},
	"slack_send":      {"channel", "text"},
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
//
// A STRING THAT IS ALREADY MARKED KEEPS ONE MARK. [TaskGraph.inheritedLocked]
// fits each prerequisite report with this function, and [familyOf] then fits
// the whole inherited brief as its ground — a second pass over the same text.
// Stacking a second `…` on a cut that already carried one would read as two
// fragments, or as a fragment of a fragment, which is how a worker loses the
// plot of whether it is holding the report or a stub of it. The mark MOVES to
// the new cut; it is never doubled.
func clip(text string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(text) <= n {
		return text
	}
	cut := n - len("…")
	if cut < 0 {
		return "…"
	}
	for cut > 0 && !utf8RuneStart(text[cut]) {
		cut--
	}
	out := text[:cut]
	if strings.HasSuffix(out, "…") {
		return out
	}
	return out + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ── usage ───────────────────────────────────────────────────────────────────

// bankedCall is one call's money on its way into the books: what it cost, who
// answered, what for, how the lane behind it behaved, and the two things that
// differ between the doors — whether the machine's ledger is owed a row (a fold
// is not a call) and whether this was a step of the conversation.
type bankedCall struct {
	used   Usage
	model  string
	role   string
	lane   laneFacts
	ledger bool
	// turn says this call was a STEP OF THE CONVERSATION and not an errand run
	// beside it, which is the whole of what [Usage.Turns] counts.
	turn bool
	// context is the provider's own count of the request just served — the
	// honest number the compaction threshold prefers over an estimate — and
	// zero where nobody said.
	context int
}

// bank is THE ONE DOOR MONEY GOES THROUGH, and it is the fix for issue #269.
//
// Before it there were two: one that moved the session's meter per call and one
// that wrote the machine's ledger per TURN, and every turn that failed to seal —
// interrupted, stopped, crashed, or simply still running while somebody looked —
// was money the first door had and the second never heard about. Four surfaces
// then quoted four numbers for the same instant.
//
// So the meter and the ledger row move on ONE call, and a caller cannot have the
// first without offering the second. The ledger's own fold rule is the single
// bit of difference: a tally a child agent already wrote down for itself moves
// this session's books and writes nothing here, or the machine's day would be
// double (usage_ledger.go's second rule).
//
// THE ROW IS RECORDED OUTSIDE a.mu. Nothing that can touch a file belongs under
// the agent's lock, and although the write itself happens on a writer goroutine
// ([RecordUsage]), the lock is released before the hand-off so no reader of the
// session's totals ever queues behind the ledger at all.
func (a *Agent) bank(call bankedCall) {
	a.mu.Lock()
	a.usage.Input += call.used.Input
	a.usage.Output += call.used.Output
	// The cache figures follow the tokens they belong to. An errand is paid for
	// out of the same pocket, so leaving them out would make the session's
	// cached share a fraction of only part of its input.
	a.usage.CacheRead += call.used.CacheRead
	a.usage.CacheWrite += call.used.CacheWrite
	a.usage.CostUSD += call.used.CostUSD
	a.usage.Calls += call.used.Calls
	a.usage.EmptyReflex += call.used.EmptyReflex
	// AND THE RUNNING TURN'S OWN SHARE MOVES ON THE SAME CALL, for this door's
	// own reason: an abandoned turn never writes the seal that would carry its
	// cost, so the only honest figure to journal it with is the one accumulated
	// here (abandon.go's [Agent.Abandon]). It is reset when a turn opens and is
	// meaningless outside one, which is why it is not exported.
	a.turnSpend.Input += call.used.Input
	a.turnSpend.Output += call.used.Output
	a.turnSpend.CacheRead += call.used.CacheRead
	a.turnSpend.CacheWrite += call.used.CacheWrite
	a.turnSpend.CostUSD += call.used.CostUSD
	a.turnSpend.Calls += call.used.Calls
	if call.turn {
		a.usage.Turns++
	}
	if call.context > 0 {
		a.contextTokens = call.context
	}
	a.mu.Unlock()
	if call.ledger {
		a.recordUsageLine(call.used, call.model, call.role, call.lane)
	}
}

// addUsage folds one response's accounting into the turn and the session, and
// remembers the provider's own context size — the honest number the compaction
// threshold prefers over an estimate.
//
// AND THIS IS WHERE THE MACHINE'S LEDGER LEARNS ABOUT THE CALL, because this is
// where the provider's usage block is in hand. It used to learn at the end of
// the turn instead and lost every unsealed turn's money (issue #269,
// usage_ledger.go's first rule). The row goes out through [Agent.bank] with the
// meter, so nothing can move one without the other.
//
// It also writes ONE JOURNAL LINE PER RESPONSE, after the folds and changing
// none of them. The seal at the end of the turn is a sum of sixty-odd calls
// with wildly different shapes, and a sum cannot answer the question a cost
// autopsy actually asks — what did a call with THIS many cached tokens cost,
// served by WHOM. That had to be reconstructed from transcript byte counts
// once; the line below is so it never has to be again. See
// [sessionFile.appendCall] for why it is evidence and never spend.
//
// `model` is the turn's own latched model, which is what a mid-turn hop leaves
// standing (runTurn re-latches on an answer from further down the chain), and
// `served` is the endpoint the router says answered, "" when nothing said.
// `lane` is what the witness measured about THIS request and nothing else.
func (a *Agent) addUsage(turn *Usage, response *ai.Response, model, served string, lane laneFacts) {
	if response == nil || response.Usage == nil {
		return
	}
	usage := response.Usage
	call := Usage{
		Input:  usage.PromptTokens,
		Output: usage.CompletionTokens,
		// The cache figures are the provider's own, in whichever dialect it
		// speaks them (ai.Usage reconciles the two spellings). They are recorded
		// on the turn AND on the session because they answer two different
		// questions: what this exchange cost against what it would have, and how
		// warm the lineage has been all day.
		CacheRead:  usage.CacheReadTokens(),
		CacheWrite: usage.CacheCreationTokens(),
		// Calls counts THIS request, and every other one the session makes. It is
		// the honest denominator Turns cannot be: Turns is the conversation's own
		// steps by law, and an auxiliary call is not one of them (see
		// [Agent.addAuxiliaryUsage]).
		Calls: 1,
	}
	if usage.Cost != nil {
		call.CostUSD = *usage.Cost
	}
	turn.Input += call.Input
	turn.Output += call.Output
	turn.CacheRead += call.CacheRead
	turn.CacheWrite += call.CacheWrite
	turn.Calls += call.Calls
	turn.CostUSD += call.CostUSD

	context := usage.TotalTokens
	if context == 0 {
		context = usage.PromptTokens + usage.CompletionTokens + usage.CacheReadTokens()
	}

	// THE ROW NAMES THE MODEL THAT ANSWERED and falls back to the turn's own
	// latch. A provider that names itself in the response is the better answer;
	// one that names nothing would otherwise leave the row saying only that
	// somebody was paid (auxiliary.go's [Agent.journalRoleCall] makes the same
	// choice for the same reason).
	answered := strings.TrimSpace(response.Model)
	if answered == "" {
		answered = strings.TrimSpace(model)
	}
	a.bank(bankedCall{used: call, model: answered, lane: lane, ledger: true, turn: true, context: context})

	a.file.appendCall(journalCall{
		Model:      strings.TrimSpace(response.Model),
		Endpoint:   strings.TrimSpace(served),
		Input:      usage.PromptTokens,
		CacheRead:  usage.CacheReadTokens(),
		CacheWrite: usage.CacheCreationTokens(),
		Output:     usage.CompletionTokens,
		CostUSD:    costOf(usage),
	})
}

// errorRowMessage bounds the sentence written on a failed call's journal row.
// The upstream's own body has its own field and its own clip; this is the
// harness's one-line account of the failure, not a place for a payload.
const errorRowMessage = 512

// journalFailedCall writes ONE FAILED CALL down (see [journalError]).
//
// THE LAW: A FAILED CALL NEVER LOOKS LIKE AN EMPTY ANSWER, AND NEVER LOOKS LIKE
// NOTHING AT ALL. Every request that succeeds leaves a call line; until this,
// every request that failed left the journal exactly as it found it, and the
// autopsy of the measured run could read three empty assistant messages and a
// dead turn without learning the status, the endpoint, the provider or a single
// word of what the upstream had said.
//
// What it can say it says, and what it cannot it leaves off — a transport error
// carries no status and no provider, and a row of zeroes would read as facts.
// `role` is the errand that made the call, absent on the conversation's own
// requests, exactly as [journalCall] spells it.
func (a *Agent) journalFailedCall(ctx context.Context, model, role string, err error, attempt, estimate int) {
	if err == nil {
		return
	}
	row := journalError{
		Model:    strings.TrimSpace(model),
		Endpoint: strings.TrimSpace(provider.ServedEndpointFrom(ctx).Name()),
		Role:     strings.TrimSpace(role),
		Attempt:  attempt,
		Input:    estimate,
		Message:  clip(err.Error(), errorRowMessage),
	}
	if refusal, ok := provider.RefusalFrom(err); ok {
		row.Status = refusal.Status
		row.Provider = strings.TrimSpace(refusal.Provider)
		row.Raw = refusal.Raw
		if said := strings.TrimSpace(refusal.Message); said != "" {
			row.Message = clip(said, errorRowMessage)
		}
	}
	// A CUT IS THE ONE FAILURE THAT GOT SOMEWHERE, so it is the one that has
	// more than a sentence to write down: who was serving, how long the request
	// ran, and how much answer had arrived before it was ended. Without the last
	// two, "the reply ran past its wall" is a claim a reader has to take on
	// trust; with them it is a measurement, and the wall itself can be argued
	// with from the file (internal/provider's [provider.StreamCut]).
	if cut, ok := provider.CutFrom(err); ok {
		if named := strings.TrimSpace(cut.Provider); named != "" {
			row.Provider = named
			if row.Endpoint == "" {
				row.Endpoint = named
			}
		}
		row.Output = cut.Tokens
		row.DurationMS = cut.Ran.Milliseconds()
	}
	a.file.appendError(row)
}

// requestEstimate is the session's own count of the tokens the request that just
// failed was carrying. It is the ONLY token figure a failed call has: the
// provider counted none, so the alternative is a row that cannot say whether the
// request was small or enormous — which is the first question an autopsy asks of
// a 400.
func (a *Agent) requestEstimate() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.estimateTokensLocked()
}

// costOf is the provider's own figure for one call, zero when it did not send
// one. Zero and absent are the same thing on the journal line, which is the
// emptiness law: a call whose cost nobody reported writes no cost, and a reader
// must not be able to tell that apart from free by looking at the number.
func costOf(usage *ai.Usage) float64 {
	if usage == nil || usage.Cost == nil {
		return 0
	}
	return *usage.Cost
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

	// THE THRESHOLD FOLLOWS THE WINDOW, AND THE WINDOW IS THE MODEL CARD'S UNTIL
	// AN ENDPOINT SAYS OTHERWISE.
	//
	// There used to be a flat ceiling here — twice [defaultContextWindow], so
	// 256k — and it was put in for a real failure: the catalog row for
	// ~deepseek/deepseek-v4-flash-latest claims 1,310,720 tokens, the trigger
	// followed the claim to 1,114,112, a conversation grew to 386,309 tokens
	// with compaction never once firing, and what came back at that size was the
	// model's own template turned inside out.
	//
	// The ceiling answered that by disbelieving EVERY claim above 256k, and the
	// bill for it was paid by every model that was telling the truth. Measured
	// on 2026-08-31: a two-and-a-half-hour run on a model advertising 1.3M
	// compacted nineteen times, each pass throwing away the prefix cache the run
	// was otherwise getting 57–61% of its prompt back from, and the model was
	// reduced to keeping its own notes file to survive the folding.
	//
	// So the ceiling is not a constant any more, it is a MEASUREMENT: the
	// narrowest prompt this model has actually been refused for, learned from
	// the overflow refusal itself and remembered across processes
	// (internal/provider's NoteServedWindow, and the branch in [Agent.runTurn]
	// that teaches it). A model nobody has refused is believed; one that has
	// refused is capped at what it refused, for good. The 386k incident now
	// costs one turn per model per machine instead of every model forever, and
	// what it costs is paid by the model that earned it.
	//
	// Two guards stand behind that trade and neither is new.
	// [Agent.guardOversizeRequest] still shrinks a transcript that has grown past
	// the window before it goes out, and the reply guard cuts an answer that has
	// stopped being language (internal/provider's CutBabble and CutMachinery),
	// which is exactly the shape the 386k incident came back in.
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

// trustedWindow is this agent's own window with everything this process has
// learned about the model in use applied over it. It is what every bound below
// is derived from, and it takes no lock for [Agent.window]'s reason.
func (a *Agent) trustedWindow() int {
	return trustedWindow(a.window(), int(a.servedWindow.Load()))
}

// learnServedWindow is the closing half of the loop the threshold rides on: an
// endpoint has just refused a prompt for being too long, so what it refused is
// now the ceiling on that model's claim — in this session from the next check
// onward, and in every later process through the memo.
//
// It is called with the estimate that was refused rather than with a figure from
// the error, because no provider states one: what is known is that THIS many
// tokens was too many, here, and that is the honest ceiling.
func (a *Agent) learnServedWindow(model string, estimate int) {
	if estimate <= 0 {
		return
	}
	provider.NoteServedWindow(model, estimate)
	if learned := provider.ServedWindow(model); learned > 0 {
		a.servedWindow.Store(int64(learned))
	}
}

// noteModelWindow refreshes what is known about the window of the model now in
// use. It is called wherever the model or the window moves, so that the memo a
// previous session wrote is in force from this session's first check.
func (a *Agent) noteModelWindow(model string) {
	a.servedWindow.Store(int64(provider.ServedWindow(model)))
}

// childWindow is how large the window of the model a CHILD agent is about to run
// on should be taken to be — a task node's worker, an adaptive run's worker, a
// forked hand, an auditor.
//
// A child on this agent's OWN model inherits the window this agent is running
// on, the learned figure included: a catalog row that arrived after construction
// is a better fact than the one Config was built with, and a child built from
// Config alone would compact against a window its parent stopped believing an
// hour ago. A child on ANOTHER model asks the same catalog this agent asks
// (Config.ContextWindowFor), because the model card is the only thing that
// knows. Zero when nothing can say, which is the honest answer and leaves this
// package's conservative default standing.
//
// UNTIL THIS EXISTED A CHILD ON ANOTHER MODEL WAS HANDED ZERO OUTRIGHT, on the
// argument that a window measured for one model is not a fact about another.
// The argument is right and the conclusion was wrong: the answer to "that is not
// a fact about this model" is to ask about this model, not to fall back to the
// smallest window the surface routes to. The bill was measured on 2026-08-31 —
// a two-and-a-half-hour run folded its work nineteen times against a 128k
// default while the models doing it advertised ten times that.
//
// It reads a.model the way its callers already do, unlocked: the model a child
// is being built for was latched by the caller a few lines earlier, and this is
// the same read at the same moment.
func (a *Agent) childWindow(model string) int {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, strings.TrimSpace(a.model)) {
		return a.window()
	}
	if a.config.ContextWindowFor != nil {
		if window := a.config.ContextWindowFor(model); window > 0 {
			return window
		}
	}
	return 0
}

func (a *Agent) compactThreshold() int {
	return compactThresholdOf(a.trustedWindow())
}

// TrustedWindow is a claimed context window with everything this process has
// learned applied over it — the figure the compaction machinery works from, as
// opposed to [Agent.window], which stays the model's own claim because the
// status meter is describing the model rather than this law.
//
// Called without a model there is nothing to have learned, so the claim comes
// back untouched; [TrustedWindowFor] is the door that applies a model's memo.
//
// It is exported for the same reason [CompactThreshold] is: a surface that
// needs to know how much room the guard leaves must read the guard, not a
// second copy of it.
func TrustedWindow(window int) int { return TrustedWindowFor("", window) }

// TrustedWindowFor is [TrustedWindow] for a NAMED model: the claim, capped by
// the narrowest prompt that model has been refused for, when this process has
// ever seen it refused.
//
// An empty model, or one nothing has been learned about, gets its claim back
// unchanged — which is the ordinary case and the emptiness law applied to a
// measurement: "nothing was learned" may not be spelled the same way as "this
// model has no room".
func TrustedWindowFor(model string, window int) int {
	return trustedWindow(window, provider.ServedWindow(model))
}

// trustedWindow is the law itself over two plain numbers, so that an agent
// holding a learned figure of its own and a surface asking about a model by name
// are running one arithmetic rather than two copies of it.
func trustedWindow(window, learned int) int {
	if window <= 0 {
		return 0
	}
	if learned > 0 && learned < window {
		return learned
	}
	return window
}

// CompactThreshold is the law itself — [derivedThreshold] unless a person has
// pinned a fill, and their fill of the window when they have ([compactThresholdOf]
// picks between the two) — exported because a surface has to be able
// to say how close a conversation is to being compacted — and a surface that
// re-derived the formula from the same two constants would be a second copy of
// it, free to drift the moment either one moves (internal/tui3 reads this for
// the status meter's accent).
//
// Zero and negative windows answer zero: a threshold against an unknown window
// is a number that means nothing, and a caller must have an answer for that
// rather than treat it as a tiny model.
//
// The window is trusted BEFORE the reserve is taken, so a claim this process has
// learned to distrust can never move the trigger past what the endpoint was
// actually willing to serve. That is the guard, and it lives here rather than at
// the door because the door is not the only way a window arrives.
func CompactThreshold(window int) int { return CompactThresholdFor("", window) }

// CompactThresholdFor is [CompactThreshold] for a NAMED model, so that a surface
// drawing how close a conversation is to being folded reads the same figure the
// trigger does — including anything this process has learned about what that
// model's endpoints really serve ([TrustedWindowFor]).
func CompactThresholdFor(model string, window int) int {
	return compactThresholdOf(TrustedWindowFor(model, window))
}

// ContextFillPinned is how full a PERSON has said a window may get before it is
// folded, and whether anybody has said so at all — the settings registry's
// `context fill` row, the `AFORGE_CONTEXT_FILL_PCT` environment pin behind it,
// and the `--context-fill` flag that sets that pin for one run
// (internal/ctxbudget's PinnedFillPercent, which is where those three meet).
//
// It is exported for [CompactThreshold]'s own reason: a surface saying WHY a
// conversation folds where it folds has to read the rule the trigger reads,
// rather than open the same three sources for itself and drift from them.
func ContextFillPinned() (int, bool) { return ctxbudget.PinnedFillPercent() }

// compactThresholdOf is the governing line, over a window that has ALREADY been
// trusted. Every door above applies the cap and then comes here, so the cap is
// applied exactly once however the window arrived.
//
// TWO LAWS CAN SET THIS LINE AND A PERSON'S OUTRANKS THE DERIVATION. Unpinned —
// which is nearly every session — the line is [derivedThreshold], the reserve
// law that follows the model's own window. Pinned, it is the fill that person
// asked for, taken of that same window ([pinnedThreshold]).
//
// The distinction is the whole of it, and it is why the fill percentage was read
// nowhere near here until now: `--context-fill` has meant sixty by default since
// it was written, so a trigger that simply honoured the number would have folded
// every conversation at sixty percent of its window — dropping the derived line
// on a 1.3M-token model from around eighty-five percent to sixty, which is the
// opposite of the thing the derivation was built to fix. What makes honouring it
// safe is knowing that somebody typed it.
func compactThresholdOf(window int) int {
	if window <= 0 {
		return 0
	}
	if fill, pinned := ContextFillPinned(); pinned {
		return pinnedThreshold(window, fill)
	}
	return derivedThreshold(window)
}

// pinnedThreshold is the line a person asked for: their fill percentage of the
// trusted window, held between a ceiling and a floor that the rest of the
// compaction chain needs in order to mean anything.
//
// THE CEILING IS THE HIGHER OF THE TWO LINES THE LAW ALREADY DRAWS, and it is
// what "the completion reserve is still honoured" comes to in arithmetic. One is
// the room every call keeps for its answer and its reasoning
// (ctxbudget.CompletionReserve, the `--completion-reserve` flag's own number):
// a conversation may not grow so far that the reply it is waiting for cannot
// fit behind it. The other is [derivedThreshold] itself, because on a small
// window the completion reserve is the larger of the two and a pin of ninety
// would otherwise land BELOW the line an unpinned session gets — a person asking
// for more room being given less, which is worse than not honouring them at all.
// Above roughly 437,000 tokens the completion reserve is the binding one, below
// it the derivation is, and the pin is honoured exactly as typed anywhere under
// whichever binds.
//
// THE FLOOR IS TWICE THE VERBATIM TAIL. A line at or under the tail is a line no
// pass can reach: the tail is never folded, so a threshold below it fires on
// every step and finds nothing to take (the chain [compactTarget] documents).
// Twice it leaves a pass something to fold and somewhere to fold it to. On a
// large window the floor is far below any fill the clamp permits and never
// binds; on a small one a fill of ten would have put the line under the tail.
func pinnedThreshold(window, fill int) int {
	line := window * fill / 100
	ceiling := window - ctxbudget.CompletionReserve()
	if derived := derivedThreshold(window); derived > ceiling {
		ceiling = derived
	}
	if line > ceiling {
		line = ceiling
	}
	if floor := 2 * keepRecent(window); line < floor {
		line = floor
	}
	return line
}

// derivedThreshold is the reserve law, and it is what governs every conversation
// nobody has pinned a fill on: the window less the larger of fifteen percent of
// it and sixteen thousand tokens.
func derivedThreshold(window int) int {
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
	// the tail is at most a quarter of it. [compactTarget] sits between the
	// two and carries the rest of the chain.
	if half := window / 2; reserve > half {
		reserve = half
	}
	return window - reserve
}

// compactTarget is how far a pass folds once it has fired, and IT IS STRICTLY
// BELOW THE THRESHOLD ON PURPOSE. Until it existed the fold's stopping line was
// the trigger itself: a pass folded the fewest batches that put the estimate
// just under [CompactThreshold], the next step's few thousand tokens carried it
// back over, and the pair ran once per step for the rest of the task — one live
// run compacted fifteen times in six minutes, four to six messages a pass, with
// the estimate never once going down. Every pass rewrites the transcript
// prefix, so each one also threw away the provider's prompt cache, and the
// session repaid the whole ~135k-token prompt on every request. The task was
// not wrong; it was slow and expensive for no reason.
//
// The headroom is half the reserve the threshold already subtracts, so the
// target sits at window − 1.5×reserve and is built from the same two constants
// as the trigger rather than a third number free to drift from them. A pass
// then buys itself roughly half a reserve of growth before the next one, which
// on the default window is nearly ten thousand tokens — several steps, and a
// prompt cache that gets to live through them.
//
// Invariant, extending the one in [CompactThreshold]:
//
//	CompactThreshold(window) > compactTarget(window) > keepRecent(window)
//
// The lower bound matters for the same reason the threshold's does: a target
// under the verbatim tail is one no pass can reach, and a pass that cannot
// reach its target folds everything foldable every time. On a small window the
// half-window clamp on the reserve puts window − 1.5×reserve EXACTLY on the
// quarter-window tail, so the headroom is also capped at half the distance
// between the threshold and the tail — the target is then never lower than the
// midpoint of the two, and the chain holds all the way down to windows where
// the numbers stop meaning anything. Neither bound moves when compaction FIRES;
// the trigger and the status meter's accent are [CompactThreshold]'s alone.
func compactTarget(window int) int {
	threshold := compactThresholdOf(window)
	if threshold <= 0 {
		return 0
	}
	headroom := (window - threshold) / 2
	if gap := (threshold - keepRecent(window)) / 2; gap < headroom {
		headroom = gap
	}
	return threshold - headroom
}

func (a *Agent) compactTargetTokens() int {
	return compactTarget(a.trustedWindow())
}

// keepRecentTokens is the verbatim tail budget, capped at a quarter of the
// window. Keeping 20k of a 200k window is a tail; keeping 20k of an 8k window
// is not a compaction at all, and without the cap a small-window session would
// find nothing to summarize and overflow with the pass "succeeding".
func (a *Agent) keepRecentTokens() int {
	return keepRecent(a.trustedWindow())
}

// keepRecent is [Agent.keepRecentTokens] as a function of the window alone, so
// [compactTarget] can hold its invariant against the same figure the cut point
// uses rather than a second reading of it.
func keepRecent(window int) int {
	keep := compactKeepRecentTokens
	if quarter := window / 4; quarter < keep {
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
	ceiling := a.trustedWindow()
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
//     be read, and the fold runs down to [compactTarget] — below the threshold
//     by half a reserve, so the pass that just ran is not the pass that runs
//     again after the next step. User messages are never folded: a person's
//     own words are the one thing in a transcript that nothing else can
//     reconstruct.
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
	// A PASS SAYS ITSELF WHILE IT RUNS, and it says itself from OUTSIDE the
	// lock. A phase post reaches a surface, and a surface answers one by asking
	// for a frame — so a phase posted with this agent's mutex held is a surface
	// waiting on a lock the pass is holding while the pass waits on the surface
	// (phasenews.go). Nothing is being claimed here, so an in-flight pass that
	// is refused below still ends this clock on the way out.
	a.tellPhase(provider.PhaseTidying, "the conversation", time.Now())
	defer a.endPhase()

	a.mu.Lock()
	if a.compacting {
		a.mu.Unlock()
		return false, ErrCompactionInFlight
	}
	a.compacting = true
	tokensBefore := a.estimateTokensLocked()
	// AND THE PASS IS ANNOUNCED THE MOMENT IT BEGINS, not only when it ends.
	// [EventCompacting] has said in its own doc comment since it was declared
	// that a pass is visible while it runs, and until this line nothing in the
	// repository ever sent it — three handlers in internal/tui3 waited on an
	// event with no sender, so a person watching a turn stop to tidy itself saw
	// the finished line and never the work. The event goes out under the lock
	// deliberately: [eventHub.send] only appends to queues and cannot block,
	// which is what makes it safe here and a phase post not.
	hub.send(Event{Kind: EventCompacting, Hint: "compacting " + approxTokens(tokensBefore) + " tokens"})

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
		pass.folded, pass.marker = a.foldLocked()
	}
	a.compacting = false

	if pass.empty() {
		// Nothing was old enough to stub and nothing was foldable: the whole
		// transcript is the person's own words and the recent tail, which is
		// what [ErrNothingToCompact] has always meant.
		a.mu.Unlock()
		// AND [EventCompacted] FOLLOWS [EventCompacting] ON EVERY PATH, which is
		// the promise the pair is declared with (session.go). A surface opens a
		// row on the first and settles it on the second; a pass that announced
		// itself and then said nothing would leave that row open for the rest of
		// the session, so a pass that found nothing says exactly that.
		hub.send(Event{Kind: EventCompacted, Hint: "nothing to compact"})
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
	//
	// The floor is COUNTED rather than shaped ([countEntries]), because the count
	// is the whole of what it is: shaping the rewritten transcript to take
	// len() of it would walk every argument and every capped result a second
	// time, under this lock, at the one moment a person is most likely to be
	// pressing Esc ([Agent.Interrupt] wants the same lock).
	a.earlier = earlier
	a.earlierFloor = countEntries(a.messages)

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
		windowReasoning := append([]provider.MessageReasoning(nil), a.messageReasoning[1:]...)
		a.file.appendCompaction(pass, tokensBefore, window, windowReasoning)
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
// FOUR THINGS ARE NEVER FOLDED, and each for its own reason:
//
//   - message[0], the system prompt, which carries the memory block and the
//     state card and is rebuilt per turn anyway;
//   - USER MESSAGES, anywhere, because a person's words are the one part of a
//     transcript that cannot be reconstructed from anything else — a question
//     they asked and never got answered has to still be in front of the model;
//   - the RUNNING TURN, because its assistant notes, exact calls and results are
//     working memory rather than conversation history; turnfold.go alone may
//     replace consumed read results while leaving that structure intact;
//   - the verbatim tail below [Agent.cutPointLocked], which is the work in hand.
//
// An assistant message and the tool results answering it go TOGETHER, always. A
// tool result whose call was folded away is an orphan every provider rejects
// with a 400 — on this request and on every request after it, because the
// transcript is append-only — so the walk moves in whole batches and stops on a
// batch boundary.
//
// The walk stops at [compactTarget], NOT at the threshold that started the
// pass: stopping at the trigger is what made a pass fire again one step later
// (see compactTarget for the run that proved it). A walk that runs out of
// foldable material before it gets there still succeeds with what it took —
// the target is how far to go, never a condition on the pass.
func (a *Agent) foldLocked() (int, string) {
	a.alignReasoningLocked()
	limit := a.cutPointLocked()
	protectTurn := a.turnContinuesLocked()
	target := a.compactTargetTokens() * bytesPerToken
	total := 0
	for _, message := range a.messages {
		total += messageBytes(message)
	}

	folded := make(map[int]bool, 16)
	first, last := -1, -1
	for index := 1; index < limit && total > target; {
		// Every message the person typed survives a fold. The session's own
		// volatile note does not: each one is superseded by the next landing,
		// and a note kept forever would put every old state of the card back
		// on the meter, uncompactable — the exact bill moving the card to the
		// tail was meant to end (agent.go's [isVolatileNote]).
		if a.messages[index].Role == "user" && !isVolatileNote(messageContentText(a.messages[index])) {
			index++
			continue
		}
		batch := index + 1
		for batch < limit && a.messages[batch].Role == "tool" {
			batch++
		}
		// THE RUNNING TURN IS NOT CONVERSATION HISTORY. It is the model's working
		// memory: its own notes, what it tried, the exact arguments and what came
		// back. Current-turn result pressure has a narrower use-aware pass in
		// turnfold.go; the general fold must not turn active work into a pointer.
		if protectTurn && index >= a.turnFloor {
			index = batch
			continue
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

	// The journal path is the required affordance: store:N is not a path grep
	// or read can open, and the original lines stay above the compaction
	// marker in the JSONL whether the store is on or not.
	journal, from, to := a.file.messageLines(a.messages[first], a.messages[last])
	// With no file to name, the store is the record that is left — but only
	// when this run actually reached it, since a post can fail and a fold that
	// sends the model to a store holding nothing is the dead pointer again.
	marker := foldMarker(len(folded), journal, from, to, a.chatlog.ref(a.messages[first]) != "")
	rebuilt := make([]ai.Message, 0, len(a.messages)-len(folded)+1)
	rebuiltReasoning := make([]provider.MessageReasoning, 0, cap(rebuilt))
	rebuilt = append(rebuilt, a.messages[0])
	rebuiltReasoning = append(rebuiltReasoning, a.messageReasoning[0])
	for index := 1; index < len(a.messages); index++ {
		if index == first {
			// The marker sits where the run it replaces sat, so the order the
			// conversation happened in survives the fold.
			rebuilt = append(rebuilt, textMessage("user", marker))
			rebuiltReasoning = append(rebuiltReasoning, provider.MessageReasoning{})
		}
		if folded[index] {
			continue
		}
		rebuilt = append(rebuilt, a.messages[index])
		rebuiltReasoning = append(rebuiltReasoning, a.messageReasoning[index])
	}
	// THE RUNNING TURN'S FLOOR MOVES WITH THE REBUILD. A fold always runs inside
	// a turn, and [Agent.turnFloor] is an index into the list this just replaced:
	// count what survived below it — the system message, every unfolded line,
	// and the marker when it landed below the floor — so [Agent.AttachReplay]
	// keeps splitting the transcript at the same conversation moment.
	if a.turnFloor > len(a.messages) {
		a.turnFloor = len(a.messages)
	}
	floor := 1
	for index := 1; index < a.turnFloor; index++ {
		if !folded[index] {
			floor++
		}
	}
	if first >= 0 && first < a.turnFloor {
		floor++
	}
	a.turnFloor = floor
	a.messages = rebuilt
	a.messageReasoning = rebuiltReasoning
	return len(folded), marker
}

// turnContinuesLocked distinguishes a tool step waiting for its next decision
// from the final prose response, which is still inside runTurn until its
// end-of-turn compaction and completion checks finish. The latest assistant
// message is the protocol's answer: tool calls mean another request follows;
// plain text means this answer has ended and may enter conversation history.
func (a *Agent) turnContinuesLocked() bool {
	if !a.running {
		return false
	}
	for index := len(a.messages) - 1; index >= a.turnFloor; index-- {
		if a.messages[index].Role == "assistant" {
			return len(a.messages[index].ToolCalls) > 0
		}
	}
	return false
}

// foldMarkerPrefix opens every fold marker. It is how [isCompactionNote]
// recognizes a line this package injected rather than something anybody said —
// a string this package controls, never a guess at wording.
const foldMarkerPrefix = "[folded "

// foldMarker is the line that stands in for what went. It names the count and
// the journal path the model can grep or read — a pointer it cannot follow is
// a dead one. The original lines stay above the compaction marker in that
// file; the path is the recovery floor.
//
//	[folded 31 messages · grep or read /home/x/.aforge/v3/sessions/abc.jsonl, lines 12..40]
//	[folded 31 messages · grep or read /home/x/.aforge/v3/sessions/abc.jsonl]
//	[folded 31 messages · full record in the store]
//	[folded 31 messages · full record in the session journal]
//
// THE PATH IS ITS OWN WORD, with the lines said after it in prose, because a
// `path:12..40` token is what neither `read` nor `grep` takes: one wants the
// file and an offset, the other wants the file. A pointer that has to be
// edited before it can be followed is most of the way back to no pointer.
//
// A session with no journal file names no path rather than inventing one: it
// says the store where the store is confirmed to hold the run, and the weaker
// true thing where nothing is confirmed at all.
func foldMarker(count int, journal string, from, to int, inStore bool) string {
	where := "full record in the session journal"
	switch {
	case journal != "":
		where = "grep or read " + journal + foldLineSpan(from, to)
	case inStore:
		// No file to name, and the store is holding this run. Saying the
		// journal here would send the model to a path that is not there.
		where = "full record in the store"
	}
	return fmt.Sprintf("%s%d message%s · %s]", foldMarkerPrefix, count, plural(count), where)
}

// foldLineSpan is what a fold marker says after its path when the journal can
// name the lines the folded run sits on, and nothing when it cannot — the path
// alone is still a file to grep, and a made-up line number is not.
//
// BOTH ENDS OR NEITHER. One end on its own would print as the location of the
// whole run, sending the model to read a line where a hundred of them went;
// the file without a span costs it one wider grep and tells it no lies.
func foldLineSpan(from, to int) string {
	switch {
	case from > 0 && to > 0 && from != to:
		return fmt.Sprintf(", lines %d..%d", from, to)
	case from > 0 && from == to:
		return fmt.Sprintf(", line %d", from)
	}
	return ""
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

// addEmptyReflexUsage is the paid-call door for a reflex request that consumed
// its whole output ceiling without answering. The tokens and price stay in the
// ordinary totals; the extra count says what that spend failed to buy.
func (a *Agent) addEmptyReflexUsage(response *ai.Response, model string) {
	a.addUsageAs(response, model, 1, string(roles.RoleReflex), true, true)
}

// addFoldedUsage is [Agent.addAuxiliaryUsage] for a tally SOMEBODY ELSE ALREADY
// JOURNALED — a task node's whole life folded into the conversation that
// spawned it ([Agent.foldTaskUsage]) — and it exists to keep that fold out of
// the machine's usage ledger.
//
// A FOLD IS NOT A CALL. The node ran its own turns in its own journal and wrote
// its own ledger lines as it went; folding the total in again is right for this
// session's books, where the law is that a conversation's spend includes the
// work it started, and would be the same money counted twice in a file whose
// whole purpose is "what did this machine spend". So the session's counters and
// the session's journal move exactly as before, and the ledger hears nothing.
//
// EVERY CHILD AGENT'S TALLY COMES HOME THROUGH THIS DOOR OR THROUGH THE ONE
// BELOW IT, and the test of which door a fold belongs to is whether the child
// wrote the machine's ledger itself: a task node, a fork's hand
// ([Agent.foldHandUsage]) and an adaptive run's worker
// ([orchestrateExec.spend]) all do, so all three fold silently. The auxiliary
// door is for a call THIS agent made and nobody else journaled.
func (a *Agent) addFoldedUsage(response *ai.Response, model string, calls int) {
	a.addFoldedUsageAs(response, model, calls, "")
}

// addFoldedUsageAs is [Agent.addFoldedUsage] with the role named, and it is
// [Agent.addAuxiliaryUsageAs]'s reason again one door along: a fold whose share
// of a turn's bill somebody has to be able to pick out afterwards says so here.
// A fork's hands are the case it exists for — their money rides inside the
// caller's own turn, so without the word `hand` on the journal line there is no
// reading of that transcript that separates what the hands spent from what the
// caller spent.
func (a *Agent) addFoldedUsageAs(response *ai.Response, model string, calls int, role string) {
	a.addUsageAs(response, model, calls, role, false, false)
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
	auxRoleCaption  = "caption"
	auxRoleTaskName = "taskname"
	// auxRoleIntake is the third for the same reason the first two are: the form
	// a subharness is launched on is something a person SEES, and a field filled
	// wrongly is unattributable without the name of the model that filled it
	// (subharness_intake.go).
	auxRoleIntake = "intake"
	// auxRoleHand is the fourth, and it names a whole child agent rather than one
	// call: a fork's hand (fork.go). It is here for a reason the first three do
	// not have — a hand's spend rides INSIDE the turn that opened it, folded into
	// the same books, so without the tag there is no way to read a session's
	// journal and say which of a turn's tokens the hands spent and which the
	// caller did.
	//
	// IT IS THE ONE ROLE THAT REACHES THE JOURNAL AND NOT THE LEDGER. The hand
	// journals its own calls, so the fold that carries this word writes no ledger
	// line ([Agent.addFoldedUsageAs]) — the tag is for the transcript, where the
	// question "which of this turn's tokens were the hands'" is asked.
	auxRoleHand = "hand"
	// auxRoleHandoff is the fifth, and it is the one that names REAL MONEY ON THE
	// MASTERMIND TIER. The brief a handed-over turn gives its worker is written by
	// a second model at the end of a turn the person did not ask for a second model
	// on (checkpoint.go's [Agent.writeHandoff]); without the tag its line is an
	// anonymous errand at the dearest price in the catalog, which is precisely the
	// shape of bill the mark reader's own journal line exists because of.
	auxRoleHandoff = "handoff"
)

// addAuxiliaryUsageAs is [Agent.addAuxiliaryUsage] with the role named. It is a
// second door rather than a fourth argument on the first because thirty callers
// fold auxiliary usage and only the two namers have anything to say here; an
// empty role journals no field at all, by the emptiness law the rest of the
// line keeps.
func (a *Agent) addAuxiliaryUsageAs(response *ai.Response, model string, calls int, role string) {
	a.addUsageAs(response, model, calls, role, true, false)
}

// addUsageAs is the body both auxiliary doors share, with one bit of difference:
// whether this tally is a CALL THIS AGENT MADE — and therefore a line in the
// machine's ledger — or a fold of work that already wrote its own
// ([Agent.addFoldedUsage]).
func (a *Agent) addUsageAs(response *ai.Response, model string, calls int, role string, ledger, emptyReflex bool) {
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
	if emptyReflex {
		aux.EmptyReflex = calls
	}
	// AN ERRAND'S ROW CARRIES NO LANE. Nothing timed this call — the witness
	// watches the turn's own stream — and the turn's figures on this row would be
	// a measurement of one request filed against another.
	a.bank(bankedCall{used: aux, model: model, role: role, ledger: ledger})
	// The write is outside the lock for the reason [Agent.sealTurn]'s is: the
	// file has its own, and holding the agent's across a disk write would put
	// every reader of the session's totals behind it.
	a.file.appendUsage(aux, model, true, role)
}
