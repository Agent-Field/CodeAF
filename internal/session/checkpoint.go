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
const checkpointSketchAsk = "[checkpoint] In one line, sketch what remains as parts and arrows: " +
	"independent parts separated by ' | ', ordered steps joined by ' > '. " +
	"Example shapes: 'A | B | C' or 'A > B > C' or 'A > (B | C)'. " +
	"Nothing else on that line. Then one sentence saying what each letter is."

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
//   - `(A | B) > C` is ONE part for the same reason read from the other end: the
//     whole line is one bracketed group, so there is no top-level separator, and
//     the thing this counts is what stands at the TOP LEVEL of the line.
//   - anything with no separator at all — a word, a sentence, an apology — is one
//     part, and a CONTINUE.
//
// A separator with nothing beside it does not make a part, so a line that opens
// or ends on one counts what is actually there. Brackets of any kind nest, and an
// unbalanced closer is ignored rather than taken below zero: a shape a model
// mis-typed is still a shape, and the honest failure is to read it as narrow.
func topLevelParts(shape string) int {
	parts, depth := 0, 0
	var current strings.Builder
	closeOne := func() {
		if strings.TrimSpace(current.String()) != "" {
			parts++
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
		case '|':
			if depth == 0 {
				closeOne()
				continue
			}
		}
		current.WriteRune(letter)
	}
	closeOne()
	return parts
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
func (a *Agent) readMark(ctx context.Context) checkpointSketch {
	ctx, done := context.WithTimeout(ctx, checkpointSketchWindow)
	defer done()
	// THE TRANSCRIPT AS THE TURN HAS BUILT IT, which is the same evidence the
	// dowry ask rides and carries the same clipping: every tool result in it was
	// bounded on its way into the transcript, so the reader sees what the running
	// model sees and no second discipline of this file's own can drift from it.
	messages := append(a.snapshot(), textMessage("user", checkpointSketchAsk))
	response, reader, err := a.callRole(ctx, roles.RoleMarkReader, "", messages,
		ai.WithMaxTokens(checkpointSketchTokens),
		ai.WithTemperature(checkpointSketchTemp))
	if err != nil || response == nil {
		return checkpointSketch{}
	}
	a.addAuxiliaryUsage(response, reader, 1)
	return parseCheckpointSketch(response.Text())
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
	// drawing is still worth its call, because it is what names the parts at the
	// head of the brief the worker opens on.
	sketch := a.readMark(ctx)
	if mark < checkpointMarks {
		if !sketch.split() {
			return false
		}
		return a.handOverRunningTurn(ctx, hub, turn, started, model, checkpointSplitNote, meter.raced, sketch)
	}
	return a.checkpointCeiling(ctx, hub, turn, started, model, meter.raced, sketch)
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
// ([checkpointNothingLeft]). The ceiling exists to move A GRIND somewhere it is
// watched, and a turn that is finishing is not a grind — the meter simply stops
// mattering the moment the turn ends. What that answer costs, if the model is
// wrong about it, is bounded to nothing: the ladder is spent, so no mark can fire
// again, and the turn carries on to the end it said it was reaching under the
// same loop detector and the same stop key every turn has. What the OTHER
// direction would cost is what was measured — a task admitted over the top of a
// finished answer, which then stops on its own, having duplicated it.
//
// EVERYTHING ELSE IT DOES IS [Agent.handOverRunningTurn]'S. It contributes the
// two things that are its own: the line, and a verdict ARMED TO SPLIT. This turn
// outran one pair of hands by measurement rather than by anybody's opinion, which
// is the strongest evidence of breadth any door into the graph has; arming costs
// nothing if it is wrong, because it only means the worker MAY discover the work
// is wide and the evidence gate still refuses a division the material does not
// support (task_divide.go).
//
// THE SKETCH RIDES ALONG WHEN THERE IS ONE. The ceiling does not need it to
// decide — nothing can stop it now but the remains contract — but a drawing of
// what is left is exactly what the worker's first paragraph should be, and the
// mark that produced it was paid for either way.
func (a *Agent) checkpointCeiling(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string, verdict routeVerdict, sketch checkpointSketch) bool {
	verdict.Wide = true
	return a.handOverRunningTurn(ctx, hub, turn, started, model, checkpointCeilingNote, verdict, sketch)
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
// AND IT CAN DECLINE, on the one ground no clock can see for itself: the model
// answering the dowry ask with [checkpointNothingLeft]. Every clock here decides
// on evidence that is old by the time it is spent — a counter of rounds already
// finished, a sketch drawn a step ago — and the model holding the findings is the
// only reader that knows whether there is any work left to hand anybody. When
// there is not, NOTHING HAPPENS: no task, no line, no gap spent, no turn sealed.
// The turn carries on and its own answer stands, which is the honest outcome for
// a turn that was already finishing.
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
func (a *Agent) handOverRunningTurn(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model, line string, verdict routeVerdict, sketch checkpointSketch) bool {
	asked := a.taskRequest()
	goal, remains := a.checkpointBrief(ctx, turn, model, asked)
	if !remains {
		// NOTHING HAPPENS, and that includes the line. A person told their answer
		// was being moved and then left watching it finish where it was would have
		// been told something that did not happen.
		return false
	}
	verdict.Work = true
	verdict.Goal = sketch.head(goal)
	if sketch.split() {
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
		// DIVISION IS STILL THE WORKER'S OWN ACT AND IS NEVER MINTED FROM CHAT. The
		// parts named above are an instruction, not a graph: the worker must put its
		// own division to the evidence gate and to the reviewer inside the task, and
		// arming only means it is allowed to try.
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
	said := a.launchRouteTask(hub, verdict, asked)

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
	return true
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
