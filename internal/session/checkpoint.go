package session

// THE CHECKPOINT: what a turn costs, priced against what handing it over costs.
//
// Two mechanisms already stand between a chat message and a grind, and both of
// them decide BEFORE there is any evidence to decide on. prompts/system.md
// teaches the model to hand work over the moment it can name the scale in front
// of it, and a model deep in tool momentum does not stop to re-consult a verb it
// rarely reaches for. route_judge.go asks a second model about the REQUEST,
// racing the turn rather than standing in front of it, and its cheap screen
// declines the very message this exists for: four independent pieces of work in
// one paragraph, answered twice in measurement by ninety-odd rounds of inline
// tool calls.
//
// What neither has is a reading taken WHILE THE WORK IS HAPPENING, when the cost
// is a fact rather than a guess. That is what this file is, and it is now the
// ONE door out of a running turn ([Agent.handOverRunningTurn]). The race used to
// be the other one; it was demoted to triage in the same wave that measured this
// question, and what its both-yes does now is TIGHTEN THIS METER so the first
// mark lands at the next step boundary instead of after the full price
// ([checkpointMeter.tighten]).
//
// ── THE FRAME IS SKI RENTAL ──
//
// Working in the conversation is RENTING: it costs a little per round and it
// never stops costing. Handing the work to the graph is BUYING: one fixed price
// — a worktree opened and closed, a brief written, somebody independent reading
// the result — and after it the work runs supervised, with a budget, a repair
// loop and a rail the person can stop it from.
//
// The competitive answer to rent-versus-buy is to rent until you have spent what
// buying costs, and then buy. That is exactly the shape here, with one softening
// and one hardening:
//
//   - THE TRIGGER IS DETERMINISTIC COST AND NEVER CONTENT. Nothing decides to
//     LOOK on account of what the turn is about. A counter of finished tool
//     rounds is the whole of the input, so there is no phrasing that defeats it
//     and no kind of work it is tuned for.
//   - THE JUDGEMENT IS READ FROM THE WORK, up to a point. At each mark the
//     harness has somebody sketch what is left, and a turn that is genuinely one
//     long job carries on with nothing said and nothing spent but that one call.
//   - AND PAST THE LAST MARK THE HARNESS STOPS READING. Two sketches saying "one
//     job" are a judgement; a third would be momentum. At the ceiling the turn
//     ends and the REMAINING work moves onto the one road, where it is watched —
//     and remaining is the whole of what is still asked there
//     ([checkpointNothingLeft]): a turn that answers the handoff by saying it is
//     finished is left to finish, which is not the harness asking again but the
//     harness having nothing to move.
//
// ── WHY THE MARKS ARE GEOMETRIC AND NOT ONE LINE ──
//
// A single threshold is a single chance to be wrong. Doubling the interval each
// time means a turn that is honestly one long job is looked at a bounded number
// of times — three, over four times the handoff price — while a turn that is four
// jobs in a trench coat is read early, when the findings are still worth handing
// over. Each look costs a model call, so looking gets rarer as the evidence that
// the answer is "carry on" accumulates. [checkpointRatio] is the whole of that
// policy.
//
// ── WHY THE MARK IS READ BY SOMEBODY ELSE ──
//
// This used to be a question INJECTED INTO THE RUNNING TURN. A line rode the
// ambient note lane at each mark, said the answer had now cost more than handing
// it over would have, and asked the model to say in one line whether what was
// left was one job or several independent parts. It was measured off-policy over
// 240 completions against real recorded transcripts at the round-ten mark
// (bench/oneroad/replay/RESULTS.md), and the finding that killed it is not that
// the answers were wrong. It is that THERE OFTEN WAS NO ANSWER: the running chat
// model emitted a tool call instead of answering between 17% and 53% of the time,
// depending on the phrasing. A mechanism whose decision is left to a model in
// tool momentum is a mechanism that is off in exactly the momentum it exists to
// interrupt.
//
// The mastermind tier answered the same questions on the same transcripts with
// 0% to 7% unanswered. So the mark is READ BY A SIDECAR ([Agent.readMark]): one
// call, on the tier that thinks, over the transcript the turn has built — and the
// running model never sees a word of it. Nothing is injected into the turn at any
// mark, which is why there is no note const in this file any more.
//
// ── AND WHAT IT IS ASKED FOR IS A SHAPE AND NOT A DECISION ──
//
// Four phrasings were measured against both models. The one that asks the reader
// to DECIDE — "one job, or several independent parts" — scored 96% on the traps
// and 67% on the one genuinely wide case. The one that asks it to SKETCH what
// remains as parts and arrows ([checkpointSketchAsk]) scored 58% on the traps and
// 100% on the wide case, with nothing ambiguous and nothing unanswered at all.
//
// THE ASYMMETRY PICKS THE SKETCH. A wrong SPLIT costs one downstream check: the
// task is armed, the worker MAY reach for divide_work, and both the evidence gate
// and the division reviewer still stand in front of any actual division
// (task_divide.go) — the reviewer now adjudicates even a floor refusal on
// judge-armed work, so an over-suggestion is refused for free. A wrong CONTINUE
// costs THE WHOLE ROAD, silently, on precisely the turn this was built for. So
// the reading that never misses width and over-suggests on single jobs is the
// right shape of wrong.
//
// ── AND THE HARNESS DOES THE DECIDING ──
//
// What comes back is PARSED AND NEVER INTERPRETED: the first line, split on a
// top-level ' | ', and two or more parts is a SPLIT ([parseCheckpointSketch]).
// Everything else is a CONTINUE — one part, a fork nested under a first step,
// prose, an empty answer, a call that never came back, an install with no
// mastermind to ask. THAT FAIL-OPEN IS SAFE BECAUSE THE CEILING IS THE RECOVERY
// BOUND: a turn whose sidecar can never be reached is a turn the last mark moves
// anyway, which is what this file did before any of it was read.
//
// ── WHAT THIS FILE DELIBERATELY DOES NOT DO ──
//
// It opens no new door into the graph. Both marks and the ceiling admit their
// task through [Agent.launchRouteTask], which is the road route_judge.go already
// takes for work nobody groomed: straight to [TaskGraph.admit], no card, told
// after.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The mark reader is registered here, beside the call it belongs to, exactly as
// the confirm, the auditor and the shaper are (internal/roles states the open
// registry law). Its tier is the mastermind's for the measured reason the role's
// own comment carries: the cheap reader and the running model both answer this
// question with a tool call too often to be relied on.
//
// AND IT STAYS THERE, ON A SECOND MEASUREMENT THAT IS WORSE THAN THE FIRST. Read
// off-policy over the same digests, the flash tier answered `(done)` on
// HALF-FINISHED batches 15 times out of 18 (bench/oneroad/replay/RESULTS-2.md) —
// which is not a cheaper reading of the work, it is the one answer that drops the
// handover a person is owed ([checkpointSketch.saysDone] is what a `(done)`
// corroborates). A tier that is wrong in that direction cannot be bought with any
// saving.
func init() { roles.Register(roles.RoleMarkReader, roles.TierMastermind) }

const (
	// checkpointPrice is what handing this turn over costs, in the same unit the
	// turn is measured in: FINISHED TOOL ROUNDS.
	//
	// IT IS COUNTED OFF THE CODE RATHER THAN ESTIMATED. A task's isolation is ten
	// git commands wide on the happy path, and every one of them is fixed work
	// that buys none of the person's goal — three to open the copy the work
	// happens in (the toplevel rev-parse, the HEAD verify, the `worktree add`)
	// and seven to bring it home (`add -A`, the droppings reset, the staged diff,
	// the commit, the merge, the `worktree remove`, the branch delete). They are
	// all in task_run.go's [prepareTaskTree] and [taskTree.comeHome].
	//
	// Ten is therefore the floor and not the whole bill: on top of it sit the
	// call that writes the brief (task_shape.go) and the independent reader that
	// decides whether the work is real (task_audit.go), neither of which the
	// conversation pays for when it simply does the job itself. Pricing the
	// handoff at the FLOOR is the honest direction to be wrong in — it makes the
	// harness slow to interrupt a turn that is working, and the first mark lands
	// where a turn has demonstrably stopped being "a few tool calls and an
	// answer", which is where prompts/system.md and route_judge.go both draw the
	// line between a conversation and work.
	checkpointPrice = 10

	// checkpointRatio is how much dearer each mark is than the one before it.
	//
	// Two, because the evidence moves. A mark whose sketch said "this is one job"
	// has said something, and the harness must charge more before doubting it
	// again — otherwise the interval measures the harness's impatience rather than
	// the turn's cost. Doubling also bounds the whole mechanism at a glance: three
	// marks span four times the price, and a turn can never be read a fourth time
	// however long it runs.
	checkpointRatio = 2

	// checkpointMarks is how many marks a turn has, and the LAST of them is the
	// ceiling rather than a fourth reading ([Agent.checkpointRound]).
	//
	// Three, because two readings are the most that are worth paying for. The loop
	// detector reached the same number from the other side — past two nudges the
	// notes have stopped working and a third is the harness talking to itself
	// (looped.go's loopNudgeCeiling) — and the answer here is the same answer:
	// stop asking and do something. What this does instead of asking again is move
	// the work somewhere it is watched.
	checkpointMarks = 3

	// checkpointBriefTokens bounds the handoff brief. The shaper writes the same
	// document under taskShapeTokens for a request nobody has worked on yet; this
	// one is written by a model with a turn's worth of findings in front of it and
	// is asked to put them down, so it gets more room — and it is still clipped to
	// taskShapeBriefLimit, which is the bound EVERY brief on this road is held to.
	checkpointBriefTokens = 2000

	// checkpointBriefWords is how many WORDS a dowry must carry before the
	// harness will hand it to somebody as an instruction.
	//
	// IT IS A TEST FOR PROSE AND NOT FOR CONTENT, and it exists because "no belt"
	// is not the same fact as "no tool grammar". The handoff ask goes out with no
	// [ai.WithTools] on it, but it goes out on a transcript saturated with tool
	// calls and to a model whose chat template still holds its tool-call special
	// tokens — and a measured turn on deepseek answered it with its raw sentinel
	// and nothing else, which then became the goal a worker was started on. There
	// is no phrasing of the ask that can guarantee otherwise, so the harness reads
	// what came back instead: an instruction somebody can work from is words with
	// spaces between them, and machine markup is one dense token. Four sits under
	// any real brief — the ask alone demands what is left, what is known, what is
	// ruled out and how anybody could tell it is done — and above every sentinel
	// this has been shown, each of which is a single unspaced token.
	checkpointBriefWords = 4

	// checkpointSketchTokens is the sidecar's whole budget. What it is asked for
	// is one line of shape and one sentence naming the letters, and a reader that
	// runs out of room halfway through the shape writes a line the parser reads as
	// ONE PART — a silent CONTINUE from the one call that was supposed to notice
	// width. Three hundred is both halves at any length a sketch of a turn's
	// remaining work honestly takes, and a fraction of the tool round it stands
	// between.
	checkpointSketchTokens = 300

	// checkpointSketchTemp is zero for the reason the route judge's is: this is a
	// reading of evidence that the harness then parses deterministically, and
	// sampling variety in it would be the same transcript answering SPLIT on one
	// run and CONTINUE on the next.
	checkpointSketchTemp = 0

	// checkpointSketchWindow is how long the mark's read gets, and unlike the
	// race's two windows (route_judge.go) SOMEBODY IS WAITING ON THIS ONE. It
	// stands at a step boundary of a turn that is running: the next round of tools
	// does not start until it answers or the clock runs out. So it is a person's
	// patience rather than a generous bound.
	//
	// Thirty seconds is what a mastermind reading a whole transcript needs, and
	// what it can cost a turn is bounded twice over — at most three of them, and
	// only on a turn that has already spent ten rounds of tool calls, which is
	// minutes. A read that misses it is a CONTINUE, exactly as an unreachable one
	// is, and the ceiling still stands behind both.
	checkpointSketchWindow = 30 * time.Second

	// checkpointSketchParts is how many top-level parts in the shape make a SPLIT.
	// Two: one part is one job, and the only question being asked is whether what
	// is left can be held by more than one pair of hands.
	checkpointSketchParts = 2

	// checkpointSketchBytes bounds each half of the sketch on its way into a
	// worker's brief. A shape is a line and a legend is a sentence; anything past
	// this is a reader that answered a different question, and the brief under it
	// is the document that matters.
	checkpointSketchBytes = 600

	// checkpointDigestTokens is the WHOLE of what a mark's reader is shown, and it
	// is the number that decides whether this mechanism is worth running at all.
	//
	// THE READER USED TO BE SENT THE TRANSCRIPT. It was the honest first version —
	// the reader sees what the running model sees — and it was measured on a crew
	// run over four pieces of work: three marks fired, the sidecar read 57k, 65k
	// and 91k input tokens on the mastermind tier, and the three reads cost $0.62
	// between them against $0.123 for the whole of the sixty-two calls that did
	// the actual work. AT THE MASTERMIND'S PRICE A RAW READ COSTS MORE THAN THE
	// WORK IT IS JUDGING, which turns a meter built to save a person money into
	// the largest line on their bill — and all three reads answered "carry on".
	//
	// So the reader is shown an ACCOUNT of the work instead ([checkpointDigest]):
	// the ask, one line per tool call, what has been written, and the last thing
	// said. Five thousand tokens is more room than any of those three turns
	// actually needed and a fifteenth of what the smallest of them was charged
	// for, which puts a read back at cents — and the thing being asked for is a
	// shape, which is a judgement about the SPREAD of the work rather than about
	// its contents. Nothing in a tool result changes the shape of what is left;
	// what changes it is what has been asked, what has been done, and what is
	// still open.
	checkpointDigestTokens = 5000

	// checkpointDigestBytes is that bound in the unit the builder can actually
	// count. [bytesPerToken] is this package's one estimator and is used here
	// rather than a second number of this file's own, for the same
	// one-source-of-truth reason [checkpointSketchBytes] is not spelled twice.
	checkpointDigestBytes = checkpointDigestTokens * bytesPerToken

	// checkpointLedgerBytes is how much of ONE tool call's argument the ledger
	// carries: the path, the command, the pattern — enough to say WHAT was
	// touched, and nowhere near enough to say what came back.
	//
	// Eighty is a long path and a real command, and it is the bound that keeps a
	// ledger of ninety calls inside the digest with room to spare. A call whose
	// argument is a whole file's contents is exactly the call this is protecting
	// the reader from: it is answered with the path, because the argument that
	// names the work is never the argument that carries the bytes.
	checkpointLedgerBytes = 80

	// checkpointSaidBytes is how much of the turn's last words the reader is
	// shown. It is the one part of the digest that is the model's own account of
	// where it has got to, and a paragraph of it is the whole of what a reader
	// needs to place the ledger above it — the same bound a sketch's own halves
	// are held to, for the same reason.
	checkpointSaidBytes = checkpointSketchBytes
)

// The digest's headings. They are SHOUTED and they are few, because what they
// have to do is let a reader tell four kinds of evidence apart at a glance in
// one message that also carries the ask.
//
// THEY NAME NOTHING ABOUT THE KIND OF WORK, which is the law [checkpointSketchAsk]
// is held to and which applies here for the same reason: a heading saying "files"
// would read as an instruction about programming to a reader watching a
// literature review. What has been written down is what has been written down,
// whether it is a module or a chapter.
//
// AND A HEADING WITH NOTHING UNDER IT IS NOT WRITTEN AT ALL — the emptiness law,
// applied to a document a model reads: an empty section is an invitation to
// answer about the emptiness.
const (
	checkpointDigestAsked   = "WHAT WAS ASKED"
	checkpointDigestDone    = "WHAT HAS BEEN DONE SO FAR, ONE LINE PER STEP"
	checkpointDigestWritten = "WHAT HAS BEEN WRITTEN OR CHANGED"
	checkpointDigestSaid    = "THE LAST THING SAID"
)

// checkpointSketchAsk is what the sidecar is asked at every mark, and it is
// PINNED WORD FOR WORD because the wording is the measurement.
//
// It is variant C of four that were scored off-policy over 240 completions
// (bench/oneroad/replay/RESULTS.md). What makes it the winner is not that it
// reasons better — it is that it asks for a DRAWING rather than a decision, and a
// model asked to draw one line answers with one line. Across both models it left
// nothing ambiguous and nothing unanswered, where every phrasing that asked for a
// judgement was ignored outright on a quarter to a half of the turns.
//
// IT NAMES NOTHING ABOUT THE KIND OF WORK, which is the law prompts/system.md and
// the ceiling's line are both held to. aforge is a general harness: a research
// sweep, a writing project and a mechanical change are one shape of problem to
// this question, and a sentence about files would read as an instruction about
// programming to a reader watching a literature review.
//
// AND IT CARRIES NO THRESHOLD AND NO PRICE. The reader is not told what the turn
// has cost, because a number invites it to answer about the number. It is shown
// the work and asked what is left of it.
//
// THE LEGEND IS ASKED FOR BECAUSE THE SHAPE ALONE IS USELESS TO A WORKER. `A | B
// | C` is what the harness parses; "A is the validation workflow, B is …" is what
// rides into the brief so that whoever takes the work has the parts already named
// ([checkpointSketch.head]).
//
// AND IT NAMES THE TWO THINGS THE DIGEST PUTS IN FRONT OF IT. The reader used to
// be handed the transcript, where "what remains" could only mean "of whatever
// this conversation is about"; it is now handed an account whose first section is
// the person's own ask ([checkpointDigest]), so the question is anchored to it —
// what remains OF THE ASK, read against what has been done. THE GRAMMAR HALF IS
// UNTOUCHED, from "independent parts" to the end: that is variant C word for
// word, it is what both models were scored on, and it is what the parser is
// pinned to.
const checkpointSketchAsk = "[checkpoint] Above is what was asked and what has been done towards it. " +
	"In one line, sketch what remains of the ask as parts and arrows: " +
	"independent parts separated by ' | ', ordered steps joined by ' > '. " +
	"Example shapes: 'A | B | C' or 'A > B > C' or 'A > (B | C)'. " +
	"Nothing else on that line. Then one sentence saying what each letter is. " +
	"If nothing remains, that line is '(done)'."

// checkpointSplitNote is the ONE line a person reads when a mark's sketch says
// the work in front of it has parts.
//
// It is the third line in this register and the last of the family: the
// mid-answer handoff's ([taskEscalationNote]), the ceiling's
// ([checkpointCeilingNote]) and this one. All three are an observation, a middle
// dot and a promise, all three are lowercase with no full stop, and nothing in
// any of them is machinery.
//
// WHAT DIFFERS IS THE OBSERVATION, because the three moments have honestly seen
// different things. The ceiling has watched an answer outrun its own price and
// says so. This one has had somebody read the work and draw what is left of it,
// and the true thing it has to say is that the drawing came back with more than
// one part in it — so it says that, and nothing about how long the turn has been
// running, which at the first mark may be no time at all.
//
// AND THE PROMISE IS THE ONE THING THIS PATH CAN PROMISE THAT THE OTHERS CANNOT.
// The ceiling says the work "can split", because arming is all it did. Here the
// parts are already named and already at the head of the brief, so the honest
// promise is about the shape of the hands rather than about a possibility — and
// it still stops short of saying it WILL split, because the evidence gate and the
// reviewer stand in front of every actual division (task_divide.go).
const checkpointSplitNote = "this has parts · handing it to a task that can take them side by side"

// checkpointCeilingNote is the ONE line a person reads when the harness stops
// reading and moves the work itself.
//
// It is [taskEscalationNote]'s register and it is that line's sibling — an
// observation, a middle dot, a promise (internal/tui3's taskWideNote) — because
// a person meets both in the same slot for the same reason: something happened
// to their turn that they did not ask for, and they are owed the reason in one
// dim line rather than an announcement. Nothing about machinery, no capital
// letter, no full stop.
//
// IT PROMISES THE TWO THINGS THAT ARE ACTUALLY TRUE AT THE MOMENT IT IS WRITTEN.
// The work is going somewhere it is watched — the rail, the budget, the checker,
// the stop key — and it is going there armed to split, which is what
// [routeVerdict.Wide] does to the task this starts. Whether it splits is the
// worker's own discovery and the roster says so when it happens, so the line says
// "can" and not "will".
const checkpointCeilingNote = "this is running long · moving it to a task that is watched and can split"

// checkpointHandoffAsk is what the model is asked for on the way out, and it is
// the LAST thing this turn does with it.
//
// IT IS NOT A READING OF ANY KIND. The decision is made by the time this is sent
// — by a mark's sketch or by the ceiling — and what is being asked for is the
// dowry: the turn's findings, written down, because the model that has just spent
// the whole turn is the only reader in the building that holds them.
//
// IT IS SENT WITH NO BELT (see [Agent.checkpointBrief]), which is what makes it
// safe to ask a model that has spent the turn grinding: with no tool to reach
// for, the only legal answer is the document.
//
// AND IT ASKS WHAT REMAINS BEFORE IT ASKS FOR THE DOCUMENT, which is the one
// question no clock into this door can answer for itself. The ceiling reads a
// counter and a mark's sketch reads a transcript that is already a step old;
// neither of them can see that the turn finished the work thirty seconds ago. A
// measured turn wrote all eight files it was asked for inline, the race's confirm
// landed at the tail of it, and the conversion started a task on the leftovers of
// a finished answer — junk work, duplicating a turn nobody needed to duplicate.
// So the model that holds the findings is asked the question it is the only
// reader able to answer, and [checkpointNothingLeft] is the answer that stops the
// handover dead.
//
// THE FIRST LINE IS THE DECLARATION AND NOT A NAME. It used to ask for a short
// name for the work there, and the name is not taken from here any more
// ([Agent.launchRouteTask] takes the converted task's title from the person's own
// words) — so the first line is free to carry the one thing the harness has to
// read out of this answer before it reads anything else.
const checkpointHandoffAsk = "[handing over] This is being handed to somebody who will finish it, and they cannot " +
	"see any of this — not what you read, not what you tried, not what you found. Say first WHAT REMAINS. " +
	"If nothing remains — everything that was asked for is already done here, and all that is left is saying so — " +
	"answer with the single line " + checkpointNothingLeft + " and write nothing else at all. " +
	"Otherwise write their instruction and nothing else: what is left to do, what you already know that they " +
	"would otherwise have to find out again, what you have ruled out, and how anybody could tell when it is done. " +
	"Do not greet them and do not describe this conversation."

// checkpointNothingLeft is the whole of the remains contract: the ONE line the
// continuation writes when the turn has already done what was asked.
//
// IT IS A TOKEN AND NOT A SENTENCE THE HARNESS SNIFFS FOR. The alternative —
// reading a brief for phrases that sound finished — is a keyword rule with a
// model's bill attached, and it would fire on the brief that says "nothing is
// left to check once the parser is rewritten". A token the ask NAMES, matched on
// the first line alone, is a thing the model chose to say rather than a thing the
// harness thought it heard.
//
// IT IS SHOUTED because the surrounding ask is prose and a token that reads as
// prose is one a model folds into a sentence. Nothing person-facing carries it:
// a turn that answers it is a turn that simply carries on to its own end.
const checkpointNothingLeft = "NOTHING LEFT TO DO"

// ── the meter ───────────────────────────────────────────────────────────────

// checkpointMeter is one turn's running cost and how much of the ladder it has
// climbed. It belongs to the turn and is built by it, for the reason the loop
// detector's window belongs to the turn (hooks.go): what this counts is a fact
// about ONE answer, and a meter that remembered yesterday's rounds would move
// work out of a conversation on the strength of a conversation that already
// ended.
type checkpointMeter struct {
	// rounds is how many tool batches this turn has FINISHED.
	rounds int
	// marks is how many of the ladder's rungs have already fired.
	marks int
	// firstAt is where the FIRST rung stands when the pre-turn race has already
	// said this message reads like work, and zero on every ordinary turn — see
	// [checkpointMeter.tighten].
	firstAt int
	// raced is what that race's both-yes actually wrote: the breadth two readers
	// agreed on and the done-condition the screen composed ([routeVerdict]).
	//
	// IT IS KEPT RATHER THAN SPENT because the race no longer starts anything on
	// its own. Whatever task this turn eventually hands over to — at a mark or at
	// the ceiling — is the task that verdict was written about, and dropping it
	// would mean a converted turn losing an acceptance a judge wrote and a
	// breadth two models agreed on for no saving whatever. The goal it carries is
	// deliberately NOT kept: the dowry replaces it, because a goal written before
	// the turn began is the one thing on the table that knows least.
	raced routeVerdict
}

// checkpointMarkAt is the round the nth ORDINARY mark stands on: the price,
// doubled once per mark already passed. It is computed from the two constants
// rather than written out as a list, so the ladder cannot disagree with the
// policy it is supposed to be.
func checkpointMarkAt(n int) int {
	at := checkpointPrice
	for step := 1; step < n; step++ {
		at *= checkpointRatio
	}
	return at
}

// markAt is the ladder AS THIS TURN ACTUALLY CLIMBS IT: [checkpointMarkAt],
// except for a first rung the race has pulled down.
func (m *checkpointMeter) markAt(n int) int {
	if n == 1 && m.firstAt > 0 {
		return m.firstAt
	}
	return checkpointMarkAt(n)
}

// tighten is what a raced both-yes DOES now, and it is the whole of the race's
// remaining power over a turn.
//
// THE RACE USED TO CONVERT THE TURN OUTRIGHT and the benchmark took that away
// from it: on the ten measured cells the raced screen converted both of the
// small-work traps, which is a message taken out of a conversation that was about
// to answer it. What the race reads is a REQUEST nobody has worked on yet, and no
// amount of confirming makes that evidence about the work. What it is genuinely
// good for is TRIAGE — noticing early that this one is worth looking at — so that
// is what it now does: the first mark moves down to the very boundary the verdict
// landed on, and the reading that decides anything is still the sidecar's, over
// the work itself.
//
// THE LATER RUNGS DO NOT MOVE. Only the first is pulled down; the second and the
// ceiling stand at the ordinary doubling marks, because what the race noticed is
// a reason to LOOK SOONER and not a reason to look oftener.
//
// AND THE VERDICT IS KEPT EVEN WHEN THE RUNG CANNOT MOVE. A yes that lands after
// the first mark has already fired has nothing left to tighten, but the reading
// of breadth and the done-condition it carries are still about this work and
// still belong to whatever task starts out of it.
func (m *checkpointMeter) tighten(verdict routeVerdict) {
	if m == nil {
		return
	}
	m.raced = verdict
	if m.marks > 0 || m.firstAt > 0 {
		return
	}
	// The boundary this is called at is the boundary [checkpointMeter.round] is
	// about to count, so this fires the first mark HERE rather than one round
	// later: the two are read in that order at one step boundary (loop.go).
	m.firstAt = m.rounds + 1
}

// round folds one finished tool round into the meter and reports which mark, if
// any, this round has just crossed. Zero is the ordinary answer.
//
// A ROUND IS THE UNIT BECAUSE IT IS THE ONE THE LOOP ALWAYS KNOWS. Tokens ride
// the turn too — [Agent.addUsage] folds them into the turn's Usage on every step
// — but a provider that reports no usage reports zero, and an accumulator that
// silently never fires on some models is a capability that is broken rather than
// absent, which this codebase does not ship. A finished batch is a fact the loop
// establishes itself, on every provider, and it is also the unit the measured
// failure was measured in: ninety-odd tool rounds.
//
// A ROUND IS ONE BATCH AND NOT ONE CALL, AND A FORK BURST IS ONE BATCH. This
// counts what it counts because the meter measures THE PERSON'S WAITING, and a
// batch is one wait however many calls are inside it — that is why eight reads
// asked for in one breath cost one round. A `fork` (fork.go) is the sharpest
// case of the same fact: one call, in one batch, with two to four whole agents
// working inside it, and the person waits once. So a lane tempted to count calls
// here would silently price a burst at four times what the person actually
// waited, and would move work off a conversation for having been parallel.
func (m *checkpointMeter) round() int {
	if m == nil {
		return 0
	}
	m.rounds++
	if m.marks >= checkpointMarks {
		return 0
	}
	if m.rounds < m.markAt(m.marks+1) {
		return 0
	}
	m.marks++
	return m.marks
}

// ── the sketch ──────────────────────────────────────────────────────────────

// checkpointSketch is what one mark's sidecar drew: the shape line, the sentence
// under it naming the letters, and how many parts the harness read out of the
// shape. An empty one is every failure — no mastermind, a fault, a window that
// ran out, an answer with nothing in it — and it decides nothing and heads
// nothing.
type checkpointSketch struct {
	shape  string
	legend string
	parts  int
}

// split reports the one decision this file takes off a sketch.
func (s checkpointSketch) split() bool { return s.parts >= checkpointSketchParts }

// drawn reports whether the reader answered with a shape at all, which is a
// different question from whether the shape has parts in it: an unreachable
// reader, a fault and a blank reply all answer no here, and so does nothing else.
func (s checkpointSketch) drawn() bool { return s.shape != "" }

// checkpointDoneShapes are the shapes that MEAN NOTHING REMAINS, and they are the
// second half of the remains contract — the reader's half.
//
// THE ASK NAMES THE ANSWER IT WANTS, `(done)`, exactly as the dowry ask names
// [checkpointNothingLeft]: a token the question taught is a thing the reader
// CHOSE to say, where a harness sniffing a free-text shape for finished-sounding
// words would be a keyword rule with a mastermind's bill attached. The handful of
// neighbours beside it are there because a model told to draw one word draws the
// synonym about as often as the word — measured on both namers (title.go) — and
// they are read through [normalizedWords], which is what makes `(done)`, `Done.`
// and `**DONE**` one answer.
//
// THE LIST IS SHORT ON PURPOSE. Everything not on it is a shape, and a shape is a
// carry-on: the failure direction here is to believe the work is finished when it
// is not, and that one drops a handover the person is owed.
var checkpointDoneShapes = map[string]bool{
	"done":               true,
	"none":               true,
	"nothing":            true,
	"finished":           true,
	"complete":           true,
	"nothing left":       true,
	"nothing remains":    true,
	"nothing left to do": true,
}

// saysDone reports that the reader ANSWERED, and answered that nothing is left.
//
// IT IS NOT THE SAME QUESTION AS "DID NOT SPLIT". A chain, a fork behind a step
// and a sentence of prose are all sketches of work that REMAINS and merely cannot
// be handed to two pairs of hands; only this says there is nothing there.
//
// AND A READER NOBODY REACHED SAYS NOTHING AT ALL. An empty sketch is a fault, a
// timeout, an install with no mastermind and a blank reply, and reading silence
// as agreement is precisely the failure this corroboration exists to close: it
// would hand the drop straight back to the running model on every turn whose
// sidecar was down. So the sketch must have been drawn.
func (s checkpointSketch) saysDone() bool {
	if !s.drawn() {
		return false
	}
	return checkpointDoneShapes[strings.Join(normalizedWords(s.shape), " ")]
}

// head puts the sketch above the dowry, so that the worker's first paragraph is
// the parts already named.
//
// IT IS THE WHOLE REASON THE LEGEND IS ASKED FOR. A worker armed to divide has to
// name its own parts to divide_work and defend them with evidence
// (task_divide.go); handing it a division somebody has already drawn out of the
// same transcript is the difference between a worker that discovers the shape and
// one that is told it. The dowry still stands underneath, unchanged and doing what
// it always did.
//
// ONLY A SKETCH WITH PARTS IN IT HEADS ANYTHING. A shape saying the work is one
// job — a chain, a fork behind a step, a single atom — has no parts to name, and
// a brief that opened on a picture of one job would be a paragraph of machinery
// in front of a worker's instruction. The ceiling reaches this with such a sketch
// routinely, because it fires whatever the last reading said.
//
// IT IS CLIPPED TO THE SAME BOUND EVERY BRIEF ON THIS ROAD IS HELD TO, from the
// front, so the shape and the legend survive a dowry that was already at the
// limit.
func (s checkpointSketch) head(goal string) string {
	if !s.split() {
		return goal
	}
	var out strings.Builder
	out.WriteString("WHAT IS LEFT, AS PARTS: ")
	out.WriteString(s.shape)
	if s.legend != "" {
		out.WriteString("\n")
		out.WriteString(s.legend)
	}
	if goal != "" {
		out.WriteString("\n\n")
		out.WriteString(goal)
	}
	return clip(out.String(), taskShapeBriefLimit)
}

// parseCheckpointSketch reads the sidecar's answer.
//
// THE FIRST NON-EMPTY LINE IS THE SHAPE AND EVERYTHING UNDER IT IS THE LEGEND,
// which is the contract [checkpointSketchAsk] states in those words. Backticks
// and emphasis come off the shape because a model told to write one line and
// nothing else writes it in a code span about as often as it writes it bare —
// measured, on both models.
//
// A reader that opened with prose instead has written a shape with no separator
// in it, which reads as one part, which is a CONTINUE. That is the fail-open
// direction and it needs no branch of its own.
func parseCheckpointSketch(answer string) checkpointSketch {
	lines := strings.Split(strings.TrimSpace(answer), "\n")
	shape, rest := "", 0
	for index, line := range lines {
		if trimmed := strings.Trim(strings.TrimSpace(line), "`*_ "); trimmed != "" {
			shape, rest = trimmed, index+1
			break
		}
	}
	if shape == "" {
		return checkpointSketch{}
	}
	return checkpointSketch{
		shape:  clip(shape, checkpointSketchBytes),
		legend: clip(strings.TrimSpace(strings.Join(lines[rest:], "\n")), checkpointSketchBytes),
		parts:  topLevelParts(shape),
	}
}

// topLevelParts counts the independent parts of a shape, and it is the whole of
// the harness's reading of what a sidecar drew.
//
// THE QUESTION IT ANSWERS IS "HOW MANY PAIRS OF HANDS COULD START NOW", which is
// the only question a mark is entitled to ask: what stands in front of the turn
// at THIS moment, before anything else has to have happened.
//
// THE RULES, WITH THE EXAMPLES THE ASK ITSELF GIVES:
//
//   - `A | B | C` is THREE parts, and a SPLIT. Nothing has to happen before
//     anything else, so three pairs of hands can start now.
//   - `A > B > C` is ONE part, and a CONTINUE. It is a chain: the second step
//     cannot begin until the first is done, so handing it out buys nothing.
//   - `A > (B | C)` is ONE part, and a CONTINUE. The fork is real, but it is
//     BEHIND a step that has not happened yet — at THIS mark the work in front of
//     the turn is A, which is one job. This is the stricter reading of the
//     benchmark's own scoring, and re-scoring variant C that way took the
//     mastermind's trap accuracy from 58% to 100%.
//   - `A > B | C > D` is TWO parts, and a SPLIT: two chains that wait on nothing
//     but themselves. THE BAR BINDS LOOSER THAN THE ARROW, which is why it is read
//     first — a reading that cut on arrows first would find `A` in front of
//     everything and call two independent chains one job.
//   - `(A | B | C) > D` is THREE parts, and a SPLIT: three jobs that can start now
//     and one step that gathers them afterwards. So is `(A | B) > C`, at two. The
//     gathering step is not a fourth part and is not lost — it is what the PARENT
//     does once the reports land (task_divide_sketch.go's [drawnDivision.afterParts]).
//   - anything with no separator at all — a word, a sentence, an apology — is one
//     part, and a CONTINUE.
//
// THE BRACKETED FIRST STAGE IS WHY THIS STOPPED READING THE WHOLE LINE ALONE, and
// it is a measurement rather than a preference. A whole-line reading counted
// `(A | B | C) > D` as one bracketed group and therefore as one part — and on the
// four-issue batch that is the shape kimi actually draws: it found the four-way
// fork 6 times out of 6 at round ten and wrote it as parts-then-gather every
// time, so the shipping parser read SPLIT on 2 of 12 batch reads where reading the
// first stage reads 6 of 6 (bench/oneroad/replay/RESULTS-2.md). The cost of the
// rule on the traps is 4 more splits out of 72 reads, every one a genuine
// two-way `(A | B) > C`, which is a division the evidence gate and the reviewer
// stand behind anyway (task_divide.go).
//
// A separator with nothing beside it does not make a part, so a line that opens
// or ends on one counts what is actually there. Brackets of any kind nest, and an
// unbalanced closer is ignored rather than taken below zero: a shape a model
// mis-typed is still a shape, and the honest failure is to read it as narrow.
func topLevelParts(shape string) int {
	return len(readShape(shape).parts)
}

// shapeReading is one shape read out: the parts that could be started now, and
// the stages that wait behind ALL of them.
//
// after is only ever filled where the parts came out of a bracketed first stage —
// `(A | B) > C` — because that is the only shape in which a trailing stage waits
// on every part rather than on one of them. In `A > B | C > D` the arrows belong
// INSIDE the parts and there is nothing standing after the division at all.
type shapeReading struct {
	parts []string
	after []string
}

// readShape is the whole of the harness's reading of what a sidecar drew, and it
// is one function rather than a count and a list because a count and a list that
// disagreed would mean the harness split a turn on three parts and then handed out
// two of them (task_divide_sketch.go builds a division out of these).
//
// Each piece is returned as the sidecar wrote it, trimmed and nothing else: the
// arrows and brackets inside one piece are that piece's own internal order, and a
// harness that tidied them away would be rewriting a drawing it did not make.
func readShape(shape string) shapeReading {
	// THE BAR FIRST, because it binds looser than the arrow: what a bar separates
	// is whole jobs, and what an arrow separates is steps of one.
	if parts := splitAtTopLevel(shape, '|'); len(parts) > 1 {
		return shapeReading{parts: parts}
	}
	// No bar at the top level leaves one line of stages, and the only parts that
	// could be started now are inside the FIRST of them.
	stages := splitAtTopLevel(shape, '>')
	if len(stages) == 0 {
		return shapeReading{}
	}
	parts := splitAtTopLevel(unbracket(stages[0]), '|')
	if len(parts) < 2 {
		// ONE JOB IN FRONT OF THE TURN, so the stages behind it are not anybody
		// else's work — they are the rest of this one, and saying otherwise would
		// hand a worker an integration step for a division that never happened.
		return shapeReading{parts: parts}
	}
	return shapeReading{parts: parts, after: stages[1:]}
}

// splitAtTopLevel cuts a shape on one separator, ignoring the separator inside
// brackets of any kind. An empty piece is not a piece: a line that opens or ends
// on a separator says what is actually beside it.
func splitAtTopLevel(shape string, separator rune) []string {
	var pieces []string
	depth := 0
	var current strings.Builder
	closeOne := func() {
		if piece := strings.TrimSpace(current.String()); piece != "" {
			pieces = append(pieces, piece)
		}
		current.Reset()
	}
	for _, letter := range shape {
		switch letter {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case separator:
			if depth == 0 {
				closeOne()
				continue
			}
		}
		current.WriteRune(letter)
	}
	closeOne()
	return pieces
}

// unbracket takes ONE enclosing pair of brackets off a stage, and only where the
// pair really does enclose the whole of it: `(A | B)` becomes `A | B`, and
// `(A | B) or (C)` is left exactly as it is because its first bracket closes in
// the middle. One pair and not all of them — a model that wrote `((A | B))` meant
// a group inside a group, and unwrapping until nothing was left would be the
// harness deciding what its brackets meant.
func unbracket(stage string) string {
	stage = strings.TrimSpace(stage)
	if len(stage) < 2 {
		return stage
	}
	var closer byte
	switch stage[0] {
	case '(':
		closer = ')'
	case '[':
		closer = ']'
	case '{':
		closer = '}'
	default:
		return stage
	}
	if stage[len(stage)-1] != closer {
		return stage
	}
	depth := 0
	for index := 0; index < len(stage); index++ {
		switch stage[index] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			// The opening bracket closes before the end, so it is not enclosing.
			if depth == 0 && index < len(stage)-1 {
				return stage
			}
		}
	}
	return strings.TrimSpace(stage[1 : len(stage)-1])
}

// readMark is the sidecar: ONE call, at one mark, asking somebody who is not the
// running model what is left of this turn.
//
// IT IS ASKED OF THE CREW ALONE — the empty `sessionDefault` — which is the same
// refusal [Agent.askRouteAhead] makes and for a sharper reason. The ladder's
// floor is the conversation's own model (auxiliary.go), and the conversation's
// own model is precisely the reader this whole mechanism was rebuilt to stop
// relying on: it answered the same question with a tool call instead of an answer
// on up to half the measured turns. Falling through to it would be the running
// model deciding whether to interrupt itself, quietly, under a different name. So
// an install with no mastermind gets NO MARK READING AT ALL, which is the
// codebase's law about a capability that cannot work being absent rather than
// broken — and the ceiling still moves the turn at the last mark, which is what
// that install had before this existed.
//
// IT IS SENT WITH NO BELT, for [Agent.checkpointBrief]'s reason: a reader with no
// hand to reach for can only answer with the drawing.
//
// AND IT IS BILLED TO THE ERRAND POCKET rather than to the turn, which is where
// this parts company with the dowry ask. The dowry is the conversation's own
// model reading the conversation's own transcript — the last step of the answer.
// This is a side-call to a different model that the person did not ask for, which
// is what the auxiliary pocket is for (loop.go's [Agent.addAuxiliaryUsage]).
//
// EVERY FAILURE IS AN EMPTY SKETCH, and an empty sketch is a CONTINUE. There is
// no retry and no repair turn: the next mark will ask again if the turn is still
// running, and the ceiling stands behind all of them.
func (a *Agent) readMark(ctx context.Context) checkpointRead {
	// AN ACCOUNT OF THE WORK AND NOT THE CONVERSATION, for the price
	// [checkpointDigestTokens] states: the reader is asked about the SHAPE of what
	// is left, and nothing inside a tool result changes that shape.
	digest := checkpointDigest(a.taskRequest(), a.snapshot())
	if digest == "" {
		// NOTHING TO READ IS NOT A READING. A turn with no ask, no tool call and
		// nothing said has nothing for a second mind to be shown, and a call made
		// on an empty page is a mastermind asked to invent an answer. It cannot
		// happen at a mark — ten finished rounds are ten ledger lines — and if it
		// ever does, the honest outcome is the fail-open one and no bill.
		return checkpointRead{}
	}
	ctx, done := context.WithTimeout(ctx, checkpointSketchWindow)
	defer done()
	messages := []ai.Message{textMessage("user", digest+"\n\n"+checkpointSketchAsk)}
	began := time.Now()
	response, reader, err := a.callRole(ctx, roles.RoleMarkReader, "", messages,
		ai.WithMaxTokens(checkpointSketchTokens),
		ai.WithTemperature(checkpointSketchTemp))
	read := checkpointRead{asked: true, model: reader, took: time.Since(began)}
	if err != nil || response == nil {
		read.failed = true
		return read
	}
	a.addAuxiliaryUsage(response, reader, 1)
	if response.Usage != nil {
		read.costUSD = costOf(response.Usage)
	}
	read.sketch = parseCheckpointSketch(response.Text())
	// AND WHAT THE READER WAS SHOWN RIDES BACK WITH WHAT IT DREW. The drawing is
	// one line of letters; the account under those letters is the only thing
	// anybody downstream could weigh as EVIDENCE, and it is honest evidence
	// because it is exactly the page a mastermind read before it said the work
	// had parts (task_divide_sketch.go).
	read.digest = digest
	return read
}

// checkpointRead is ONE mark's reading as an EVENT rather than as a drawing: the
// sketch, and what asking for it actually cost.
//
// It exists because the cost is the thing this mechanism has to be held to. Three
// reads on one measured run cost five times the work they were judging and none
// of it was written down anywhere — the money arrived as three anonymous
// auxiliary lines, and what was asked, what came back and what the harness did
// about it existed in no file at all. So the call's own figures ride back with
// the drawing, and [Agent.journalMarkRead] puts them in the journal beside the
// decision they bought.
//
// asked is whether a request was made at all, which is a different fact from
// failed: a turn with nothing to show a reader never asks, and a line about a
// reading that never happened would be the emptiness law broken in the one file a
// person reads.
// digest is the account the reader was shown ([checkpointDigest]). It is kept
// because a drawing is not evidence and this is: see [drawnDivision].
type checkpointRead struct {
	sketch  checkpointSketch
	digest  string
	asked   bool
	failed  bool
	model   string
	costUSD float64
	took    time.Duration
}

// drawn is this reading as the DIVISION IT PROPOSES, which is the one thing about
// it that outlives the turn. Everything else here — the cost, the model, the
// window it took — is accounting for a call that has already been made.
func (r checkpointRead) drawn() drawnDivision {
	return drawnDivision{sketch: r.sketch, digest: r.digest}
}

// journalMarkRead writes one mark's reading down (sessionfile.go's
// [journalMark]), with the decision the harness took off it.
//
// A READING THAT NEVER HAPPENED WRITES NOTHING, and a reading that FAILED writes
// itself as a failure whatever the caller was expecting: a mark whose sidecar
// could not be reached did not decide to carry on, it decided nothing, and a file
// that spelled those the same way could not tell a cheap install from a broken
// one.
func (a *Agent) journalMarkRead(read checkpointRead, mark, rounds int, decision string) {
	if !read.asked {
		return
	}
	if read.failed {
		decision = checkpointDecisionFailed
	}
	a.file.appendMark(journalMark{
		N:          mark,
		Rounds:     rounds,
		Model:      read.model,
		CostUSD:    read.costUSD,
		Sketch:     read.sketch.shape,
		Decision:   decision,
		DurationMS: read.took.Milliseconds(),
	})
}

// The decisions a journaled mark and a journaled ceiling can carry. They are
// constants because the bench reads them and a decision spelled two ways is two
// decisions to whatever is counting.
const (
	checkpointDecisionSplit    = "split"
	checkpointDecisionContinue = "continue"
	checkpointDecisionFailed   = "failed"

	checkpointCeilingMoved   = "moved"
	checkpointCeilingNothing = "dropped:nothing-left"
	checkpointCeilingNoBrief = "dropped:no-brief"
)

// ── the digest ──────────────────────────────────────────────────────────────

// checkpointDigest is what the mark's reader is shown: A SHORT ACCOUNT OF THE
// WORK, assembled by the harness from the transcript with no model in the loop.
//
// ── WHY AN ACCOUNT AND NOT THE CONVERSATION ──
//
// [checkpointDigestTokens] carries the measured bill. The short version is that a
// mastermind reading a raw transcript costs more than the work it is judging, and
// on the run that was measured it did so three times to say "carry on" three
// times.
//
// ── AND WHY THIS IS NOT A LOSS ──
//
// The question is what REMAINS OF THE ASK, and the four things that answer it are
// all here:
//
//   - THE ASK ITSELF, verbatim and first, because everything else is measured
//     against it and it is the one thing on this road nobody may rewrite
//     (task_brief.go).
//   - THE LEDGER: one line per tool call, the tool's name and the argument that
//     says what it touched. WITH NO RESULTS AT ALL — that is where the tokens went
//     and it is also where the noise is. What a search returned does not change
//     the shape of what is left; that the search happened does.
//   - WHAT HAS BEEN WRITTEN OR CHANGED, deduplicated out of the same ledger,
//     because a thing already produced is a part of the ask already discharged and
//     that is exactly what the reader is being asked to subtract.
//   - THE LAST THING SAID, clipped, which is the running model's own account of
//     where it has got to and the only part of this a person would recognise.
//
// ── AND IT IS ASSEMBLED DETERMINISTICALLY ──
//
// No summariser, for [Agent.checkpointBrief]'s reason: a pass over text this
// session already holds is expensive at the worst moment and lossy by
// construction. Every line here is a fact the transcript states.
//
// AN EMPTY DIGEST IS THE HONEST ANSWER TO AN EMPTY TURN, and [Agent.readMark]
// spends nothing on one.
func checkpointDigest(asked string, messages []ai.Message) string {
	ledger, written := checkpointLedger(messages)

	var head strings.Builder
	if asked = strings.TrimSpace(asked); asked != "" {
		head.WriteString(checkpointDigestAsked)
		head.WriteString("\n")
		head.WriteString(asked)
		head.WriteString("\n\n")
	}

	// THE TAIL IS BUILT BEFORE THE LEDGER IS FITTED, because the ledger is the one
	// section that grows without bound and the other three must not be squeezed out
	// by it. A turn of ninety tool calls is exactly the turn whose last words and
	// written things matter most.
	var tail strings.Builder
	if len(written) > 0 {
		tail.WriteString(checkpointDigestWritten)
		tail.WriteString("\n")
		tail.WriteString(strings.Join(written, "\n"))
		tail.WriteString("\n\n")
	}
	if said := clip(checkpointLastSaid(messages), checkpointSaidBytes); said != "" {
		tail.WriteString(checkpointDigestSaid)
		tail.WriteString("\n")
		tail.WriteString(said)
		tail.WriteString("\n")
	}

	var out strings.Builder
	out.WriteString(head.String())
	if len(ledger) > 0 {
		out.WriteString(checkpointDigestDone)
		out.WriteString("\n")
		// THE OLDEST CALLS ARE WHAT GOES when the room runs out, and one line says
		// how many. The recent end of a ledger is the work in front of the turn,
		// which is what the reader is being asked about; the far end is history it
		// can infer from what has been written. A silent truncation would instead
		// let a reader believe a turn had done less than it had.
		// The room reserved for that line is measured at its WIDEST — the count can
		// never exceed the whole ledger — so a digest that ends up needing it cannot
		// be pushed over the bound by saying so.
		elision := func(dropped int) string {
			return fmt.Sprintf("… and %d earlier steps\n", dropped)
		}
		kept, dropped := checkpointLedgerThatFits(ledger,
			checkpointDigestBytes-out.Len()-tail.Len()-len(elision(len(ledger))))
		if dropped > 0 {
			out.WriteString(elision(dropped))
		}
		for _, line := range kept {
			out.WriteString(line)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	out.WriteString(tail.String())
	// The final clip is a backstop and not the policy: the fitting above is what
	// keeps the sections whole, and this is what guarantees the bound whatever a
	// future section does.
	return clip(strings.TrimSpace(out.String()), checkpointDigestBytes)
}

// checkpointLedgerThatFits keeps the NEWEST lines that fit in room, and reports
// how many older ones it dropped. Room that is gone already keeps nothing, which
// is the honest answer rather than one line over the bound.
func checkpointLedgerThatFits(lines []string, room int) ([]string, int) {
	spent := 0
	first := len(lines)
	for index := len(lines) - 1; index >= 0; index-- {
		cost := len(lines[index]) + 1
		if spent+cost > room {
			break
		}
		spent += cost
		first = index
	}
	return lines[first:], first
}

// checkpointLedger walks the transcript once and answers the two questions the
// digest asks of it: what was DONE, one line per tool call, and what of that was
// WRITTEN.
//
// The two come out of one pass because they are one fact read twice — a file
// written is a `write` in the ledger — and because a second walk could disagree
// with the first the day a tool is renamed.
func checkpointLedger(messages []ai.Message) (ledger, written []string) {
	seen := make(map[string]bool)
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			name := strings.TrimSpace(call.Function.Name)
			if name == "" {
				continue
			}
			line := name
			if argument := checkpointArgument(call.Function.Arguments); argument != "" {
				line += " " + argument
			}
			ledger = append(ledger, line)
			if !checkpointWriters[name] {
				continue
			}
			path := checkpointArgumentNamed(call.Function.Arguments, "path")
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			written = append(written, path)
		}
	}
	return ledger, written
}

// checkpointWriters are the verbs on this belt that CHANGE something a person
// keeps (internal/exec/bare). Everything else on the belt reads, searches, looks
// or asks, and a ledger line is all the account those need.
//
// IT IS A SET AND NOT A GUESS AT NAMES. A tool renamed here falls out of the
// written section and stays in the ledger, which is a digest that says less
// rather than one that says something untrue.
var checkpointWriters = map[string]bool{"write": true, "edit": true}

// checkpointArgumentKeys is what to show of ONE tool call, in the order the
// answer is looked for: the thing being run, then the thing being looked for,
// then the thing being touched.
//
// THE ORDER IS THE POINT AND IT IS NOT ALPHABETICAL. A search names itself by its
// pattern and a read by its path; asking for the path first would draw every
// search in a turn as the same directory over and over.
var checkpointArgumentKeys = []string{"command", "query", "pattern", "path", "url"}

// checkpointArgument is the one clipped thing a ledger line says about a call.
//
// A CALL WHOSE ARGUMENTS NAME NONE OF THE KEYS still gets a line, off the raw
// arguments — clipped like everything else, so a tool nobody anticipated is
// described badly rather than not at all. What it can never do is carry a
// payload: `write` names a path here, which is the key that stands ahead of its
// content precisely because the content is the thing this digest exists to leave
// out.
func checkpointArgument(arguments string) string {
	for _, key := range checkpointArgumentKeys {
		if value := checkpointArgumentNamed(arguments, key); value != "" {
			return clip(value, checkpointLedgerBytes)
		}
	}
	return clip(strings.Join(strings.Fields(arguments), " "), checkpointLedgerBytes)
}

// checkpointArgumentNamed reads one string field out of a call's arguments, and
// answers "" for everything else: arguments that are not an object, a field that
// is not there, a field that is not a string. A model's arguments are text off a
// wire and this is the one place that has to be true of them.
func checkpointArgumentNamed(arguments, key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &fields) != nil {
		return ""
	}
	raw, present := fields[key]
	if !present {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// checkpointLastSaid is the last words the running model actually said, skipping
// the assistant messages that were nothing but tool calls — which on a grinding
// turn is most of them, and none of which says anything about where the work has
// got to.
func checkpointLastSaid(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "assistant" {
			continue
		}
		var said strings.Builder
		for _, part := range messages[index].Content {
			said.WriteString(part.Text)
		}
		if text := strings.TrimSpace(said.String()); text != "" {
			return text
		}
	}
	return ""
}

// ── the turn's side ─────────────────────────────────────────────────────────

// checkpointRound is this file's whole place in a turn, called once from
// [Agent.runTurn] at the step boundary, after a batch's results are in the
// transcript.
//
// It reports whether THE TURN IS OVER, which is the one thing a hook could not
// have said: the control plane's law is that only pre-action may stop something
// (hooks.go), and both a split and the ceiling stop a turn. So it stands in the
// loop as a line of its own, beside the triage seam that feeds it.
//
// WHAT COSTS ANYTHING IS THE MARK AND NOT THE ROUND. A round that crosses no mark
// returns before a single call is made, which is every round of almost every turn
// this session will ever run.
//
// AND A MARK THAT SAYS CONTINUE COSTS THE TURN NOTHING BUT THE CALL. Nothing is
// injected, nothing is said, the model is not told it was looked at, and the
// meter simply walks on to the next mark.
func (a *Agent) checkpointRound(ctx context.Context, hub *eventHub, user userMessage, meter *checkpointMeter, turn *Usage, started time.Time, model string) bool {
	if !a.checkpoints(ctx, user) {
		return false
	}
	mark := meter.round()
	if mark == 0 {
		return false
	}
	// THE SKETCH IS READ AT EVERY MARK, THE CEILING'S INCLUDED. At the first two
	// it is the decision; at the ceiling the decision is already made and the
	// drawing is still worth its call for TWO reasons now — it is what names the
	// parts at the head of the brief the worker opens on, and it is the second mind
	// whose agreement the remains contract needs before a handover may be dropped
	// ([Agent.handOverRunningTurn]).
	read := a.readMark(ctx)
	rounds := meter.rounds
	if mark < checkpointMarks {
		if !read.sketch.split() {
			a.journalMarkRead(read, mark, rounds, checkpointDecisionContinue)
			return false
		}
		a.journalMarkRead(read, mark, rounds, checkpointDecisionSplit)
		return a.handOverRunningTurn(ctx, hub, turn, started, model,
			checkpointSplitNote, meter.raced, read).moved
	}
	// THE CEILING'S OWN READ IS JOURNALED AS A CARRY-ON, because that is what it
	// did: it decided nothing, and the ceiling line written a moment later is
	// where what happened to the turn is recorded.
	a.journalMarkRead(read, mark, rounds, checkpointDecisionContinue)
	return a.checkpointCeiling(ctx, hub, turn, started, model, rounds, meter.raced, read)
}

// checkpoints reports whether this turn may be checkpointed at all.
//
// THE GATES ARE THE ROUTE JUDGE'S GATES, in the same order and for the same
// reasons (route_judge.go), because they are gates on MOVING WORK OUT OF A
// CONVERSATION and not on which moment noticed:
//
//   - A NODE NEVER CHECKPOINTS. Work inside a task already lives under
//     governance — its own step cap, its own no-progress detector, its own
//     deadline and its own checker (task_run.go) — so a second ceiling over the
//     top of those would be the harness governing the governed, and the task it
//     started would be a third level of a tree that is two deep by law.
//   - A SCREENLESS SESSION NEVER CHECKPOINTS. `--once` and anything else running
//     with nobody watching has no one to read the line, and a ceiling there would
//     end a turn somebody is waiting on the answer of with a task nobody will see
//     land.
//   - AND ONLY WHAT A PERSON TYPED. A woken turn and an authored one are the
//     session talking to itself — a task's report landing, a standing run's own
//     instruction — and both already carry budgets of their own. Ending one of
//     those and starting a task against it would be the session spending money on
//     its own sentence, which is the law harness.go, route_judge.go and
//     task_brief.go all keep.
//   - AND NOT MID-INTERRUPT. A turn the person has just stopped is a turn they
//     have said they do not want; moving its remains onto the rail would be
//     answering an interrupt with a task.
//
// IT IS ALSO WHAT STOPS THE SIDECAR BEING BILLED ON THOSE TURNS, because it
// stands in front of the meter and therefore in front of every call this file
// makes.
func (a *Agent) checkpoints(ctx context.Context, user userMessage) bool {
	if a.config.InTask || !a.config.AskConsent {
		return false
	}
	if user.empty() || user.wake || user.authored {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed
}

// checkpointCeiling ends the turn and moves what is left of it onto the one
// road. Past the last mark there is no branch back into the conversation on
// account of the WORK — the work was read twice and the harness has stopped
// reading — and it reports whether the turn is over.
//
// THERE IS EXACTLY ONE ANSWER THAT LEAVES THE TURN RUNNING, and it is not a third
// sketch: it is the model saying nothing remains at all
// ([checkpointNothingLeft]) AND THE MARK'S OWN READER AGREEING. The ceiling
// exists to move A GRIND somewhere it is watched, and a turn that is finishing is
// not a grind — but a running model declaring itself finished is the model
// grading its own work, which is exactly the reading this whole file was rebuilt
// to stop relying on. It was measured doing it: a turn declared nothing was left
// at the ceiling, the handover was dropped, and the same model then ground on for
// twenty more rounds unwatched. So the drop needs two minds
// ([Agent.handOverRunningTurn]).
//
// EVERYTHING ELSE IT DOES IS [Agent.handOverRunningTurn]'S. It contributes the
// two things that are its own: the line, and a verdict ARMED TO SPLIT. This turn
// outran one pair of hands by measurement rather than by anybody's opinion, which
// is the strongest evidence of breadth any door into the graph has; arming costs
// nothing if it is wrong, because it only means the worker MAY discover the work
// is wide and the evidence gate still refuses a division the material does not
// support (task_divide.go).
//
// THE SKETCH RIDES ALONG WHEN THERE IS ONE, and it now does two jobs rather than
// one. A drawing of what is left is exactly what the worker's first paragraph
// should be — and it is the second mind the remains contract needs, because the
// only thing that can stop the ceiling is the two of them agreeing.
//
// AND THIS IS THE ONE MOMENT THAT WRITES ITSELF DOWN. What the ceiling did to a
// turn used to exist nowhere: a run where the handover was dropped and a run where
// it never fired read identically in the file, which is why the measured failure
// could not be attributed without re-reading provider logs. So one line, with the
// round it fired on and what it decided (sessionfile.go's [journalCeiling]).
func (a *Agent) checkpointCeiling(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string, rounds int, verdict routeVerdict, read checkpointRead) bool {
	verdict.Wide = true
	over := a.handOverRunningTurn(ctx, hub, turn, started, model, checkpointCeilingNote, verdict, read)
	a.file.appendCeiling(journalCeiling{Rounds: rounds, Decision: over.decision, TaskID: over.taskID})
	return over.moved
}

// handOverRunningTurn ENDS A TURN THAT IS STILL RUNNING and moves what is left
// of it onto the one road. It is one function because two moments reach it and
// there must not be two versions of what happens next:
//
//   - a MARK whose sketch came back with independent parts in it
//     ([Agent.checkpointRound]);
//   - the CEILING, when an answer has cost more than handing it over would have
//     and the harness has stopped reading ([Agent.checkpointCeiling]).
//
// BOTH OF THEM ARE THIS FILE NOW. The race was the third and is not any more: a
// read of a REQUEST nobody has worked on yet was measured converting turns whose
// work was small, so what it does instead is pull this file's first mark forward
// (route_judge.go's [Agent.routeTriage]). Its verdict still arrives here, on the
// meter, because the breadth and the done-condition it wrote are about this work.
//
// The caller brings the line the person reads, a verdict carrying whatever any
// earlier reading knew, and the sketch if a mark drew one. What this adds is the
// five things that are the same however the decision arrived: the dowry, the
// parts at the head of it, the task, the gap, and a turn sealed with the
// transcript left in a state the next turn can open on.
//
// AND IT CAN DECLINE, ON TWO MINDS AGREEING AND NEVER ON ONE.
//
// The ground is the one no clock can see for itself: the model answering the
// dowry ask with [checkpointNothingLeft]. Every clock here decides on evidence
// that is old by the time it is spent — a counter of rounds already finished, a
// sketch drawn a step ago — and the model holding the findings is the only reader
// that knows whether it finished the work thirty seconds ago.
//
// BUT IT IS ALSO THE MODEL BEING INTERRUPTED, and a running model mid-grind
// answering "everything is done" is that model grading its own work at the exact
// moment it has a reason to. It was measured: a turn declared nothing was left at
// the ceiling, the handover was dropped on its say-so, and it then ground on for
// twenty more rounds with nobody watching — the failure this whole file exists to
// prevent, let through by the one door built to be kind to it.
//
// So the declaration is CORROBORATED against the sketch the mark's own reader
// drew at that same moment ([checkpointSketch.saysDone]). Two minds agreeing that
// nothing remains is evidence; one mind saying so about itself is a claim. When
// they agree, NOTHING HAPPENS: no task, no line, no gap spent, no turn sealed —
// the turn carries on and its own answer stands, which is the honest outcome for
// a turn that was already finishing. When they do not, the work moves, and it
// moves on the person's own sentence, because a continuation that answered with
// the token wrote no instruction to hand anybody. That is the same fallback a
// dowry of machine markup gets, for the same reason: a task that could not start
// at all would be the guarantee broken.
//
// THE MARK'S OWN SPLIT CAN NEVER BE DROPPED, and it needs no rule of its own to
// say so. A sketch with independent parts in it is a reader stating that work
// remains, so it cannot corroborate a claim that none does.
//
// THE PERSON'S OWN WORDS ARE READ FIRST AND ARE NEVER WRITTEN BY ANYBODY. They
// ride the spec's request, verbatim, exactly as they do on every other door into
// the graph, and [composeBrief] prints them above the work under the rule that
// says where the two read differently theirs are what was asked for
// (task_brief.go).
//
// AND THE GOAL IS THE DOWRY, UNDER THE SKETCH. The model that has just spent the
// turn is the only reader in the building holding what the turn found out; the
// sketch above it is the shape a second reader drew of what is left. A goal
// written before the turn began — the race's own, from the request alone — would
// be the one thing on the table that knows least, which is why it is dropped
// where the other two fields of that verdict are kept.
func (a *Agent) handOverRunningTurn(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model, line string, verdict routeVerdict, read checkpointRead) checkpointHandover {
	sketch := read.sketch
	asked := a.taskRequest()
	goal, remains := a.checkpointBrief(ctx, turn, model, asked)
	if !remains {
		if sketch.saysDone() {
			// NOTHING HAPPENS, and that includes the line. A person told their answer
			// was being moved and then left watching it finish where it was would have
			// been told something that did not happen.
			return checkpointHandover{decision: checkpointCeilingNothing}
		}
		// UNCORROBORATED, so the work moves on the person's own words. The
		// continuation spent its answer on the token instead of on an instruction,
		// which leaves nothing else to give a worker.
		goal = asked
	}
	if strings.TrimSpace(goal) == "" {
		// AND THERE IS THE ONE OTHER WAY THIS ENDS WITH NO TASK: nothing to write
		// down for anybody. No dowry and no sentence of the person's own is not a
		// narrow brief, it is no brief — and a task admitted on it would be a worker
		// started on a blank page.
		return checkpointHandover{decision: checkpointCeilingNoBrief}
	}
	verdict.Work = true
	verdict.Goal = sketch.head(goal)
	// AND THE DRAWING TRAVELS WITH THE WORK, which is the whole of what changed
	// after the parts stopped being only a paragraph.
	//
	// A SKETCH WITH PARTS IN IT IS A DIVISION SOMEBODY HAS ALREADY WRITTEN. It was
	// drawn by a mastermind, out of an account of this turn, and putting it at the
	// head of the brief and then hoping a cheap worker would re-derive it was
	// measured failing outright: over three converted cells the worker never once
	// reached for `divide_work`, and every task that had been read as four jobs ran
	// as one. So the drawing rides the spec ([taskSpec.drawn]) and the harness
	// submits it FOR the worker at the moment the worker is started — through the
	// same verb, the same gates and the same reviewer a worker's own division goes
	// through (task_divide_sketch.go). Nothing is minted from the conversation: the
	// parts exist only if the road inside the task admits them.
	//
	// IT IS ONLY CARRIED WHERE THERE ARE PARTS TO CARRY. A shape saying one job has
	// no division in it, and the ceiling reaches this with such a shape routinely.
	drawn := drawnDivision{}
	if sketch.split() {
		drawn = read.drawn()
		// AND THE ROAD IS ARMED BY THE READER THAT ACTUALLY READ THE WORK. A
		// mastermind was shown this turn's transcript and drew independent parts out
		// of it, which is the same fact the sizing judge banks at the typed `/task`
		// door and is banked the same way ([Agent.rememberDivisible]) — so the task
		// is armed `judged` rather than `wide`, and [TaskNode.armedByJudgement] can
		// tell a model that read the work from a counter reading its own text
		// (task_divide.go). It is banked against the goal this hands over, and
		// admission happens on the next line, so there is nothing in between for the
		// single-entry bank to lose.
		//
		// IT STILL MATTERS WITH THE DRAWING CARRIED. Arming is what puts the verb on
		// the worker's belt at all, and it is what the below-floor tiebreak turns on
		// — a division of four whole jobs enumerates nothing a counter can see, so a
		// harness-submitted one reaches the reviewer by exactly the road a worker's
		// own would.
		a.rememberDivisible(verdict.Goal)
	}

	// THE LINE GOES ABOVE THE TASK'S OWN, which is where taskEscalationNote stands
	// over the card it explains (task.go). It is [EventNotice] for that line's
	// reason: the dim one-liner a surface already draws for something the harness
	// did without stopping to ask.
	hub.send(Event{Kind: EventNotice, Text: line})
	// AND THE TASK IS NAMED FROM THE PERSON'S OWN WORDS AND NEVER FROM THE DOWRY.
	// The other door into [Agent.launchRouteTask] cuts its title off the front of a
	// goal a judge wrote to be a goal, which is survivable there; here the goal is
	// a continuation written on a transcript full of tool calls, and its first line
	// has been measured arriving as a provider's tool-call sentinel and as the
	// closing remark of a finished answer. The person's sentence is the one thing
	// on this road nobody writes, so it is the one thing that cannot come back as
	// machinery — and the namer improves it a second later anyway (taskname.go).
	said, id := a.launchRouteTask(hub, verdict, asked, drawn)

	// THE GAP IS SPENT, because the person has just been interrupted by a task and
	// does not care which of the moments noticed. routeJudgeGap exists so that work
	// appearing over the top of a conversation cannot be followed immediately by
	// more of it (route_judge.go), and every road into here is exactly that
	// interruption. The counter itself is not moved — it belongs to the front of a
	// turn and this is the middle of one.
	a.mu.Lock()
	a.routeOffered = a.routeTurns
	a.mu.Unlock()

	// AND THE TRANSCRIPT MUST NOT BE LEFT WITH A REQUEST NOBODY REPLIED TO. The
	// next turn would open on it, and a model that reads an unanswered question at
	// the top of its context answers it — which is this whole mechanism undone one
	// turn later. Nothing is streamed a second time; the person has already read
	// both lines as dim notes.
	a.record(textMessage("assistant", line+"\n"+said))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(*turn, started, model)})
	// And the name, on the terms every other turn shape takes it (title.go).
	a.maybeTitle(ctx, hub)
	return checkpointHandover{moved: true, decision: checkpointCeilingMoved, taskID: id}
}

// checkpointHandover is what one handover actually DID, and it is a struct rather
// than the bool it used to be because two of its three outcomes are the same bool.
//
// moved is the only thing the turn's loop needs — it says the turn is over — and
// the other two fields are what the journal needs: which of the ways this ended
// it was, and the node that took the work when one did. Nothing person-facing
// reads any of it.
type checkpointHandover struct {
	moved    bool
	decision string
	taskID   uint64
}

// checkpointBrief is the dowry: what this turn found out, written down for
// somebody who will never see it.
//
// ── WHY THE MODEL'S OWN CONTINUATION AND NOT A SUMMARISER ──
//
// There is no summariser. Compaction used to pay one and does not any more —
// "a pass is a rearrangement of text this session already has", loop.go's
// [Agent.compact] says, and it says why: the prose was expensive at the worst
// moment and lossy by construction. What replaced it is the state card, which is
// maintained by the POST-TURN extractor (card.go) and therefore holds the
// conversation as it stood before this turn began — every finding this turn made
// is exactly what it does not have. A brief written from it would hand the worker
// the conversation's history and none of its work.
//
// The shaper (task_shape.go) is the other candidate and reads the REQUEST alone,
// which is the same loss by a different road. The mark's own sidecar is not a
// candidate either: it drew a SHAPE, in one line, which is exactly the thing a
// worker cannot work from on its own.
//
// So it is asked of the one reader that actually holds the findings: the model
// that has just spent the turn. One more request, on the turn's own model and the
// turn's own transcript, and then the turn is over.
//
// ── AND IT IS ASKED WITH NO BELT ──
//
// No [ai.WithTools], which is the whole safety of asking a model that has just
// ground through the ceiling to do one more thing: with no hand to reach for, the
// only answer it can give is the document.
//
// AND NO BELT IS NOT THE SAME FACT AS NO TOOL GRAMMAR, which is the thing this
// was measured being wrong about. An omitted tools array takes the hand away; it
// does not take away the special tokens the model's own chat template is built
// around, and it does nothing about a transcript in which every previous turn
// called a tool. On deepseek this request came back as the raw tool-call sentinel
// and nothing else. So the safety of the shape is real but partial, and the part
// it cannot cover is covered by reading the answer instead.
//
// ── AND IT CANNOT LOSE THE ASK ──
//
// Because the ask does not go through it. The person's words ride the spec's
// request verbatim (see [Agent.handOverRunningTurn]); this writes the brief and
// nothing else, and when it fails — a provider fault, an interrupt, an empty
// reply — the brief falls back to their words, which is [unshaped]'s answer to
// the same failure at the typed door. A task started on the person's own sentence
// is a task that lost the dowry; a task that could not start at all would be the
// guarantee broken.
//
// ── AND IT READS WHAT CAME BACK ──
//
// Two answers to this ask are not briefs, and both were measured on real turns:
//
//   - MACHINE MARKUP, for the reason above. It is refused exactly as a provider
//     fault is refused — the person's words stand alone as the brief — because a
//     document nobody can read is worth what no document is worth. The test is
//     STRUCTURAL ([briefIsProse]) rather than a list of sentinels: an instruction
//     is words with spaces between them whoever wrote it, and a rule spelled in
//     one provider's special tokens is a rule that is wrong on the next provider.
//   - NOTHING LEFT TO DO, which is not a failure at all. It is the remains
//     contract answered ([checkpointNothingLeft]), and it drops the handover
//     rather than writing a brief.
//
// So it reports the brief AND whether there is anything to hand over, which are
// two facts rather than one: falling back to the person's ask and dropping the
// handover are opposite answers to opposite failures.
func (a *Agent) checkpointBrief(ctx context.Context, turn *Usage, model, asked string) (string, bool) {
	messages := append(a.snapshot(), textMessage("user", checkpointHandoffAsk))
	// WITHOUT THE TURN'S STREAM, for the reason every errand in this package is
	// made without it (auxiliary.go's [Agent.callRole]): the loop installed an
	// observer that types deltas into the room as the assistant speaking, and this
	// answer is a worker's instruction rather than a word to the person. Left on
	// the stream it would paint the brief over the top of the answer it is ending.
	response, err := a.client.CompleteWithMessages(provider.WithoutStream(ctx), messages,
		ai.WithModel(model), ai.WithMaxTokens(checkpointBriefTokens))
	if err != nil || response == nil {
		return asked, true
	}
	// The person pays for it on the turn it belongs to rather than out of the
	// auxiliary pocket, because this is the conversation's own model reading the
	// conversation's own transcript — the errand pocket is for the session's
	// side-calls, and this is the last step of the answer.
	turn.Turns++
	a.addUsage(turn, response, provider.ServedEndpointFrom(ctx).Name())

	brief := strings.TrimSpace(response.Text())
	// THE REMAINS CONTRACT IS READ FIRST, because it is the only answer here that
	// is about the WORK rather than about the document.
	if declaresNothingLeft(brief) {
		return "", false
	}
	// An empty reply, a whitespace one and a sentinel are all the same failure to
	// this line: nothing came back that anybody could work from. The person's own
	// words stand alone, which is what a provider fault already falls back to.
	if !briefIsProse(brief) {
		return asked, true
	}
	// THE SAME BOUND EVERY BRIEF ON THIS ROAD IS HELD TO, and that constant rather
	// than a second number of this file's own (task_shape.go's
	// taskShapeBriefLimit): two spellings of one bound are two answers to the
	// question of how long a worker's instruction may be.
	return clip(brief, taskShapeBriefLimit), true
}

// declaresNothingLeft reports whether the continuation answered the remains
// contract with its token rather than with an instruction.
//
// IT READS THE FIRST LINE AND MATCHES ON WORDS. The ask says to write the token
// alone, and a model that has just been told to shout one line writes it wrapped
// in emphasis, or followed by the reason, about as often as it writes it bare —
// so the match is on the first line BEGINNING with the token's words, folded
// through [normalizedWords] exactly as both namers' answers are (title.go). What
// it will not do is find the token in the middle of a brief, because a paragraph
// that happens to contain those four words is a brief and not a declaration.
func declaresNothingLeft(brief string) bool {
	said := normalizedWords(firstLine(brief))
	token := normalizedWords(checkpointNothingLeft)
	if len(said) < len(token) {
		return false
	}
	for index, word := range token {
		if said[index] != word {
			return false
		}
	}
	return true
}

// briefIsProse reports whether a dowry is something a person could work from.
//
// IT IS A TEST FOR SENTENCES AND NOT FOR SENTINELS. What it counts is WORDS — a
// whitespace-separated run carrying letters — because that is the one property
// every instruction in every language has and no machine markup has: prose is
// spaced, and a tool-call sentinel is one dense token however it is spelled.
// Matching the sentinels themselves would be a list of one provider's special
// tokens, out of date the first time a model ships a new template, and wrong
// about the next provider by construction.
//
// A digit-only field is not a word, which is what stops "8" and "2026-08-24"
// from carrying a brief past the bar on their own.
func briefIsProse(brief string) bool {
	words := 0
	for _, field := range strings.Fields(brief) {
		letters := 0
		for _, letter := range field {
			if unicode.IsLetter(letter) {
				letters++
			}
		}
		if letters < 2 {
			continue
		}
		words++
		if words >= checkpointBriefWords {
			return true
		}
	}
	return false
}
