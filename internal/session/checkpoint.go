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
// is a fact rather than a guess. That is what this file is. The race lands at
// the same boundary this does and through the same door
// ([Agent.handOverRunningTurn]) — it is the same event on a different clock, and
// what differs is only which of them noticed first.
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
//   - THE TRIGGER IS DETERMINISTIC COST AND NEVER CONTENT. Nothing here reads a
//     word of what the turn is about. A counter of finished tool rounds is the
//     whole of the input, so there is no phrasing that defeats it and no kind of
//     work it is tuned for.
//   - THE JUDGEMENT STAYS THE MODEL'S, up to a point. At each mark the harness
//     states the fact and asks one question; the model answers it in the turn it
//     is already in, and a turn that is genuinely one long job carries on.
//   - AND PAST THE LAST MARK THE HARNESS STOPS ASKING. Two refusals to hand over
//     are a judgement; a third is momentum. At the ceiling the turn ends and the
//     REMAINING work moves onto the one road, where it is watched — and remaining
//     is the whole of what is still asked there ([checkpointNothingLeft]): a turn
//     that answers the handoff by saying it is finished is left to finish, which
//     is not the harness asking again but the harness having nothing to move.
//
// ── WHY THE MARKS ARE GEOMETRIC AND NOT ONE LINE ──
//
// A single threshold is a single chance to be wrong. Doubling the interval each
// time means a turn that is honestly one long job is interrupted a bounded
// number of times — three, over four times the handoff price — while a turn that
// is four jobs in a trench coat is asked early, when the findings are still worth
// handing over. The person's attention and the model's context are both spent by
// asking, so asking gets rarer as the evidence that the answer is "carry on"
// accumulates. [checkpointRatio] is the whole of that policy.
//
// ── WHAT THIS FILE DELIBERATELY DOES NOT DO ──
//
// It opens no new door into the graph. The ceiling admits its task through
// [Agent.launchRouteTask], which is the road route_judge.go already takes for
// work nobody groomed: straight to [TaskGraph.admit], no card, told after. It
// invents no new message kind either — a mark's checkpoint rides the ambient
// note lane the loop detector's nudge rides (looped.go), which is plain user-role
// text drained into the next request exactly as a person's steering is.

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

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
	// Two, because the evidence moves. A model that has answered one checkpoint
	// with "this is one job" has said something, and the harness must charge more
	// before doubting it again — otherwise the interval measures the harness's
	// impatience rather than the turn's cost. Doubling also bounds the whole
	// mechanism at a glance: three marks span four times the price, and a turn
	// can never be interrupted a fourth time however long it runs.
	checkpointRatio = 2

	// checkpointMarks is how many marks a turn has, and the LAST of them is the
	// ceiling rather than a fourth question ([Agent.checkpointRound]).
	//
	// Three, because two questions are the most a note can usefully ask. The loop
	// detector reached the same number from the other side — past two nudges the
	// notes have stopped working and a third is the harness talking to itself
	// (looped.go's loopNudgeCeiling) — and the answer here is the same answer:
	// stop writing notes and do something. What this does instead of asking the
	// person is move the work somewhere it is watched.
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
)

// checkpointNote is what the model reads at a mark, and every line of it is
// deliberate.
//
// IT IS META AND CARRIES NO THRESHOLD. It states one fact — this has cost more
// than handing it over would have — and asks one question. It never says how
// many rounds, because a number invites the model to count toward the next one
// instead of to look at the work in front of it, which is the same law
// prompts/system.md is pinned to (task_escalation_test.go).
//
// IT NAMES NOTHING ABOUT THE KIND OF WORK. aforge is a general harness: a
// research sweep, a writing project and a mechanical change are one shape of
// problem to this question, and a sentence about files would read as an
// instruction about programming to a model in the middle of a literature review.
//
// IT NAMES THE HAND, THOUGH. The measured failure is precisely a model that has
// propose_task on its belt and does not reach for it, so a note that said "hand
// it over" without saying with what would be the prompt's advice repeated at a
// model that has already not taken it. The dowry clause is system.md's law said
// once more at the moment it is needed: whoever takes this cannot see any of it.
const checkpointNote = "[checkpoint] This answer has now cost more than handing it over would have. " +
	"Say in one line what is still left: one job, or several independent parts. " +
	"Several parts — hand it over now with propose_task, and write into the brief everything you " +
	"have learned here, because whoever takes it cannot see any of this. " +
	"One job — say what is left, and carry on."

// checkpointCeilingNote is the ONE line a person reads when the harness stops
// asking and moves the work itself.
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

// checkpointHandoffAsk is what the model is asked for at the ceiling, and it is
// the LAST thing this turn does with it.
//
// IT IS NOT A THIRD CHECKPOINT. The decision is made by the time this is sent;
// what is being asked for is the dowry — the turn's findings, written down —
// because the model that has just spent the whole turn is the only reader in the
// building that holds them.
//
// IT IS SENT WITH NO BELT (see [Agent.checkpointBrief]), which is what makes it
// safe to ask a model that has spent the turn grinding: with no tool to reach
// for, the only legal answer is the document.
//
// AND IT ASKS WHAT REMAINS BEFORE IT ASKS FOR THE DOCUMENT, which is the one
// question neither clock into this door can answer for itself. The ceiling reads
// a counter and the race reads a request; neither of them can see that the turn
// finished the work thirty seconds ago. A measured turn wrote all eight files it
// was asked for inline, the race's confirm landed at the tail of it, and the
// conversion started a task on the leftovers of a finished answer — junk work,
// duplicating a turn nobody needed to duplicate. So the model that holds the
// findings is asked the question it is the only reader able to answer, and
// [checkpointNothingLeft] is the answer that stops the handover dead.
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

// isCheckpointNote reports whether one recorded message is a checkpoint this
// file wrote into a running turn.
//
// IT EXISTS FOR ONE READER, and that reader is [Agent.alreadyWorking] (task.go).
// A checkpoint lands in the transcript as plain user-role text — that is the
// whole of why it reaches the model at all — and the question "has this answer
// already done work" is answered by walking back to the last thing said to this
// agent. Without this the harness's own question would read as the start of a
// fresh answer, and the mid-answer handoff line would go missing on exactly the
// handoffs the checkpoint caused.
//
// It matches the text rather than a flag because the transcript keeps messages
// and not the [userMessage] they arrived as, and because the alternative — a
// second record of which lines the session wrote — is a second account of one
// fact that can fall out of step with the first (task.go states the same
// argument for reading this from the transcript at all).
func isCheckpointNote(message ai.Message) bool {
	if message.Role != "user" {
		return false
	}
	for _, part := range message.Content {
		if strings.Contains(part.Text, checkpointNote) {
			return true
		}
	}
	return false
}

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
}

// checkpointMarkAt is the round the nth mark stands on: the price, doubled once
// per mark already passed. It is computed from the two constants rather than
// written out as a list, so the ladder cannot disagree with the policy it is
// supposed to be.
func checkpointMarkAt(n int) int {
	at := checkpointPrice
	for step := 1; step < n; step++ {
		at *= checkpointRatio
	}
	return at
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
	if m.rounds < checkpointMarkAt(m.marks+1) {
		return 0
	}
	m.marks++
	return m.marks
}

// ── the turn's side ─────────────────────────────────────────────────────────

// checkpointRound is this file's whole place in a turn, called once from
// [Agent.runTurn] at the step boundary, after a batch's results are in the
// transcript.
//
// It reports whether THE TURN IS OVER, which is the one thing a hook could not
// have said: the control plane's law is that only pre-action may stop something
// (hooks.go), and the ceiling stops a turn. So it stands in the loop as a line of
// its own, beside the two route-judge seams that also end turns.
//
// EVERYTHING BUT THE CEILING IS AN ASIDE. A mark queues one note and the turn
// carries on exactly as it would have; a gated turn does not even count.
func (a *Agent) checkpointRound(ctx context.Context, hub *eventHub, user userMessage, meter *checkpointMeter, turn *Usage, started time.Time, model string) bool {
	if !a.checkpoints(ctx, user) {
		return false
	}
	mark := meter.round()
	if mark == 0 {
		return false
	}
	if mark < checkpointMarks {
		// THE AMBIENT LANE (agent.go), which is the lane the loop detector's nudge
		// takes and for the same two reasons: the note belongs to the turn it is
		// about, so nobody is waiting to be told about it, and a note dropped at a
		// step boundary rides into the next request exactly as a person's steering
		// does. The drain at the top of [Agent.runTurn] is what puts it in front of
		// the model, which is why nothing here has to reach the wire itself.
		a.enqueueAmbientNote(checkpointNote)
		return false
	}
	return a.checkpointCeiling(ctx, hub, turn, started, model)
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
// account of the WORK — the model was asked twice and the harness has stopped
// asking — and it reports whether the turn is over.
//
// THERE IS EXACTLY ONE ANSWER THAT LEAVES THE TURN RUNNING, and it is not a
// third refusal: it is the model saying nothing remains at all
// ([checkpointNothingLeft]). The ceiling exists to move A GRIND somewhere it is
// watched, and a turn that is finishing is not a grind — the meter simply stops
// mattering the moment the turn ends. What that answer costs, if the model is
// wrong about it, is bounded to nothing: the ladder is spent, so no mark can fire
// again, and the turn carries on to the end it said it was reaching under the
// same loop detector and the same stop key every turn has. What the OTHER
// direction would cost is what was measured — a task admitted over the top of a
// finished answer, which then stops on its own, having duplicated it.
//
// EVERYTHING ELSE IT DOES IS [Agent.handOverRunningTurn]'S, because the ceiling is
// not the only clock that can decide mid-turn that this belongs on the rail. It
// contributes the two things that are its own: the line, and a verdict ARMED TO
// SPLIT. This turn outran one pair of hands by measurement rather than by
// anybody's opinion, which is the strongest evidence of breadth any door into
// the graph has; arming costs nothing if it is wrong, because it only means the
// worker MAY discover the work is wide and the evidence gate still refuses a
// division the material does not support (task_divide.go).
func (a *Agent) checkpointCeiling(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string) bool {
	return a.handOverRunningTurn(ctx, hub, turn, started, model, checkpointCeilingNote, routeVerdict{Wide: true})
}

// handOverRunningTurn ENDS A TURN THAT IS STILL RUNNING and moves what is left
// of it onto the one road. It is one function because two clocks reach it and
// there must not be two versions of what happens next:
//
//   - the CEILING above, when an answer has cost more than handing it over
//     would have ([Agent.checkpointRound]);
//   - the RACE, when the second model reading the request said it was work and
//     the answer is only seconds old ([Agent.routeConvert], route_judge.go).
//
// The caller brings the line the person reads and a verdict carrying whatever
// its own reading knew — breadth, a done-condition, the one line about why. What
// this adds is the four things that are the same however the decision arrived:
// the dowry, the task, the gap, and a turn sealed with the transcript left in a
// state the next turn can open on.
//
// AND IT CAN DECLINE, on the one ground neither clock can see for itself: the
// model answering the dowry ask with [checkpointNothingLeft]. Both clocks decide
// on evidence that is old by the time it is spent — a counter of rounds already
// finished, a verdict about a request the turn may since have answered — and the
// model holding the findings is the only reader that knows whether there is any
// work left to hand anybody. When there is not, NOTHING HAPPENS: no task, no
// line, no gap spent, no turn sealed. The turn carries on and its own answer
// stands, which is the honest outcome for a turn that was already finishing.
//
// THE PERSON'S OWN WORDS ARE READ FIRST AND ARE NEVER WRITTEN BY ANYBODY. They
// ride the spec's request, verbatim, exactly as they do on every other door into
// the graph, and [composeBrief] prints them above the work under the rule that
// says where the two read differently theirs are what was asked for
// (task_brief.go). That is what makes the choice of brief-writer below a
// question about the DOWRY alone: no writer on this path can lose the ask,
// because the ask does not travel through any of them.
//
// AND THE GOAL IS THE DOWRY, WHICHEVER CLOCK CALLED. The model that has just
// spent the turn is the only reader in the building holding what the turn found
// out, and on the race's path that is the whole point of converting rather than
// starting over: the seconds of inline work already done are not thrown away,
// they are written down for whoever takes it. A goal written before the turn
// began — the race's own, from the request alone — would be the one thing on the
// table that knows least.
func (a *Agent) handOverRunningTurn(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model, line string, verdict routeVerdict) bool {
	asked := a.taskRequest()
	goal, remains := a.checkpointBrief(ctx, turn, model, asked)
	if !remains {
		// NOTHING HAPPENS, and that includes the line. A person told their answer
		// was being moved and then left watching it finish where it was would have
		// been told something that did not happen.
		return false
	}
	verdict.Work = true
	verdict.Goal = goal

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
	// more of it (route_judge.go), and both roads into here are exactly that
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
// which is the same loss by a different road.
//
// So it is asked of the one reader that actually holds the findings: the model
// that has just spent the turn. One more request, on the turn's own model and the
// turn's own transcript, and then the turn is over.
//
// ── AND IT IS ASKED WITH NO BELT ──
//
// No [ai.WithTools], which is the whole safety of asking a model that has just
// ground through the ceiling to do one more thing: with no hand to reach for, the
// only answer it can give is the document. That is also why this is not a third
// checkpoint — it cannot be answered with more work.
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
// request verbatim (see [Agent.checkpointCeiling]); this writes the brief and
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
	a.addUsage(turn, response)

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
