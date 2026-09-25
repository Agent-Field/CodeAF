package session

// ── THE ONLY WAIT A PERSON EXPERIENCES IS THE MAIN MODEL GENERATING ─────────
//
// THAT IS A LAW OF THIS FILE, and it is stated here because it is the one rule
// the turn loop cannot enforce with a type. Everything a turn asks that is not
// the person's own model — the recall that picks which remembered lines this
// message needs, the judge that asks whether a message was work, the reader that
// sketches what is left of a long answer, a title, a caption, a name — is an
// AUXILIARY READING. Every one of them runs BESIDE the work, and a reading may
// only ever INTERRUPT the work; none of them may precede it.
//
// So the two moments this file owns are measured in milliseconds, and there is a
// test that fails the build when they stop being (loop_speed_test.go):
//
//	message → the main request on the wire      ≤ 50 ms
//	tool result → the next request on the wire  ≤ 50 ms
//
// WHAT THAT COST BEFORE IT WAS A LAW. The call census of 2026-09-11 measured the
// pre-turn recall gate at a mean of 4.3 seconds and a maximum of 10.7 over
// sixteen messages, with the person's own model not asked until a reflex machine
// had answered — 1.7s, 2.3s, 3.0s and one of 20.4s at 09:30:45 — and the mark's
// reader at 8.1 seconds between a tool result and the next step, with the next
// request going out two milliseconds after it returned. Neither of those readings
// decided anything on those turns. They were paid for anyway, by the person, in
// front of every message.
//
// AND QUALITY IS KEPT BY APPLYING EVERY DECISION, NEVER BY AWAITING IT. That is
// the half of this law that is easy to lose, so it is spelled as three rules:
//
//   - A decision that arrives before it is spent is applied to THIS step, exactly
//     as it always was.
//   - A decision that arrives late is applied to the NEXT step, or journaled as
//     late with what it would have changed — never silently dropped, because a
//     dropped reading is a reading whose ledger never learns (memory.go's
//     [recallAside], checkpoint.go's [markAside]).
//   - A decision that cannot be applied after the fact must be made cheap enough
//     to precede: at most [lanes.SpokenWithin], and skipped for this step past it.
//
// AND A READING DECIDES WHERE IT USED TO ACT. A reading whose EFFECT fires from
// inside its own goroutine is this mechanism used in name and broken in fact:
// the effect lands whenever the calls under it happen to return rather than at
// the moment the turn could still spend it, and a turn about to be re-opened has
// already had work started over the top of it. So every reading answers a value
// and the turn is what acts on it (route_judge.go's [judgeRuling] is the one
// that had to be taken apart to say so).
//
// THERE IS EXACTLY ONE EXCEPTION AND IT IS NAMED. The guardian — "is this one
// tool call plainly safe to run without asking" (consent.go) — decides whether a
// tool RUNS, so there is nothing to run beside it and nothing it can be applied
// to afterwards. It keeps its full ten-second window, and what it owes the person
// instead is a phase word for the whole of it, because a wait that is real is
// reported (docs/design/waiting/DESIGN.md).
//
// THE LAW IS ALSO TRUE READ FROM THE OTHER END: NOTHING THE TURN *TELLS* MAY
// HOLD IT UP EITHER. A reading is something a turn asks for; a phase is
// something it says, and for as long as saying one was a straight call into the
// surface's reader, the last act of every model call waited out a whole Bubble
// Tea draw on this goroutine — because internal/tui3 asks for its frame down an
// unbuffered channel. News is left on a desk now and the teller walks away
// (sidecar.go's [desk], phasenews.go, lanenews.go), which is the same law with
// the producer and the consumer swapped.
//
// AND THE END OF A TURN IS A DIFFERENT WAIT FROM THIS ONE. Once the model has
// stopped writing there is no work left to run beside, so the two readers that
// decide whether the answer finished the ask and whether it should have been work
// are RACED AGAINST EACH OTHER rather than taken in series, and the phase clock
// says which of them is holding the turn open.

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

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/ctxbudget"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/guard"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// ── retry constants (verbatim from internal/exec/bare) ───────────────────────

// retryBaseDelay is the first wait of the retry ladder; each attempt doubles it,
// so 2s, 4s, 8s — the ladder this loop has always walked.
//
// IT IS INTERPOLATED AND NOT TYPED OUT, and so is the attempt count that used to
// sit beside it as `maxRetries = 3`. The ladder a request actually walks is now
// the response boundary's, resolved from the person's settings
// (taxonomy_boundary.go's [Agent.failureLimits]); a second spelling of either
// number here would be the version that drifts, and a shipped install with no
// profile behind it walks exactly the ladder it always did because the
// boundary's own defaults ARE these two.
const retryBaseDelay = taxonomy.DefaultTransportBackoff

// detachedFromTurn is [Agent.addDetachedUsageAs]'s argument spelled as a word,
// because `true` at a call site says nothing about which of two booleans it is.
const detachedFromTurn = true

// truncationContinuations gives a cut-off answer two chances to finish in
// smaller pieces. The bound matters because a model that ignores the note can
// otherwise turn one bad output ceiling into an unbounded, silent spend.
const truncationContinuations = 2

// ── what a cut stream is worth asking again ─────────────────────────────────
//
// A stream the guard cut (internal/provider's streamguard.go) is a different
// kind of failure from a torn connection, and it spends a different allowance:
// the request never failed, so there is nothing here to back off from, and the
// same four attempts that make sense for a socket would keep a model that has
// lost the thread going four times over a context that is only getting worse.
//
// THE NUMBERS ARE NOT HERE ANY MORE, and that is the point of the change they
// moved in. They are [taxonomy.SilentCutAttempts] and its two neighbours, read
// by the one policy that decides what any failed request is worth
// (internal/taxonomy's transportBudget), so that a cut and a refusal are one
// story told from one place rather than two budgets kept in two files that
// answered "is there another model to ask" differently.

const truncationContinuationNote = "Your last reply was cut off at the output limit. " +
	"Continue the work in smaller parts. Use tool calls to save any large deliverable " +
	"when writing is in scope — a long file is written in parts, a first write and then " +
	"write calls with append:true — and keep the final report short."

// ── retry classification (verbatim from internal/exec/bare) ──────────────────

// nonRetryablePattern matches provider errors that are permanent (quota,
// billing, account limits).
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

// retryablePattern matches transient provider errors. A message is retryable
// when it matches this AND does not match nonRetryablePattern.
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

// isRetryable reports whether a provider error is retryable: it must match the
// retryable pattern and NOT match the non-retryable (quota/billing) pattern.
//
// IT IS EVIDENCE AND NEVER A DECISION, which is what this pattern lost on
// 2026-09-10 and has back. It sets one fact on one struct — the socket shape
// nothing typed could name, [taxonomy.Evidence.Wire] — and the move is chosen
// from the whole of the evidence by [taxonomy.Classify]. Until this wave the
// turn loop asked it a SECOND time, forty lines after that verdict had been
// computed, and returned on the answer: `isContextOverflow(errMsg) ||
// !isRetryable(errMsg)`, a pair of regexes over the provider's prose overruling
// a typed classification that had already read the same failure. A routing 404
// matched no pattern, so a turn with three moves left ended on the router's own
// sentence. Its companion `isContextOverflow` is gone entirely: the request not
// fitting is a fact the transport decides from the request-too-large status and
// the error envelope's own code (internal/provider's [overflowRefusal]).
func isRetryable(errMsg string) bool {
	if nonRetryablePattern.MatchString(errMsg) {
		return false
	}
	return retryablePattern.MatchString(errMsg)
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

// settleBoundTripped reports whether the settle turn has spent its bound, and
// the count that says so — the calls it has made, or the money it has spent.
// It is false for every turn nobody marked as a settle wake ([settleWake]), so
// an ordinary turn, and every wake that is not a landing handing over a
// decision, runs exactly as it always did.
//
// IT READS THE CEILING OFF THE CONTEXT and the spend off the turn, which is the
// one place both facts are in hand. The money arm exists only where a [Steward]
// is armed: a run with no ceiling of its own has no money to take a share of, and
// inventing one would bound a turn by a number nobody set (turnwall.go's own
// argument about a ceiling with no wall).
func (a *Agent) settleBoundTripped(ctx context.Context, turn *Usage, calls int) (string, bool) {
	wake, settle := settleWakeFrom(ctx)
	if !settle {
		return "", false
	}
	if calls >= wake.ceiling {
		return strconv.Itoa(calls) + " call" + plural(calls), true
	}
	if steward := a.steward(); steward != nil {
		if budget := steward.Budget(); budget.USD > 0 {
			if share := budget.USD / settleBoundShare; share > 0 && turn.CostUSD >= share {
				return fmt.Sprintf("$%.2f", turn.CostUSD), true
			}
		}
	}
	return "", false
}

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
	// AND A SETTLE TURN OPENS ITS OWN WINDOW HERE, where no lock is held: the length
	// is read off the [Steward] ([Agent.settleWindow]), and that reading runs the
	// run's spend closure, which takes the agent's lock — so it cannot be made
	// where the wake was decided under it. The window is opened through
	// [openCallWindow], the one way this package opens a window the model is told,
	// and the deadline it puts on the context both tells every request how long is
	// left and cuts the turn itself ([Agent.settleBoundTripped] reads the ceiling
	// at the loop's boundary). Every other turn is left exactly as it was.
	if _, settle := settleWakeFrom(ctx); settle {
		windowed, closeWindow := openCallWindow(ctx, a.settleWindow(), callWindow{})
		defer closeWindow()
		ctx = windowed
	}
	// A CUT STREAM'S RECEIPT ARRIVES AFTER THIS TURN'S CALL HAS RETURNED. The
	// sink belongs on the turn context before any of its calls or errands derive
	// children from it, so every streamed request can hand that late fact back to
	// this agent without making the turn wait for it.
	ctx = provider.WithReconcile(ctx, a.reconciled)
	// AND THE MODELS THIS TURN IS PUT TO ARE RECORDED ON IT. One record per turn,
	// opened here so that every call and errand derived from this context writes
	// to the same one — which is what makes "has this model already been asked"
	// a fact the one model hop can read rather than a count it keeps
	// (internal/provider's modelstried.go, [Agent.nextFallback]).
	ctx = provider.WithModelsTried(ctx)
	started := time.Now()
	// AND THE TURN TIMES ITSELF AGAINST ITS OWN LAW, from the first instruction of
	// the turn rather than from the wire ([turnPace]). What it measures is the two
	// gaps this file promises to keep in milliseconds, and it is written down at
	// the end so the next audit can read the decomposition instead of deriving it.
	pace := &turnPace{began: started}
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

	// AND THE READING THAT USED TO BE THE LAST THING BEFORE THE FIRST REQUEST:
	// which of the things this person has had codeaf remember bear on what they
	// just said (memory.go).
	//
	// IT IS NOT A LINE OF THIS FUNCTION ANY MORE. It is already in flight — it was
	// started beside the title, at the one place a turn begins
	// ([Agent.startRecallLocked]) — and what happens here is that the turn picks
	// up the handle and carries it. This is the file's own law in the one place it
	// was most expensively broken: the call census of 2026-09-11 measured the main
	// model not being asked until a mistral-nemo call returned, on every single
	// message.
	//
	// Everything about it still fails open — an empty block is a turn exactly as
	// it would have been — and everything it finds is still applied: to this
	// request if it is back before the first token, to the next step if it is not.
	recall := a.takeRecall()
	// AND THE RECALL DIES WITH THE TURN, for the route race's reason below: a
	// block nobody can land any more must not go on paying for a reflex call.
	defer recall.end()

	// AND WHAT THE OTHER WINDOWS ON THIS PROJECT HAVE BEEN DOING (taskdelta.go),
	// beside the work for the same law. It makes no model call — it opens the
	// project index and one small JSON per live window — but a shared directory on
	// a cold disk is still a wait, and the only wait a person experiences here is
	// the model generating. It fails open the same way: no folder, no index and no
	// other window each answer an empty block, and a turn with an empty block is a
	// turn as it always was; a read that lands after the first request rides the
	// next step, which is the same bargain the recall takes.
	//
	// IT GOES THROUGH [readBeside] AND NOT THROUGH A `go` OF ITS OWN, which is
	// sidecar.go's whole reason for existing: a reading with its own goroutine has
	// its own idea of what a cancellation means, and this one had none at all —
	// it was measured stamping a conversation's folder after that conversation
	// had closed. Nothing takes its answer, because its answer IS the assignment
	// it makes; what the door buys is the cancellation — the reading carries the
	// context it is given and obeys it ([Agent.refreshElsewhere]) — and a reading
	// the beside-watch can see, so a fixture waits on the fact instead of guessing.
	//
	// AND A TURN THAT MAY NOT BE TOLD STARTS NO READING AT ALL. A task node and a
	// conversation with no folder are both answered by one predicate
	// ([Agent.tellsElsewhere]); asking it here keeps a reading that would return
	// on its first line out of the watch, where it would be one more thing a
	// fixture waits for and nothing at all beside the work.
	//
	// THE `ask` IS WRITTEN OUT HERE AND NOT HOISTED INTO A VARIABLE, because the
	// set of readings the sidecar law watches is READ OFF THE TREE — whatever an
	// inline [readBeside] ask calls is a reading, by construction
	// (sidecar_law_test.go). A reading handed in through a name is a reading that
	// law cannot see, which is how the next one stops being watched.
	var elsewhere *sidecar[struct{}]
	if a.tellsElsewhere() {
		elsewhere = readBeside(ctx, func(read context.Context) struct{} {
			a.refreshElsewhere(read)
			return struct{}{}
		}, nil)
	}
	defer elsewhere.end()

	// AND, FOR A MANAGER, THE TEAM IT MANAGES (team.go), on the same terms and
	// the same door as the block above: members' journals and the team's
	// Traffic are disk, read beside the work, and a digest that lands after the
	// first request rides the next step. A conversation with no profile and a
	// task node start no reading at all.
	var team *sidecar[struct{}]
	if a.config.teamProfile() != "" {
		team = readBeside(ctx, func(read context.Context) struct{} {
			a.refreshTeamDigest(read)
			return struct{}{}
		}, nil)
	}
	defer team.end()

	// partial accumulates what the model has streamed for the CURRENT step.
	// It is the transcript's answer for an interrupted step, where no response
	// ever comes back.
	partial := &partialBuffer{}
	// reached is THE reading of what this attempt has put in front of the person,
	// written by the stream observer below and asked by every door that may
	// re-ask the request — the recall's block, and the person's own word
	// (steer.go's [reachedThePerson]). It lives here beside the buffers it is a
	// fact about, and is emptied with them at the top of every attempt.
	reached := &reachedThePerson{}
	recall.watch(reached)
	// Reasoning is accumulated beside, never inside, the partial answer. A
	// completed step keeps it for continuation; an interrupted attempt drops it.
	reasoning := &reasoningBuffer{}

	// warm holds the read-only calls this turn started while their response was
	// still streaming. It belongs to the turn and is emptied per attempt — see
	// [warmBatch] for the law that decides what may start early at all.
	warm := &warmBatch{}
	// The turn's read sweep ledger (readhandoff.go): same lifetime as the warm
	// batch, consulted at the one seam every batch passes through below.
	sweep := &readSweep{}

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
			// AND EVERY DOOR THAT MAY RE-ASK THIS REQUEST IS TOLD THE PERSON HAS
			// STARTED READING. Past this instant a silent re-ask would take words
			// off a screen somebody is looking at, so the recall lands on the next
			// step instead (memory.go's [recallAside]) and the person's own word
			// waits for the boundary rather than cutting (steer.go's
			// [reachedThePerson], which is the ONE reading both of them ask). One
			// atomic store per delta.
			reached.drew()
			pace.word(time.Now())
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
			pace.word(time.Now())
			// Reasoning is NOT written to partial: it is the model's working, not
			// its answer. The sidecar is recorded only after the response completes.
			reasoning.write(event)
			if event.Delta != "" {
				// AND VISIBLE THINKING IS SOMETHING THE PERSON HAS READ. It is drawn
				// (internal/tui3's feed, EventReasoning) and #760 made a
				// reasoning-only turn a thing somebody watches, so the reading is
				// told HERE, where the words actually leave for the page, and not
				// on every reasoning event — a delta that says nothing draws
				// nothing.
				reached.drew()
				hub.send(Event{Kind: EventReasoning, Text: event.Delta})
			}
		case provider.StreamNotice:
			// The adapter reshaping the request to get it accepted at all
			// (internal/provider's endpoints.go). It is not the model speaking and
			// not a failure, so it rides as a note rather than as text: nothing of
			// it reaches the transcript, and the person sees which attempt they
			// are on and what was taken off to get there.
			hub.send(Event{Kind: EventNotice, Text: event.Delta})
		case provider.StreamRowNews:
			// A ROW THE PERSON WROTE IS NO LONGER BEING SENT, and this is the
			// only place they are told (internal/provider's lanepin.go). It is
			// its own kind rather than a notice because a notice is narration
			// about one request's shape and this is news about a setting: a
			// surface is free to fold the first away once the answer lands and
			// must not fold this one, which is exactly what happened to it
			// while the two shared a channel.
			hub.send(Event{Kind: EventRowNews, Text: event.Delta})
		case provider.StreamReplaced:
			// A rescue on another machine is this step being asked again, so what
			// the dead machine streamed is void exactly as a cut attempt's is. The
			// room withdraws it on EventRetrying and the loop must not journal it
			// either, or an interrupt after the rescue would record both halves as
			// one answer — the same per-attempt boundary
			// [completeWithRetryReasoning] resets on a cut.
			partial.reset()
			reasoning.reset()
			warm.reset()
			forming.reset()
			// AND THE READING GOES WITH THEM. What the dead machine drew is
			// withdrawn on the event below, so the person has none of it in front
			// of them any more — and a reading left standing would tell the doors
			// that may re-ask this request that they still do (steer.go's
			// [reachedThePerson]).
			reached.reset()
			hub.send(Event{Kind: EventRetrying, Text: event.Delta})
		case provider.StreamToolCallForming:
			// The seconds BEFORE the announcement, which the person used to
			// watch as silence. Nothing here starts anything and nothing here
			// is parsed as an instruction: the batch decides how often this
			// call may speak and what it can honestly say about arguments that
			// are still arriving, and the answer rides as a row that the
			// announcement below will replace.
			if formed, speak := forming.note(event); speak {
				// AND A ROW THAT IS SPOKEN IS ON THE PAGE, so it counts exactly as a
				// word of the answer does: a request re-asked under it would leave
				// the dead attempt's call sitting above the replacement's.
				reached.drew()
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
	// unwatched node's seconds are worth nothing and the seconds of a node whose
	// run somebody is sitting in front of are worth what theirs are.
	//
	// AND ALL OF IT SAID ONCE, AS A ROLE. internal/lane's roles.go holds the
	// table — what a second of this errand's wait is worth, what quality bar it
	// needs, how many calls it will make, and whether a person is reading THIS
	// stream — and the role is the only thing a call site names. The intent
	// below is kept beside it because what a wait is worth is still read off it
	// and a dozen other packages still set it, but it is now a READING of the
	// role rather than a second opinion about the same fact (internal/provider's
	// roles.go).
	ctx = provider.WithRole(ctx, a.laneRole())
	// AND WHOSE ERRAND IT IS, beside what kind of errand it is, for the reason
	// internal/provider's roles.go states: an engine that is a separate process
	// from the surface registers ONE phase reader for every conversation it is
	// running, and news that could not name its own conversation would be drawn
	// on every window at once. This is the one place it is stamped, because
	// every request a turn makes — the talk turn itself and every node under it
	// — descends from this context (newskey.go's [Agent.newsKey]).
	ctx = provider.WithSession(ctx, a.newsKey())
	// AND WHAT THE ERRAND IS ABOUT, which is a different question from whose it
	// is: a conversation and every task node under it share one conversation,
	// and each of them is a subject a window may be looking straight at. It is
	// stamped beside the session for the same reason — every request this turn
	// makes descends from this context — and it is EMPTY for the conversation
	// itself, which is what makes a build that never had the stamp behave
	// exactly as it always did (newskey.go's [Agent.newsSubject]).
	ctx = provider.WithNode(ctx, a.newsSubject())
	if a.config.InTask {
		ctx = provider.WithRoutingIntent(ctx, provider.IntentBackground)
	}
	// A FIRST PROMPT HAS NO SAVED TALK MODEL. The stall rescue that fires
	// before a word arrives used to say nothing, and a fresh profile sat
	// ninety seconds discovering `/model` on its own (F42).
	if !a.config.InTask && config.FirstPrompt(a.config.ProfileDir) {
		ctx = provider.WithFirstPrompt(ctx)
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

	// The model is latched here and re-read at ONE other place: the request
	// boundary, where the person's own word is taken (steer.go's THE PERSON'S
	// WORD WINS). Everything between two requests rides the latched value, which
	// is what stops a swap arriving mid-stream from sending one model the
	// transcript another model was half-way through writing — and the boundary is
	// what stops a person's pick waiting out a twenty-minute step.
	//
	// The effort rung is latched WITH it, in the same breath and for the same
	// reason — and it is resolved for THIS model, so a swap mid-turn cannot
	// leave the turn sending one model's level with another model's name. It is
	// the LADDER'S answer and not a field ([Agent.effortFor]): a rung set on the
	// conversation, on the work, or on the install reaches this turn through the
	// same call the dialled level does, which is what makes one resolver true.
	model := ""
	if wake, settle := settleWakeFrom(ctx); settle {
		model = wake.model
	}
	model = a.latchModelAs(model)
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
	// fifteen seconds — and it is counted for the ROW a person reads, never for a
	// budget: what bounds the re-asking is `emptyUntil` below.
	emptyReplies := 0
	// AND THE RE-ASKING IS BOUNDED BY TIME AND NOT BY A COUNT. It used to walk
	// the person's `response.attempts` through the boundary, which is one of the
	// six session-side ladders docs/design/recovery/DESIGN.md §7 names: a count
	// under a transport that was already bounded by the plan's deadline, so the
	// two multiplied and neither could be stated. This is the same give-up the
	// call under it runs on, started at the FIRST empty answer — a turn doing
	// real work between two of them is not a turn that is failing, and the clock
	// that matters is how long this has been going nowhere. `emptyOwed` is the
	// waiting this loop asked for, charged against that clock whether or not the
	// clock really moved — see THE TURN'S OWN CLOCK below for why.
	var emptyUntil time.Time
	var emptyOwed time.Duration
	// emptyWait is what the NEXT empty answer costs before the one after it is
	// asked for, and it is zero until the second.
	//
	// THE FIRST EMPTY ANSWER IS ANSWERED AT ONCE AND BY SOMEBODY ELSE. Waiting
	// mends an endpoint under strain and an endpoint that answered instantly with
	// nothing is not under strain — being served by somebody else is what mends
	// it, which costs no time at all (internal/taxonomy's waitFor says the whole
	// of that argument and returns no backoff here). A SECOND one says the asking
	// again is not working, and from there the wait doubles: not to mend anything,
	// but because a deadline over a ladder that pays nothing is a deadline reached
	// as fast as an endpoint can say nothing, which is a great many full-price
	// requests for one answer.
	var emptyWait time.Duration

	// meter is what this turn has COST, in finished tool rounds, priced against
	// what handing it over would cost (checkpoint.go). It belongs to the turn for
	// the reason the loop window and the change ledger do: it is a fact about one
	// answer, and a meter that remembered yesterday's rounds would move work out
	// of a conversation on the strength of a conversation that already ended.
	meter := &checkpointMeter{}

	// settleCalls is HOW MANY PROVIDER CALLS THIS TURN HAS PUT ON THE WIRE, which
	// is only ever read for a settle turn: it is the count a settle turn's ceiling
	// bounds ([Agent.settleBoundTripped]), and it lives here rather than on the
	// meter because it counts requests and the meter counts finished tool rounds
	// (checkpoint.go). It is one per step, incremented as the request goes out.
	settleCalls := 0

	// AND THE MARK'S READING RIDES BESIDE THE WORK (checkpoint.go's [markAside]).
	// It belongs to the turn for the meter's reason — it is a fact about ONE
	// answer — and it is let go of on every way out, including the ones that end
	// the turn mid-round, so no reading outlives the turn that bought it.
	marked := &markAside{}
	defer func() {
		// IT IS SPENT HERE AND NOT LOST. A reading rides beside the work and is
		// spent at the next boundary, which is the right rule for every boundary
		// but the LAST one: a mark crossed by the round that ends a turn — a
		// re-open is a round — has no next boundary, and a mastermind call paid
		// for and thrown away is a ledger line nobody can ever count.
		//
		// AND THIS IS ONE OF THE TWO PLACES THIS ENGINE WAITS ON A READING, for
		// the reason the CEILING's own drawing is still read in line and the
		// reason the guardian blocks: the turn is over, so there is nothing left
		// to run beside. A deferred body runs when everything has already been
		// decided, which is what makes the wait honest here and nowhere else
		// (sidecar.go's [sidecar.takeAtTheEnd], sidecar_law_test.go is the law).
		// What it costs is bounded twice — by [checkpointSketchWindow] and by the
		// drawing having started a whole round earlier.
		//
		// AND WHAT IS WRITTEN DOWN IS WHAT THE DRAWING SAID. A reading that lands
		// here landed with no boundary left to spend it at, which is a different
		// fact from a reader that decided to carry on — and the file spelled them
		// alike until #956 ([checkpointSketch.endOfTurnDecision]).
		if landed, ok := marked.takeAtTheEnd(); ok {
			a.journalMarkRead(landed.read, landed.mark, landed.rounds, landed.read.sketch.endOfTurnDecision())
		}
		marked.end()
	}()

	// AND THE TURN WRITES DOWN WHAT IT SPENT ITS TIME ON, in one row, on every
	// way out (see [Agent.journalTurnPace]). It is the evidence this file's law is
	// held to — how long the person waited to be sent anywhere, how long they
	// waited for a word, the worst gap between a tool result and the next request,
	// and which readings were in flight beside the work rather than in front of
	// it — and it is DEFERRED for the phase clock's reason: there are a dozen ways
	// out of the loop below, a turn handed over or stopped is exactly the turn an
	// audit wants the decomposition of, and a row written at each exit is one law
	// copied at every one of them.
	defer func() { a.journalTurnPace(pace, recall, marked) }()

	// TOOL COMPACTION MAY ONLY TOUCH HISTORY THAT WAS FROZEN BEFORE THIS TURN'S
	// FIRST REQUEST. Every result appended below will have appeared verbatim in
	// one request before a later round could call it old; rewriting it then
	// would throw away the byte-stable prefix the provider has already cached.
	frozenToolHistory := len(a.snapshot())

	for {
		// The cancel check comes BEFORE the drain: steering typed in the
		// instant before an interrupt must not be spliced into a transcript
		// this turn is abandoning unanswered. Nothing is lost — the turn's end
		// drains the queue under the same lock that clears running (agent.go),
		// so a leftover lands ahead of the next Submit's message.
		if ctx.Err() != nil {
			// A SETTLE TURN'S WINDOW RAN OUT, WHICH IS A BOUND AND NOT A STOP.
			// The node comes back to the person with the reason on its report
			// ([Agent.markSettleBound]), which is what a bound that ends a turn
			// does here — never a silent wall stop.
			if _, settle := settleWakeFrom(ctx); settle {
				a.markSettleBound(settleWindowCount)
			}
			a.endStoppedTurn(ctx, hub, partial, turn, started, model)
			return false
		}
		// AND A SETTLE TURN STOPS AT ITS OWN BOUND, at this boundary rather than
		// at a new loop: the meter the turn already climbs is where the count
		// lives, and the hand-back is the end-of-turn floor's ([Agent.handBackUnsettled]),
		// asked here only for the sentence it carries ([Agent.markSettleBound]).
		if count, tripped := a.settleBoundTripped(ctx, &turn, settleCalls); tripped {
			a.markSettleBound(count)
			return false
		}

		// AND THE READING STARTED AT THE LAST BOUNDARY IS SPENT AT THIS ONE
		// (checkpoint.go's [Agent.checkpointSettle]). It is the mark's half of the
		// same non-blocking shape [Agent.routeTriage] keeps below: a reading still
		// in flight costs this boundary one closed-channel test and is asked again
		// at the next one, and a drawing with independent parts in it ends the turn
		// here — which is the boundary its own cut opened a moment ago.
		if a.checkpointSettle(ctx, hub, &turn, started, model, meter, marked, nil) {
			return true
		}

		// A COMMAND THIS WORK IS STILL WAITING FOR IS NOT A QUESTION FOR THE MODEL
		// (task_job_park.go). It is here, before the drain, because the drain is what
		// puts the ending in front of the model: the wait ends when the news is
		// queued, and the very next line carries it.
		a.parkOnOwedJob(ctx)

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
		sentAt := time.Now()
		a.turnLane.sent(sentAt)
		// THE SETTLE CEILING COUNTS THIS REQUEST. It is incremented here, at the one
		// line a request actually leaves on, so the count and the wire agree.
		settleCalls++
		// AND THE TURN'S OWN LAW IS TIMED ON THE SAME LINE, which is the only line
		// where it can be: this is where a request actually leaves, so it is where
		// the person's wait to be sent anywhere ends and where a tool-result gap
		// closes.
		pace.sending(sentAt)
		response, answered, err := a.completeWithRetryReasoning(ctx, hub, model, rung, partial, reached, reasoning, warm, forming, frozenToolHistory)
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
			// AND A MARK'S CUT TAKES THE SAME ROAD, for the same reason spelled the
			// other way round: the turn context is alive, whatever the model wrote
			// before the cut is work the person watched arrive and stays in the
			// transcript, and the loop continues to the boundary at its head — where
			// the drawing that cut it is spent ([Agent.checkpointSettle]). What
			// differs from a steer is only who asked for the boundary.
			if errors.Is(err, errSteerCut) || errors.Is(err, errMarkCut) {
				turn.Turns++
				// THE LANE IS READ BEFORE THE MONEY IS BANKED, because the row the
				// money writes is this call's and the witness is what measured it.
				facts := a.turnLane.answered(readHedge(hedge, served.Name()), responseOutput(response))
				a.addUsage(&turn, response, model, served.Name(), facts)
				a.tellLaneNews(model, facts, hedge)
				droppedCall := forming.any() || warm.anyAnnounced()
				a.keepSteeredPartial(partial, reasoning, droppedCall, hub, cutDroppedCallNote(err))
				warm.reset()
				forming.reset()
				continue
			}
			// Interrupt (or the caller's own deadline). Whatever was streamed
			// before the cut is real work the person watched arrive, so it
			// stays in the transcript and the turn ends normally — and WHICH
			// DOOR ENDED IT is written down and, where it was not the person's
			// own stop, said out loud (stopcause.go).
			if ctx.Err() != nil {
				a.endStoppedTurn(ctx, hub, partial, turn, started, model)
				return false
			}
			// Overflow is the one error with an answer other than reporting
			// it: compact and re-send the same step. It fires regardless of
			// CompactEnabled — that flag gates the automatic pass, not the
			// recovery from a request the provider has already refused.
			//
			// AND IT IS THE VERDICT THAT SAYS SO, not a regex over the sentence.
			// [taxonomy.ActionCompact] is the [taxonomy.Shape] policy's answer to
			// a request that did not fit, and `overflowCompacted` is what it is
			// told through [taxonomy.Evidence.Compacted] — so the once-per-turn
			// rule is stated in the policy and read here rather than kept in two
			// places that could come to disagree (taxonomy_boundary.go's
			// [Agent.readOverflow]).
			if a.readOverflow(err, model, overflowCompacted).Compacts() {
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

		// Stop condition: the loop ends when the assistant response has NO tool
		// call.
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
			assistantDone := false
			if turnBroke(response) {
				a.journalFailedCall(ctx, model, "", errEmptyAnswer, emptyReplies+1, a.requestEstimate())
				emptyReplies++
				if emptyUntil.IsZero() {
					emptyUntil = turnNow().Add(a.giveUp())
				}
				// AND IT GOES THROUGH THE SAME VERDICT ROAD AS EVERY OTHER FAILED
				// CALL, SAYING SO. This was the second retry road in the turn: the
				// boundary decided it, the wait was taken, and the surface was told
				// NOTHING — no [EventRetrying], no [RetryNews], no phase — so a
				// person watched a clock counting a request that had already come
				// back empty (docs/design/recovery/DESIGN.md §2.8, one of the four
				// silent waits). The verdict is the same one a refusal earns, the
				// allowance is the same allowance, and now the line is the same
				// line.
				verdict := a.readEmptyReply(model, emptyReplies,
					!turnNow().Add(emptyOwed).Before(emptyUntil))
				if verdict.Retries() {
					partial.reset()
					wait := verdict.Backoff
					if wait <= 0 && emptyReplies > 1 {
						emptyWait = nextMoveWait(emptyWait, a.failureLimits().TransportBackoff)
						wait = emptyWait
					}
					if wait > 0 {
						a.tellPhase(provider.PhaseRetrying,
							retryOrdinal(emptyReplies+1, verdict.Attempts), time.Now())
					}
					waitBegan := turnNow()
					waitErr := turnBackoff(ctx, wait)
					if took := turnNow().Sub(waitBegan); took < wait {
						emptyOwed += wait - took
					}
					if wait > 0 {
						a.endPhase()
					}
					if waitErr == nil {
						// SAID ONLY AFTER THE WAIT SUCCEEDS, on the ladder's own rule
						// and for its reason: a stop during the wait keeps whatever
						// the person was already reading rather than announcing a
						// replacement that will never be asked for.
						hub.send(Event{Kind: EventRetrying, Text: emptyReplyNotice,
							Retry: retryNews(model, emptyReplies, verdict, nil, "")})
						continue
					}
				}
			} else {
				a.recordAssistant(ai.Message{Role: "assistant", Content: assistantContent(response)}, reasoning.snapshot())
				assistantDone = true
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
				assistantDone = false
			}
			if assistantDone {
				// THE WORDS ARE DURABLE BEFORE THEIR BOUNDARY IS VISIBLE. A room
				// joining after this event reads the response from the journal and
				// must not replay the same streamed text from its catch-up lane.
				hub.send(Event{Kind: EventAssistantDone})
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
			// AND THE TWO READERS AT THE END OF A TURN RUN AGAINST EACH OTHER, not
			// one after the other. They were serial, and they are the longest stretch
			// of an ordinary tool-less message: a mastermind asked whether the answer
			// finished the ask, and then — only once that had come back — a cheap
			// screen and a second mastermind asked whether it should have been work,
			// the last of them on ten minutes of patience with no window of its own.
			// The end of a turn is the one place this file's law cannot help, because
			// the model has stopped and there is no work left to run beside; what
			// there is instead is another reading, so they read together and the turn
			// waits max() rather than sum().
			//
			// THE JUDGE IS STARTED FIRST AND SPENT LAST, which is the order its own
			// effect demands: what it does is START WORK, and work must not be
			// started on top of a turn the reader below is about to re-open. SO IT
			// DECIDES AND DOES NOT ACT — the reading answers a [judgeRuling] and
			// [Agent.applyRouteJudge] below is the only thing with an effect in it
			// (route_judge.go). A reading that starts work from inside its own
			// goroutine is this mechanism used in name and broken in fact.
			// A REPLY THAT ANSWERS THE READER'S NOTE WITH [NoChangeReply] IS NO ANSWER
			// TO JUDGE OR LEARN FROM. The transcript decides whether the token really
			// answered that note; elsewhere it is an ordinary reply, even though every
			// surface still leaves a reply containing only the token undrawn.
			answer := response.Text()
			withdrawn := IsNoChangeReply(answer) && answeredNoteUnchanged(a.snapshot())
			if withdrawn {
				answer = checkpointLastSaid(a.snapshot())
			}
			var judge *judgeRace
			if !withdrawn {
				judge = a.judgeAhead(ctx, user, usedTools, answer)
			}
			a.tellPhase(provider.PhaseChecking, "whether the work is finished", time.Now())
			again, over := a.checkpointReopen(ctx, hub, user, meter, &turn, started, model, response, marked)
			a.endPhase()
			if over {
				judge.end()
				return true
			} else if again {
				// A RE-OPENED TURN IS NOT A TURN THAT ANSWERED IN WORDS ALONE, so the
				// question the judge is holding is about an answer that no longer
				// exists. It is let go of, and the turn is asked again when it really
				// does end.
				judge.end()
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
			ruling, _ := judge.takeAtTheEnd()
			a.endPhase()
			// AND THE TURN IS WHAT SPENDS IT, here, past the two roads above that
			// would have made it wrong: this turn answered in words, nothing is
			// re-opening it and nothing is moving it, so work started now is work
			// started on a turn that is really over.
			a.applyRouteJudge(hub, ruling)
			hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
			// The name comes after the turn is done and before the hub closes:
			// the person is not kept waiting on a title, and the event still has
			// a stream to land on (title.go).
			a.maybeTitle(ctx, hub)
			// AND THE EXCHANGE IS READ FOR ANYTHING WORTH KEEPING, off this
			// goroutine entirely and on the session's own lifetime rather than
			// the turn's (memory.go). Nobody is waiting for it, nothing it finds
			// reaches this turn, and it says nothing whatever happens to it.
			a.learnFromTurn(user.text(), answer)
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

		// THE ENVELOPE RIDES THE ONE SEAM EVERY EXECUTION PASSES THROUGH. A bash-belt
		// worker's submission is validated HERE, before anything runs — the branch
		// bash never starts early (earlyTools admits read-only calls and the branch
		// belt carries none of them), so nothing can have reached the world before
		// this check, and a rejected batch is answered with nothing run at all.
		// See bashbelt_envelope.go for the envelope and its reject roads.
		if skip, ended := a.enforceBashEnvelope(ctx, hub, calls, &turn, started, model); ended {
			return true
		} else if skip {
			// The reads this response started early are dropped exactly as a
			// provider retry drops them, and for the same reason the early-start
			// law admits read-only calls only: a discarded read costs the work and
			// nothing else, and the model must not be handed the answer to a call
			// the harness has just refused to run.
			warm.reset()
			continue
		}

		// A SWEEP THE MODEL WILL NOT HAND OFF IS HANDED OFF HERE (readhandoff.go).
		// The prompt taught the judgement and the models recited it without acting,
		// so the loop — the one place the reading's cost is a fact rather than an
		// instruction — spends the hand-off itself. Conversations only: a task
		// worker's own reads are its work, not a sweep.
		var results []toolResult
		if !a.config.InTask && chatRunEngine != nil && sweep.due(calls) {
			if handed := a.handoffReadSweep(toolCtx, episode, hub, user, calls, sweep, warm); handed != nil {
				results = handed
			} else {
				results = a.runToolsWarm(toolCtx, episode, calls, hub, warm)
			}
		} else {
			results = a.runToolsWarm(toolCtx, episode, calls, hub, warm)
			sweep.count(calls)
		}
		// THE GAP THE LAW IS ABOUT STARTS HERE. Everything between this line and
		// the next request leaving is the turn's own work — recording the results,
		// the fold, the readings that ride beside it — and the law says it is
		// milliseconds.
		pace.resultsIn(time.Now())
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
		if a.checkpointRound(ctx, hub, user, meter, &turn, started, model, calls, nil, marked) {
			return true
		}

		// The ordinary stub citizen remains an end-of-turn pass: running it here
		// would rewrite old turns in the middle of this one and change its cache
		// economics. Only the current-turn fold belongs at every step boundary.
		a.foldTurnOutputs(episode.seenThrough, episode.consumedReads, hub)
		a.maybeCompact(ctx, hub)
	}
}

// ── THE TURN'S OWN DECOMPOSITION ────────────────────────────────────────────

// turnPace times the two moments this file's law is about, and nothing else.
//
// IT IS A SEPARATE CLOCK FROM [laneWitness] ON PURPOSE. That one is per REQUEST
// — it is reset by every attempt, because its job is to time one lane's stream —
// and the law here is about the TURN: the person pressed enter once, and what
// they measured was how long it took for anything to leave and how long until a
// word came back. A figure taken off the witness would have been the last
// attempt's and would have read as a fast turn on a turn that was re-asked three
// times.
//
// The mutex is the partial buffer's: the first word is stamped by whoever is
// reading the provider connection, and the sends are stamped by the loop.
type turnPace struct {
	mu        sync.Mutex
	began     time.Time
	firstSend time.Time
	firstWord time.Time
	results   time.Time
	worstGap  time.Duration
	steps     int
}

// sending stamps a request leaving, and closes whichever gap it ends: the
// person's wait to be sent anywhere on the first one, and a tool-result-to-wire
// gap on every one after a batch.
func (p *turnPace) sending(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps++
	if p.firstSend.IsZero() {
		p.firstSend = now
	}
	if !p.results.IsZero() {
		if gap := now.Sub(p.results); gap > p.worstGap {
			p.worstGap = gap
		}
		p.results = time.Time{}
	}
}

// word stamps the first thing the person read. Called from the stream observer
// on every delta, so it stays one lock and one zero test.
func (p *turnPace) word(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.firstWord.IsZero() {
		p.firstWord = now
	}
}

// resultsIn stamps the instant a tool batch's results are in hand, which is the
// moment the NEXT request becomes owed.
func (p *turnPace) resultsIn(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.results = now
}

// row is the turn as the journal holds it, with the aside names the caller knows
// and this clock does not.
func (p *turnPace) row(aside []string) journalPace {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.firstSend.IsZero() {
		// A TURN THAT NEVER REACHED THE WIRE HAS NO PACE. Negative is the "write
		// nothing" the appender reads, rather than a zero that would claim an
		// instant send.
		return journalPace{SendMS: -1}
	}
	row := journalPace{
		SendMS: p.firstSend.Sub(p.began).Milliseconds(),
		Steps:  p.steps,
		Aside:  aside,
	}
	if !p.firstWord.IsZero() {
		row.FirstWordMS = p.firstWord.Sub(p.firstSend).Milliseconds()
	}
	row.StepGapMS = p.worstGap.Milliseconds()
	return row
}

// journalTurnPace writes the decomposition down, naming which readings were in
// flight beside the work.
//
// THE NAMES ARE WHAT MAKE THE ROW EVIDENCE rather than two numbers. A turn that
// sent in four milliseconds because its recall had been deleted and a turn that
// sent in four milliseconds with the recall, the work-or-words race and a mark's
// drawing all running beside it are the same row without them.
func (a *Agent) journalTurnPace(pace *turnPace, recall *recallAside, marked *markAside) {
	var aside []string
	if recall.everAsked() {
		aside = append(aside, "recall")
		if recall.reasked() {
			// THE RE-ASK IS NAMED because it is the one move this law buys with a
			// second request, and an audit that could not see it could not price it.
			aside = append(aside, "recall:reasked")
		}
		if recall.wasLate() {
			// AND SO IS THE LATENESS, which is the quality half: the block was
			// routed, it could not be spent on the request it was routed for, and it
			// rode the next step instead. Silence here is how a dropped reading
			// looks exactly like a turn that had nothing to recall.
			aside = append(aside, "recall:late")
		}
	}
	if marked.everAsked() {
		aside = append(aside, "mark")
	}
	a.file.appendPace(pace.row(aside))
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

// endStoppedTurn is the ONE place a turn that ended with no answer is put on the
// record, and it exists because there used to be no such place.
//
// A turn whose context was cancelled kept its partial reply, sealed itself and
// returned — which is right — and wrote NOTHING about why. On a reply that was
// still thinking there is no partial to keep either, so the whole ending was a
// transcript with a question in it and nothing after, a model-call row saying
// `context canceled`, and an idle status line. Nobody could tell a stop the
// person pressed from a window that took the conversation over.
//
// TWO ACCOUNTS, FOR TWO READERS. The machine's goes in the journal, in the
// machine's own words, so that the file can answer the question afterwards. The
// person's goes to the surface, in theirs, and only where they did not do it
// themselves: telling somebody what they just pressed is noise, and it is the
// one door that already drew its own ending.
//
// THE JOURNAL ROW IS FOR THE MACHINERY DOORS ONLY. An ordinary stop is a thing
// a person did and watched happen, and a failed-call row on every esc would
// turn the record of a healthy session into a list of failures.
func (a *Agent) endStoppedTurn(ctx context.Context, hub *eventHub, partial *partialBuffer, turn Usage, started time.Time, model string) {
	a.keepPartial(partial, hub)
	if door, stopped := stopCause(ctx); stopped && door != StopByPerson {
		cause := context.Cause(ctx)
		if _, ours := StoppedBy(cause); !ours {
			cause = stopFor(door)
		}
		a.journalFailedCall(ctx, model, "", cause, 1, a.requestEstimate())
		if said := stopSentence(door, stopName(cause)); said != "" {
			hub.send(Event{Kind: EventNotice, Text: said})
		}
	}
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(turn, started, model)})
}

// cutDroppedCallNote is WHY a half-arrived tool call is not in the record, in
// the transcript's own voice.
//
// It is a function rather than a constant because there are two doors into the
// boundary a cut opens now — the person's steer and the mark's reading — and a
// record that told the model it had been steered when a sidecar had read the turn
// would be this build putting words in somebody's mouth. The model reads this
// line on its next request, so it has to be true.
func cutDroppedCallNote(err error) string {
	if errors.Is(err, errMarkCut) {
		return "[incomplete tool call dropped when this answer was read and handed over]"
	}
	return "[incomplete tool call dropped when you steered]"
}

// keepSteeredPartial records the legal assistant half of a cut generation.
// Tool calls are deliberately absent: a call whose result can never follow is
// a provider-invalid assistant message. When fragments had arrived, the text
// says why that instruction is not in the record; otherwise only the text and
// continuation metadata actually received are kept.
func (a *Agent) keepSteeredPartial(partial *partialBuffer, reasoning *reasoningBuffer, droppedCall bool, hub *eventHub, dropped string) {
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
		text += dropped
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
	// The tally sits at the seal because the seal is the shape of a turn: every
	// turn that seals counts one, whether or not telemetry is sent later.
	telemetry.CountTurn()
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

// completeWithRetry sends one provider request and asks again while the
// boundary's verdict says asking again is the move and this model still has
// give-up left ([Agent.weighLadder]). The counted schedule this comment used to
// name — 2s, 4s, 8s, max 3 retries — describes budgets that were deleted before
// the recovery wave; there is one deadline now and it is stated on the loop
// below. The transcript is append-only and the failing response was never
// appended, so asking again re-sends exactly the same messages (the pop a
// retry would need is a no-op in this shape — see internal/exec/bare/loop.go).
// The model is the person's, latched by runTurn and re-read at the request
// boundary (steer.go's THE PERSON'S WORD WINS).
//
// The reasoning level is stamped HERE, on the request path and nowhere else, so
// it reaches every step and every retry of the turn and reaches nothing else:
// the title call and the memory reflex are the session's own errands, not the
// person's question, and a level they asked for their conversation to be thought
// about would be an odd thing to spend on naming it.
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
	// A reading of its own, because this entry has no stream observer feeding one:
	// nothing is ever drawn under it, which is the true answer for a caller that
	// streams to nobody.
	return a.completeWithRetryReasoning(ctx, hub, model, rung, partial, &reachedThePerson{}, &reasoningBuffer{}, warm, forming, len(a.snapshot()))
}

func (a *Agent) completeWithRetryReasoning(ctx context.Context, hub *eventHub, model string, rung effort.Rung, partial *partialBuffer, reached *reachedThePerson, reasoning *reasoningBuffer, warm *warmBatch, forming *formingBatch, frozenToolHistory int) (*ai.Response, string, error) {
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
	// MONEY MOVES AT MOST ONCE FOR ONE TURN. The provider keeps this guard across
	// its own repairs, rungs and hedge arms; the session adds it outside the
	// model ladder so a later model cannot buy the same metered overflow again.
	ctx = provider.WithPlanOverflowGuard(ctx)
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
	// oneMachine says every cut this step took came back from a request with no
	// endpoint diversity at all. It is ANDed rather than ORed: one cut that did
	// have a pool to draw from means the step had one, and the narrower
	// allowance is the honest one (provider's [provider.StreamCut.OneMachine]).
	oneMachine := true
	// waitingSince is when the first of this step's cuts arrived, which is what
	// the person is told the length of while an unbounded wait goes on
	// ([waitingOnOneMachine]). Zero until there is a cut to date.
	waitingSince := time.Time{}
	// hopped is the models this step has already moved to, in order, and its
	// length is where the chain is read from next. It is what the failure
	// sentence names when even the fallbacks could not answer. `origin` is kept
	// beside it because the chain is always the chain of the model the step
	// started on, however far along it the step has walked.
	origin := model
	var hopped []string
	// ── THE LADDER HAS NO LENGTH, AND THE DEADLINE IS THE WHOLE OF THE BOUND ──
	//
	// THE MEASURED FAILURE (docs/design/recovery/DESIGN.md §2 problem 1). This
	// count and the transport's own MULTIPLIED. Each of these attempts is a whole
	// call under the dispatcher's plan — up to `lane.Role.GiveUp` of walking
	// machines and climbing rungs — so three of them was three times the bound
	// the dispatcher believed it was keeping, and then the hop multiplied it
	// again by the length of the chain. Nobody could state the product, which is
	// exactly why the census found chains running eleven minutes.
	//
	// ONE MODEL GETS ONE GIVE-UP. It is the same figure the call under it is
	// bounded by, so the two agree instead of composing: whichever of them ends
	// first, the turn moves on to the next model or says so. A NEW MODEL GETS A
	// FRESH ONE, for the reason the hop below already states about the counts —
	// what the last model did says nothing about this one.
	//
	// AND THE COUNT THAT USED TO SIT BESIDE IT IS GONE. `attempts` was
	// `taxonomy.Limits.TransportAttempts`, the person's `response.attempts`, read
	// once outside this loop and walked as its length. It is the same setting
	// read the honest way now — it SCALES this deadline (lane's [lane.UsePatience],
	// published where the limits are resolved) — so somebody who asks for more
	// patience gets more time rather than more identical requests, and this loop
	// has exactly one bound again.
	deadline := turnNow().Add(a.giveUp())
	// moveOn is THE BUDGET IS SPENT, SO THE MODEL MOVES, and it is a closure
	// because two roads reach it: a verdict that says hop, and a wait that would
	// outlast the deadline (below). Asking the same weights again is the one
	// thing already known not to work; the chain is the adapter's, the same one
	// every other road in this build walks (internal/provider's endpoints.go),
	// and the hop is SAID rather than done quietly, because the rest of this
	// reply arrives in a different voice and the person is watching it happen.
	var moveOn func(taxonomy.Verdict)
	// owed is the part of a wait this loop asked for that the clock did not
	// really take — see THE TURN'S OWN CLOCK above. `unpaid` is what the NEXT
	// move costs when the failure itself asks for no wait ([nextMoveWait]).
	var owed, unpaid time.Duration
	spentAt := func() time.Time { return turnNow().Add(owed) }
	// freshModel is A MODEL GETTING A WHOLE BUDGET OF ITS OWN, and it is a
	// closure because two roads reach it: the rescue chain's hop below, and the
	// person naming a model at the boundary. What the last model did says nothing
	// about this one — both kinds of budget and both clocks start again — and the
	// attempt counter is deliberately NOT reset here, because the two roads sit
	// either side of this loop's own post-statement and want different numbers.
	freshModel := func(next string) {
		model = next
		// AND THE MODEL THE WORK IS ON IS PUBLISHED, because the door that decides
		// whether a person's pick is news has to compare against THIS and not
		// against the session's own model (steer.go's [Agent.rideModel]).
		a.rideModel(next)
		rung = a.effortFor(model)
		cuts, rerouted, oneMachine, waitingSince = 0, false, true, time.Time{}
		deadline, owed, unpaid = turnNow().Add(a.giveUp()), 0, 0
	}
	// takeTheModel is THE ONE PLACE THIS STEP CHANGES MODEL, and `root` is the
	// only thing that differs between the two roads into it — which is a fact
	// about the MOVE and never about who made it.
	//
	// A model a person named is the ROOT OF A NEW CHAIN: the fallbacks that come
	// after it are read off it, and nothing the step walked before them is held
	// against it. A rescue's hop is a STEP ALONG the chain the step is already on,
	// so it is added to what has been tried and the chain keeps its own origin.
	// Reading fallbacks off a rescue target instead is how a bounded chain of two
	// becomes an unbounded walk ([Agent.nextFallback] states that half).
	takeTheModel := func(next string, root bool) {
		if root {
			origin, hopped = next, nil
		} else {
			hopped = append(hopped, next)
		}
		freshModel(next)
	}
	attempt := 0
	for ; ; attempt++ {
		// AND IT IS ASKED AT THE TOP, because it is the only thing that ends this
		// ladder. A failure below has its own reading — the boundary is told the
		// deadline is gone and answers hop-or-end through the one classifier —
		// and this is the case where nothing failed at all: the turn spent its
		// whole give-up on attempts that were cut and re-asked.
		if !spentAt().Before(deadline) && attempt > 0 {
			break
		}
		// ── THE PERSON'S WORD IS TAKEN HERE, AND NOWHERE ELSE ────────────────
		//
		// This is the request boundary the law in steer.go names: the one moment
		// between two requests, reached within [lane.SpokenWithin] of the word
		// because an unproductive request is cut the instant it is said. The chain
		// starts again from the model they named — `origin` is what the fallbacks
		// are read off, and a chain still walking away from the model they just
		// moved off would spend their turn on the choice they had rejected.
		if next, said := a.takeModelWord(); said && next != model {
			takeTheModel(next, true)
			attempt = 0
		}
		// Each attempt streams the reply from the beginning, so the buffer
		// starts empty: an attempt that dies half-way through its text and an
		// interrupt during the next one would otherwise record the two halves
		// concatenated as one answer.
		partial.reset()
		// AND SO IS THE READING OF WHAT THE PERSON HAS IN FRONT OF THEM. It is the
		// same fact about the same dead attempt as the four buffers around it, and
		// it is what every door that may re-ask this request asks (steer.go's
		// [reachedThePerson]).
		reached.reset()
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
		recordEffort(attemptCtx, model, rung)
		// What this call is FOR, for the model-call log. A conversation's own
		// turn, a task child's turn and a checker's turn run the identical loop,
		// and what tells them apart is what this agent IS: a checker answers for
		// a crew role and names that role's own word, a task child IS a node and
		// names the node's, and a conversation is neither and answers as the
		// turn. The node beside the word follows the same fact — a child names the
		// node it IS, a checker the node it CHECKS, a conversation none.
		//
		// THE WORD GOES TO THE DOOR AND THE NODE STAYS HERE. A purpose is what a
		// request is FOR and the door is the one place it is spelled onto a call
		// (clientdoor.go); which node made it is a fact only this line knows.
		//
		// A CREW ROLE WINS OVER taskID, AND THE TWO ARE NEVER BOTH SET: a checker
		// is handed the node it checks in [Config.checksNode] and is deliberately
		// not the node itself, so it leaves taskID at 0 (session.go's law).
		purpose := purposeTurn
		switch {
		case a.config.crewRole != "":
			purpose = callPurpose(a.config.crewRole)
			if a.config.checksNode != 0 {
				attemptCtx = provider.WithCallNode(attemptCtx, strconv.FormatUint(a.config.checksNode, 10))
			}
		case a.config.taskID != 0:
			purpose = purposeTask
			attemptCtx = provider.WithCallNode(attemptCtx, strconv.FormatUint(a.config.taskID, 10))
		}
		messages, carried := a.snapshotWithReasoning()
		if wake, settle := settleWakeFrom(ctx); settle && wake.prompt != "" && len(messages) > 0 {
			rolePage := textMessage("system", strings.TrimSpace(wake.prompt))
			messages = append(messages[:1:1], append([]ai.Message{rolePage}, messages[1:]...)...)
		}
		// OLD FROZEN TOOL RESULTS ARE ALREADY CONSUMED EVIDENCE. The live
		// transcript keeps them whole — the journal is the record — and the
		// request the model is about to read does not. compactToolHistory leaves
		// the system prompt, the newest frozen batch and everything this turn
		// has already sent verbatim, and every result it reduces names where the
		// whole of it can be read back (toolcompact.go).
		// The place a pointer may name is read ONCE for the whole request, under
		// the lock an anchor takes to move it (toolcompact.go).
		place := a.resultPlaceNow()
		messages = a.compactToolHistory(messages, frozenToolHistory,
			func(message ai.Message) string { return a.fullResultPointer(message, place) })
		attemptCtx = provider.WithMessageReasoning(attemptCtx, carried)
		attemptCtx, generation := a.beginGeneration(attemptCtx, reached)
		response, err := a.completeWithModel(attemptCtx, purpose, messages, model,
			ai.WithTools(a.beltDefinitions()))
		cause := a.endGeneration(generation)
		if errors.Is(cause, errSteerCut) {
			return response, model, errSteerCut
		}
		// A CUT THAT LOST ITS RACE IS NOT A CUT. Any of the three doors can fire in
		// the microsecond between a good answer arriving and this line reading the
		// cause, and throwing that answer away would be the machine spending a
		// person's money to obey itself. The steer above is the one exception and
		// it is deliberate: a person typing into a turn has said they want the
		// boundary whatever else happened.
		if err == nil {
			return response, model, nil
		}
		// AND THE MARK'S READING TRAVELS OUT, because what it wants is the
		// BOUNDARY and not another request: the drawing it made is spent by
		// [Agent.checkpointSettle] at the top of the turn's own loop, which is
		// where the turn can actually be handed over (checkpoint.go).
		if errors.Is(cause, errMarkCut) {
			return response, model, errMarkCut
		}
		// AND THE RECALL'S RE-ASK IS TAKEN HERE, silently, because nothing failed.
		// The block of remembered lines landed in the transcript while this request
		// was in flight and before the person had read a word of it, so the request
		// is simply assembled again with it in — one move, no backoff, no row and
		// no sentence, since there is nothing a person could act on and nothing
		// they saw. It happens at most once a turn ([recallAside]), and the
		// provider's own prefix cache makes the second send the cheap one.
		if errors.Is(cause, errRecallCut) {
			continue
		}
		// AND SO IS THE PERSON'S OWN WORD, for the same reason and one more: they
		// have read nothing of this request ([activeGeneration.productive] is what
		// let it be cut at all), so there is no failure to report and nothing to
		// apologise for. The boundary at the top of this loop takes the model they
		// named and the request is assembled again on it. The RECORD still gets its
		// line — money was spent reaching a machine that never answered, and an
		// autopsy of a long step has to be able to see why the request ended.
		if errors.Is(cause, errPersonCut) {
			// AND THE ROW CARRIES NO DOOR, deliberately. A door on a failed call is
			// this machine saying it stopped the TURN (sessionfile.go reads exactly
			// that bit, and resume.go acts on it); this turn did not stop, it moved.
			// What the row is for is the autopsy of a long step — money was spent
			// reaching a machine that never answered — so it says why in words a
			// person can read and leaves the shape of the conversation alone.
			a.journalFailedCall(ctx, model, "", cause, attempt+1, a.requestEstimate())
			// AND THE ROOM IS TOLD, THROUGH THE ONE DOOR THAT ALREADY MEANS THIS.
			// EventRetrying is "the attempt you are watching is void": it withdraws
			// what that attempt drew and restarts the wait clock the surface is
			// counting up (internal/tui3's feed.retry and app.awaited). Saying
			// nothing would leave the person's screen counting the dead request —
			// the very `waiting · 13m 37s` line they spoke to get rid of.
			//
			// IT IS A MOVE AND IT SAYS SO. `Next` is what separates a hop from a
			// retry for every surface (retrynews.go), and this is a hop the person
			// made: the row reads as moving to the model they named, with no
			// arithmetic, because the count belongs to a patience nobody spent.
			said, _ := a.peekModelWord()
			hub.send(Event{Kind: EventRetrying, Text: personCutNotice, Retry: &RetryNews{
				Model: model, Next: said, Reason: personCutReason,
			}})
			continue
		}
		lastErr = err

		if ctx.Err() != nil {
			// THE TURN ITSELF IS OVER and no rung of this ladder can be climbed
			// on a dead context. What is owed here is the ACCOUNT, so the cause
			// travels out in place of a bare `context canceled` — it unwraps to
			// [context.Canceled], so every caller that only asks whether the
			// turn was cancelled still gets its answer (stopcause.go).
			return nil, model, context.Cause(ctx)
		}
		// AND A GENERATION CUT WITH THE TURN STILL ALIVE IS RE-ASKED RATHER THAN
		// REPORTED. A steer is answered above, by the boundary it exists to open;
		// anything else that cuts one request out from under a live turn — now
		// or later — is machinery, and machinery that takes a reply away owes the
		// person another attempt at it rather than a turn that stops. The three
		// resets at the top of this loop are exactly what such a cut needs: the
		// text that was streamed, the reads it started and the half-arrived calls
		// are all thrown away before the next request is assembled, so nothing of
		// the dead attempt reaches the replacement. It costs a slice of the
		// give-up above, which is what stops a door that cuts every generation
		// from cutting them forever.
		//
		// AND THE ROW SAYS HOW FAR IN WITHOUT SAYING HOW FAR THERE IS TO GO.
		// `Attempts` was the ladder's length and there is no length any more, so
		// it is left at nothing — the surface draws `2 of 4` only when it has
		// both halves and draws neither when it has one (internal/tui3's
		// failureCountWord), which is the emptiness law doing exactly its job.
		if cause != nil {
			a.journalFailedCall(ctx, model, "", cause, attempt+1, a.requestEstimate())
			hub.send(Event{Kind: EventRetrying, Text: cutShortNotice, Retry: &RetryNews{
				Model: model, Attempt: attempt + 1,
				Reason: "the reply was cut short",
			}})
			continue
		}
		// AND THE FAILURE IS WRITTEN DOWN BEFORE ANYTHING DECIDES WHAT TO DO
		// ABOUT IT. Every other outcome of a request reaches the journal; this
		// one reached nothing at all, and a measured run (see [journalError])
		// left five hours of budget unspent with the whole record of why being a
		// turn that stopped. It is journaled per ATTEMPT, so a ladder of three
		// reads as a ladder.
		a.journalFailedCall(ctx, model, "", err, attempt+1, a.requestEstimate())
		// A GUARD'S CUT IS A DIFFERENT KIND OF SPENDING, and it is counted apart
		// from the outright failures rather than answered apart from them. The
		// request was served and the REPLY came apart, so the loop's own attempt
		// number does not advance for one: a cut is not evidence that the
		// endpoint is failing, and it must not shorten the patience a real fault
		// gets. The three resets at the top of this loop are exactly what a cut
		// needs — the soup that was streamed, the reads it started, the calls it
		// was half-way through asking for — so a cut re-enters through the same
		// door a fault does with the junk already gone.
		cut, isCut := provider.CutFrom(err)
		if isCut {
			if cut.Rerouted {
				rerouted = true
			}
			if !cut.OneMachine {
				oneMachine = false
			}
			if waitingSince.IsZero() {
				waitingSince = turnNow()
			}
			cuts++
		}
		// AND THE BOUNDARY READS IT. The row above says WHAT the provider said;
		// this says what the harness took it to MEAN, which is the only half of
		// the record the money turns on (taxonomy_boundary.go).
		//
		// ONE ROAD LEAVES THIS POINT. The verdict says ask again, move to the
		// next model, or stop — and a cut stream and a refused request differ
		// only in the evidence they arrive carrying. They used to differ in the
		// CODE: the cut had its own three budgets and hopped on its own
		// authority here, while a refusal walked the ladder and then ended the
		// turn, so a chain the person configured was reachable from one road and
		// invisible from the other. One 502 and three 429s inside seventy-five
		// seconds ended a turn on 2026-09-10 with two other models sitting
		// unasked in the same session, and that is the road this is.
		next, haveFallback := a.nextFallback(ctx, origin, hopped)
		// AND A PERSON WHO HAS NAMED A MODEL IS SOMEWHERE LEFT TO GO. This reading
		// is what the boundary is told about whether the step can move at all, and
		// a step with an empty chain used to end the turn on "there is nowhere else
		// to try" with the model they had just chosen sitting unasked. It PEEKS —
		// this line runs on every failure, including the ones that go on to ask the
		// same model again, and a word taken by a move that never happened would be
		// a word the person never got (steer.go's [Agent.peekModelWord]).
		standing, standingSaid := a.peekModelWord()
		haveFallback = haveFallback || standingSaid
		// The hop, bound to THIS attempt's facts — which model comes next, and
		// whether the thing that failed was a cut. See the declaration above.
		moveOn = func(verdict taxonomy.Verdict) {
			// ── THE PERSON'S WORD IS THE HEAD OF EVERY CHAIN ─────────────────
			//
			// The chain is this build's guess at where a failing step should go
			// next. A model the person named while the step was failing is not a
			// guess, so it is where the step goes and the ladder is not consulted
			// at all — and the word is taken HERE, where the move is really being
			// made, rather than a moment later at the boundary, because the
			// sentence below names where the reply went and a hop that announced
			// the ladder's next rung and was then overruled would have named a
			// model the reply never reached.
			// AND IT CANNOT BE EMPTY. The peek above is what told the boundary this
			// step had anywhere to go, so when the ladder offered nothing the word
			// standing then is the only reason this closure exists. Peek, take and
			// this line all run on the turn's own goroutine, which is what makes
			// "what I peeked is what I take" a fact rather than a hope; the fallback
			// is that invariant written down rather than assumed, because a move
			// announced to a model named `""` would say `moving to` and then nothing.
			to, theirs := next, false
			if word, said := a.takeModelWord(); said {
				to, theirs = word, true
			} else if to == "" && standing != "" {
				to, theirs = standing, true
			}
			if to == "" {
				// Nowhere to go after all — unreachable, because the boundary read
				// `fallback` off the same two facts. Two lines to make an announcement
				// of a move to nothing impossible rather than unlikely.
				return
			}
			// AND THE STATUS LINE SAYS SO WHILE IT HAPPENS, by the one word that
			// means a person's answer is changing hands
			// ([provider.PhaseSwitchingModel]). That word used to be posted by the
			// adapter's own model hop, which is deleted — this is the only model
			// change in the build now, so it is the only thing that can say it, and
			// a hop that sent only a feed event left the status line drawing the old
			// model's clock.
			a.tellPhaseThen(provider.PhaseSwitchingModel, "", to, time.Now())
			hub.send(Event{Kind: EventRetrying, Text: hopNotice(cut, verdict, to),
				Retry: retryNews(model, spentOn(attempt, cuts, isCut), verdict, cut, to)})
			// A NEW MODEL GETS A WHOLE BUDGET OF ITS OWN — both kinds of it, and
			// its own give-up. What the last one did says nothing about this one,
			// and a fallback that inherited a spent budget would be given up on
			// before it had answered once. It is [takeTheModel] above, the one
			// place this step changes model, which the request boundary reaches
			// through the same call.
			takeTheModel(to, theirs)
			// The attempt counter is put one BEHIND its first rung, because the
			// loop's own post-statement is what advances it.
			attempt = -1
		}
		// ── A SPENT DEADLINE IS A SPENT BUDGET, SAID IN THE ONE WORD THE
		// BOUNDARY ALREADY UNDERSTANDS ─────────────────────────────────────
		//
		// The verdict says whether asking again is the right MOVE; the deadline
		// says whether this model still has any of the person's turn left to
		// spend on it. Rather than a second road out of this switch, a spent
		// deadline is told to the boundary as a spent ladder — which is what it
		// is — so the answer comes back through the ONE classifier as hop or
		// end, exactly as a spent count does. It is the same idiom
		// `movesForFailure` uses for a node that died on the wire.
		ladder := transportLadder{
			attempt:    attempt + 1,
			cuts:       cuts,
			degenerate: isCut && degenerateCut(cut),
			rerouted:   rerouted,
			oneMachine: cuts > 0 && oneMachine,
			watched:    a.config.Interactive && !a.config.isWorker(),
			fallback:   haveFallback,
			outOfTime:  !spentAt().Before(deadline),
		}
		// TWO READS ARE ONE FAILURE. The verdict is settled first — whether
		// asking again is the right move, and then whether there is time to —
		// and the line is written once, at the end of this switch, carrying the
		// answer that was acted on (taxonomy_boundary.go's [Agent.weighLadder]).
		verdict, evidence := a.weighLadder(err, ladder)
		// ── AND WHETHER THERE IS TIME TO ASK AGAIN IS PART OF THE SAME READING ──
		//
		// Asking again is only a move if there is time to make it. A ladder that
		// paid its wait and came back to find the give-up gone would give up
		// SILENTLY, with a chain the person configured sitting unasked — the exact
		// failure #794 closed, and the reason the ending and the hop both come
		// from the one classifier. So the wait is worked out here, and if it
		// would outlast the deadline the same failure is read once more with
		// that fact on it ([taxonomy.Evidence.OutOfTime]) and answers hop-or-end.
		//
		// AND A FAILURE THAT ASKS FOR NO WAIT STILL PAYS ONE AFTER THE FIRST, or
		// the deadline over this ladder is reached as fast as the endpoint can
		// fail ([nextMoveWait] states the whole argument).
		wait := time.Duration(0)
		if verdict.Retries() {
			if wait = verdict.Backoff; wait <= 0 && attempt > 0 && !isCut {
				wait = nextMoveWait(unpaid, a.failureLimits().TransportBackoff)
			}
			// A CUT USED TO BE UNABLE TO ASK FOR A WAIT AT ALL, and the guard
			// that did it read `!isCut` here — written when no cut had a backoff
			// to ask for, and left standing when one did. A verdict carrying a
			// wait that the loop then dropped is the loop and the boundary
			// disagreeing in silence, so the verdict's own figure is honoured
			// whatever shape produced it, and only the FALLBACK wait for a
			// failure that named none stays a refusal's alone.
			//
			// AND AN UNBOUNDED WAIT IS NOT CONVERTED BY THE DEADLINE. It is the
			// one verdict the give-up does not end (taxonomy's [waitsForEver]),
			// because the person who can see it waiting is the bound.
			if !verdict.Unbounded && !spentAt().Add(wait).Before(deadline) {
				ladder.outOfTime = true
				verdict, evidence = a.weighLadder(err, ladder)
			}
		}
		// AND THE LINE IS WRITTEN ONCE, HERE, carrying the verdict that is about
		// to be acted on rather than one that was reconsidered
		// (taxonomy_boundary.go).
		a.writeLadderVerdict(verdict, evidence, model, "")
		if provider.IsConnectionUnavailable(err) {
			return nil, model, err
		}
		// ── WHOSE MISTAKE WAS IT? THE VERDICT ANSWERS, AND ONLY THE VERDICT ──
		//
		// A 4xx that named no upstream is the router reading OUR OWN BYTES and
		// saying no, and every endpoint alive will say the same thing about the
		// same request — so the ladder must stop rather than spend 2s, 4s and 8s to
		// be told it three times. A 4xx that DID name an upstream is that
		// upstream's refusal, another endpoint may serve it, and the adapter has
		// already taken the refusing lane out of the ledger so the next attempt is
		// routed elsewhere (internal/provider's velocity.go, refuseUpstream).
		//
		// THAT DISTINCTION USED TO BE DRAWN HERE TOO, one line below the
		// classification that had just drawn it: `refusal.OurRequest()` with a bare
		// return under it, the second of the three rules a 404 was classified by
		// (docs/design/recovery/DESIGN.md §2.6). It is gone. The shape reaches the
		// switch below as [taxonomy.Shape] carrying [taxonomy.ActionReshape], which
		// ends the request through the same door every other ending uses — and,
		// unlike the bare return, names the SHAPE in the row a person reads instead
		// of calling our own bad bytes a verdict about their work.
		// AND NOTHING BELOW THIS LINE READS THE SENTENCE. There used to be one
		// more gate here — `isContextOverflow(errMsg) || !isRetryable(errMsg)`,
		// two regexes over the provider's prose — and it RETURNED, forty lines
		// after the verdict above had read the same failure off typed evidence. A
		// string decided and the verdict was thrown away, which is how the
		// measured turn of 2026-09-10 ended on a routing 404 that matched no
		// retryable pattern while two other models sat unasked, and it is the
		// exact bug class `Evidence.Routing` had to be added to work around.
		//
		// EVERY SHAPE THAT GATE ANSWERED FOR IS A FACT ON THE EVIDENCE NOW, and
		// the switch below is the only road out: an overflow is
		// [taxonomy.Evidence.Overflow] and was answered at the top of this loop,
		// our own bytes are [taxonomy.Evidence.OurBytes], a model the router has
		// put down is [taxonomy.Evidence.Withdrawn], an account that could not be
		// served is [taxonomy.Evidence.Unserved], and a socket that hung up is
		// [taxonomy.Evidence.Wire] — which is where [isRetryable] still lives, as
		// evidence rather than as an answer.

		switch {
		case verdict.EndsTurn():
			// NOT THE WIRE AT ALL, so there is nothing here to ask again and
			// nothing to move to: the provider read this request and answered
			// about it. The failure travels out with its TYPE intact, because the
			// layers that read it decide by that and never by our sentence
			// (taxonomy_boundary.go's [providerCouldNotServe]) — and with the
			// person's own words in front of it, because the router's sentence is
			// not one anybody outside this process can act on ([endingWords]).
			return nil, model, endingWords(err, verdict, a.failureServiceWord(model))
		case verdict.Retries():
			// A CUT IS SAID AT ONCE AND THEN PAYS THE VERDICT'S WAIT LIKE ANY
			// OTHER FAILURE. Its junk is already gone from the page, so the
			// discard is announced before the wait rather than after it, and the
			// loop's own attempt number does not advance for it (a cut is not
			// evidence the endpoint is failing).
			//
			// THIS BRANCH USED TO `continue` HERE, ABOVE THE WAIT (#1358). A pool
			// cut asks for no wait, so nothing was lost there; but the one
			// machine's unbounded retry carries a wait that climbs to
			// [taxonomy.OneMachineCutCeiling], and skipping it re-asked a server
			// that keeps cutting in a tight loop for ever — forty cuts were
			// forty-one requests in a millisecond, with no status line, and the
			// held-down attempt number kept the deadline from ever being read.
			if isCut {
				hub.send(Event{Kind: EventRetrying, Text: cutNotice(cut),
					Retry: retryNews(model, cuts, verdict, cut, "")})
				attempt--
				if wait <= 0 {
					continue
				}
			} else {
				unpaid = wait
			}
			// AND THE WAIT IS SAID OUT LOUD. This ladder is the longest silence
			// in the whole request path — two seconds, then four, then eight,
			// with a failed request in front of each of them — and until this
			// line it told the surface nothing at all, so a person watching a
			// turn back off for fourteen seconds saw a clock counting a request
			// that had already failed.
			//
			// AND THE ORDINAL IS DRAWN ONLY WHERE THERE REALLY IS A DENOMINATOR.
			// It used to read `2 of 4` off the person's `response.attempts`,
			// which is not a count of anything any more — it is how much time
			// this model gets — so the line says `trying again` on an ordinary
			// failure and keeps its arithmetic for a reply that came apart, which
			// has a real allowance ([taxonomy.transportBudget]). Nothing is
			// invented to fill the gap: unknown renders as nothing.
			// AND AN UNBOUNDED WAIT SAYS HOW LONG IT HAS BEEN WAITING, because
			// it is the one retry with no denominator to count towards, and a
			// phase that says only `retrying` for ten minutes is a hang as far
			// as the person can tell ([waitingOnOneMachine]). A bounded cut
			// counts its own allowance, which is cuts and not attempts.
			detail := retryOrdinal(attempt+2, verdict.Attempts)
			if isCut {
				detail = retryOrdinal(cuts+1, verdict.Attempts)
			}
			if verdict.Unbounded && cuts > 0 {
				detail = waitingOnOneMachine(cuts, turnNow().Sub(waitingSince))
			}
			a.tellPhase(provider.PhaseRetrying, detail, time.Now())
			waitBegan := turnNow()
			waitErr := turnBackoff(ctx, wait)
			if took := turnNow().Sub(waitBegan); took < wait {
				owed += wait - took
			}
			a.endPhase()
			if waitErr != nil {
				return nil, model, waitErr
			}
			// THE PAGE DISCARDS THE SAME ATTEMPT AS THE JOURNAL. The next loop
			// resets partial, reasoning and forming before requesting a
			// replacement; a phase-clock update alone cannot remove the old
			// streamed answer. Say this only after the wait succeeds: a stop
			// during backoff keeps its partial reply. A cut said its own discard
			// before the wait, so it is not said twice.
			if hub != nil && !isCut {
				hub.send(Event{Kind: EventRetrying, Text: retryNotice,
					Retry: retryNews(model, attempt+1, verdict, nil, "")})
			}
			continue
		case verdict.Hops():
			moveOn(verdict)
			continue
		}
		// NOWHERE LEFT TO ASK. The sentence names what happened in the person's
		// own terms and, when a chain was actually walked, the models that also
		// could not answer — because "try a different model" said to somebody who
		// has just watched two of them fail is the surface not knowing what it did.
		if isCut {
			return nil, model, cutFailure(cut, cuts, hopped)
		}
		return nil, model, transportFailure(lastErr, verdict, origin, attempt+1, hopped, a.failureServiceWord(model))
	}
	// AND THE LOOP FALLS OUT HERE ONLY WHEN THE DEADLINE WENT WITHOUT A FAILURE
	// TO READ — every attempt cut short and re-asked until the give-up was gone.
	// The count in the sentence is what this turn actually SPENT rather than the
	// constant it used to be bounded by, because there is no constant any more;
	// the prefix itself is kept exactly as it was, because a surface reads it
	// (internal/tui3's stripRetryPrefix).
	if lastErr == nil {
		lastErr = errors.New("the model could not be reached")
	}
	return nil, model, fmt.Errorf("after %d retries: %w", attempt-1, lastErr)
}

// nextMoveWait is what a ladder pays before its next move WHEN THE FAILURE ITSELF
// ASKS FOR NO WAIT: [retryBaseDelay] first and then double, capped so a
// long-lived turn's doubling stays arithmetic rather than overflowing into a
// negative duration. The deadline over the ladder is the real bound.
//
// WHY THERE IS A WAIT AT ALL FOR A FAILURE THAT WAITING DOES NOT MEND. An empty
// 200 and a mangled tool call are not an endpoint under strain — it answered, at
// once, with something that was not an answer — and what mends them is being
// served by somebody else, which costs no time (internal/taxonomy's waitFor says
// exactly this and returns no backoff for either). That argument is about the
// FIRST one, and it is kept: the first such failure moves at once. It stops
// being true for the ones after it, because a ladder bounded by a deadline that
// pays nothing between two requests is a ladder that sends as fast as an
// endpoint can say nothing — the deadline is reached, honestly, in a great many
// full-price requests. So the first is free and the rest double.
func nextMoveWait(paid, first time.Duration) time.Duration {
	if first <= 0 {
		first = retryBaseDelay
	}
	if paid <= 0 {
		return first
	}
	if paid >= lanes.TurnGiveUp {
		return lanes.TurnGiveUp
	}
	return paid * 2
}

// retryOrdinal is `2 of 4`, and nothing at all when either half is missing.
//
// BOTH HALVES OR NEITHER. A denominator this build cannot stand behind is the
// emptiness law's own example: `2 of 0` is not a smaller truth than `2 of 4`, it
// is a different and false one. internal/provider's `ordinalOf` and
// internal/tui3's `failureCountWord` are the same rule said at the two other
// grains a person reads it at.
func retryOrdinal(at, of int) string {
	if at <= 0 || of <= 0 || at > of {
		return ""
	}
	return fmt.Sprintf("%d of %d", at, of)
}

// waitingOnOneMachine is what a person reads while the harness keeps asking the
// only machine there is.
//
// IT IS THE HALF THAT MAKES THE OTHER HALF SAFE. Asking for ever is patience
// when somebody can see it happening and dishonest when they cannot: the same
// loop behind a phase that says `retrying` and nothing else is indistinguishable
// from a wedged program, and the person's only move is to guess. So the line
// carries the two facts they would ask for, how many times it has asked and how
// long that has taken, and the one thing they can do about it.
//
// It says nothing about WHY the machine is quiet, because this layer does not
// know: weights still loading, one slot already busy, and a request the server
// will never accept all arrive here as the same silence.
func waitingOnOneMachine(asks int, waited time.Duration) string {
	if asks < 1 {
		return ""
	}
	times := "once"
	if asks > 1 {
		times = fmt.Sprintf("%d times", asks)
	}
	return fmt.Sprintf("no answer %s in %s · still asking · esc stops", times, roundWait(waited))
}

// roundWait is a waiting length in the shortest honest words: seconds under a
// minute, whole minutes over one. A person watching a spinner wants to know
// whether this has been going for twenty seconds or twenty minutes, and no
// grain finer than that changes anything they would do.
func roundWait(d time.Duration) string {
	if d < time.Minute {
		if s := int(d.Round(time.Second) / time.Second); s > 0 {
			return fmt.Sprintf("%ds", s)
		}
		return "0s"
	}
	return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
}

// giveUp is how long one model may spend answering this agent's turn: the
// role's own measured patience ([lane.Role.GiveUp]) with the person's own
// factor already on it (`response.attempts`, published where the limits are
// resolved — taxonomy_boundary.go). It is asked through the limits so the
// factor is resolved before the first request of the first turn goes out.
func (a *Agent) giveUp() time.Duration {
	a.failureLimits()
	return a.laneRole().GiveUp()
}

// emptyReplyNotice is the dim line for a 200 that carried nothing. It says what
// happened rather than "the request failed", because from the person's side
// nothing failed: the model answered, and the answer was empty — which is the
// one transport shape they can actually see the shape of.
const emptyReplyNotice = "nothing came back from the model — asking again"

// retryNotice is the dim line for a request that FAILED and is being sent again.
// It says nothing about the shape of the failure, which is the honest register
// for something the person can neither hurry nor answer; [RetryNews.Reason] on
// the same event carries the shape for a surface that draws one.
const retryNotice = "the request failed — asking again"

// degenerateCut says a cut was the reply ceasing to be language rather than the
// stream going quiet. The two spend different allowances and the reason is
// stated where the allowance is (internal/taxonomy's transportBudget).
func degenerateCut(cut *provider.StreamCut) bool {
	return cut != nil && (cut.Reason == provider.CutBabble || cut.Reason == provider.CutMachinery)
}

// spentOn is how much of this model's budget is gone, in the units the verdict
// counted it in: cut attempts for a cut, ladder attempts for anything else.
func spentOn(attempt, cuts int, isCut bool) int {
	if isCut {
		return cuts
	}
	return attempt + 1
}

// retryNews is one [EventRetrying]'s payload (retrynews.go). `cut` is nil when
// the attempt failed outright rather than being cut, and `next` is empty unless
// the step is moving to another model.
func retryNews(model string, spent int, verdict taxonomy.Verdict, cut *provider.StreamCut, next string) *RetryNews {
	reason := transportWords(verdict)
	if cut != nil {
		// A CUT KNOWS MORE ABOUT ITSELF THAN ITS CLASS DOES. The taxonomy groups
		// every quiet stream under one shape, which is right for a journal line
		// counting a thousand of them and thin for a person who is owed the
		// difference between a model that went quiet and one that ran on forever.
		reason = cutWords(cut)
	}
	return &RetryNews{
		Model:    model,
		Attempt:  spent,
		Attempts: verdict.Attempts,
		Reason:   reason,
		Next:     next,
	}
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

// Degeneration is the one thing the gate says nothing about: soup is a claim
// about the transcript and the weights reading it, never about which endpoint
// delivered it, so it keeps its own short allowance either way. A machinery leak
// shares that allowance for the same reason — the answer came back wrong-shaped,
// and asking the same lane again returns the same shape ([degenerateCut]).
//
// AN OVERRUN IS AN ENDPOINT CLAIM and shares the silence allowance deliberately.
// A reply that ran past the wall its own lane earned is that lane failing to
// finish, exactly as a reply that went quiet is — the ledger struck it either
// way (internal/provider's noteCutProvider) — so the question "did anything
// actually move" governs both.

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
//
// ── IT READS WHAT WAS TRIED AND NO LONGER COUNTS ITS OWN HOPS ───────────────
//
// It used to index the chain blind: `options[len(hopped)]`, where `hopped` was a
// list this loop kept itself. That is only right while this loop is the ONLY
// thing that changes a model, and until 2026-09-10 it was not — the adapter's
// endpoint ladder walked the same `FallbackModels` at its foot and neither knew
// the other had been there, so a turn could pay for one fallback twice and skip
// another entirely (docs/design/recovery/DESIGN.md §2.2). The adapter's walk is
// deleted and this is the one model hop in the build; what it reads is the FACT
// of which models the dispatcher has actually put on the wire for this turn
// ([provider.ModelsTried]), folded with this loop's own record, and it answers
// with the first model on the chain that is on neither.
//
// A context with no record answers nothing, and the fold is what makes that
// safe: `hopped` alone still bounds the walk exactly as it always did.
func (a *Agent) nextFallback(ctx context.Context, origin string, hopped []string) (string, bool) {
	// AND `--one-model` IS A PERSON SAYING NO TO THIS, in the one file that has
	// to honour it rather than only in the door that empties the chain. The flag
	// already leaves the adapter with no fallbacks to offer, so this is belt and
	// braces — and it is worth having: the promise is "every text call this
	// session makes rides the model you named" ([Config.OneModel]), a completer
	// that offers a chain anyway is a completer this session must refuse, and the
	// law is stated in this file's own comment above [cutBudget]'s old home while
	// being enforced nowhere in it. It is the same guard the checker's failover
	// keeps for the same reason (taxonomy_boundary.go's failoverCheckerModel).
	if a.config.OneModel {
		return "", false
	}
	options, ok := a.modelFallbackChain(origin)
	if !ok {
		return "", false
	}
	// AND THE CHAIN IS STILL BOUNDED BY ITS OWN LENGTH. A turn may move as many
	// times as the chain is long and no further — the cap is the adapter's
	// (internal/provider's maxFallbackModels) and is not re-decided here — so a
	// question whose every fallback has been asked has nowhere left to go, which
	// is what makes the difference between moving on and giving up.
	if len(hopped) >= len(options) {
		return "", false
	}
	tried := map[string]bool{normalizeHop(origin): true}
	for _, model := range hopped {
		tried[normalizeHop(model)] = true
	}
	for _, model := range provider.ModelsTried(ctx) {
		tried[normalizeHop(model)] = true
	}
	for _, model := range options {
		if !tried[normalizeHop(model)] {
			return model, true
		}
	}
	return "", false
}

// normalizeHop folds a model id the way a comparison between two records of it
// has to be folded: this loop's own list is written from the chain, and the
// adapter's is written from whatever spelling actually reached the wire.
func normalizeHop(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// cutShortNotice is the dim line for a request that was cut out from under a
// turn that is still going — not by the stream guard, which has its own words
// below, but by machinery inside this process.
//
// It is [cutNotice]'s register and for the same reason: the turn is still going,
// nobody has to decide anything, and the person is owed the fact that the reply
// they were watching is being started over rather than an unexplained pause.
const cutShortNotice = "the reply was cut short — asking again"

// What a person reads when their own word is what let go of the request.
// NOTHING FAILED HERE and the words may not suggest one did: they chose a model,
// nothing of the request had reached them, and the step is asking again on
// theirs. The reason is the half a surface composes its own row from
// ([RetryNews.Reason]); the notice is the whole sentence for one that does not.
const (
	personCutReason = "you chose another model"
	personCutNotice = "you chose another model — asking it instead"
)

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

// cutWords is what a cut WAS, with nothing about what is being done next. It is
// the shape [RetryNews.Reason] carries, and it is spelled apart from [cutNotice]
// because a sentence and a fact are not the same thing: the line says "asking
// again" because a person is watching a wait, and the fact is drawn into whatever
// row a surface has already built.
func cutWords(cut *provider.StreamCut) string {
	switch cut.Reason {
	case provider.CutBabble:
		return "the reply lost its thread"
	case provider.CutStalled:
		return "the model went quiet mid-reply"
	case provider.CutOverrun:
		return "the reply kept going and never finished"
	case provider.CutMachinery:
		return "the model answered in its own internal markup instead of words"
	default:
		return "nothing came back from the model"
	}
}

// hopNotice is the line the person reads when the step gives up on one model
// and finishes the reply on another.
//
// It is [cutNotice]'s register — what happened, then what is being done — with
// the one difference that matters: it NAMES THE MODEL. The rest of the answer
// will arrive in a different voice, at a different price, and somebody watching
// text appear is owed the reason before it does.
//
// `cut` is nil when what spent the model's budget was the request FAILING rather
// than the reply coming apart — a refusal, a reset, a deadline — and the verdict
// is what says which of those it was (taxonomy_boundary.go's transportKeptWords).
func hopNotice(cut *provider.StreamCut, verdict taxonomy.Verdict, next string) string {
	if cut == nil {
		return transportKeptWords(verdict) + " — finishing this one on " + next
	}
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

// transportFailure is the sentence a turn ends on when the request kept FAILING
// — a refusal, a reset, a deadline — and there was nowhere left to ask.
//
// IT IS TWO SENTENCES, and which one it is turns on whether a chain was actually
// walked. When one was, the models that also could not answer are named and the
// advice to try another model is dropped, because it has already been taken
// twice ([cutFailure] states the whole argument). When none was, the sentence is
// the one this build has always ended on — `after 3 retries: …` — which is not
// prose anybody loves and IS what several layers out and a good deal of the
// record already read, so it is left exactly as it was.
func transportFailure(err error, verdict taxonomy.Verdict, origin string, attempts int, hopped []string, service string) error {
	if said, ok := terminalFailureWords(err, service); ok {
		return &transportGaveUp{err: err, said: said}
	}
	if len(hopped) == 0 {
		return fmt.Errorf("after %d retries: %w", attempts-1, err)
	}
	return &transportGaveUp{
		err: err,
		said: fmt.Sprintf("%s: %s was asked %s, and %s. /model to pick another one yourself",
			transportWords(verdict), origin, timesWord(attempts), alsoTried(hopped)),
	}
}

// endingWords puts the person's own account of a failure in front of the
// provider's, keeping the failure itself reachable underneath.
//
// NO RAW ROUTER SENTENCE REACHES A SCREEN. `API error (404): No endpoints found
// matching your data policy` is a true thing to write in a journal and a useless
// thing to show somebody whose turn has just stopped: it names machinery they
// have no access to, about a decision they did not make. The boundary has
// already said what the failure WAS in a person's vocabulary
// (taxonomy_boundary.go's [transportWords]), so that is the sentence, and the
// verdict's own reason is what it is derived from.
//
// IT WRAPS RATHER THAN REPLACES, on [transportGaveUp]'s terms exactly: every
// layer that decides anything about a provider failure decides it from the
// error's TYPE, so the typed refusal stays reachable through Unwrap and only the
// words on the front change.
func endingWords(err error, verdict taxonomy.Verdict, service string) error {
	if err == nil {
		return nil
	}
	if said, ok := terminalFailureWords(err, service); ok {
		return &transportGaveUp{err: err, said: said}
	}
	said := strings.TrimSpace(transportWords(verdict))
	if said == "" {
		return err
	}
	return &transportGaveUp{err: err, said: said}
}

// terminalFailureWords preserves the three endings whose typed error carries
// a more useful action than the generic transport taxonomy can. The service is
// resolved by [Agent.failureServiceWord], from the same source set that built
// the client, so a payment refusal names the connection the person chose.
func terminalFailureWords(err error, service string) (string, bool) {
	if errors.Is(err, codexauth.ErrSignInExpired) {
		return codexauth.ErrSignInExpired.Error(), true
	}
	if paused, ok := provider.PlanPauseFrom(err); ok {
		return provider.PlanPauseSentence(paused.Reset, paused.OverflowDoor), true
	}
	refusal, ok := provider.RefusalFrom(err)
	if !ok || !refusal.AccountCannotPay() || strings.TrimSpace(service) == "" {
		return "", false
	}
	said := strings.TrimSpace(refusal.Message)
	if said == "" {
		said = strings.TrimSpace(refusal.Body)
	}
	return config.ConnectionOutcomeWord(service, modelsource.Outcome{
		Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: said,
	}), true
}

// failureServiceWord is the written service name behind the model that failed.
// It asks the source set rather than splitting the slug again, so custom names
// and an unqualified default model are read exactly as the client door read them.
func (a *Agent) failureServiceWord(model string) string {
	sources := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL)
	service, _ := sources.For(model)
	return strings.TrimSpace(service.Source.Written)
}

// transportGaveUp is that sentence WITH the failure still reachable under it, on
// [cutGaveUp]'s terms and for its reason: every layer that decides anything about
// a provider failure decides it by the error's TYPE (taxonomy_boundary.go's
// [providerCouldNotServe], task_run.go's [terminalProviderFailure]), and a
// decision made by matching substrings of a sentence is a decision that breaks
// the next time somebody rewords it.
type transportGaveUp struct {
	err  error
	said string
}

func (e *transportGaveUp) Error() string { return e.said }

func (e *transportGaveUp) Unwrap() error { return e.err }

// alsoTried names the models a step actually moved to. It replaces the advice
// to try another model, because "try another model" said to somebody who has
// just watched two of them fail is the surface not knowing what it did.
func alsoTried(hopped []string) string {
	return strings.Join(hopped, " and ") + " could not finish it either"
}

// timesWord counts the way a person counts. Small numbers have words.
//
// It runs to six because that is about as far as a ladder bounded by a turn's
// give-up gets on a real pool, and `was asked 4 times` in the middle of a
// sentence somebody reads while their turn is failing is the harness counting
// rather than speaking. PAST SIX IT IS A NUMERAL, which is the honest thing: a
// person who really was asked nine times should read nine, and a build that
// spelled every figure would be inventing English for a number nobody says.
//
// THE CEILING USED TO BE A CONSTANT'S. It was four rungs by default
// (`taxonomy.DefaultTransportAttempts`) plus a little room for a person who had
// raised it, and that constant is deleted: what a call may spend is a deadline
// now, so how many times it was asked is something only the call can say
// afterwards and never something this word can be sized from.
func timesWord(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	case 3:
		return "three times"
	case 4:
		return "four times"
	case 5:
		return "five times"
	case 6:
		return "six times"
	}
	return fmt.Sprintf("%d times", n)
}

// ── THE TURN'S OWN CLOCK, AND WHAT IT CHARGES ITSELF FOR ────────────────────
//
// turnNow is the clock the give-up is read against, and turnBackoff is the wait
// it pays between two attempts. Both are vars for the reason internal/provider's
// `dispatchNow` and `Client.wait` are: a scenario has to be able to state ninety
// seconds without spending ninety of them, and nothing in production replaces
// either.
//
// AND A WAIT THIS LOOP ASKED FOR IS SPENT WHETHER OR NOT THE CLOCK MOVED. Under
// a count that was harmless — the count was what ended the ladder. Under a
// deadline it is not: a seam that makes waiting free makes the deadline
// unreachable and the loop it bounds the unbounded one this wave exists to
// delete. So the loop charges itself for what it ASKED for (`owed` in
// [Agent.completeWithRetryReasoning]). In production it is a rounding error; in
// a scenario it is the whole of the fiction, honestly kept.
//
// AND THE NAMING LADDER'S WAIT IS A SEAM OF ITS OWN (title.go's `titleBackoff`),
// because it runs BESIDE a turn rather than inside one: two ladders sharing one
// stubbed wait would have each of them spending the other's deadline, where in
// real time they spend the same seconds once.
var (
	turnNow     = time.Now
	turnBackoff = backoffWait
)

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
	// refusedBy names the pre-action citizen that said no, and is empty on
	// every result that is not a veto. Nobody is shown it: it exists so the
	// debug record can say WHO refused a call, because a refusal recorded as a
	// failure sends somebody debugging the tool instead of the gate
	// (internal/trace's ToolEvent).
	refusedBy string
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
			Kind:   EventToolBegin,
			Tool:   call.Function.Name,
			Hint:   a.gloss(call),
			Args:   rendered[index],
			CallID: call.ID,
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

	// WHETHER EACH CALL CAME BACK A FAILURE IS RECORDED HERE AND NOWHERE ELSE
	// (admission.go). The flag the tool returned does not survive into the
	// transcript — a result there is a string — so work handed out later in this
	// turn could otherwise only guess at it from the words, which is exactly the
	// invention the admission context refuses to make.
	a.noteCallOutcomes(calls, results)

	// The end events carry Args, Output, and the provider's call ID. Carrying
	// them rather than making the surface remember the begin event costs nothing
	// — the rendering is the one done above — and buys an end event that is
	// self-contained, which is what a surface that renders a finished row from
	// one event needs. The ID remains necessary when one batch calls the same
	// tool more than once, because neither its name nor completion order identifies
	// the row.
	for index, call := range calls {
		if results[index].isError {
			a.sendBeltStep(ctx, hub, Event{
				Kind:   EventToolFailed,
				Tool:   call.Function.Name,
				Hint:   clip(firstLine(results[index].text), hintLimit),
				Args:   rendered[index],
				Output: capOutput(results[index].text),
				CallID: call.ID,
				// WHOSE FAILURE THIS WAS travels with it. Everything counting
				// steps out of band — the runner's no-progress ledger above all —
				// reads events and not results, so a fact kept only on the result
				// is a fact no counter can act on (withdrawn.go).
				HarnessMade: results[index].harness,
				// AN ATTEMPTED ACTION A DOOR REFUSED is the one harness-made
				// answer with a veto's author on it ([episode.preAction]); a
				// correction about the reply's form has none.
				Refused: results[index].harness && results[index].refusedBy != "",
			})
			continue
		}
		// A successful tool's hint is empty: the result belongs to the model,
		// and the person already read what the call was going to do. Output is
		// there for a person who asks to see it anyway.
		a.sendBeltStep(ctx, hub, Event{
			Kind:   EventToolEnd,
			Tool:   call.Function.Name,
			Args:   rendered[index],
			Output: capOutput(results[index].text),
			CallID: call.ID,
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
// AND IT IS WHERE THE DEBUG RECORD LEARNS ABOUT TOOLS. Everything a call can
// become — it ran, it failed, a door refused it before it ran, the hand was not
// on the belt at all — comes back through this one function, so the record is
// written around it rather than inside the four exits below (debugrecord.go).
func (a *Agent) executeTool(ctx context.Context, ep *episode, hub *eventHub, call ai.ToolCall, rendered string) toolResult {
	started := time.Now()
	result := a.dispatchTool(ctx, ep, hub, call, rendered)
	// Only a call that RAN counts: a door that refused it before it ran, or a
	// hand withdrawn off the belt, is the harness's own answer and rides on
	// [toolResult.harness] for this reason — every counter that judges the
	// model by its steps skips it. The error flag is the verdict.
	if !result.harness {
		telemetry.CountToolCall(!result.isError)
	}
	recordToolCall(ctx, call, result, started, time.Since(started))
	return result
}

// dispatchTool is the dispatch itself: find the hand, ask the doors, run it.
func (a *Agent) dispatchTool(ctx context.Context, ep *episode, hub *eventHub, call ai.ToolCall, rendered string) toolResult {
	for _, tool := range a.beltTools() {
		if tool.Name != call.Function.Name {
			continue
		}
		if invalid := invalidBashArguments(call.Function.Name, json.RawMessage(call.Function.Arguments)); invalid != "" {
			return a.finishToolResult(ep, call, toolResult{text: invalid, isError: true})
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
		// A read for bytes this conversation already has is answered from above,
		// not from the disk (heldreads.go) — the gate above already judged it.
		if pointer, held := a.heldClaim(call.Function.Name, args); held {
			return a.finishToolResult(ep, call, toolResult{text: pointer})
		}
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
		took := time.Since(started)
		// AND THE RECORD KEEPS THE FIGURE where the event is sent, with the same
		// id: a page opened after the batch — or a room rebuilt from the journal
		// after a landing — has no stream to watch, and without this line the
		// rows came back with Args and Output but no duration (sessionfile.go's
		// [sessionFile.appendTook]).
		a.file.appendTook(call.ID, took)
		if hub != nil {
			hub.send(Event{
				Kind:   EventToolFinished,
				Tool:   call.Function.Name,
				Args:   rendered,
				CallID: call.ID,
				Took:   took,
			})
		}
		if err != nil {
			// Harness-level failure: the model sees the Go error as the tool
			// result, the same shape a thrown error takes on the wire.
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
		result := a.finishToolResult(ep, call, toolResult{text: text, isError: isError})
		// The held-range ledger records on the FINISHED text — exactly the bytes
		// the transcript will carry — and only on a result that ran (heldreads.go).
		a.heldNote(call.Function.Name, args, result.text, isError)
		return result
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
	body := result.text
	result = a.withJobState(ep, ep.noteToolOutcome(call, result))
	result.text = footersInsideTheCap(body, result.text)
	return result
}

// footersInsideTheCap keeps what this chokepoint appends INSIDE the bound the
// result already respected, rather than on top of it.
//
// EVERY CAP IN THIS PROGRAM IS A PROMISE ABOUT WHAT THE MODEL WILL BE HANDED,
// and both of the appends above are made after the tool has already cut its
// output to fit [bare.MaxResultBytes]. A 50 KB read plus a job footer plus a
// fix line was 50 KB and change — small on one call and not small at all on the
// fortieth, which is a window the person never agreed to spend. So the BODY
// gives up the room, exactly as task_audit.go's boundedResult does it: the
// footers are what the model most needs to see, and the body is the half it can
// go and read the rest of.
//
// A result that was ALREADY over the cap on its own is left alone. The footer is
// not what busted it, and a chokepoint that quietly cut every oversized result
// would be a second cap with no notice on it — a read_document's own bound, or a
// task's report, would be cut here by a rule written for a shell command.
func footersInsideTheCap(body, grown string) string {
	if len(grown) <= bare.MaxResultBytes {
		return grown
	}
	// The appends are suffixes of the body with its trailing newlines trimmed
	// (jobfooter.go's withJobState, fixrecall.go's fixAnnotate), so what is not
	// the body is the footers, exactly.
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" || !strings.HasPrefix(grown, trimmed) || len(trimmed) > bare.MaxResultBytes {
		return grown
	}
	footers := grown[len(trimmed):]
	room := bare.MaxResultBytes - len(footers) - capMarkerRoom
	if room <= 0 {
		return grown
	}
	return capBytes(trimmed, room) + footers
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
	// A manager's look at one member, and the member a stop is aimed at.
	"team_read": "handle",
	"team_stop": "handle",
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
	// A line into a team is who it goes to and what it says. A start is not
	// here: it reads as its own sentence ([teamStartGloss]).
	"team_send": {"to", "text"},
	"team_post": {"to", "text"},
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
	if name == teamStartToolName {
		if said := teamStartGloss(call.Function.Arguments); said != "" {
			return said
		}
	}
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
// capMarkerRoom is what [capBytes] needs for the "… (N more bytes)" it writes in
// place of what it cut, and it is the slack a caller leaves when it is fitting a
// body and a footer into one budget together. It is generous on purpose: the
// count inside it is an unbounded integer and the marker must never be the thing
// that pushes the pair back over the cap it was measured against.
const capMarkerRoom = 32

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
// answered, what for, how the lane behind it behaved, and the things that
// differ between the doors — whether the machine's ledger is owed a row (a fold
// is not a call), whether this was a step of the conversation, and whether its
// money arrived too late to belong to the turn now running.
type bankedCall struct {
	used   Usage
	model  string
	role   string
	lane   laneFacts
	ledger bool
	// late says this money reached the books after the turn that spent it had
	// already ended. It still belongs in the session and the machine ledger, but
	// the RUNNING turn's share must not move for money spent by an earlier one.
	late bool
	// reconciled marks the ledger row as figures supplied by a provider receipt
	// after the stream ended without its usage block.
	reconciled bool
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
	if !call.late {
		a.turnSpend.Input += call.used.Input
		a.turnSpend.Output += call.used.Output
		a.turnSpend.CacheRead += call.used.CacheRead
		a.turnSpend.CacheWrite += call.used.CacheWrite
		a.turnSpend.CostUSD += call.used.CostUSD
		a.turnSpend.Calls += call.used.Calls
	}
	if call.turn {
		a.usage.Turns++
	}
	if call.context > 0 {
		a.contextTokens = call.context
	}
	a.mu.Unlock()
	if call.ledger {
		a.recordUsageLine(call)
	}
}

// reconciled receives the one late answer for a call whose stream ended before
// its usage block. A missing receipt moves only the visible gap counter; a
// found one enters through [Agent.bank], the same door as every ordinary call,
// so the session meter and the machine ledger cannot diverge.
func (a *Agent) reconciled(receipt provider.Reconciled) {
	if !receipt.Found || receipt.Billed.Empty() {
		a.recordUnbilledReceipt(receipt.Model)
		return
	}
	used := Usage{
		Input:   receipt.PromptTokens,
		Output:  receipt.CompletionTokens,
		CostUSD: receipt.Cost,
		Calls:   1,
	}
	lane := laneFacts{Hedged: receipt.Hedged}
	if receipt.Hedged {
		// HedgeWasteUSD is a receipt column and is never added to a money total,
		// so repeating this call's own USD there identifies waste without double
		// counting it. Only rescue arms carry the context marker; an original arm
		// that loses has none and reconciles as an ordinary cut.
		lane.Waste = receipt.Cost
	}
	a.bank(bankedCall{
		used: used, model: receipt.Model, lane: lane, ledger: true,
		late: true, reconciled: true,
	})
	// AND THE REQUEST LEAVES ITS OWN LINE, for [journalCall]'s law said another
	// way: EVERY request this session makes writes one, and a request whose
	// usage block never arrived wrote none — so the journal's call lines summed
	// to barely half the measured session's bill, and exactly the expensive
	// half was missing. The row is evidence and never spend; the money moved
	// through the door above and nowhere else.
	a.file.appendCall(armCall(receipt))
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
	// The row's endpoint is the answer's own naming or, for a service that
	// declared the one machine it is, that declared name ([Agent.attributedEndpoint]).
	// `lane` was read from the answer alone, so what the lane layer learns and
	// draws is untouched by the declaration.
	served = a.attributedEndpoint(model, served)
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
	// AND A TURN THAT ANSWERS FOR A CREW ROLE BANKS THAT ROLE'S WORD, so the
	// ledger row says what the turn was — `auditor` — rather than nothing at all.
	// It is the same fact the model-call log's tag carries (loop.go), read from
	// the one config row that states it; an ordinary conversation or worker
	// leaves it empty and the row names no role, which is what it always did.
	a.bank(bankedCall{used: call, model: answered, lane: lane, ledger: true, turn: true, context: context, role: string(a.config.crewRole)})

	// AND THE TURN KEEPS THE SAME SHARE PER ANSWERING MODEL, under the very
	// name the row above banks, so a turn that hopped seals one usage line per
	// model instead of one sum under the last name standing
	// ([sessionFile.appendUsage] is the reader, and the only one).
	turn.addShare(answered, call)

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
	// AND A STOP OF OURS SAYS SO IN A FIELD AND NOT ONLY IN ITS SENTENCE. The
	// message already reads `turn ended: taken over`, which is enough for a
	// person opening the file and not enough for the reader that has to decide
	// whether the question above this row is still owed an answer: that reader
	// must not be parsing prose (stopcause.go, resume.go).
	if door, ours := StoppedBy(err); ours {
		row.Door = string(door)
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
// `context fill` row, the `CODEAF_CONTEXT_FILL_PCT` environment pin behind it,
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
//  2. THE FOLD. If the transcript is still above [compactTarget] — the headroom
//     line, not the trigger the pass fired on — the oldest ASSISTANT
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
	// THE FOLD IS ASKED FOR AGAINST THE TARGET, NOT THE TRIGGER. The pass fires
	// at the threshold, and the stub pass alone routinely lands the estimate just
	// under it — below the trigger, above the target, with no headroom at all. The
	// gate was the threshold, so the fold was skipped, the next step's few
	// thousand tokens crossed the line again, and the session was back in exactly
	// the once-per-step thrash [compactTarget] exists to end. What buys the
	// headroom is the same figure the fold already stops at.
	if a.estimateTokensLocked() > a.compactTargetTokens() {
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
		// AND IT SAYS WHICH OF THE TWO THINGS THIS EVENT MEANS. The kind alone
		// cannot: it is sent on both paths, so a reader that rebased on it
		// rebased on a replacement that did not happen ([Event.Unchanged]).
		hub.send(Event{Kind: EventCompacted, Hint: "nothing to compact", Unchanged: true})
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
//	[folded 31 messages · grep or read /home/x/.codeaf/v3/sessions/abc.jsonl, lines 12..40]
//	[folded 31 messages · grep or read /home/x/.codeaf/v3/sessions/abc.jsonl]
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
	estimate := EstimateTokens(total)
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

// addEmptyReflexUsage is the paid-call door for a reflex request that returned
// no answer. The tokens and price stay in the ordinary totals; the extra count
// says what that spend failed to buy.
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

// addDetachedUsageAs is [Agent.addAuxiliaryUsageAs] for an errand that is NOT
// ON ANY TURN'S CLOCK — one started beside a turn, on the session's own
// lifetime, that may land while a different turn is running or while none is
// (title.go).
//
// The money is the session's and the machine's exactly as any errand's is; what
// it must not move is a.turnSpend, which is the figure an abandoned turn is
// journaled with. A name bought by the first turn and paid for during the
// third would otherwise appear as the third turn's cost, and a title that
// landed during a turn that was then abandoned would be journaled as money that
// turn spent. That is the `late` bit at [bankedCall], said the other way round:
// late means "the turn that spent this has ended", and detached means "no turn
// ever owned it".
func (a *Agent) addDetachedUsageAs(response *ai.Response, model string, calls int, role string) {
	a.addUsageAs(response, model, calls, role, true, false, detachedFromTurn)
}

// addUsageAs is the body both auxiliary doors share, with one bit of difference:
// whether this tally is a CALL THIS AGENT MADE — and therefore a line in the
// machine's ledger — or a fold of work that already wrote its own
// ([Agent.addFoldedUsage]).
//
// detached, when it is passed, keeps the tally off the RUNNING turn's share —
// see [Agent.addDetachedUsageAs]. It is variadic so that the thirty callers that
// are on a turn's clock say nothing and mean it.
func (a *Agent) addUsageAs(response *ai.Response, model string, calls int, role string, ledger, emptyReflex bool, detached ...bool) {
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
	a.bank(bankedCall{used: aux, model: model, role: role, ledger: ledger, late: len(detached) > 0 && detached[0]})
	// The write is outside the lock for the reason [Agent.sealTurn]'s is: the
	// file has its own, and holding the agent's across a disk write would put
	// every reader of the session's totals behind it.
	a.file.appendUsage(aux, model, true, role)
}
