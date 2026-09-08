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
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/lane"
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

// The handoff writer is registered here for the same reason and on the same
// terms: beside the call it belongs to, on the mastermind tier, and CREW-ONLY —
// [Agent.callRole] is given an empty `sessionDefault`, so an install with no
// mastermind gets no handoff writer at all rather than a fall-through to the
// conversation's own model.
//
// THAT REFUSAL IS THE WHOLE POINT OF THE ROLE. What this writes is a
// REPLACEMENT for a document the running model already wrote badly: measured on
// a real ten-hour run, the turn's own model answered the dowry ask with 5,882
// characters of degeneration — 85 `Also:` clauses of which 39 were distinct, 62%
// of it one six-sentence loop, and not one term from the task's own domain in any
// of it. Falling back to that same model here would be asking the author of the
// broken document to be its editor, which is the reading this file has already
// been measured being wrong about twice ([Agent.readMark]).
func init() { roles.Register(roles.RoleHandoff, roles.TierMastermind) }

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

	// checkpointBriefSentences is how many sentences a brief must carry before
	// the harness will judge it for REPETITION at all ([briefRepeats]).
	//
	// Eight, because a variety ratio over a short document is noise: a four-line
	// brief whose two middle lines both begin "the parser" is a writer being
	// consistent, and refusing it would cost a regeneration on the honest case
	// this check exists to catch the dishonest one of. Every measured degeneration
	// was tens of sentences long — the loop is what makes it long — so the floor
	// costs the check nothing it was built to see.
	checkpointBriefSentences = 8

	// checkpointBriefVariety is the share of a brief's sentences that must be
	// DISTINCT, as a percentage, before it may become somebody's instruction.
	//
	// A model that has run out of things to say does not stop; it loops. The
	// measured brief was 85 clauses of which 39 were distinct — 45% — and
	// [briefIsProse] passed it without a murmur, because a repeat loop is words
	// with spaces between them. So the second structural test is about the
	// document rather than the token: prose written by somebody who still had
	// something to say repeats almost nothing, and sixty sits far above every
	// honest brief and far below every degeneration this has been shown.
	//
	// IT IS STRUCTURE AND NEVER CONTENT, which is the law this whole file is held
	// to. Nothing here reads what the brief is about, asks whether it names the
	// domain, or scores it for usefulness — a rule that did would be a rule tuned
	// to one kind of work.
	checkpointBriefVariety = 60

	// checkpointBriefTries is how many times a brief may be asked for before the
	// harness stops paying for one. Two: one draft and one regeneration, because
	// a mastermind that looped once may not loop twice and a mastermind that
	// looped twice is a mastermind that is going to. The fallback ladder underneath
	// ([Agent.handOverRunningTurn]) is what catches the second failure, and it
	// costs nothing.
	checkpointBriefTries = 2

	// checkpointSketchTokens is the sidecar's whole budget. What it is asked for
	// is one line of shape and one sentence naming the letters, and a reader that
	// runs out of room halfway through the shape writes a line the parser reads as
	// ONE PART — a silent CONTINUE from the one call that was supposed to notice
	// width. Three hundred is both halves at any length a sketch of a turn's
	// remaining work honestly takes, and a fraction of the tool round it stands
	// between.
	checkpointSketchTokens = 300

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

	// checkpointHandoffWindow is how long the mastermind gets to WRITE the brief,
	// and it is longer than the sketch's window for the one honest reason: what is
	// being asked for is a document of up to [checkpointBriefTokens] rather than a
	// line of shape, and a bound that fits a sketch would time out most of the
	// documents this exists to produce.
	//
	// It is still a person's patience and not a generous bound. It stands at the
	// END of a turn that has already run to its ceiling — minutes of tool calls —
	// and the answer to missing it is the fallback ladder, which is the draft the
	// running model already wrote, then the person's own sentence. Nothing hangs.
	checkpointHandoffWindow = 90 * time.Second

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
	// the ask, one line per tool call, WHAT CAME BACK FROM THE NEWEST OF THOSE
	// CALLS, what has been written, and the last thing said. Five thousand tokens
	// is more room than any of those three turns actually needed and a fifteenth of
	// what the smallest of them was charged for, which puts a read back at cents.
	//
	// AND IT USED TO CARRY NO RESULTS AT ALL, ON AN ARGUMENT THAT WAS MEASURED
	// FALSE. The argument was that nothing inside a tool result changes the SHAPE
	// of what is left — that the shape is decided by what was asked, what was done
	// and what is still open. On a real ten-hour benchmark run that turned the
	// reader into the one participant who could not see the evidence: the turn had
	// `Loaded 68186 golden test points … Passed: 0` in front of it, had enumerated
	// twelve protocol methods and had ninety-seven compile errors on the screen,
	// and the reader — shown a ledger of verbs and paths — sketched a serial chain
	// three times running. A result is not noise when it is the only statement of
	// how much of the ask is actually discharged. So the results ride, bounded hard
	// (see [checkpointResultBytes]) and newest-first, and the ledger keeps saying
	// what the older calls touched.
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

	// checkpointResultBytes is how much of ONE tool result the reader is shown,
	// and it is the SINGLE SOURCE for that bound — no other file may spell a
	// second one, by the law CLAUDE.md states about a number written twice.
	//
	// FOUR HUNDRED IS A VERDICT AND NOT AN OUTPUT. What a result says about the
	// ask is almost never in its middle: it is `Passed: 0 / 68186`, `97 errors`,
	// `no such file`, the three lines a suite prints after it has run. Enough for
	// those and nowhere near enough for a file's contents, which is what keeps a
	// digest of ninety calls inside [checkpointDigestBytes] with the ledger whole.
	//
	// AND IT IS THE TAIL THAT IS KEPT, from the end backwards. A tool's opening
	// bytes are its preamble — the command echoed back, the header, the first of
	// four hundred matches — and its closing bytes are what it concluded. A digest
	// that kept the head would show the reader that a suite had started and never
	// that it had failed.
	checkpointResultBytes = 400

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
	checkpointDigestAsked = "WHAT WAS ASKED"
	checkpointDigestDone  = "WHAT HAS BEEN DONE SO FAR, ONE LINE PER STEP"
	// checkpointDigestFound heads the results, and its heading SAYS THE ORDER
	// because the order is not the one a reader would assume. The newest call is
	// printed first, so a reader that runs out of attention has spent it on the
	// evidence in front of the turn rather than on the evidence behind it — and
	// the same order is what the fitting drops from, oldest end first.
	checkpointDigestFound   = "WHAT CAME BACK, NEWEST FIRST"
	checkpointDigestWritten = "WHAT HAS BEEN WRITTEN OR CHANGED"
	// checkpointDigestMoved heads ONE LINE: when the work last changed, and what
	// has come back since (novelty.go's [workClock]). It is the fact a reader of
	// a ledger cannot get from the ledger — ninety lines of activity look the
	// same whether the thing being made moved on step three or on step
	// eighty-nine — and it is stated rather than judged, so the reader is left
	// to decide what it means for the ask.
	checkpointDigestMoved = "HOW THE WORK HAS MOVED"
	checkpointDigestSaid  = "THE LAST THING SAID"
)

// checkpointResultArrow joins one call to what came back from it. It is a
// character and not a word for the digest's own reason: a heading that named
// what a result was would be a sentence about the kind of work, and an arrow is
// the same mark the sketches are drawn with.
const checkpointResultArrow = " → "

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
//
// AND IT TEACHES THE SECOND TOKEN FOR THE SAME REASON IT TEACHES THE FIRST.
// `(done)` exists because a reader that was never told how to say "nothing is
// left" says it in a sentence nobody can parse; `(waiting)` exists because a
// reader that was never told that work already handed out is not a part DRAWS IT
// AS ONE. Measured live 2026-09-01: a conversation with four pieces out spent a
// turn watching them, the honest sketch of that turn was "wait for the second |
// wait for the third | wait for the fourth", and three parts is a split — so the
// turn was converted into a task whose whole brief was to review reports and
// accept work that a worker in its own copy cannot see, let alone accept. The
// exclusion is stated in the ask, and the fold that reads it back is
// [checkpointSketch.handsBack].
const checkpointSketchAsk = "[checkpoint] Above is what was asked and what has been done towards it. " +
	"In one line, sketch what remains of the ask as parts and arrows: " +
	"independent parts separated by ' | ', ordered steps joined by ' > '. " +
	"Example shapes: 'A | B | C' or 'A > B > C' or 'A > (B | C)'. " +
	"Nothing else on that line. Then one sentence saying what each letter is. " +
	"If nothing remains, that line is '(done)'. " +
	"Work you have already handed out is not a part: if all that remains is waiting on it, " +
	"reviewing what comes back or accepting it, that line is '(waiting)'."

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

// checkpointBriefingWho is the noun the briefing phase wears on the clock, and
// with the phase's own word in front of it the row reads `briefing a worker`.
//
// IT NAMES THE READER AND NOT THE DOCUMENT. "writing a handoff brief" is the
// harness describing its own paperwork; what a person watching their turn stop
// needs to know is that somebody else is about to take the work and is being
// told what it is. The word is `worker` because that is what this surface calls
// the thing a task runs (task_run.go, and the manual's own pages), and a second
// name for it here would be a third vocabulary for one job.
const checkpointBriefingWho = "a worker"

// ── what the handoff writer is shown, and asked ─────────────────────────────

// The sections of the message [Agent.writeHandoff] puts in front of the
// mastermind. They are SHOUTED and named for what each one IS rather than for
// what it is worth, because the ask below is what says which of them wins.
//
// THE DRAFT IS LAST AND IT IS NAMED AS A DRAFT. It was written by the model that
// has just spent the turn, which is the only reader in the building holding the
// findings — and on the measured run it was also the model that had run out of
// anything to say. A section headed with the word "draft" is a document the
// writer may take from and may throw away; a section headed "the brief" is one it
// will paraphrase.
const (
	checkpointHandoffAskedHeading = "WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS"
	checkpointHandoffStateHeading = "WHERE THE CONVERSATION HAD GOT TO BEFORE THIS TURN"
	checkpointHandoffWorkHeading  = "WHAT THIS TURN ACTUALLY DID, AND WHAT CAME BACK"
	checkpointHandoffDraftHeading = "A DRAFT THE MODEL THAT DID THE WORK WROTE, WHICH MAY BE WRONG OR MAY BE EMPTY"
)

// checkpointHandoffWriteAsk is what the MASTERMIND is asked for, and it demands
// exactly the four things [checkpointHandoffAsk] demands of the draft.
//
// THE CONTRACT IS THE SAME CONTRACT ON PURPOSE. A worker's instruction has one
// shape on this road — what is left, what is already known, what has been ruled
// out, how anybody can tell it is done — and a second door that asked for a
// different four would be two kinds of brief in a graph whose readers cannot tell
// which one they are holding.
//
// AND IT NAMES THE ONE THING IT MAY NOT DO. The person's own words are above it,
// verbatim, and they are what the work is finished against; a writer that
// narrowed the ask down to the piece the turn happened to be holding is the
// measured failure this whole role exists to correct, where a ten-hour ask became
// "make it compile".
//
// IT IS ASKED FOR PROSE AND NOT FOR HEADINGS. The reader of this document is a
// worker opening on it cold, and a brief with four shouted headings over four
// empty sections is a form somebody filled in.
const checkpointHandoffWriteAsk = "[write the handoff] The work above is being handed to somebody who will finish it, and they " +
	"cannot see any of this — not the conversation, not the tool results, not the draft. Write their instruction " +
	"and nothing else: what is left to do, what is already known that they would otherwise have to find out " +
	"again, what has been ruled out, and how anybody could tell when it is done. Finish against the person's own " +
	"words at the top, never against whatever the draft happens to be holding. Do not greet them, do not " +
	"describe this conversation, and do not repeat yourself."

// checkpointRemainsAsk is what the mark's reader is asked AT THE END OF A TURN,
// and it is the other half of defect four: a turn ends, and nothing has ever
// checked whether the ask ended with it.
//
// IT IS THE SAME READER, THE SAME DIGEST AND THE SAME REMAINS CONTRACT as the
// ceiling's ([checkpointNothingLeft]), which is why it is a second ask and not a
// second mechanism. What differs is the shape of the answer: a mark wants a
// DRAWING because the harness parses it for width, and this wants ONE LINE
// because what the harness does with it is hand it back to the running model as
// the thing still to do.
//
// AND IT NAMES NOTHING ABOUT THE KIND OF WORK, by the law [checkpointSketchAsk]
// is held to.
const checkpointRemainsAsk = "[still asked] Above is what the person asked for and what has been done towards it. " +
	"The model working on it has just stopped. In one line, say what of the ASK is still not done. " +
	"If everything they asked for is done, answer with the single line " + checkpointNothingLeft +
	" and write nothing else at all. Otherwise write that one line and nothing else: no preamble, " +
	"no list, no question."

// checkpointCarryOnNote is the ONE line a person reads when a turn stopped and
// the ask had not.
//
// It is the fourth in the register ([checkpointSplitNote], [checkpointCeilingNote],
// task.go's [taskEscalationNote]): an observation, a middle dot, a promise, all
// lowercase, no full stop, no machinery. WHAT IT OBSERVES IS THE ONE HONEST THING
// AT THIS MOMENT — somebody who is not the running model read the ask against the
// work and said the ask is not finished — and what it promises is the only thing
// this door does, which is carry on rather than start anything.
const checkpointCarryOnNote = "the ask is not finished · carrying on rather than stopping here"

// checkpointCarryOnCap is HOW MANY TIMES ONE ASK MAY BE CARRIED ON before the
// harness stops carrying it.
//
// THE MEASURED FAILURE. 2026-08-31, 15:36:25–15:41:45Z: a conversation was
// waiting on GitHub's checks for two pull requests, with a watch of its own
// running over `gh pr checks`. Every turn ended by saying so — "the watch fires
// when the pending count settles; nothing actionable until then" — and every
// turn was read, found unfinished (it WAS unfinished; the checks had not
// landed), and carried on. Twenty times in five minutes, each one a reader call
// and another poll of the same command, for about a third of a dollar and no
// progress at all, until the ceiling converted the wait into a task whose
// acceptance nobody could ever fail.
//
// THE GATE ABOVE ([Agent.turnIsWaitingOnItsOwnWork]) is what that particular
// conversation needed, and this is what every OTHER shape of the same loop
// needs: a reader that answers "not finished" to the same stopped turn three
// times running has stopped being evidence and started being an echo. THREE,
// because one carry-on that lands is the whole feature working, a second is a
// model that needed telling twice, and by the third the reader is repeating
// itself — and because the price of being wrong is small in one direction and
// was a third of a dollar in five minutes in the other.
//
// IT IS COUNTED ON THE TURN'S METER AND NOWHERE ELSE, so it means what its
// name says: a carry-on continues the turn it re-opened (loop.go), the meter
// belongs to that turn, and the count therefore spans exactly one ask and dies
// with it.
const checkpointCarryOnCap = 3

// checkpointCarriedOnSeen bounds how much of what was observed reaches the line
// above. It is a NOTICE and not a brief: the whole of what was read is already in
// the turn the note is about, and a dim one-liner that wraps four times is a line
// nobody finishes.
const checkpointCarriedOnSeen = 200

// checkpointCarriedOnNote is the ONE line a person reads when an ask has been
// carried on as many times as it is going to be.
//
// It is the sixth in the register ([checkpointCarryOnNote] names four of the
// others): an observation, a middle dot, a promise, all lowercase, no full stop.
//
// WHAT IT OBSERVES IS WHAT WAS ACTUALLY READ, and that is the whole of #468's
// fifth law. It used to say "it is still not finished" as a FACT — a claim about
// the work that nobody had taken a reading of, written on a road whose only
// observation was the same unmet set three times over. So it names what the
// reading showed instead ([Decision.Observed]: the items the checks and the
// landings left unmet, or the reader's own line), and lets whoever is reading the
// note judge it.
//
// It is a function and not a constant because the number in it is
// [checkpointCarryOnCap] and a number written twice is a number that will drift.
func checkpointCarriedOnNote(observed []string) string {
	// NOTHING OBSERVED IS SAID AS NOTHING OBSERVED. It is the shape a session
	// with no second model to read with would reach if it ever got here, and a
	// note that invented a reason on its behalf would be the sentence this
	// function was rewritten to remove.
	seen := clip(strings.Join(observed, "; "), checkpointCarriedOnSeen)
	if seen == "" {
		return fmt.Sprintf("carried on %d times and nothing was read back · stopping here rather than carrying on again",
			checkpointCarryOnCap)
	}
	return fmt.Sprintf("carried on %d times · the last reading showed: %s · stopping here rather than carrying on again",
		checkpointCarryOnCap, seen)
}

// checkpointCarryOnLead opens the synthetic continuation the running model is
// handed, and it is the reader's line that follows it.
//
// IT SAYS WHO IS SPEAKING, because the alternative is a model reading an
// instruction in the person's lane that the person did not type and answering it
// as though they had — "you asked me to…" over a sentence nobody said. This is
// the harness's own line and it says so, in the same plain register the loop
// detector's notes use.
//
// AND ITS MARKER IS ITS OWN. It opened with [checkpointRemainsAsk]'s marker for
// exactly one measured minute: the continuation then sat at the tail of the very
// next request, and anything reading a request's last message to tell the ask
// apart from the answer read the continuation as the question. Two lanes, two
// markers.
const checkpointCarryOnLead = "[carry on] You stopped, but what was asked is not finished. " +
	"Somebody reading the work against the request says this is what is left. " +
	"Carry on with it, and do not summarise what you have already done:\n"

// ── the meter ───────────────────────────────────────────────────────────────

// checkpointMeter is one turn's running cost and how much of the ladder it has
// climbed. It belongs to the turn and is built by it, for the reason the loop
// detector's window belongs to the turn (hooks.go): what this counts is a fact
// about ONE answer, and a meter that remembered yesterday's rounds would move
// work out of a conversation on the strength of a conversation that already
// ended.
type checkpointMeter struct {
	// rounds is how many tool batches this turn has FINISHED AS WORK.
	rounds int
	// watched is how many of this turn's batches did nothing but LOOK at work the
	// conversation already has out ([roundWasWatching]), and it is here so that
	// what a turn spent waiting is a fact somebody can read rather than a gap in
	// the count. It prices nothing: see [checkpointMeter.round].
	watched int
	// carriedOn is how many times this turn has already been re-opened by
	// [Agent.checkpointReopen], and it is what [checkpointCarryOnCap] bounds.
	//
	// IT LIVES HERE FOR THE REASON EVERYTHING ELSE ON THIS METER DOES: it is a
	// fact about ONE answer. A counter that remembered yesterday's carry-ons
	// would refuse to carry on a conversation that had never asked for it.
	carriedOn int
	// marks is how many of the ladder's rungs have already fired.
	marks int
	// shareSpent says the wall's share has already opened its door in this turn,
	// so that a handover the two minds declined is not asked for again at every
	// boundary after (turnwall.go's [Agent.pastTurnWallShare] claims it).
	//
	// IT LIVES ON THIS METER BECAUSE THE THING IT LATCHES IS A FACT ABOUT ONE
	// TURN, which is what everything else here is: the share bounds the stretch
	// ONE answer spends inline, and a latch that outlived the turn would let a
	// session's second long turn run to the wall unwatched.
	shareSpent bool
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

// checkpointWatchTools are the belt's two windows onto work this conversation
// ALREADY HAS OUT: the task rail and the background jobs. They are the only two
// there are — each is deliberately the single vocabulary for its kind of thing
// (tools_tasks.go, tools_jobs.go) — so a batch drawn entirely from them is a
// batch that touched nothing but work that is already running somewhere else.
//
// IT IS THE TOOL AND NOT THE ARGUMENT, which is a decision rather than a
// shortcut. Steering a running piece and stopping one are the conversation's own
// verbs exactly as looking at one is: a worker in its own copy cannot steer a
// sibling either, so a turn that spent its rounds doing those has no more to hand
// out than a turn that spent them reading. Reading arguments here would buy a
// second parser and change no answer.
var checkpointWatchTools = map[string]bool{
	"tasks": true,
	"jobs":  true,
}

// roundWasWatching reports that a finished batch did nothing but look at work
// this conversation already has out.
//
// A BATCH WITH NO CALLS IN IT IS NOT ONE OF THESE. The other road into the meter
// is a turn that has already stopped ([Agent.checkpointReopen]), which has no
// batch at all, and reading its silence as watching would exempt from the ladder
// the one shape that reaches it.
func roundWasWatching(calls []ai.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !checkpointWatchTools[call.Function.Name] {
			return false
		}
	}
	return true
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
func (m *checkpointMeter) round(worked bool) int {
	if m == nil {
		return 0
	}
	// AND WAITING IS NOT WORKING, WHICH IS THE RUNNER'S OWN LAW SAID ON THE OTHER
	// SIDE OF THE TREE.
	//
	// A node that hands its work out and waits on the reports is PARKED: its
	// deadline is pushed by exactly the parked time and its no-progress counter
	// starts again, because a stretch in which it made no request and took no step
	// is not a stretch of the thing the deadline bounds (task_child_run.go's
	// [childRun.park]). A conversation cannot park — nobody hands it a lane — so
	// the same law has to be written in the unit this meter counts in, and a round
	// is that unit: a batch that only looked at work already out is not a batch of
	// work, so it does not move the ladder and the marks stand exactly as far
	// ahead as they did before it.
	//
	// WITHOUT IT A PERSON WHO SAYS "KEEP AN EYE ON THOSE" MANUFACTURES A
	// CONVERSION. Measured live 2026-09-01: a conversation with four pieces out
	// spent a turn reading their logs, crossed a mark on the strength of the
	// reading alone, and was converted into a task twice over — about fifteen
	// minutes of a worker that could act on none of it.
	if !worked {
		m.watched++
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
	// handsBack says the drawing is THE CONVERSATION'S OWN COORDINATION and not
	// work anybody else could take: waiting on pieces that are already out,
	// reading what they report, accepting them. See [checkpointHandBack].
	handsBack bool
}

// split reports the one decision this file takes off a sketch.
//
// AND COORDINATION IS NEVER A DIVISION, WHICH IS THE ONE THING THE COUNT CANNOT
// SEE. Three parts is three pairs of hands only where the parts are work; "wait
// for the second | wait for the third | wait for the fourth" is three parts by
// the separator and NO hands at all, because every one of them is a verb this
// conversation owns and a worker in its own copy does not have. The refusal is
// stated once, here, so that everything downstream agrees with it: the mark that
// would convert the turn ([Agent.checkpointRound]), the head of the brief
// ([checkpointSketch.head]) and the division a drawing proposes to a worker
// (task_divide_sketch.go's [drawnDivision.proposes]) are three readings of this
// one answer.
func (s checkpointSketch) split() bool {
	return s.parts >= checkpointSketchParts && !s.handsBack
}

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

// carryOnDecision is what a sketch that did not split WROTE ITSELF DOWN AS
// (sessionfile.go's [journalMark]).
//
// A carry-on and a hand-back are two different things that happen to end the same
// way, and a file that spelled them alike could not tell a turn that is one long
// job from a turn that was only ever watching its own pieces — which is the
// reading the incident this exists for had to be reconstructed from provider logs
// to establish. So the refusal is written down, exactly as `(done)` is written
// down, rather than being a branch that quietly returns false.
func (s checkpointSketch) carryOnDecision() string {
	if s.handsBack {
		return checkpointDecisionWaiting
	}
	return checkpointDecisionContinue
}

// ── the conversation's own verbs ────────────────────────────────────────────
//
// THE LAW: WHAT A CONVERSATION DOES TO WORK IT ALREADY HANDED OUT IS NOT WORK IT
// CAN HAND OUT AGAIN.
//
// A worker runs in a copy of its own with a brief and a budget. It cannot see
// another piece's report, it cannot accept anything on anybody's behalf, and it
// cannot wait for something it was never told about — so a part that asks for any
// of those is a part with nobody to give it to. Handing one over buys a worker
// that opens its brief, finds nothing it can act on, and spends its whole
// deadline failing to.
//
// THE READING IS THE SHAPE'S OWN AND NOT A RULE ABOUT ENGLISH, as far as a
// vocabulary can be: it folds the two places a drawing puts a coordination verb —
// the word a part OPENS on, and a last stage that is a BARE verb with no object —
// and it folds nothing else. `review the manuscript` is work and reads as work,
// because the verb has something after it; `the second report > review` is a
// hand-back, because the stage the drawing ends on names nothing to review that
// this conversation is not already holding. It is the same fold
// [checkpointDoneShapes] makes of `(done)`, applied per part.
//
// AND IT TAKES ALL THE PARTS OR NONE. One coordination part beside two real ones
// is a turn with real work in it, and refusing that split would cost the road the
// very turns it was built for. Only a drawing that is coordination THROUGHOUT has
// nothing to hand out.

// checkpointWaitWords are the words a part that is a WAIT opens on. A part
// beginning with one of these is a hand-back however it goes on, because there is
// no reading of "wait for X" that is work somebody else can be given.
var checkpointWaitWords = map[string]bool{
	"wait":     true,
	"waits":    true,
	"waiting":  true,
	"awaiting": true,
	"pending":  true,
}

// checkpointHandBackVerbs are the verbs that, STANDING ALONE as the last stage of
// a part, name something only this conversation can do. The list is short for
// [checkpointDoneShapes]'s reason: everything not on it is work, and the failure
// direction here is to refuse a division that was real.
var checkpointHandBackVerbs = map[string]bool{
	"accept":  true,
	"approve": true,
	"check":   true,
	"review":  true,
}

// checkpointHandBack reports that a whole drawing is the conversation's own
// coordination. It is the one place that decides it.
func checkpointHandBack(shape string) bool {
	parts := readShape(shape).parts
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !partHandsBack(part) {
			return false
		}
	}
	return true
}

// partHandsBack reads ONE part of a drawing, in the two places a coordination
// verb lands: the word the part opens on, and a last stage that is a bare verb.
func partHandsBack(part string) bool {
	stages := splitAtTopLevel(unbracket(strings.TrimSpace(part)), '>')
	if len(stages) == 0 {
		return false
	}
	if opening := normalizedWords(stages[0]); len(opening) > 0 && checkpointWaitWords[opening[0]] {
		return true
	}
	last := normalizedWords(stages[len(stages)-1])
	return len(last) == 1 && (checkpointWaitWords[last[0]] || checkpointHandBackVerbs[last[0]])
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
		// THE COORDINATION READING IS TAKEN HERE, ONCE, beside the count it
		// qualifies — so no reader downstream can hold a count without the answer
		// that says what the count is worth ([checkpointSketch.split]).
		handsBack: checkpointHandBack(shape),
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

// markReaderAbsent reports that THIS INSTALL HAS NOBODY TO READ A MARK WITH, and
// writes that down once for the session.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN (CLAUDE.md), and this road
// had it exactly the wrong way round. [roles.RoleMarkReader] answers to the crew
// alone, so a profile with no mastermind resolves to nothing at all — and every
// mark and every end-of-turn reading still made the call, failed in under two
// milliseconds, and journaled itself as a mark that FAILED. A run under one model
// wrote that row on every round of an evening, which reads in the file exactly
// like a mastermind that was there and could not be reached (#468).
//
// IT ASKS THE LADDER THE SAME QUESTION [Agent.callRole] IS ABOUT TO ASK IT, in
// the same words, so the two can never disagree about whether there is a reader:
// the pin, then the tier, then the floor — which is empty for a crew-only caller
// unless `--one-model` put the conversation's own model there (auxiliary.go).
// A session with no client at all is the same answer for the other reason.
func (a *Agent) markReaderAbsent() bool {
	a.mu.Lock()
	source, client, floor := a.config.RolesSource, a.client, ""
	if a.config.OneModel {
		floor = a.model
	}
	a.mu.Unlock()
	if client != nil {
		if _, err := roles.Ladder(roles.Source(source), roles.RoleMarkReader, floor); err == nil {
			return false
		}
	}
	a.noteReaderAbsent()
	return true
}

// noteReaderAbsent writes the absence into the journal ONCE FOR THE SESSION.
//
// ONCE, because it is a fact about the INSTALL and not about this round: a line
// per round would be the same failed-mark spam the absence was found in, wearing
// an honest word. The row carries no model, no cost and no duration, which is the
// emptiness law doing the rest of the work — there was no call, so there are no
// figures about one.
func (a *Agent) noteReaderAbsent() {
	a.mu.Lock()
	if a.readerAbsentNoted {
		a.mu.Unlock()
		return
	}
	a.readerAbsentNoted = true
	file := a.file
	a.mu.Unlock()
	file.appendMark(journalMark{Decision: checkpointDecisionNoReader})
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
	// A READER THAT CANNOT WORK IS ABSENT, NOT FAILING, so nothing is attempted.
	if a.markReaderAbsent() {
		return checkpointRead{}
	}
	// A REDIRECT AFTER ESC ALREADY SAYS WHAT CHANGED. Re-reading the
	// conversation to sketch what is left is the 57-message stall (F13): the
	// person's next words are the decision, and a second mind asked to
	// rediscover them is the duplicate planner pass.
	if a.interrupt.redirecting() {
		return checkpointRead{}
	}
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
	// AND THE PERSON IS TOLD WHAT THIS SILENCE IS, because it is one: a turn stops
	// mid-round, a mastermind is shown an account of the work and asked what is
	// left of the ask, and the reading is bounded at [checkpointSketchWindow] —
	// ten to thirty seconds on the measured runs, with nothing whatever drawn for
	// it until now.
	//
	// `taking stock` AND NOT `checking`, which is the phase the gates at the end
	// of a turn wear (loop.go). Those read an ANSWER and decide whether it
	// finished; this reads the whole ask against everything that has been done, in
	// the middle of the work, and the two waits mean different enough things that
	// spelling them the same way would be the surface saying one sentence for two
	// stages. The stage lasts the length of the call and the clock counts the
	// whole of it, because a held phase says itself again while it lasts
	// (phasenews.go).
	//
	// AND IT COMES OFF ON EVERY WAY OUT — the reading that answered, the one that
	// failed, the one the window cut short — which is what the defer is for.
	a.tellPhase(provider.PhaseTakingStock, "", began)
	defer a.endPhase()
	response, reader, err := a.callRole(ctx, roles.RoleMarkReader, "", messages,
		ai.WithMaxTokens(checkpointSketchTokens))
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
	read.asDrawn = read.sketch.shape
	// AND WHAT THIS CONVERSATION IS STILL HOLDING COMES OUT OF THE DRAWING HERE,
	// once, in front of every decision anybody takes off it.
	//
	// THREE ROADS READ THIS ONE SKETCH — the mark that would split a turn, the
	// ceiling, and the write seam — and a reduction made further down would have
	// been a harness that refused to convert a turn on a drawing and then handed
	// the same drawing to a worker, which is two answers to one question. The
	// reader's own line is kept above for the journal, which records what was
	// drawn and never what was done with it (checkpoint_custody.go).
	//
	// AND THE LEDGER IS READ ONCE AND RIDES BACK, which is the one-source-of-truth
	// law in the one place it is easy to break twice: a road that asked the graph
	// again further down would be deciding the brief against a ledger the drawing
	// was never reduced against, and a piece that settled in the seconds between
	// the two reads would leave a drawing missing a part for work that is now
	// back. One question, one answer, carried.
	read.held = a.piecesStillOut()
	read.sketch, read.ownRemainder = read.sketch.withoutHeldWork(read.held)
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
	sketch checkpointSketch
	// asDrawn is the shape the reader ANSWERED WITH, before anything this
	// conversation is still holding was taken out of it. The journal writes this
	// one: a file that recorded the reduced drawing could not tell a sidecar that
	// drew one job from a sidecar that drew four and had three of them withheld.
	asDrawn string
	// ownRemainder is the part of the drawing that STAYS WITH THIS CONVERSATION:
	// the parts about work it is still holding, written back out in the order they
	// were drawn (checkpoint_custody.go's [checkpointSketch.withoutHeldWork]). It
	// is empty on every read that withheld nothing, which is nearly all of them.
	ownRemainder string
	// held is the ledger the reduction above was taken against
	// ([Agent.piecesStillOut]), carried so that every later reading on this road
	// answers to the same one. See [Agent.readMark].
	held    []heldPiece
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
		Sketch:     read.asDrawn,
		Kept:       read.ownRemainder,
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
	// checkpointDecisionNoReader is the ONE row a session with no second model
	// writes ([Agent.noteReaderAbsent]). It is a different fact from `failed` and
	// the file has to be able to tell them apart: one is a reader that could not
	// be reached, the other is an install that never had one.
	checkpointDecisionNoReader = "no reader"
	// checkpointDecisionWrote is the write seam's own word (writeseam.go). It is
	// distinct from the ceiling's because the two moments are different facts
	// about a turn — one outran the reading, one outran the small edit — and a
	// bench that spelled them the same could not tell them apart afterwards.
	checkpointDecisionWrote = "wrote"
	// checkpointDecisionWaiting is what a mark wrote down when the drawing it read
	// was the conversation's own coordination ([checkpointSketch.handsBack]). It is
	// distinct from `continue` because the two are different facts about a turn —
	// one is one long job, one is a turn watching pieces it already handed out —
	// and only the second says a conversion was REFUSED rather than never earned.
	checkpointDecisionWaiting = "waiting"

	checkpointCeilingMoved   = "moved"
	checkpointCeilingNothing = "dropped:nothing-left"
	checkpointCeilingNoBrief = "dropped:no-brief"
	// checkpointCeilingStopped is the ending a handover takes when the session's
	// own goal owner read it and STOPPED THE RUN rather than let the work move
	// ([Agent.endTurnUnderSteward]). It is spelled apart from the three above
	// because it is a different fact: the road did not fail to hand anything
	// over, it was told not to — and the steward's own `decided` row beside it
	// says why.
	checkpointCeilingStopped = "dropped:stopped"
	// checkpointCeilingDone is the ending a handover takes when the session's
	// principal read the ending and said the ask is finished: the turn is sealed
	// there and no task is started ([Agent.endTurnUnderSteward]).
	checkpointCeilingDone = "dropped:done"
	// checkpointCeilingHeldWork is the road that HAD a page and found it was
	// somebody else's already: every rung of the brief ladder, the person's own
	// sentence included, was about work this conversation is still holding
	// (checkpoint_custody.go). It is spelled apart from `dropped:no-brief` because
	// the two send whoever reads the file to different places — one to a writer
	// that produced nothing, this one to a turn that was only ever coordinating.
	checkpointCeilingHeldWork = "dropped:work-already-out"
	// checkpointCeilingTrivial is the spawn floor (spawnfloor.go): the ask
	// itself is one command, so nothing moves, whatever the work has cost.
	checkpointCeilingTrivial = "dropped:trivial-ask"
)

// THE LAW: ONE ENDING ROW PER ENDING, WRITTEN AT THE SEAM THAT TOOK IT.
//
// The row used to be written by the CEILING alone, which was true to its name and
// false to the file: a handover the mark road or the write seam declined wrote no
// decision word anywhere, and the real-model runs behind #567 all ended with no
// ending row at all while the refusal had plainly happened. So the row is written
// where the ending is DECIDED ([Agent.handOverRunningTurn]) and not by the clock
// that noticed, and these say which of the three doors it came through.
//
// A ROW WITH NO SEAM PREDATES THIS and is a ceiling by construction, because the
// ceiling was the only writer (sessionfile.go's [journalCeiling]).
//
// AND THE WALL'S SHARE IS A FOURTH DOOR (turnwall.go), spelled apart from the
// other three for the same reason they are spelled apart from each other: a bench
// reading the file has to tell a turn the clock moved from a turn the counters
// moved, and it can only do that if the row says so.
const (
	checkpointSeamMark    = "mark"
	checkpointSeamWrite   = "write"
	checkpointSeamCeiling = "ceiling"
	checkpointSeamWall    = "wall"
)

// ── the carry ladder ────────────────────────────────────────────────────────
//
// THE LAW: A FALLBACK THAT CHANGES WHAT A WORKER IS STARTED ON IS AN EVENT, NOT
// A DEFAULT.
//
// THE MEASURED FAILURE. SWE-Marathon s4, 00:01:54Z. The round-40 ceiling ran this
// ladder. The running model's draft came back as seven tokens nobody could work
// from; the mastermind that writes the real brief was asked and never answered,
// and its call was cut by [checkpointHandoffWindow] exactly ninety seconds later.
// Both upper rungs returned "", the task opened on the person's raw request, and
// the worker then spent twelve minutes and seventy calls re-deriving what the
// chat already held. The same run's s2 twin ran the same ladder in 1.5 s and its
// task opened on a 3.5 KB account of what had been learned.
//
// AND NOTHING WHATEVER WAS WRITTEN DOWN. The two runs left IDENTICAL journals at
// that moment — one ceiling line reading `moved` — so the difference between a
// worker that knew everything and a worker that knew nothing existed in no file.
// The failure itself was silent twice over: [Agent.writeHandoff] answered "" for
// six different reasons with one spelling, and the errand underneath returned on
// its own dead deadline before the row that would have named it was written
// (auxiliary.go's [Agent.callRole]).
//
// So EVERY RUNG SAYS WHAT IT DID, on its own journal line ([journalCarry]), and
// the ceiling line names the rung that supplied the brief. A move that can carry
// nothing but the ask is still allowed to proceed — a blind worker beats a
// stalled chat — but it says so, in the file and to the person.
const (
	// The three rungs, in the order they are tried: the document a second mind
	// wrote out of the turn, the draft the running model managed, and the
	// person's own sentence, which is the floor and is never written by anybody.
	carryRungHandoff = "handoff"
	carryRungDraft   = "draft"
	carryRungAsk     = "ask"

	// What one rung DID. `written` is words somebody could work from;
	// `degenerate` is words that were not words, or words that had stopped
	// saying new things; `failed` is a rung whose call did not come back, and
	// its reason is the provider's own sentence; `skipped` is a rung that was
	// never asked, and its reason says why; `nothing-left` is the remains
	// contract answered instead of a brief; `empty` is a rung with nothing on
	// it at all.
	carryWritten     = "written"
	carryDegenerate  = "degenerate"
	carryFailed      = "failed"
	carrySkipped     = "skipped"
	carryNothingLeft = "nothing-left"
	carryEmpty       = "empty"
)

// What the JOURNAL is told when the reason is the harness's own and not a
// provider's. A provider that refused says so in its own words and those are
// written instead ([carryFault]).
const (
	carryNoPage      = "no ask, no account and no draft to write from"
	carryNotProse    = "what came back was not words anybody could work from"
	carryStillLoops  = "the writer was asked twice and looped both times"
	carryNoSentence  = "the person's own words were empty"
	carryDraftLooped = "the draft had stopped saying new things"
	carryHeldWork    = "it assigned work this conversation is still holding"
	carryRedirect    = "a redirect after stop does not re-read the conversation"
)

// And what the PERSON is told, which is the same fact in the register every dim
// one-liner this harness writes is held to (CLAUDE.md's vocabulary law). The
// provider's sentence goes in the journal and never here: a person owed one
// short reason is not owed an upstream's error body.
const (
	carrySaidTooSlow        = "the second model did not answer in time"
	carrySaidUnreachable    = "the second model could not be reached"
	carrySaidNothingNew     = "the second model had nothing new to say"
	carrySaidNoMaterial     = "there was nothing to write it from"
	carrySaidWorkAlreadyOut = "it was written about work that is already out"
	// AND A ROLE WITH NO MODEL IS NOT A WIRE EVENT. A tier nobody filled in and a
	// session with no client are configuration, not silence on a socket, and
	// telling somebody the model could not be reached sends them to look at their
	// network for a row they never wrote (#443).
	carrySaidNoSecond = "no second model is set"
)

// carryAskOnlyNote is what is added to the line a person reads when the move
// carried NOTHING BUT THEIR OWN SENTENCE.
//
// It is a suffix rather than a line of its own because it is the same event: the
// person is being told their turn was moved, and how much went with it is part of
// that sentence and not a second announcement. It keeps the register of the line
// it extends — lowercase, middle dot, no full stop — and it names no machinery:
// "the second model" is what the manual already calls the reader on this road.
const carryAskOnlyNote = " · carrying the ask only — the brief could not be written: "

// carryStep is what ONE rung of the ladder did, on its way to a journal line.
//
// It carries TWO reasons on purpose. reason is for the file and is as specific as
// the failure allows — a provider's own sentence where there was one — because
// the question an autopsy asks is why THIS rung produced nothing. said is for the
// person and is one of a fixed few plain phrases, because the question they are
// answering is whether their work went somewhere knowing anything.
type carryStep struct {
	rung    string
	outcome string
	reason  string
	said    string
	chars   int
	used    bool
}

// carryFault turns one failed call into the two reasons a rung owes.
//
// THE PROVIDER'S WORDS WHERE THERE ARE ANY, exactly as a failed call's own
// journal row takes them (loop.go's [Agent.journalFailedCall]). A DEADLINE IS
// TOLD APART FROM A REFUSAL because on the measured run it was the deadline —
// [checkpointHandoffWindow] elapsed with no answer — and "could not be reached"
// would have sent whoever read the line looking at the wrong thing.
//
// AND A ROLE WITH NO MODEL IS TOLD APART FROM BOTH, for the same reason twice
// over: [roles.ErrNoModel] is a tier nobody filled in and [errNoCompleter] is a
// session with no client, and neither of them is a wire that failed. The measured
// run under `--one-model` had exactly this — no rung to call at all — and read as
// an unreachable provider, which is the wrong thing to go and check (#443).
//
// The journal keeps err.Error() throughout: the file is where the exact sentence
// belongs, and `roles: no model for role "handoff"` is the row that names it.
func carryFault(err error) (reason, said string) {
	if err == nil {
		return carryNotProse, carrySaidNothingNew
	}
	said = carrySaidUnreachable
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		said = carrySaidTooSlow
	case errors.Is(err, roles.ErrNoModel), errors.Is(err, errNoCompleter):
		said = carrySaidNoSecond
	}
	reason = err.Error()
	if refusal, ok := provider.RefusalFrom(err); ok {
		if words := strings.TrimSpace(refusal.Message); words != "" {
			reason = words
		}
	}
	return clip(reason, errorRowMessage), said
}

// journalCarryLadder writes the whole ladder down, in the order it was tried.
//
// ONE LINE PER RUNG AND NEVER A SUMMARY, because the rungs are separate facts: a
// handoff that faulted and a draft that looped are two different things to fix,
// and a single line naming only the winner would say neither. The rung that
// supplied the brief carries `used`, which is how a mark's split — the other door
// into [Agent.handOverRunningTurn], and one that writes no ceiling line — still
// records what its worker opened on.
func (a *Agent) journalCarryLadder(ladder []carryStep) {
	for _, step := range ladder {
		a.file.appendCarry(journalCarry{
			Rung:    step.rung,
			Outcome: step.outcome,
			Reason:  step.reason,
			Chars:   step.chars,
			Used:    step.used,
		})
	}
}

// carryLine is the line the person reads, with the truth about how much went with
// their work on the end of it when that truth is "almost nothing".
//
// IT SAYS SOMETHING ONLY WHEN THE ASK IS ALL THERE IS. A brief written by the
// second model and a draft written by the first are both a worker that starts
// knowing what the turn found out, and a person does not need to be told which of
// two documents it was. A worker starting on their bare sentence is a different
// event and reads as one.
func carryLine(line, carried string, top carryStep) string {
	if carried != carryRungAsk {
		return line
	}
	said := top.said
	if strings.TrimSpace(said) == "" {
		said = carrySaidNoMaterial
	}
	return line + carryAskOnlyNote + said
}

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
//     says what it touched. It is the COMPLETE account — ninety calls at eighty
//     bytes is seven thousand — and it is fitted before the results for that
//     reason: a reader that has lost the far end of the history has lost less than
//     a reader that has lost the newest thing it learned.
//   - WHAT CAME BACK, from the newest calls, each clipped to
//     [checkpointResultBytes] and printed newest-first. This is where the evidence
//     is. A ledger says a suite was run; a result says it reported `Passed: 0`,
//     which is the difference between a reader that can subtract the finished part
//     of the ask and a reader guessing at it.
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
	ledger, written, results, moved := checkpointLedger(messages)

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
	// IT RIDES IN THE TAIL, which is the section fitted before the ledger, so the
	// one line that says whether the work is moving cannot be squeezed out by
	// ninety lines that say it was busy.
	if line := moved.digestLine(); line != "" {
		tail.WriteString(checkpointDigestMoved)
		tail.WriteString("\n")
		tail.WriteString(line)
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
		kept, dropped := checkpointNewestThatFit(ledger,
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
	// AND THE RESULTS TAKE WHAT ROOM IS LEFT, WHICH IS THE WHOLE OF THE PRIORITY
	// RULE AND IT IS AN ORDER OF EVICTION RATHER THAN AN OPINION.
	//
	// The boundary sections are fitted first, as they always were. Then the ledger,
	// which is bounded per line and is the complete account of what was touched.
	// Then the results, newest-first, into whatever is left — so a digest under
	// pressure DROPS THE OLDEST RESULTS FIRST, and only a digest whose ledger alone
	// cannot fit goes on to drop the oldest ledger lines. Nothing can push out the
	// ask, which is the one thing on this road nobody may rewrite.
	//
	// A HEADING WITH NOTHING UNDER IT IS NOT WRITTEN AT ALL, so a turn whose
	// results were all squeezed out says nothing about results rather than heading
	// an empty section.
	if len(results) > 0 {
		elision := func(dropped int) string {
			return fmt.Sprintf("… and %d earlier results\n", dropped)
		}
		room := checkpointDigestBytes - out.Len() - tail.Len() -
			len(checkpointDigestFound) - len("\n\n") - len(elision(len(results)))
		kept, dropped := checkpointNewestThatFit(results, room)
		if len(kept) > 0 {
			out.WriteString(checkpointDigestFound)
			out.WriteString("\n")
			for index := len(kept) - 1; index >= 0; index-- {
				out.WriteString(kept[index])
				out.WriteString("\n")
			}
			if dropped > 0 {
				out.WriteString(elision(dropped))
			}
			out.WriteString("\n")
		}
	}
	out.WriteString(tail.String())
	// The final clip is a backstop and not the policy: the fitting above is what
	// keeps the sections whole, and this is what guarantees the bound whatever a
	// future section does.
	return clip(strings.TrimSpace(out.String()), checkpointDigestBytes)
}

// checkpointNewestThatFit keeps the NEWEST lines that fit in room, and reports
// how many older ones it dropped. Room that is gone already keeps nothing, which
// is the honest answer rather than one line over the bound.
//
// IT SERVES BOTH GROWING SECTIONS — the ledger and the results — because the
// eviction rule is one rule: the far end of a turn is history a reader can infer,
// and the near end is the work in front of it. Two copies of this arithmetic
// would be two answers to the question of what a digest drops first.
func checkpointNewestThatFit(lines []string, room int) ([]string, int) {
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

// checkpointLedger walks the transcript once and answers the three questions the
// digest asks of it: what was DONE, one line per tool call; what CAME BACK from
// each of those calls; and what of it was WRITTEN.
//
// The three come out of one pass because they are one fact read three ways — a
// file written is a `write` in the ledger and a confirmation in the results — and
// because a second walk could disagree with the first the day a tool is renamed.
//
// A RESULT IS TIED TO ITS CALL BY ID AND NEVER BY POSITION. A batch's results are
// recorded in the order the calls were issued rather than the order they finished
// (loop.go), a warm call may have started a step early, and a turn that was
// interrupted mid-batch has calls with no result at all. So a result whose id
// names no call this walk has seen is dropped rather than attached to whichever
// line happens to be beside it: a digest that credited one tool's output to
// another tool's line would be evidence that is worse than none.
func checkpointLedger(messages []ai.Message) (ledger, written, results []string, moved workClock) {
	seen := make(map[string]bool)
	// The clock is folded in on the same walk and for the same reason the other
	// three are: it is the same facts read a fourth way — a call is a step, a
	// writer's call is the work moving, a result is lines that were or were not
	// new (novelty.go).
	lines := newLineNovelty()
	// The step each call took, by id, so a result that arrives after a later
	// batch's write is not counted against it.
	at := make(map[string]int)
	// The line each call wrote, by id, so its result can be printed under the same
	// words the ledger used and a reader can match the two.
	calls := make(map[string]string)
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
			moved.step()
			if id := strings.TrimSpace(call.ID); id != "" {
				calls[id] = line
				at[id] = moved.steps
			}
			if !checkpointWriters[name] {
				continue
			}
			path := checkpointArgumentNamed(call.Function.Arguments, "path")
			if path == "" {
				continue
			}
			// THE CLOCK MOVES ON EVERY WRITE AND THE LIST ONLY ON A NEW NAME. A
			// file written for the third time is one entry under WHAT HAS BEEN
			// WRITTEN and three separate moments at which the work changed.
			moved.wrote()
			if seen[path] {
				continue
			}
			seen[path] = true
			written = append(written, path)
		}
		if message.Role != "tool" {
			continue
		}
		id := strings.TrimSpace(message.ToolCallID)
		line, known := calls[id]
		if !known {
			continue
		}
		var came strings.Builder
		for _, part := range message.Content {
			came.WriteString(part.Text)
		}
		if at[id] > moved.changedAt {
			fresh, weighed := lines.measure(line, stripJobFooter(came.String()))
			moved.read(fresh, weighed)
		}
		if tail := checkpointResultTail(came.String()); tail != "" {
			results = append(results, line+checkpointResultArrow+tail)
		}
	}
	return ledger, written, results, moved
}

// checkpointResultTail is the END of what one call returned, bounded by
// [checkpointResultBytes] and marked where it was cut.
//
// IT CUTS FROM THE FRONT, which is the opposite of [clip] and is the whole point:
// what a tool concluded is in its last lines, and a result kept from the head
// would show a reader that a suite had started and never that it had failed. The
// cut walks forward to a rune boundary for [clip]'s reason — a string cut through
// a multi-byte character is not a string anybody can read.
//
// A RESULT THAT SAID NOTHING PRODUCES NO LINE, by the emptiness law: a heading
// pointing at an empty arrow is an invitation to answer about the emptiness.
func checkpointResultTail(came string) string {
	came = strings.TrimSpace(came)
	if len(came) <= checkpointResultBytes {
		return came
	}
	cut := len(came) - checkpointResultBytes + len("…")
	for cut < len(came) && !utf8RuneStart(came[cut]) {
		cut++
	}
	return "…" + came[cut:]
}

// checkpointWriters are the verbs on this belt that CHANGE something a person
// keeps (internal/exec/bare). Everything else on the belt reads, searches, looks
// or asks, and a ledger line is all the account those need.
//
// IT IS A SET AND NOT A GUESS AT NAMES. A tool renamed here falls out of the
// written section and stays in the ledger, which is a digest that says less
// rather than one that says something untrue.
var checkpointWriters = map[string]bool{"write": true, "edit": true}

// checkpointArgument is the one clipped thing a ledger line says about a call,
// and IT KNOWS NO TOOL'S NAME FOR ANYTHING.
//
// IT USED TO BE A LIST OF KEYS — command, query, pattern, path, url — and the
// list was measured being the wrong shape of rule. `fork` (fork.go) takes
// `parts`, so a turn that had already fanned out twice was drawn in the digest as
// two bare lines reading `fork`, and the reader sketched serial work over the top
// of a turn that was demonstrably already parallel. A list of anticipated keys is
// a list that is wrong about every verb added after it was written, silently, in
// the one document a second mind reads the turn out of.
//
// SO IT READS THE ARGUMENTS AS THEY CAME AND TAKES THE FIRST THING THAT SAYS
// ANYTHING: the first string, or the first array rendered as its elements. Wire
// order rather than a preference, because the order a model writes its arguments
// in IS the order it thinks about them — a search leads with its pattern and a
// read with its path — and a harness cannot know the order for a verb it has
// never seen.
//
// AND IT STILL CANNOT CARRY A PAYLOAD, which is the one thing the old list bought
// that had to be kept. The first pass skips any argument that is longer than the
// bound a ledger line is held to, so `write`'s content — the argument this digest
// exists to leave out — never wins over the path beside it, whichever order they
// arrive in. Only if nothing shorter says anything does the second pass take the
// long one, clipped; and a call whose arguments are an object of numbers and
// booleans falls through to the compacted JSON, so a verb nobody anticipated is
// described badly rather than not at all.
func checkpointArgument(arguments string) string {
	if value := checkpointFirstArgument(arguments, true); value != "" {
		return value
	}
	if value := checkpointFirstArgument(arguments, false); value != "" {
		return clip(value, checkpointLedgerBytes)
	}
	return clip(strings.Join(strings.Fields(arguments), " "), checkpointLedgerBytes)
}

// checkpointFirstArgument walks one call's arguments IN THE ORDER THEY WERE
// WRITTEN and answers the first value that says what is being touched.
//
// A decoder rather than a map, because a map has no order and the order is the
// whole of the rule above. Everything that is not an object — arguments a model
// sent as a bare string, as an array, as nothing at all — answers "" and lets the
// caller fall through to the compacted form.
//
// short asks for the first value that FITS a ledger line; false takes the first
// that exists. The two passes are one function because a second walk could
// disagree with the first about what order the arguments were in.
func checkpointFirstArgument(arguments string, short bool) string {
	decoder := json.NewDecoder(strings.NewReader(arguments))
	opening, err := decoder.Token()
	if err != nil {
		return ""
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return ""
	}
	for decoder.More() {
		// The key, which is read and thrown away: this is deliberately blind to
		// what an argument is CALLED.
		if _, err := decoder.Token(); err != nil {
			return ""
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return ""
		}
		said := checkpointArgumentValue(value)
		if said == "" || (short && len(said) > checkpointLedgerBytes) {
			continue
		}
		return said
	}
	return ""
}

// checkpointArgumentValue renders ONE argument: a string as itself, an array as
// its elements — strings bare, anything else compacted — and everything else as
// nothing at all.
//
// A NUMBER AND A BOOLEAN SAY NOTHING ABOUT WHAT WAS TOUCHED. `{"lines": 200,
// "path": "./x"}` must draw the path, and a rule that took the first value of any
// kind would draw `200`. So the scalar kinds that cannot name work are skipped
// and the walk goes on to the next argument.
func checkpointArgumentValue(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return ""
		}
		return strings.TrimSpace(value)
	case '[':
		var elements []json.RawMessage
		if json.Unmarshal(raw, &elements) != nil {
			return ""
		}
		said := make([]string, 0, len(elements))
		for _, element := range elements {
			var text string
			if json.Unmarshal(element, &text) == nil {
				text = strings.TrimSpace(text)
			} else {
				text = strings.Join(strings.Fields(string(element)), " ")
			}
			if text != "" {
				said = append(said, text)
			}
		}
		return strings.TrimSpace(strings.Join(said, " "))
	}
	return ""
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
func (a *Agent) checkpointRound(ctx context.Context, hub *eventHub, user userMessage, meter *checkpointMeter, turn *Usage, started time.Time, model string, calls []ai.ToolCall, taken *Decision) bool {
	if !a.checkpoints(ctx, user) {
		return false
	}
	// THE WRITE SEAM IS ASKED FIRST, and it is asked at every boundary rather
	// than at a mark: the allowance is a count of what this turn has DONE to the
	// disk, and a turn that crosses it on round three must not wait until round
	// ten to be noticed (writeseam.go). It fires once, and past it the marks and
	// the ceiling govern the turn exactly as they always did.
	if a.writeMeterNow().pastAllowance() {
		return a.checkpointWriting(ctx, hub, turn, started, model, meter.rounds, meter.raced, taken)
	}
	// AND THE BATCH IS PRICED FOR WHAT IT WAS. A round the turn spent looking at
	// work it already has out is not a round of work, and the ladder does not move
	// for it ([checkpointMeter.round]).
	mark := meter.round(!roundWasWatching(calls))
	// AND THE WALL IS ASKED AT THE BOUNDARIES THE LADDER HAS NOTHING TO SAY
	// ABOUT. Under a steward with a wall, an inline stretch is bounded by a share
	// of it as well as by the two counts, because a turn that reads and runs tests
	// for twelve minutes crosses neither and the run is out of clock all the same
	// (turnwall.go). THE ORDER IS THE ENFORCEMENT: a boundary that crossed a rung
	// takes the mark ladder below and is never asked the clock, so the two
	// readings can neither race nor be paid for twice at one step.
	if wallShareIsAsked(mark, calls) && a.pastTurnWallShare(meter, started) {
		return a.checkpointOverWallShare(ctx, hub, turn, started, model, meter.rounds, meter.raced, taken)
	}
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
			a.journalMarkRead(read, mark, rounds, read.sketch.carryOnDecision())
			return false
		}
		a.journalMarkRead(read, mark, rounds, checkpointDecisionSplit)
		return a.handOverRunningTurn(ctx, hub, turn, started, model,
			checkpointSplitNote, checkpointSeamMark, rounds, meter.raced, read, taken).moved
	}
	// THE CEILING'S OWN READ IS JOURNALED AS A CARRY-ON, because that is what it
	// did: it decided nothing, and the ceiling line written a moment later is
	// where what happened to the turn is recorded.
	a.journalMarkRead(read, mark, rounds, read.sketch.carryOnDecision())
	return a.checkpointCeiling(ctx, hub, turn, started, model, rounds, meter.raced, read, taken)
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
//   - A SCREENLESS SESSION WITH NOBODY LEFT IN CHARGE NEVER CHECKPOINTS. A
//     headless session that holds a [Person] has no one to read the line, and a
//     ceiling there would end a turn somebody is waiting on the answer of with a
//     task nobody will see land. A screenless session with a goal owner has
//     exactly the reader an unattended ending needs, so it takes the same road as
//     a watched conversation.
//   - AND NOT A LINE THE SESSION WROTE THAT NOBODY OWES AN ANSWER FOR. An ambient
//     note — a standing run's own instruction, a delta nobody has to reply to — is
//     the session talking to itself, and ending one of those with a task would be
//     the session spending money on its own sentence.
//   - AND NOT MID-INTERRUPT. A turn the person has just stopped is a turn they
//     have said they do not want; moving its remains onto the rail would be
//     answering an interrupt with a task.
//
// AND A WOKEN TURN IS METERED NOW, WHICH IS A REVERSAL AND A MEASURED ONE.
//
// The rule used to be "only what a person typed", and both of the shapes a wake
// arrives in were refused by it: a note the model owes an answer for is `wake`,
// and the turn it starts opens with an EMPTY message because the note itself is
// on the steering queue ([Agent.wakeLocked]). So the first two lines of the old
// gate turned the meter off for every turn a landing task began.
//
// ON A REAL TEN-HOUR RUN THAT WAS THE WHOLE FAILURE. A task landed, the wake note
// started a turn, and that turn made 127 tool calls over 46 minutes with no mark,
// no ceiling and no handover — it ended when the model stopped talking, and the
// harness then sat idle for the remaining seven and a half hours of the ask. The
// argument for the refusal was that a woken turn "already carries a budget of its
// own", and it does not: a budget belongs to the TASK that landed, and the
// conversation turn that reads its report is an ordinary chat turn with an
// ordinary chat turn's total absence of governance.
//
// SO THE ONLY THING THAT STILL DECIDES IS WHETHER ANYBODY IS OWED AN ANSWER. A
// wake is owed one by definition ([userMessage.wake]) and an empty opening
// message is what a wake looks like from in here, so both are metered exactly as
// a typed message is — same ladder, same price, same ceiling, same handover.
//
// IT IS ALSO WHAT STOPS THE SIDECAR BEING BILLED ON THOSE TURNS, because it
// stands in front of the meter and therefore in front of every call this file
// makes.
func (a *Agent) checkpoints(ctx context.Context, user userMessage) bool {
	if a.config.InTask {
		return false
	}
	// A SCREENLESS SESSION WITH NOBODY LEFT IN CHARGE HAS NO ONE TO READ THE
	// LINE, while a screenless session with a goal owner has exactly the reader
	// an unattended ending needs and checkpoints like a watched conversation.
	if !a.config.AskConsent && a.steward() == nil {
		return false
	}
	if user.authored && !user.wake {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	// A TRIVIAL ASK IS NEVER LOOKED AT FOR CONVERSION. The write seam, the
	// marks and the ceiling all start a task through this gate; a commit
	// that has already staged five files is still a commit, and looking at
	// the work is how F26 converted it.
	if trivialAsk(a.taskRequest()) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed
}

// ── a turn ends; the ask does not ───────────────────────────────────────────

// checkpointReopen is the FIFTH moment, and it is the only one in this file that
// looks at a turn AFTER the model has stopped rather than while it is running.
//
// ── THE HOLE IT FILLS ──
//
// Everything above this line prices a turn that is going on too long. Nothing
// anywhere priced a turn that stopped too soon, and on a measured ten-hour
// benchmark all three harnesses in the comparison — this one included — ended
// with hours of the ask unused. A turn ends when the model emits no tool call,
// and a model emits no tool call for two quite different reasons: because the
// work is done, and because it has reached a natural-sounding place to stop. "I
// have finished the parser, next I will wire up the handlers" is the second one,
// and it ended the turn just as firmly as the first.
//
// ── SO THE ASK IS READ AGAINST THE WORK, BY SOMEBODY ELSE ──
//
// The same reader, the same digest and the same remains contract the ceiling uses
// ([checkpointRemainsAsk]). MET ends the turn exactly as today. NOT MET re-opens
// it with the reader's one line as a synthetic continuation, and the running
// model carries on from where it stopped.
//
// ── AND THREE THINGS BOUND IT ──
//
//   - THE PERSON'S OWN QUESTION ENDS A TURN, ALWAYS. A turn whose last words ask
//     the person something is a turn WAITING, and re-opening it would be the
//     harness answering a question that was addressed to somebody else. It is read
//     structurally ([endsAskingThePerson]) and never by keyword, because a rule
//     that knew what "shall I" looked like would be a rule about English.
//
//   - A TURN THAT TOUCHED NOTHING AND THE METER NEVER THOUGHT WORTH ONE READING
//     IS NOT WORTH A REMAINS-READING EITHER. The price gate is the mark ladder's
//     FIRST RUNG ([checkpointMeter.markAt]), which keeps it a cost gate in the
//     meter's own currency rather than a content one, exactly like every other
//     trigger in this file — and keeps it on the SAME number, from the same
//     constant, that already decides when a running turn has become dear enough to
//     interrupt. It was one finished round until that was measured: `bash ls` is
//     one round, so EVERY small turn that touched a tool paid a mastermind call —
//     $0.001 to $0.002, a third to a half of a tiny ask's whole bill — and in eight
//     of nine measured runs the call re-opened nothing. A turn that only LOOKED has
//     not done enough work to have left any of it half-done, and charging it a
//     mastermind call anyway would be the bill this file's own digest exists to
//     prevent. route_judge.go already reads the turn that answered in words alone
//     and asks the other question about it. THIS RUNG IS A PERSON'S TURN'S RUNG:
//     it protects a cheap message somebody is sitting in front of, and a WOKEN
//     turn — a task landing's, nobody typed and nobody waiting — is never held by
//     it, because there is no person to carry the cheap one on (see the wake
//     carve-out at the gate itself, and [Agent.wakeLocked]).
//
//   - BUT THE READER IS GATED ON EXPOSURE, NOT ON PRICE. A turn that CHANGED THE
//     WORKING TREE and then stopped without anybody looking at the change is read
//     for what remains whatever it cost ([turnLeftTheTreeUnchecked]) — because the
//     thing a cheap turn cannot leave behind is a half-done job, and the thing it
//     very much CAN leave behind is an unbuilt edit.
//
//     SWE-Marathon s10, 10:35Z: a turn woken by a job's exit ran six rounds,
//     overwrote an 18,771-byte source file, said "now let me build and run the full
//     test suite" WITHOUT calling anything, and sealed. Six is under the first rung,
//     so the price gate returned early and nothing read the turn; the file it had
//     just written was the six compile errors the run shipped with, and three hours
//     of the ask went unspent. The rounds were cheap. The exposure was total.
//
//     WHAT COUNTS AS EXPOSURE IS THE DIGEST'S OWN FACT AND NOT A SECOND OPINION
//     ABOUT TOOLS: [checkpointLedger] already names the writers and the workClock
//     already holds the step the deliverable last moved on (novelty.go), so the
//     whole rule is "the last thing this turn did was change something". A turn
//     that wrote and then ran ANYTHING — a build, a test, a read of what it just
//     wrote — has had its change looked at by the one party that could, and it goes
//     back on the price gate like every other turn. That bounds the new spend to
//     turns whose last act was a write, which is a small set and is exactly the set
//     that was measured going wrong.
//
//   - AND THE METER IS THE ONLY COUNTER. A re-open is charged as a ROUND, through
//     the ordinary [Agent.checkpointRound], which is what "on the same meter"
//     has to mean if it is to mean anything: the marks still fire, a re-opened
//     turn that reaches the ceiling hands off as usual, and a model that would
//     answer the continuation with the same sentence forever is stopped by the
//     ceiling rather than by a second number invented here. There is no re-open
//     counter, and there must not be one.
//
// EVERY FAILURE ENDS THE TURN, which is the opposite fail-open direction from the
// marks and is the honest one here. A reader nobody can reach, a window that ran
// out, an install with no mastermind: each answers MET, and the turn ends as it
// did before this existed. The alternative — re-opening on silence — is a harness
// that will not let a conversation finish on the day its sidecar goes down.
//
// It reports whether the turn CARRIES ON, and whether it is OVER: a re-open that
// crossed the ceiling is a turn that ended by being handed over, which is neither
// of the two ordinary answers and belongs to the caller's `return true`.
// AND THE LAW THAT COST THE MEASURED RUN ITS EVENING ─────────────────────────
//
// A TURN THAT ENDED IN AN ERROR IS NEVER READ FOR WHAT REMAINS. THE ERROR IS
// WHAT REMAINS, and the retry ladder above already owns it.
//
// SWE-Marathon run s2, 22:45 UTC: three calls in fifteen seconds came back with
// no content, no tool call and no usage — failed calls, dressed as empty answers
// (loop.go) — and each one looked to this function exactly like a turn that had
// stopped short. It read the remains three times at mark-reader prices, re-opened
// three times, and the turn then died on the refusal that had been underneath all
// along. Re-opening a broken turn buys a fourth identical failure; what a broken
// turn needs is the error path, which is the one thing a re-open takes it away
// from. So [turnBroke] is asked before anything is spent.
func (a *Agent) checkpointReopen(ctx context.Context, hub *eventHub, user userMessage, meter *checkpointMeter, turn *Usage, started time.Time, model string, response *ai.Response) (again, over bool) {
	if turnBroke(response) {
		return false, false
	}
	said := response.Text()
	if !a.checkpoints(ctx, user) {
		return false, false
	}
	if meter == nil {
		return false, false
	}
	// THE CHEAP EXCLUSIONS FIRST, then the two gates. A question to the person is
	// a string test on what was just said and a live job is a walk of a short
	// slice; the exposure reading walks the turn's messages, and there is no
	// sense walking them for a turn that is waiting.
	if endsAskingThePerson(said) {
		return false, false
	}
	// AND A TURN WAITING ON ITS OWN BACKGROUND WORK IS WAITING, NOT STOPPING.
	//
	// This is [endsAskingThePerson]'s law with the other party changed. That gate
	// exists because a turn that asked a question has a wake-up coming — the
	// person's answer — and re-opening it would be the harness answering
	// something addressed to somebody else. A turn that ends while a job this
	// conversation started is still running is in the same position: a process
	// job, a render and a hand each queue their exit as an OWED note that starts
	// a turn by itself the moment it lands ([Agent.enqueueJobNote]), so the
	// continuation the reader would buy already exists and is already on its way.
	//
	// A WATCH IS INCLUDED AND IT WAKES TOO, which is the half this gate was
	// written before. A watch's repeated DELTAS ride the ambient lane and wait for
	// a turn boundary, but the tick that FIRES it — `until` matched, the output
	// went quiet, the command failed its way out — is owed and starts a turn like
	// any other ending ([Agent.enqueueWatchNote]). So the continuation exists for
	// this shape as well, and the twenty polls carrying on bought, measured, were
	// polls of the command that was about to report.
	//
	// See [checkpointCarryOnCap] for the five minutes this was measured in.
	if a.turnIsWaitingOnItsOwnWork() {
		return false, false
	}
	// A WOKEN TURN OUTRANKS THE PRICE, WHICH IS THE WHOLE OF WHAT THE MEASURED RUN
	// STILL GOT WRONG.
	//
	// The price gate below is the right rule for a turn A PERSON TYPED: a small
	// read-only turn that stops is a person's to carry on, they are sitting in
	// front of the answer, and charging a reader against every cheap message would
	// be the bill this file's digest exists to prevent. NONE OF THAT IS TRUE OF A
	// TURN A TASK LANDING STARTED. Nobody typed it, nobody is holding the channel,
	// and there is no person who will pick up where it stopped — so the cheapness
	// that lets a typed turn end unread is not a reason to let a woken one end
	// unread, it is the exact opposite. SWE-Marathon s14, 18:16Z: a task came home
	// failed at 46329/68186, the note woke a turn, it read for six rounds — four
	// under the first rung — and sealed on "let me diagnose the failures
	// systematically rather than rewriting everything", a stated next step and no
	// question. The price gate returned early, nothing read the ask against the
	// work, and the cell settled idle with seven and a half of its ten hours
	// unspent on the best clean seed of the run. A WOKEN TURN IS READ FOR WHAT
	// REMAINS WHATEVER IT COST ([Agent.wakeLocked] sets the bit); it is owed an
	// answer by definition, and [endsAskingThePerson] and [turnBroke] above are
	// the only two endings that still stop the reader from being spent on one.
	//
	// THE PRICE GATE, AND THE EXPOSURE THAT OUTRANKS IT, FOR EVERY OTHER TURN.
	//
	// THE ROUND COUNT IS WHAT MAKES THE SECOND READING THIS TURN'S. The ledger
	// walks the whole transcript (the digest's own habit), so "the last call was a
	// write" is a fact about the CONVERSATION until something says this turn made
	// calls at all — and a turn that made at least one round owns every call at
	// the newest end of that transcript, because its rounds are the newest thing
	// in it. Without this a person typing "thanks" after a turn that wrote would
	// pay a reader for a message that touched nothing, which is the exact bill the
	// price gate exists to prevent.
	if !user.wake &&
		meter.rounds < meter.markAt(1) &&
		!(meter.rounds > 0 && turnLeftTheTreeUnchecked(a.snapshot())) {
		return false, false
	}
	// AND THE READING IS PUT TO WHOEVER THIS SESSION IS WORKING FOR.
	//
	// The reader's line used to BE the decision: something to say re-opened the
	// turn, silence ended it. That is the right rule when a person is sitting in
	// front of the answer, and it is the whole of what went wrong when nobody is
	// — the reader is shown a ~300-token digest of what the session SAID, and a
	// conversation that ends confidently reads as finished whatever the tree
	// says. So the line goes to the principal along with the evidence a person
	// would have looked at before agreeing, and the principal decides
	// (principal.go). A [Person] answers exactly what this function answered
	// before the interface existed, by construction and by test.
	decision := a.decideRemains(ctx, a.readRemains(ctx), said)
	switch decision.Verb {
	case DecideDone:
		return false, false
	case DecideStop:
		// THE RUN IS OVER AND IT SAYS WHY. A session that spent its hours and
		// went quiet is a session nobody can learn anything from, which is the
		// second half of the same failure this file's re-open closed: the
		// evidence exists either way, and the only question is whether anybody
		// is told.
		hub.send(Event{Kind: EventNotice, Text: checkpointStoppedNote + decision.Reason})
		return false, false
	}
	// AND CARRYING ON HAS A CEILING OF ITS OWN, WHICH IS THE ONLY COUNTER BESIDE
	// THE METER THIS FILE ALLOWS AND THE REASON IT IS ALLOWED.
	//
	// The re-open's own digest says there is no re-open counter and there must not
	// be one, and that stands for the QUESTION IT WAS ABOUT: how long a turn may
	// run is the meter's to answer, and a second clock over the same thing would
	// be two policies pretending to be one. This answers a different question —
	// how many times the same reading may be believed about the same stopped turn
	// — and the measured run is what says it needs answering separately, because
	// the meter said yes twenty times running while nothing whatever moved
	// ([checkpointCarryOnCap]).
	//
	// IT IS ASKED AFTER THE READING AND NOT BEFORE IT, which costs one call and
	// buys the only thing worth having here: the line a person reads is TRUE. A
	// cap that fired before the reader would have to guess that the ask was still
	// unfinished, and would say so out loud on the turn where the model had
	// finally finished it.
	if meter.carriedOn >= checkpointCarryOnCap {
		hub.send(Event{Kind: EventNotice, Text: checkpointCarriedOnNote(decision.Observed)})
		return false, false
	}
	// THE METER IS CHARGED BEFORE THE CONTINUATION IS WRITTEN, so a re-open that
	// lands on the ceiling hands the work over instead of asking the model for one
	// more round nobody is going to watch. The mark's own reading is made there and
	// not reused from here: they ask different questions, and a shape is what the
	// handover needs.
	//
	// AND CARRY-ONS CAN NO LONGER REACH THE CEILING BY THEMSELVES. The rungs
	// stand at [checkpointPrice] and its doublings ([checkpointMarkAt]), and the
	// gate above lets this line be reached at most [checkpointCarryOnCap] times
	// in a turn — a count that does not reach the FIRST rung, let alone the
	// ceiling. So a turn that still crosses it crossed it on rounds of its own
	// WORK, which is the exact turn the ceiling was written for, and there is
	// nothing further to guard here.
	// THE RE-OPEN CARRIES NO BATCH, and that is the honest reading: this road is
	// reached by a turn that has STOPPED, so there is no round of looking to
	// discount and the ladder is climbed exactly as it always was.
	if a.checkpointRound(ctx, hub, user, meter, turn, started, model, nil, &decision) {
		return false, true
	}
	meter.carriedOn++
	hub.send(Event{Kind: EventNotice, Text: checkpointCarryOnNote})
	a.record(textMessage("user", checkpointCarryOnLead+decision.Brief))
	return true, false
}

// checkpointStoppedNote opens the one line a person reads when the session's
// goal owner ended the run.
//
// It is the fifth in the register ([checkpointCarryOnNote] names the other
// four): an observation, a middle dot, and then the reason in the goal owner's
// own words — which is why this one, alone among them, is a lead rather than a
// whole sentence. What stopped a run is the only thing worth reading about it,
// and a constant that swallowed it would make every ending read the same.
const checkpointStoppedNote = "stopping here · "

// decideRemains puts the end of a stopped turn to this session's principal.
//
// ── THE ORDER IS THE COST ───────────────────────────────────────────────────
//
// The reader's line is already in hand and everything the first Decide is shown
// beside it is a read of what the session already holds — how its units of work
// landed, and the acceptance for the whole ask (principal_wire.go). Nothing is
// run, nothing is spent, and a [Person] never gets past this line: their answer
// is the reader's line and silence, exactly as it always was.
//
// THE SECOND DECIDE IS WHERE THE TREE IS LOOKED AT. A principal that says the
// ask is MET has said the one thing this build never had any way to check, so
// it is checked: the session's declared checks are re-run from clean and the
// same principal is asked again with their results in front of it
// (principal_audit.go). An acceptance is what says a principal is in a position
// to be asked that — a person holds their own and is shown nothing — so the
// second reading belongs to a session that has one and to no other.
//
// AND THE SWEEP RIDES WITH IT, which is why the audit is not skipped when the
// second answer turns out to be "carry on": what a session left lying beside
// its deliverable is left lying there whether or not this particular turn was
// the last one, and a tidy that only ran on a clean ending would never run on
// the runs that need it.
func (a *Agent) decideRemains(ctx context.Context, reader readerLine, said string) Decision {
	principal := a.who()
	remains := a.remainsFor(said, reader)
	a.journalAbsorbed(remains)
	decision := principal.Decide(remains)
	if decision.Verb == DecideDone && remains.Acceptance != "" {
		var found reconciliation
		decision, found = a.decideOverTheChecks(ctx, remains)
		// AND THE SWEEP HAPPENS ONLY AT AN ENDING. A principal that reads the
		// checks and carries on may be about to read the very files this would
		// remove, so what was sorted is acted on only once the session is
		// actually finished with them (principal_audit.go's [Agent.sweepSession]).
		if decision.Verb != DecideCarryOn {
			a.sweepSession(found)
		}
		a.journalDecision(decision)
		return decision
	}
	// A RUN THAT STOPPED IS AN ENDING TOO, and it is the ending most likely to
	// leave a mess: the budget ran out, or the same thing has failed three times,
	// and neither of those goes anywhere near the reading above. The tidy is owed
	// to whoever comes to look at the tree afterwards, however the run ended.
	if decision.Verb == DecideStop {
		a.sweepSession(reconcile(a.createdList(), a.deliverableTree()))
	}
	a.journalDecision(decision)
	return decision
}

// journalAbsorbed writes down every unit of work that stopped counting as
// unfinished because somebody else did it and brought it home.
//
// A GAP THAT CLOSES WITHOUT ANYBODY DECIDING ANYTHING HAS TO SAY WHO CLOSED IT.
// The reading below simply stops naming an absorbed unit ([Remains.absorbedBy]),
// so without this line the file would show a run that carried on over a gap and
// then stopped mentioning it, with nothing anywhere saying why — which is the
// exact shape of the failure this whole change is about, in the other direction.
//
// AND EACH IS SAID ONCE. The reading is taken at the end of every reply and the
// answer does not change, so a row per turn would be one fact written thirty
// times. What is already written down is remembered for the life of the session.
// decideOverTheChecks is the SECOND READING a done has to survive: the session's
// declared checks are run from clean over the tree as it stands, what was
// already red before the work is folded in beside them, and the principal is
// asked again with the results in front of it. Both roads that can end a run on
// a done — the stopped turn ([Agent.decideRemains]) and the handover
// ([Agent.decideHandover]) — take it, so neither can finish on a done nobody
// checked. It also returns what the audit's sweep found, for the caller that
// tidies.
//
// THE BASELINE IS RE-READ WITH THE CHECKS. The reading handed in was assembled
// before the checks ran, when the before-reading may not have landed;
// [Agent.terminalAudit] waits for it, so by here it has, and a reading still
// carrying the old "not yet" would count nothing against the tree it has just
// measured.
func (a *Agent) decideOverTheChecks(ctx context.Context, remains Remains) (Decision, reconciliation) {
	checks, found, stashed := a.terminalAudit(ctx)
	remains.Checks = checks
	// AND WHAT GIT IS HOLDING OUT OF THE TREE COMES BACK WITH THEM. The reading
	// handed in was assembled from the session's own ledgers, which know a path
	// was written and not whether the writing is still there; the stash is the
	// one account of that nobody in this session can take for themselves
	// ([Remains.Stashed]).
	remains.Stashed = stashed
	remains.WasFailing, remains.Unread, remains.BaselineRead = a.baselineRedChecks()
	return a.who().Decide(remains), found
}

func (a *Agent) journalAbsorbed(remains Remains) {
	lines := remains.absorbed()
	if len(lines) == 0 || a.steward() == nil {
		return
	}
	a.mu.Lock()
	file := a.file
	if a.absorbed == nil {
		a.absorbed = map[string]bool{}
	}
	fresh := lines[:0:0]
	for _, line := range lines {
		if !a.absorbed[line] {
			a.absorbed[line] = true
			fresh = append(fresh, line)
		}
	}
	a.mu.Unlock()
	if file == nil || len(fresh) == 0 {
		return
	}
	file.appendPrincipal(journalPrincipal{Who: "steward", Event: "absorbed", Kept: fresh})
}

// journalDecision writes down what the goal owner decided, because a run that
// carried on and a run that stopped read identically in this file otherwise —
// which is the same gap [journalCeiling] was written to close one road over.
//
// A PERSON'S SESSION WRITES NOTHING HERE, and that is [Person]'s emptiness law
// reaching the journal: the interactive session must not be able to tell that
// any of this arrived, and a new line in somebody's transcript is something
// they can tell. What a person decided is in the conversation, where they said
// it.
func (a *Agent) journalDecision(decision Decision) {
	steward := a.steward()
	if steward == nil {
		return
	}
	a.mu.Lock()
	file := a.file
	a.mu.Unlock()
	if file == nil {
		return
	}
	budget := steward.Budget()
	file.appendPrincipal(journalPrincipal{
		Who:      "steward",
		Event:    "decided",
		Decision: string(decision.Verb),
		Reason:   decision.Reason,
		Brief:    clip(decision.Brief, checkpointSketchBytes),
		WallMS:   budget.SpentWall.Milliseconds(),
		CostUSD:  budget.SpentUSD,
	})
}

// turnLeftTheTreeUnchecked reports the one thing about a finished turn that
// outranks its price: IT CHANGED SOMETHING, AND NOTHING LOOKED AT THE CHANGE.
//
// THE FACT IS ALREADY IN THE DIGEST AND THIS ONLY ASKS FOR IT. [checkpointLedger]
// walks the turn once and hands back the workClock (novelty.go) it kept on the
// way: `steps` is how many calls the turn made and `changedAt` is the step the
// deliverable last moved on. So "wrote, then stopped" is one comparison — the
// last call this turn made was the write — and there is no second list of tool
// names here to disagree with [checkpointWriters] the day a verb is renamed.
//
// A TURN THAT WROTE AND THEN RAN SOMETHING IS NOT EXPOSED BY THIS ARM. Whatever
// came after the write — the build, the test, a read of the file it had just
// saved — is the change having been looked at, and a harness cannot tell which of
// those the model meant by looking at its name. It may still be read at the ≥10
// arm like any other dear turn; what it is not is read for FREE.
//
// AND A TURN THAT NEVER WROTE ANSWERS FALSE, which is the whole of the
// regression-wave economics: a small read-only turn is priced exactly as it was
// before this existed and pays no reader.
func turnLeftTheTreeUnchecked(messages []ai.Message) bool {
	_, _, _, moved := checkpointLedger(messages)
	return moved.changedAt > 0 && moved.changedAt == moved.steps
}

// turnBroke reports that the step which ended this turn FAILED rather than
// finished, which is the one ending the remains-reader must never be shown.
//
// TWO SIGNS, AND BOTH ARE THE CALL'S OWN ACCOUNT OF ITSELF rather than a reading
// of what the model wrote:
//
//   - the provider SAID it stopped on an error — `error`, `network_error` — which
//     is a finish reason and not a judgement about content
//   - or the step is EMPTY: no words, no tool call. A model that answers nothing
//     has not stopped short of the ask, it has not answered at all, and on the
//     measured run three of these in a row were an upstream that had already
//     started refusing (internal/provider's sse.go now makes that refusal an
//     error where the router sends one; this stands behind it for the endpoints
//     that send a bare empty 200 instead)
//
// A missing finish reason is NOT one of the signs. Plenty of endpoints simply do
// not send one, and reading their silence as breakage would switch this whole
// feature off against them.
//
// A nil response is broken by definition: nothing at all came back.
func turnBroke(response *ai.Response) bool {
	if response == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(provider.FinishReason(response))) {
	case "error", "network_error":
		return true
	}
	return strings.TrimSpace(response.Text()) == "" && len(response.ToolCalls()) == 0
}

// readRemains asks the mark's own reader the one question the end of a turn
// raises: is the person's ask finished?
//
// IT IS [Agent.readMark] WITH A DIFFERENT ASK AND A DIFFERENT ANSWER SHAPE, and
// it is a second function rather than a flag on the first because the two answers
// are read by completely different code — a sketch is parsed for width, and this
// is handed back to the running model as prose.
//
// AN EMPTY ANSWER MEANS THE ASK IS MET, and every failure produces one: no
// mastermind, a fault, a window that ran out, a reply that is not prose, and the
// remains contract answered with its own token. Reading silence as "there is more
// to do" would re-open turns on every install without a crew.
//
// AND IT IS BILLED TO THE ERRAND POCKET, for [Agent.readMark]'s reason: it is a
// side-call to a different model that the person did not ask for.
func (a *Agent) readRemains(ctx context.Context) readerLine {
	// AND THE QUESTION IS NOT ASKED WHEN THERE IS NOBODY TO ASK IT OF. The empty
	// answer below is what every failure produces, so an install with no second
	// model used to reach it through a failed call on every single round — the
	// same "" for "nothing left" and for "nothing answered", bought each time
	// (#468). It is the same reading [Agent.readMark] takes, one road over.
	if a.markReaderAbsent() {
		return readerLine{}
	}
	digest := checkpointDigest(a.taskRequest(), a.snapshot())
	if digest == "" {
		return readerLine{}
	}
	ctx, done := context.WithTimeout(ctx, checkpointSketchWindow)
	defer done()
	messages := []ai.Message{textMessage("user", digest+"\n\n"+checkpointRemainsAsk)}
	response, reader, err := a.callRole(ctx, roles.RoleMarkReader, "", messages,
		ai.WithMaxTokens(checkpointSketchTokens))
	if err != nil || response == nil {
		return readerLine{}
	}
	a.addAuxiliaryUsage(response, reader, 1)
	said := strings.TrimSpace(response.Text())
	if declaresNothingLeft(said) {
		// THE READER SAYING NOTHING IS LEFT IS A FACT, NOT A SILENCE. It used to
		// be spelled with the same empty string as "there was nobody to ask" and
		// "the call failed", and the three are not the same news: only this one is
		// a second reader agreeing that the work is finished, and only this one
		// may let a session that did its work INLINE reach done
		// ([Remains.finishedSomething], #513).
		return readerLine{answered: true, nothingLeft: true}
	}
	if !briefIsProse(said) {
		return readerLine{answered: true}
	}
	// ONE LINE, because that is what was asked for and because what the harness
	// does with it is hand it to a model as the thing still to do. A reader that
	// wrote an essay is clipped to its first line rather than argued with.
	return readerLine{said: clip(firstLine(said), checkpointSketchBytes), answered: true}
}

// readerLine is what the mark reader answered about what is left, in the three
// states it actually has.
//
// THE EMPTY STRING USED TO MEAN ALL THREE. "Nobody was asked", "the call failed"
// and "a reader looked and said nothing is left" all came back as "", and the
// third is the only one that is evidence about the work. A session that did its
// whole job inline — no task, nothing to land — has no settled landing to prove
// it finished anything, so the reader agreeing is the only second opinion there
// is, and collapsing it into silence is what left one measured run reading
// "nothing has been finished yet" over a green tree it had just written
// ([Remains.finishedSomething], #513).
type readerLine struct {
	// said is what is still left, in the reader's own words, and "" when it said
	// nothing is left or was never asked.
	said string
	// answered says a reader was asked and replied at all.
	answered bool
	// nothingLeft says the reply was that nothing is left.
	nothingLeft bool
}

// endsAskingThePerson reports that a turn's last words put a question to whoever
// is reading them.
//
// IT IS STRUCTURAL AND IT NAMES NO WORDS, which is the law every trigger in this
// file is held to: a list of openers — "shall I", "would you like", "do you want"
// — is a rule about English, and this harness answers in whatever language it was
// asked in. A question mark at the end of the last thing said is the one mark
// every written language that has questions actually uses for them.
//
// THE TRAILING DECORATION COMES OFF FIRST, because a model that ends on a
// question ends on `**…?**` and on `"…?"` about as often as it ends on the bare
// mark — the same fold [parseCheckpointSketch] makes of a shape written in a code
// span, for the same measured reason.
//
// AND A TURN THAT SAID NOTHING ASKED NOTHING. Silence is not a question, and
// reading it as one would exempt from this check exactly the turns that stopped
// without explaining themselves.
func endsAskingThePerson(said string) bool {
	said = strings.TrimRight(strings.TrimSpace(said), "`*_\"'”’) \t\n")
	return strings.HasSuffix(said, "?")
}

// turnIsWaitingOnItsOwnWork reports that this conversation started background
// work that is still running at the moment the turn ended.
//
// IT IS THE LIVE WORK TREE'S OWN ANSWER and not a second reading of the
// registry: [Agent.jobsWorkingNow] is what the head count over the roster
// column is drawn from (jobrow.go), so "aforge thinks something is running" is
// one fact with one definition, and the gate above cannot come to disagree with
// the number a person is looking at while they read its line.
//
// A TASK NODE IS DELIBERATELY NOT ONE OF THESE, which falls out of borrowing
// that function rather than being decided here. A node's landing wakes a turn
// through machinery of its own and is READ FOR WHAT REMAINS when it does — that
// is the price gate's `user.wake` law a few lines up — and a gate that went
// quiet while any sub-task was out would take that reading away from the very
// turn it was written for.
func (a *Agent) turnIsWaitingOnItsOwnWork() bool {
	return len(a.jobsWorkingNow()) > 0
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
func (a *Agent) checkpointCeiling(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string, rounds int, verdict routeVerdict, read checkpointRead, taken *Decision) bool {
	verdict.Wide = true
	// AND THE ROW IS NOT WRITTEN HERE ANY MORE. It is written by the function
	// below, on every way out it has, because the ending is decided there and the
	// two other doors into it were leaving the file silent — see the seam
	// constants above. This road's remaining job on that line is nothing.
	return a.handOverRunningTurn(ctx, hub, turn, started, model,
		checkpointCeilingNote, checkpointSeamCeiling, rounds, verdict, read, taken).moved
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
func (a *Agent) handOverRunningTurn(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model, line, seam string, rounds int, verdict routeVerdict, read checkpointRead, taken *Decision) (over checkpointHandover) {
	// ONE ENDING ROW PER ENDING, WRITTEN AT THE SEAM THAT TOOK IT.
	//
	// IT IS A DEFER BECAUSE THERE ARE SEVERAL WAYS OUT of this function and the
	// file has to hold exactly one row for whichever it was — moved, the three
	// declines, and the two endings a goal owner takes below. A row written at
	// each `return` instead would be one law copied at every exit, and the way
	// that fails is silently: somebody adds a way out and the file simply does not
	// mention it, which is precisely how the mark road and the write seam came to
	// write nothing at all (sessionfile.go's [journalCeiling]).
	//
	// AND IT IS REGISTERED ABOVE THE GOAL OWNER'S OWN READING, so a run stopped or
	// finished at this seam is written down as an ending like any other. The
	// steward's `decided` row says what it decided; this one says what became of
	// the turn (#513, #567).
	//
	// AND THE CEILING NO LONGER WRITES ITS OWN ([Agent.checkpointCeiling]), so
	// there is one writer and no road can produce two rows for one ending.
	defer func() {
		a.file.appendCeiling(journalCeiling{
			Rounds: rounds, Seam: seam, Decision: over.decision,
			Reason: over.reason, TaskID: over.taskID, Carry: over.carry,
		})
	}()
	// THE FLOOR STANDS IN FRONT OF EVERYTHING. A caller that reached here
	// through [Agent.checkpoints] already asked, but looped.go's looping
	// handoff and any future door share this function, and a handover that
	// paid for the goal owner's reading, a name and a brief before being
	// declined would still have converted the ask in every way that costs
	// money. So the ask is read first and nothing is spent: the ending row
	// above writes the drop, and the turn carries on.
	if trivialAsk(a.taskRequest()) {
		return checkpointHandover{decision: checkpointCeilingTrivial}
	}
	// A HANDOVER IS AN ENDING, AND AN UNATTENDED SESSION'S PRINCIPAL READS EVERY
	// ENDING (see [Agent.endTurnUnderSteward]). It is asked FIRST, before the
	// name, the phase clock and the two model calls below, because the whole
	// point of an ending that stops the run is that none of them is spent.
	reading, handover, ended := a.endTurnUnderSteward(ctx, hub, turn, started, model, taken)
	if ended {
		return handover
	}
	sketch := read.sketch
	asked := a.taskRequest()
	// THE NAME IS ASKED FOR NOW, beside the two calls below rather than after
	// them (taskname.go's [nameAhead]). This stage is the longest silence on the
	// road, and the namer used to start only when it ended — so the line that
	// announced the task, and the row it put on the rail, carried the person's
	// raw sentence and were renamed under their eyes a moment later, or never,
	// when the task failed first. Asked here, the name has the whole stage to
	// land in. A road that declines below lets the call go.
	ahead := a.nameAhead(asked)
	defer ahead.release()
	// AND THE PERSON IS TOLD WHAT THE SILENCE IS, because the two calls below are
	// the longest stretch of this whole road with nothing drawn.
	//
	// A handover is two model runs before anything exists to point at — the turn
	// writing down what it found, and a mastermind turning that into an
	// instruction — and measured together they are fifteen to thirty seconds
	// between the last thing the model said and the task appearing on the rail.
	// The transcript's own notes cannot cover it: they are settled facts, and this
	// road can still DECLINE below, so a line saying the work was moving would be
	// a sentence left standing over something that did not happen.
	//
	// SO IT IS THE PHASE CLOCK AND NOT A NOTE (phasenews.go). It is the lane this
	// harness already uses for a wait inside a turn — the same pair loop.go puts
	// around the readers at the end of a turn — it says only what is true while it
	// is true, and it takes itself off the screen when the stage ends, whichever
	// way this ends.
	//
	// AND IT IS SAID ONCE, FOR THE WHOLE STAGE. It used to be posted twice, once
	// per model call, because nothing beat and a surface drops a phase it has not
	// heard again for [provider.PhaseWindow] — so a single post went dark halfway
	// through a thirty-second stage. That is no longer true of any holder: a
	// phase held open re-says itself while it lasts (phasenews.go's
	// [phaseHeldBeat]), so the two calls below are one stage with one clock on
	// it, counting from here to whichever ending this road takes.
	a.tellPhase(provider.PhaseBriefing, checkpointBriefingWho, time.Now())
	draft, remains, drafted := a.checkpointBrief(ctx, turn, model)
	if !remains {
		// AND THE PRINCIPAL'S CARRY-ON OUTRANKS BOTH MINDS. The decline rests on
		// two readers of the WORK agreeing that none of it is left; the session's
		// goal owner has just read the same ending against the ask itself — what
		// landed, what the checks said, what the acceptance was — and answered
		// that it is not finished. That is not a third opinion to be weighed, it
		// is the one the other two are opinions ABOUT, so where it says carry on
		// there is nothing here to decline. Measured (#513): a cell's write seam
		// fired at round 34 and its ceiling at 40, the goal owner said carry on
		// at both — "not yet confirmed" — and the two-minds decline threw both
		// handovers away, leaving the turn to run 830 seconds and end inside a
		// git stash with the fix uncommitted.
		//
		// A PERSON'S SESSION IS UNTOUCHED: [Person] never reads this road at all,
		// so [stewardReading.carriesOn] is false and the decline stands exactly
		// as it did.
		if sketch.saysDone() && !reading.carriesOn() {
			// NOTHING HAPPENS, and that includes the line AND the ladder's own lines.
			// A person told their answer was being moved and then left watching it
			// finish where it was would have been told something that did not happen,
			// and a file carrying a ladder for a handover that never happened would
			// say the same thing to whoever reads it afterwards. The ceiling line
			// written a moment later says the drop and why.
			//
			// AND THE CLOCK COMES OFF WITH IT: the stage above was real and is over,
			// and a phase left standing is the surface drawing work nobody is doing.
			a.endPhase()
			return checkpointHandover{decision: checkpointCeilingNothing}
		}
		// UNCORROBORATED, or corroborated and overruled, so the work moves — and
		// the continuation spent its answer on the token instead of on an
		// instruction, so there is no draft of the turn's findings. The writer
		// below still has the ask and the digest, which is more than the person's
		// bare sentence and is the whole reason it is asked at all.
		//
		// AND WHERE THE GOAL OWNER OVERRULED, ITS OWN BRIEF STANDS IN FOR THE
		// DRAFT. It is the only account of what is left that anybody in the
		// building has just written down — the model's was the token — and
		// handing the writer nothing where a remainder exists would be this road
		// discarding the reason it did not decline.
		draft = reading.remainder()
	}
	// AND THE BRIEF IS WRITTEN BY SOMEBODY WHO DID NOT SPEND THE TURN.
	//
	// THE DRAFT IS THE FINDINGS AND THE WRITER IS THE JUDGEMENT, which is the split
	// the measurement forced. The running model is the only reader holding what the
	// turn learned, so it still drafts; but it is also a tired weak model at the end
	// of forty rounds, and what it produced on the measured run was 5,882 characters
	// of loop that [briefIsProse] passed and a worker was then started on. Nothing
	// stood between that document and a spec. This does.
	//
	// AND THE LADDER UNDER IT DESCENDS THROUGH THE THINGS THAT ARE STILL TRUE. The
	// written brief, then the draft the runner wrote, then the person's own
	// sentence — which is what this road used to reach SECOND and now reaches LAST,
	// because a bare ask hands a worker everything the turn found out except the
	// findings.
	//
	// AND EVERY RUNG OF IT IS AN EVENT. Which rung answered is the difference
	// between a worker that opens on an account of the turn and a worker that
	// opens on the person's raw sentence, and on the measured run that difference
	// was twelve minutes of a cold worker re-deriving what the chat already knew —
	// with nothing in any file saying it had happened. So the ladder is walked
	// with its outcomes in hand, written down rung by rung, and the rung that
	// supplied the brief rides the ceiling's own line (see the carry ladder above).
	written, wrote := a.writeHandoff(ctx, asked, read.digest, draft)
	// AND A RUNG THAT ASSIGNS WORK THIS CONVERSATION IS STILL HOLDING IS NOT A
	// RUNG, which is the same law the drawing was reduced by one step earlier
	// (checkpoint_custody.go) applied to the two rungs a MODEL wrote.
	//
	// THE DRAWING IS NOT THE ONLY ROAD INTO THE BRIEF. The writer below is shown
	// the account of the turn, and a turn that spent its rounds beside four
	// running pieces has those pieces all over its account; the draft is written
	// by the model that was standing in the middle of them. So a document that
	// comes back telling a worker to wait for, read or land a piece THIS
	// conversation is holding is degenerate for the same reason a looping one is
	// — nobody could work from it — and it descends the ladder exactly as one.
	//
	// AND THE PERSON'S OWN SENTENCE IS NEVER TOUCHED. It is the floor of the
	// ladder and the one thing on this road nobody writes; a harness that edited
	// what they typed would be answering a different ask from the one they made.
	if held := read.held; len(held) > 0 {
		if namesHeldWork(written, held) {
			written, wrote = "", carryStep{rung: carryRungHandoff, outcome: carryDegenerate,
				reason: carryHeldWork, said: carrySaidWorkAlreadyOut}
		}
		if namesHeldWork(draft, held) {
			draft, drafted = "", carryStep{rung: carryRungDraft, outcome: carryDegenerate,
				reason: carryHeldWork, said: carrySaidWorkAlreadyOut}
		}
	}
	// AND THE CLOCK COMES OFF WITH THE WRITING, which is where the stage the
	// person was watching actually ends: everything below is bookkeeping over
	// text already in hand.
	a.endPhase()
	goal, carried := written, carryRungHandoff
	if strings.TrimSpace(goal) == "" {
		goal, carried = draft, carryRungDraft
	}
	if strings.TrimSpace(goal) == "" {
		goal, carried = asked, carryRungAsk
	}
	stood := carryStep{rung: carryRungAsk, outcome: carryWritten, chars: len(strings.TrimSpace(asked))}
	if strings.TrimSpace(asked) == "" {
		stood = carryStep{rung: carryRungAsk, outcome: carryEmpty, reason: carryNoSentence}
	}
	ladder := []carryStep{wrote, drafted, stood}
	for index := range ladder {
		ladder[index].used = ladder[index].rung == carried && strings.TrimSpace(goal) != ""
	}
	a.journalCarryLadder(ladder)
	if strings.TrimSpace(goal) == "" {
		// AND THERE IS THE ONE OTHER WAY THIS ENDS WITH NO TASK: nothing to write
		// down for anybody. No dowry and no sentence of the person's own is not a
		// narrow brief, it is no brief — and a task admitted on it would be a worker
		// started on a blank page. The ladder above is written down first: this is
		// the outcome whose reasons matter most, and it is the one that used to
		// leave the file saying only that a ceiling had dropped.
		//
		// AND THE CEILING LINE NAMES NO RUNG, because none of them supplied
		// anything — the emptiness law, and the decision beside it already says
		// what happened.
		return checkpointHandover{decision: checkpointCeilingNoBrief}
	}
	// AND A HANDOVER STARTS NOTHING WHEN EVERYTHING IT COULD CARRY IS ABOUT WORK
	// ALREADY OUT.
	//
	// THE FLOOR IS THE ONE RUNG THE REDUCTION CANNOT REACH. The two above it were
	// written by models and are blanked when they assign work this conversation is
	// holding; the floor is the PERSON'S OWN SENTENCE, and nobody on this road may
	// edit that. But the sentence that opened the measured incident was itself
	// coordination — "land everything once the other two report" — so a ladder that
	// had blanked both upper rungs came to rest on it and started a worker on the
	// very duty the reduction had just taken away. That is the bug reproducing
	// through the last door.
	//
	// SO THE ANSWER IS NOT TO EDIT IT, IT IS TO DECLINE. There is nothing here
	// anybody else could be given, which is a different fact from having nothing
	// written down at all ([checkpointCeilingNoBrief]) and is spelled apart from it:
	// one is a road that could not write a page, this is a road that wrote one and
	// found it was somebody else's already.
	//
	// AND THE WORK IS NOT LOST, BECAUSE NOTHING TOOK IT. The turn carries on, the
	// model that was doing the work still holds it, and what stayed is in the file
	// beside the drawing it came out of (sessionfile.go's [journalMark]'s `kept`).
	//
	// SO NOTHING IS WRITTEN INTO THE TRANSCRIPT HERE, and that is the difference
	// between this ending and the moved one at the bottom of this function. That
	// record exists because a MOVED turn is SEALED with the person's request
	// unanswered and the next turn would open on it; a turn that carries on is not
	// sealed, and an assistant message the model did not write appearing in the
	// middle of its own context is a thing this file has never done. The two other
	// non-moving endings ([checkpointCeilingNothing], [checkpointCeilingNoBrief])
	// record nothing for the same reason.
	//
	// AND THE TURN IS NOT ENDED EITHER, which is the same answer `dropped:no-brief`
	// gives and is not a gap to be closed here: what bounds a turn that has passed
	// its ceiling is the carried-on counter, and ENDING one is its own road (#513).
	//
	// IT FIRES ONLY ON WHAT SURVIVED THE LADDER, so a goal that is real work is
	// handed over exactly as it was before any of this existed.
	//
	// ── AND THE READING OF THE GOAL IS NOT WHAT CLOSES THE FLOOR ──
	//
	// A DRAWING THIS HARNESS HAD TO TAKE APART IS NOT A DRAWING A BARE SENTENCE CAN
	// STAND IN FOR. `namesHeldWork` reads a number beside the noun or a name quoted
	// whole, and the sentence that opened the measured incident was neither — "land
	// everything once the other two report" refers to two pieces without naming
	// either. A gate resting on that reading alone would be a gate the incident
	// itself walks through, so the second half of this rests on a FACT the harness
	// already holds: the drawing had to be divided at all.
	//
	// THE ASK IS THE LEAST SPECIFIC DOCUMENT ON THE TABLE. It is the person's whole
	// sentence, typed before any of this happened and never written for a worker;
	// when the drawing made out of that same turn had to be split into what can go
	// and what cannot, the sentence is BY CONSTRUCTION about the mixture — it is the
	// one document on the road that could not have distinguished the halves, because
	// it predates the reading that found them.
	//
	// SO THIS NARROWLY OVERRIDES "A BLIND WORKER BEATS A STALLED CHAT". That law
	// stands everywhere else on this road and is why the ask is a rung at all; here,
	// and only here, a worker opening on the bare ask would be opening on the very
	// mixture the reduction exists to prevent, and a blind worker does not beat a
	// duty nobody can discharge.
	//
	// AND THE PRICE IS SAID PLAINLY: where BOTH model rungs failed on provider
	// faults and the person's sentence happened to be self-contained after all, a
	// handover that would have been fine is dropped and the turn carries on. That is
	// the direction this errs in, deliberately, and it costs a conversation some
	// parallelism where the other direction cost a worker its whole deadline.
	saysHeldWork := len(read.held) > 0 && namesHeldWork(goal, read.held)
	bareAskOnADividedDrawing := carried == carryRungAsk && strings.TrimSpace(read.ownRemainder) != ""
	if saysHeldWork || bareAskOnADividedDrawing {
		hub.send(Event{Kind: EventNotice, Text: checkpointHeldWholeNote})
		// AND THE ROW SAYS WHY. `dropped:work-already-out` names the ending; the
		// rung's own word for it names what was already out, and the two belong on
		// one line so that a grep for the decision does not send somebody hunting
		// for the reason in a ladder that may not even have one.
		return checkpointHandover{decision: checkpointCeilingHeldWork, reason: carryHeldWork}
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
	//
	// AND IT SAYS SO WHEN THE WORK IS GOING WITH NOTHING BUT THE ASK. A move that
	// could carry no state beyond the person's own sentence is still allowed to
	// proceed — a blind worker beats a stalled chat — but a person watching their
	// turn move is owed the difference, because it is the difference between a task
	// that starts where the turn got to and a task that starts over ([carryLine]).
	line = carryLine(line, carried, wrote)
	// AND IT SAYS SO WHEN PART OF THE DRAWING DID NOT GO. A person watching their
	// turn move is owed the difference between "this is now somebody else's" and
	// "the handable half is now somebody else's and the rest is still yours" —
	// otherwise the coordination they asked for looks dropped, and the next thing
	// they do is ask for it again.
	if strings.TrimSpace(read.ownRemainder) != "" {
		line += checkpointHeldRestNote
	}
	hub.send(Event{Kind: EventNotice, Text: line})
	// AND THE TASK IS NAMED FROM THE PERSON'S OWN WORDS AND NEVER FROM THE DOWRY.
	// The other door into [Agent.launchRouteTask] cuts its title off the front of a
	// goal a judge wrote to be a goal, which is survivable there; here the goal is
	// a continuation written on a transcript full of tool calls, and its first line
	// has been measured arriving as a provider's tool-call sentinel and as the
	// closing remark of a finished answer. The person's sentence is the one thing
	// on this road nobody writes, so it is the one thing that cannot come back as
	// machinery — and the namer improves it a second later anyway (taskname.go).
	said, id := a.launchRouteTask(hub, verdict, asked, drawn, ahead)

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
	//
	// AND A PART THAT COULD NOT BE HANDED TO ANYBODY IS STILL WORK, AND IT STAYS
	// WITH THE ONLY READER THAT CAN SEE ITS OBJECT. This is the load-bearing half
	// of the reduction (checkpoint_custody.go): withholding the coordination part
	// from the worker's brief protects the worker, and writing it down HERE is
	// what keeps the person's own ask alive — the turn woken by tasks 4 and 8
	// landing opens on this line and finds the integration, the review and the
	// pull request still owed, in the order they were drawn. Without it the
	// reduction would be a harness quietly dropping half of what was asked for.
	a.record(textMessage("assistant", line+"\n"+said+heldRestRecord(read.ownRemainder)))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(*turn, started, model)})
	// And the name, on the terms every other turn shape takes it (title.go).
	a.maybeTitle(ctx, hub)
	return checkpointHandover{moved: true, decision: checkpointCeilingMoved, taskID: id, carry: carried}
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
	// carry names the rung of the brief ladder that supplied what the worker
	// opened on, and it is the field the measured failure needed: two handovers
	// that both read `moved` are not the same event when one of them carried the
	// person's bare sentence (see the carry ladder above).
	carry string
	// reason is why, WHERE THE DECISION WORD DOES NOT ALREADY SAY IT. It is empty
	// on nearly every ending — the ladder's own lines say why a brief could not be
	// written, and a turn two minds agreed was finished has no reason to give — and
	// carries one where an autopsy grepping the word would otherwise be left
	// looking (sessionfile.go's [journalCeiling]).
	reason string
}

// endTurnUnderSteward puts a HANDOVER to the session's goal owner, and ends the
// turn where the answer is that the run is over.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// A handover SEALS A TURN. Every other way a turn ends under a steward passes
// through [Agent.checkpointReopen], which reads what is left and asks the
// principal what to do about it; this road asked nobody anything. Three
// unattended cells (one model, `--yolo`, a fifteen-minute wall) each handed
// their only turn to a task inside two minutes, and from that moment on the only
// thing that could have ended the run was a landing waking the head — which
// woke a turn that handed over again, and again, until the wall. Each cell
// finished its work, was green on the tree, and still ran to the wall with its
// top task reading `needs you` (#513).
//
// ── SO EVERY HANDOVER ROAD IS READ, AND ONE THING BOUNDS WHAT THE READING DOES ─
//
// A TURN IS NOT ENDED OVER RESULTS ITS MODEL HAS NOT READ. This road is usually
// reached at a step boundary, with a batch's results in the transcript and the
// model yet to say a word about them, and "the ask is finished" is not a claim
// anybody may make over the top of that — the results may hold the failure that
// answers it. So DONE IS NEVER AN ENDING HERE, and it is structural rather than
// guarded: the only verb this function acts on is the stop. The ask being
// finished is decided at the next stopped turn, where the model HAS read them
// and [Agent.checkpointReopen] already asks ([Agent.decideHandover] states what
// that costs).
//
// THAT LAW IS ABOUT DONE AND NOTHING ELSE, which is why the READING happens on
// every road whatever the model last did. A ceiling can only carry on or stop,
// and a ceiling that skipped the reading because the last thing in the
// transcript was a batch is a ceiling deciding an ending with the one opinion
// that matters left unasked — measured at round 40 of the attrs cell, where the
// handover the session had asked for one second earlier was thrown away (#513).
// Where a reading has ALREADY been taken for this ending it is handed down
// rather than taken again (see the `taken` argument).
//
// WHAT THE READING BUYS IS THE STOP AND THE OVERRULE. The stop: a run whose
// budget has gone, or whose guard has fired, or that is about to write down what
// it wrote down last time, ends here with its reason instead of paying for a
// handover and a task nobody will read. The overrule: a goal owner that says the
// ask is not finished is the one reader the two-minds decline may not talk over
// ([Agent.handOverRunningTurn]).
//
// AND THE READING AND THE SEAL ARE ONE STEP, taken without letting go of the
// graph's lock in between ([Agent.sealTurnWithNothingMoving]): a task admitted
// between the two — by a landing, by another window — is work this session has
// that the reading did not, and a turn sealed over it would be an ending declared
// across live work. So a stop over work that is moving becomes the carry-on it
// actually is, the work moves onto a task as it always did, and the goal owner
// says the same thing again at the next ending. THE ONE EXCEPTION IS THE
// BUDGET'S OWN STOP ([Decision.Spent]): a run that has spent its hours cannot buy
// the wait, so it seals over the moving work and says out loud that it did.
//
// AND A PERSON'S SESSION NEVER REACHES ANY OF IT. [Agent.steward] is the gate,
// so an attended turn hands over precisely as it always has and nothing new is
// spent, said or written down on its behalf.
//
// THE TURN IS SEALED HERE, because the callers read `moved` as "the turn is
// over" and return out of the loop without sealing (loop.go). The note is
// recorded as the turn's last words for [Agent.handOverRunningTurn]'s own
// reason: the next turn would otherwise open on a batch of tool results with
// nothing saying why the model stopped talking.
func (a *Agent) endTurnUnderSteward(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string, taken *Decision) (stewardReading, checkpointHandover, bool) {
	if a.steward() == nil {
		return stewardReading{}, checkpointHandover{}, false
	}
	// A READING ALREADY TAKEN FOR THIS ENDING IS USED, NEVER TAKEN AGAIN.
	//
	// [Agent.checkpointReopen] puts this very question to this very principal and
	// then walks the ladder with no batch, so the write seam or the ceiling can
	// fire in the next breath. Asking a second time over one reading is not a
	// second opinion: it is the standstill floor ([Steward.standstill]) being
	// shown the same unmet set twice with no reply in between, stopping a run
	// that had just been told to carry on. So the answer travels down the ladder
	// instead, which is also the only reason the road below can be told the goal
	// owner overruled its decline — it returned early here before this, and the
	// measured cell's ceiling at round 40 dropped a handover the session had
	// asked for one second earlier (#513).
	//
	// IT IS NEVER A STOP. That road returns out of [Agent.checkpointReopen]
	// before the ladder is walked at all, so what is handed down is a carry-on
	// and the sealing below is reached only by a reading taken here.
	if taken != nil {
		return stewardReading{read: true, decision: *taken}, checkpointHandover{}, false
	}
	// THE READER'S LINE IS TAKEN THE WAY A STOPPED TURN TAKES IT, which costs
	// nothing on an install with no mastermind: [Agent.readRemains] answers ""
	// without a call when there is nobody to ask, and the decision is then made
	// on what landed and what the checks said, exactly as #507 makes it. What was
	// said is the model's own last words, so a principal that reads them is shown
	// what this session said rather than a blank where its voice should be.
	decision := a.decideHandover(ctx, a.readRemains(ctx), checkpointLastSaid(a.snapshot()))
	// THE ROW IS WRITTEN ON THE WAY OUT, whichever road is taken from here. What
	// the goal owner made of an ending includes what became of its answer — a
	// stop the moving work turned back into a carry-on is a different thing to
	// have decided than a stop — and a row written before the flight was read
	// could not say which of the two happened.
	defer func() { a.journalDecision(decision) }()
	if decision.Verb != DecideStop && decision.Verb != DecideDone {
		return stewardReading{read: true, decision: decision}, checkpointHandover{}, false
	}
	// DONE SEALS, EXACTLY AS A STOP DOES. A handover is an ending the principal
	// reads, and an ending it has answered "done" to is an ending: the ask is
	// met, so there is no work left to move and no task worth starting. It used
	// to hand over anyway, on the reasoning that a step boundary is a moment the
	// model has not read its last results and "finished" should wait for the
	// next stopped turn. Measured (#513), a cell's goal owner said done at 5m10s
	// over a green tree, the write seam handed over nineteen seconds later, and
	// the two tasks it started ran to the fifteen-minute wall. The principal's
	// done is not the model's claim over unread results: it is read off the
	// tree, the landings, the checks and a second reader, none of which another
	// round of the model would have added to.
	usage, moving, sealed := a.sealTurnWithNothingMoving(decision.Spent, *turn, started, model)
	if !sealed {
		decision = heldForMovingWork(decision, moving)
		return stewardReading{read: true, decision: decision}, checkpointHandover{}, false
	}
	note, ending := checkpointDoneNote, checkpointCeilingDone
	if decision.Verb == DecideStop {
		if len(moving) > 0 {
			decision.Reason += stopLeftItMovingTail
		}
		note, ending = checkpointStoppedNote+decision.Reason, checkpointCeilingStopped
	} else {
		// A DONE THAT SEALED IS FINISHED WITH WHAT IT MADE, so the tidy the
		// stopped-turn road takes with its second reading is taken here, now that
		// the turn is known to end ([Agent.decideHandover]).
		a.sweepSession(reconcile(a.createdList(), a.deliverableTree()))
	}
	hub.send(Event{Kind: EventNotice, Text: note})
	a.record(textMessage("assistant", note))
	hub.send(Event{Kind: EventTurnDone, Usage: usage})
	// And the name, on the terms every other turn shape takes it (title.go).
	a.maybeTitle(ctx, hub)
	return stewardReading{read: true, decision: decision},
		checkpointHandover{moved: true, decision: ending}, true
}

// checkpointDoneNote is the ONE LINE a person reads when a handover met a goal
// owner that had just read the ending and said the ask is finished. It is the
// done twin of [checkpointStoppedNote]: the turn ends here and nothing is
// started.
const checkpointDoneNote = "finishing here · what was asked is done"

// stewardReading is what a handover road learns from the session's goal owner,
// and it exists so the roads below can tell THREE things apart that a bare
// [Decision] cannot: a session with no goal owner at all, one that was read and
// said carry on, and one that was read and ended the turn.
//
// A PERSON'S SESSION READS AS UNREAD, which is the point. [Person] is never
// asked here, so every rule written against this answers false for them and the
// roads below behave exactly as they did before any of this existed.
type stewardReading struct {
	// read is whether there was a goal owner to ask at all.
	read bool
	// decision is what it answered, and it is meaningless unless read.
	decision Decision
}

// carriesOn answers the one question the handover road asks of it: did the
// session's goal owner just say the ask is NOT finished?
//
// It is the clause that outranks the two-minds decline, so it is deliberately
// narrow — an unread reading and any verb but carrying on both answer false, and
// the decline stands where it always did.
func (r stewardReading) carriesOn() bool {
	return r.read && r.decision.Verb == DecideCarryOn
}

// remainder is the goal owner's own account of WHAT IS LEFT, for the road that
// has to hand a writer something and has just been told the running model's
// answer was the token.
//
// It is empty for everybody else, which keeps the ladder below exactly as it
// was: a person's session, a session with no goal owner, and a reading that did
// not carry on all hand the writer nothing and it falls to the ask and the
// digest as it always has.
func (r stewardReading) remainder() string {
	if !r.carriesOn() {
		return ""
	}
	return strings.TrimSpace(r.decision.Brief)
}

// stopLeftItMovingTail is said by the one stop that may end a turn over work
// that is still going, so neither the person's line nor the journal's row claims
// a quiet ending it did not have ([Decision.Spent]).
const stopLeftItMovingTail = " · work was still going and was left where it was"

// heldForMovingWork turns a stop the moving work would not let end the turn into
// the carry-on it actually became.
//
// THE ROW HAS TO SAY WHAT HAPPENED, not what was first answered: the work moved
// onto a task and the session went on, so a row reading `stop` beside it would be
// the journal disagreeing with the transcript. The reason says why the stop was
// not taken, which is the only part of the original answer still worth keeping —
// the goal owner has not changed its mind, and says so again at the next ending.
func heldForMovingWork(decision Decision, moving []string) Decision {
	brief := decision.Brief
	if strings.TrimSpace(brief) == "" {
		// A HELD DONE HAS NO BRIEF OF ITS OWN — nothing was left — so the
		// continuation says the one true thing rather than falling back to the
		// person's bare ask and starting the work over: what is left is the work
		// still moving, by name.
		brief = heldDoneBrief + strings.Join(moving, ", ")
	}
	return Decision{
		Verb:     DecideCarryOn,
		Brief:    brief,
		Reason:   stopHeldReason,
		Observed: decision.Observed,
	}
}

// heldDoneBrief opens the carry-on a done becomes when work is still moving:
// nothing is left but that work, and the names follow.
const heldDoneBrief = "nothing is left to do but wait for the work still running: "

// stopHeldReason is why a stop did not end the turn it was answered at.
const stopHeldReason = "work is still going, so it was not ended here"

// sealTurnWithNothingMoving is [Agent.endTurnUnderSteward]'s seal, AND THE
// READING THAT ALLOWS IT, IN ONE STEP.
//
// The flight used to be read with the graph's lock taken and let go again, and
// the turn sealed a few lines later: a task admitted in the gap — by a landing
// coming home, by another window, by this turn's own last batch — was work the
// session had that the reading did not, and the turn was sealed over it. So the
// lock is held ACROSS BOTH: nothing can be admitted between the answer and the
// ending it justifies, because admitting takes the same lock (task_run.go).
//
// ONE STOP IS ALLOWED TO SEAL ANYWAY, and it is the budget's ([Decision.Spent]).
// A run that has spent its hours or its money cannot buy the wait: letting the
// moving work finish is more of exactly the thing that ran out. What it owes
// instead is the truth about it, which is why the moving work is handed back to
// the caller and said out loud ([stopLeftItMovingTail]).
//
// WHAT IS HELD UNDER THE LOCK IS THE SEAL AND NOTHING ELSE. [Agent.sealTurn]
// takes the agent's own lock and writes the journal's usage line; the notice, the
// transcript line and the naming call all happen after this returns, so nothing
// that fans an event out to a surface is waiting on the task graph.
func (a *Agent) sealTurnWithNothingMoving(spent bool, turn Usage, started time.Time, model string) (Usage, []string, bool) {
	graph := a.tasker()
	if graph == nil {
		return a.sealTurn(turn, started, model), nil, true
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	moving := graph.flightLocked().moving
	if len(moving) > 0 && !spent {
		return Usage{}, moving, false
	}
	return a.sealTurn(turn, started, model), moving, true
}

// decideHandover puts a handover to the principal, and it is
// [Agent.decideRemains] with ONE THING MOVED.
//
// THE SECOND READING IS TAKEN HERE TOO. Done is an ending on this road now
// ([Agent.endTurnUnderSteward]), so a done that had not been answered by
// re-running the session's declared checks from clean would be a run finishing
// on a done nobody checked — the one thing the stopped-turn road exists to
// prevent. It used to be left out because done did not end a handover, and the
// checks would have been a process each bought to change nothing.
//
// WHAT IS MOVED IS THE DONE'S TIDY. On the stopped-turn road the sweep rides
// with the second reading; here a done can still be HELD over work that is
// moving and turned back into a carry-on, and a sweep taken before that was
// known would delete what the session is about to go on working with. So the
// caller sweeps once the turn is actually sealed. The stop's own tidy stays
// where it was, for [Agent.decideRemains]'s reason: a run that stopped is the
// ending most likely to leave a mess.
//
// AND THE ANSWER IS JOURNALED BY THE CALLER, whichever it is: the row says what
// the goal owner made of every ending rather than only of the ones it acted on,
// and it is written where what BECAME of the answer is also known
// ([Agent.endTurnUnderSteward]).
func (a *Agent) decideHandover(ctx context.Context, reader readerLine, said string) Decision {
	remains := a.remainsFor(said, reader)
	a.journalAbsorbed(remains)
	decision := a.who().Decide(remains)
	if decision.Verb == DecideDone && remains.Acceptance != "" {
		decision, _ = a.decideOverTheChecks(ctx, remains)
	}
	if decision.Verb == DecideStop {
		a.sweepSession(reconcile(a.createdList(), a.deliverableTree()))
	}
	return decision
}

// checkpointBrief is the DRAFT of the dowry: what this turn found out, written
// down by the one reader that holds it.
//
// ── IT IS A DRAFT NOW AND IT USED TO BE THE DOCUMENT ──
//
// Everything below about WHY it is asked of the running model is unchanged and
// still true: this model is the only one in the building that knows what the turn
// found out. What changed is what happens to the answer. It used to become the
// worker's brief with two structural tests in front of it — the remains contract
// and [briefIsProse] — and a measured run walked straight between them: 5,882
// characters, 85 clauses of which 39 distinct, 62% one six-sentence loop, and a
// cold worker was started on it. So this hands a DRAFT to [Agent.writeHandoff]
// and the mastermind there writes what the worker actually opens on.
//
// AND WHAT IT REPORTS ON FAILURE CHANGED WITH IT. It used to answer the person's
// own ask when the draft was unusable, which made a bare sentence the SECOND best
// document on the table. It now answers the empty string, because the second best
// document is the one the writer below composes out of the digest, and the bare
// ask is what is left when even that cannot be had.
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
func (a *Agent) checkpointBrief(ctx context.Context, turn *Usage, model string) (string, bool, carryStep) {
	// DO NOT RE-READ THE CONVERSATION FOR A REDIRECT. The measured stall sent
	// fifty-seven messages to write a brief the person had just spoken in one
	// sentence (F13). Their words stand as the brief; the draft rung is skipped.
	if a.interrupt.redirecting() {
		return "", true, carryStep{rung: carryRungDraft, outcome: carrySkipped, reason: carryRedirect}
	}
	messages := append(a.snapshot(), textMessage("user", checkpointHandoffAsk))
	// WITHOUT THE TURN'S STREAM, for the reason every errand in this package is
	// made without it (auxiliary.go's [Agent.callRole]): the loop installed an
	// observer that types deltas into the room as the assistant speaking, and this
	// answer is a worker's instruction rather than a word to the person. Left on
	// the stream it would paint the brief over the top of the answer it is ending.
	//
	// AND WITHOUT THE TURN'S ROLE EITHER. This is a fold-up written beside an
	// answer rather than the answer itself, so it is priced and drawn as the
	// errand it is: a person is reading the turn this ends, and the clock over
	// their answer is not this call's to move (internal/lane's roles.go).
	response, err := a.client.CompleteWithMessages(
		provider.WithRole(provider.WithoutStream(ctx), lane.RoleAuxiliary), messages,
		ai.WithModel(model), ai.WithMaxTokens(checkpointBriefTokens))
	if err != nil || response == nil {
		// AND THE FAULT IS CARRIED OUT OF HERE RATHER THAN SPELLED AS SILENCE. This
		// rung answering "" used to be indistinguishable from a rung that answered
		// machine markup, and the caller's journal cannot tell a fault from a
		// degeneration it was never told about.
		reason, said := carryFault(err)
		return "", true, carryStep{rung: carryRungDraft, outcome: carryFailed, reason: reason, said: said}
	}
	// The person pays for it on the turn it belongs to rather than out of the
	// auxiliary pocket, because this is the conversation's own model reading the
	// conversation's own transcript — the errand pocket is for the session's
	// side-calls, and this is the last step of the answer.
	//
	// AND ITS ROW CARRIES NO LANE. This request was not streamed and nothing
	// watched it, and the turn's own figures filed against it would be a
	// measurement of one request written down about another (usage_ledger.go's
	// emptiness law for the five lane keys).
	turn.Turns++
	a.addUsage(turn, response, model, provider.ServedEndpointFrom(ctx).Name(), laneFacts{})

	brief := strings.TrimSpace(response.Text())
	// THE REMAINS CONTRACT IS READ FIRST, because it is the only answer here that
	// is about the WORK rather than about the document.
	if declaresNothingLeft(brief) {
		return "", false, carryStep{rung: carryRungDraft, outcome: carryNothingLeft}
	}
	// An empty reply, a whitespace one and a sentinel are all the same failure to
	// this line: nothing came back that anybody could work from.
	//
	// AND SO IS A LOOP, which is the second structural test and the one the
	// measured failure needed ([briefRepeats]). A draft that has stopped saying new
	// things is not findings this turn holds — it is a tired model filling its
	// token budget — and the writer below is better off with the digest alone than
	// with a document that will drag its own repetition into the spec.
	if !briefIsProse(brief) {
		return "", true, carryStep{rung: carryRungDraft, outcome: carryDegenerate,
			reason: carryNotProse, said: carrySaidNothingNew}
	}
	if briefRepeats(brief) {
		return "", true, carryStep{rung: carryRungDraft, outcome: carryDegenerate,
			reason: carryDraftLooped, said: carrySaidNothingNew}
	}
	// THE SAME BOUND EVERY BRIEF ON THIS ROAD IS HELD TO, and that constant rather
	// than a second number of this file's own (task_shape.go's
	// taskShapeBriefLimit): two spellings of one bound are two answers to the
	// question of how long a worker's instruction may be.
	brief = clip(brief, taskShapeBriefLimit)
	return brief, true, carryStep{rung: carryRungDraft, outcome: carryWritten, chars: len(brief)}
}

// writeHandoff is the OTHER HALF of the dowry: the mastermind that turns what the
// running model drafted into the document a cold worker opens on.
//
// ── WHY THERE ARE TWO CALLS AND NOT ONE ──
//
// They hold different things and neither can be the other. The runner has the
// FINDINGS — it read the files, it ran the suite, it knows which of four
// approaches was ruled out — and nothing else in the building does. The
// mastermind has the JUDGEMENT: it is fresh, it is not the model that just spent
// forty rounds, and it is the tier this file has twice measured as the only one
// that answers this kind of question at all ([Agent.readMark]).
//
// Asking one model to be both was the shipping arrangement and it was measured
// failing in the way a tired model fails: it wrote until it hit its cap, and what
// it wrote past the point of having anything to say was the same six sentences
// over and over. Asking the mastermind ALONE would lose the findings, which is
// exactly [Agent.checkpointBrief]'s own argument against the shaper and the state
// card. So both, in that order.
//
// ── WHAT IT IS SHOWN ──
//
// The person's ask verbatim, the state card where the session keeps one
// (card.go), the digest WITH the results in it ([checkpointDigest]), and the
// draft. The digest is the same page the mark's reader was shown a moment ago and
// is taken from the read rather than rebuilt, so the two calls cannot disagree
// about what happened this turn; a ceiling reached on a reader nobody could
// reach has no page, and one is assembled here rather than the writer being sent
// a document with a hole in it.
//
// ── AND IT IS READ EXACTLY AS THE DRAFT IS ──
//
// Prose and not a loop, on the same two structural tests, because a mastermind
// asked to write two thousand tokens can run out of things to say too. A
// degenerate answer buys ONE regeneration ([checkpointBriefTries]) and then this
// gives up and answers "", which drops the caller onto the draft and then onto
// the person's own sentence. It never retries a fault: a provider that failed is
// a provider, and the ladder underneath is what that failure is for.
//
// ── AND IT IS BILLED UNDER ITS OWN NAME ──
//
// To the errand pocket and tagged with the role ([Agent.addAuxiliaryUsageAs]),
// which is where this parts company with the draft. The draft is the
// conversation's own model reading the conversation's own transcript — the last
// step of the answer, and the turn pays. This is a side-call to a different model
// that the person did not ask for, and a journal that could not name it would
// leave a mastermind-priced line on the bill with nothing beside it saying what
// it bought.
func (a *Agent) writeHandoff(ctx context.Context, asked, digest, draft string) (string, carryStep) {
	// THE REDIRECT IS THE BRIEF. A mastermind asked to rewrite the conversation
	// after Esc is the 36-second stall (F14) and the second of the two planner
	// passes one keystroke used to buy (F17).
	if a.interrupt.redirecting() {
		return "", carryStep{rung: carryRungHandoff, outcome: carrySkipped, reason: carryRedirect}
	}
	if strings.TrimSpace(digest) == "" {
		digest = checkpointDigest(asked, a.snapshot())
	}
	page := checkpointHandoffPage(asked, a.stateCardText(), digest, draft)
	if page == "" {
		// NOTHING TO WRITE FROM IS NOT A DOCUMENT. A turn with no ask, no account
		// and no draft has nothing a second mind could compose out of, and a call
		// made on an empty page is a mastermind asked to invent an instruction.
		//
		// AND A RUNG NOBODY EVEN ASKED IS STILL A RUNG THAT SAYS SO. `skipped` with
		// its reason is a different fact from a writer that faulted, and reading the
		// two as one silence is what the measured failure cost.
		return "", carryStep{rung: carryRungHandoff, outcome: carrySkipped,
			reason: carryNoPage, said: carrySaidNoMaterial}
	}
	ctx, done := context.WithTimeout(ctx, checkpointHandoffWindow)
	defer done()
	messages := []ai.Message{textMessage("user", page+"\n\n"+checkpointHandoffWriteAsk)}
	for try := 0; try < checkpointBriefTries; try++ {
		// NO BELT, for [Agent.checkpointBrief]'s reason: a writer with no hand to
		// reach for can only answer with the document.
		response, writer, err := a.callRole(ctx, roles.RoleHandoff, "", messages,
			ai.WithMaxTokens(checkpointBriefTokens))
		if err != nil || response == nil {
			// THE PROVIDER'S OWN WORDS COME OUT WITH THE FAILURE. On the measured run
			// this rung died on [checkpointHandoffWindow] — ninety seconds, to the
			// millisecond, with no answer — and the file said nothing at all, because
			// "" is the same answer this returns for five other reasons.
			reason, said := carryFault(err)
			return "", carryStep{rung: carryRungHandoff, outcome: carryFailed, reason: reason, said: said}
		}
		a.addAuxiliaryUsageAs(response, writer, 1, auxRoleHandoff)
		brief := strings.TrimSpace(response.Text())
		if !briefIsProse(brief) {
			return "", carryStep{rung: carryRungHandoff, outcome: carryDegenerate,
				reason: carryNotProse, said: carrySaidNothingNew}
		}
		if !briefRepeats(brief) {
			brief = clip(brief, taskShapeBriefLimit)
			return brief, carryStep{rung: carryRungHandoff, outcome: carryWritten, chars: len(brief)}
		}
	}
	return "", carryStep{rung: carryRungHandoff, outcome: carryDegenerate,
		reason: carryStillLoops, said: carrySaidNothingNew}
}

// checkpointHandoffPage lays out what the writer is shown. It is a function of
// its own for [composeBrief]'s reason (task_brief.go): a layout written at the
// call site is a layout that will disagree with itself the day a second caller
// appears, and the headings are read by a model rather than by a person.
//
// AN EMPTY SECTION IS ABSENT, not an empty heading — the emptiness law, applied
// to a document a model reads. A session with no state card, a turn whose draft
// was refused, a reader nobody could reach: each simply has fewer sections.
//
// THE DRAFT IS CLIPPED LIKE EVERY OTHER BRIEF ON THIS ROAD, so a runner that
// filled its whole token budget cannot push the ask and the account off the top
// of the page it is supposed to be read against.
func checkpointHandoffPage(asked, card, digest, draft string) string {
	var out strings.Builder
	section := func(heading, body string) {
		if body = strings.TrimSpace(body); body == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(heading)
		out.WriteString("\n")
		out.WriteString(body)
	}
	section(checkpointHandoffAskedHeading, clip(strings.TrimSpace(asked), briefAskLimit))
	section(checkpointHandoffStateHeading, card)
	section(checkpointHandoffWorkHeading, digest)
	section(checkpointHandoffDraftHeading, clip(strings.TrimSpace(draft), taskShapeBriefLimit))
	return out.String()
}

// briefRepeats reports that a brief has stopped saying new things.
//
// IT IS THE SECOND STRUCTURAL TEST AND IT IS NOT A TEST FOR CONTENT. [briefIsProse]
// asks whether an answer is words at all; this asks whether those words are still
// going somewhere. Neither reads what the brief is ABOUT, because a rule that did
// would be a rule tuned to one kind of work — the law this whole file is held to.
//
// WHAT IT COUNTS IS DISTINCT SENTENCES AGAINST TOTAL SENTENCES, folded through
// [normalizedWords] so that "Also: rerun the suite." and "**Also: rerun the
// suite**" are the one sentence they plainly are. A model that has run out of
// things to say does not stop, it loops — the measured brief was 85 clauses of
// which 39 were distinct — and a loop is the one degeneration that is invisible
// to every test for shape.
//
// A SHORT BRIEF IS NEVER DEGENERATE, which is [checkpointBriefSentences]: a ratio
// over four sentences is noise, and refusing an honest short brief would cost a
// regeneration on exactly the document that needed none.
//
// SENTENCES AND NOT N-GRAMS, deliberately. A sliding window over tokens catches
// the same loop and catches a great deal else — a brief that names one file in
// six places, a list with a repeated stem — and this file would rather miss a
// degeneration than refuse a brief that was doing its job.
func briefRepeats(brief string) bool {
	sentences := briefSentences(brief)
	if len(sentences) < checkpointBriefSentences {
		return false
	}
	distinct := make(map[string]bool, len(sentences))
	for _, sentence := range sentences {
		distinct[sentence] = true
	}
	return len(distinct)*100 < len(sentences)*checkpointBriefVariety
}

// briefSentences cuts a brief into the units [briefRepeats] counts, normalised so
// that two spellings of one sentence are one string.
//
// IT CUTS ON LINES AND ON SENTENCE PUNCTUATION BOTH, because a brief written as a
// list repeats whole bullets and a brief written as prose repeats whole sentences,
// and a rule that knew only one of those shapes would be blind to half of what it
// is for. A piece with no letters in it is not a sentence — a bullet's dash, a
// rule of hyphens — and dropping those is what stops a heavily formatted brief
// counting its own decoration as variety.
func briefSentences(brief string) []string {
	pieces := strings.FieldsFunc(brief, func(letter rune) bool {
		switch letter {
		case '.', '!', '?', ';', '\n':
			return true
		}
		return false
	})
	sentences := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if words := normalizedWords(piece); len(words) > 0 {
			sentences = append(sentences, strings.Join(words, " "))
		}
	}
	return sentences
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
