package session

// THE QUICK ROAD OUT OF A CHECKPOINT: a turn that only READ is handed to a
// quick node, and the drawing is the brief.
//
// ── WHAT THE OTHER ROAD BUYS, AND WHAT IT COSTS ──
//
// checkpoint.go's handover buys supervision: a fresh copy of the folder, a brief
// written by somebody who did not spend the turn, a checker reading the result,
// and a landing. Every one of those is worth its price when the turn being moved
// has been CHANGING THINGS — that is writeseam.go's whole argument, and the run
// it was written from was forty-eight edits in somebody's live checkout.
//
// None of it is worth anything to a turn that has read forty files and written
// none. There is nothing to isolate, because nothing was touched; nothing to
// merge, because there is no branch; nothing for a checker to judge, because the
// answer IS the last message. What the person is waiting for is the reading,
// done faster, and the full road makes them wait longer for it: two model calls
// to write a brief the drawing already is, a worktree they will never open, and
// an audit of a paragraph.
//
// ── SO THE READING IS THE WHOLE OF THE DECISION, AND IT IS A COUNTER ──
//
// The gate is [writeMeter.untouched] — the same counter the write seam prices
// its allowance against, read for zero instead of for five. It is deliberately
// not a second reading of what the turn was ABOUT: a turn is research because it
// did not touch the disk, which is a fact the harness already holds, and any
// judgement of the subject would be the content-sniffing checkpoint.go's header
// refuses in the same breath as the marks themselves.
//
// AND THE DRAWING STILL DECIDES THAT THERE IS ANYTHING TO HAND OVER. The road is
// only entered where the mark's reader drew independent parts
// ([checkpointSketch.split]), exactly as the full road is: a turn with one long
// job left in front of it is not made better by being restarted somewhere else,
// whichever kind of node takes it.
//
// ── THE ITEMS ARE THE PARTS, AND THAT IS WHY NO BRIEF IS WRITTEN ──
//
// A quick node has a `line` and `items` and nothing else to open on
// (docs/design/quick-task/DESIGN.md). The line is the person's own sentence — the
// one document on this road nobody writes — and the items are the drawing, read
// out in the order it was drawn. That is already the account of what is left, so
// [Agent.writeHandoff] is not called and the carry ladder is not walked: a
// ninety-second call to a second model, to produce a paragraph the items say
// better, is the exact wait this road exists to remove. The ceiling row says
// `quick` where it would otherwise name a rung ([carryRungQuick]).
//
// AND [taskSpec.drawn] IS LEFT ZERO. The parts are the items of ONE worker
// working through them in order; carrying the drawing as well would put the same
// parts to `divide_work` and hand them out a second time
// (task_divide_sketch.go's [Agent.sizeBeside]).

import (
	"context"
	"strings"
	"time"
)

// checkpointQuickNote is the ONE line a person reads when a write-free turn's
// parts are taken by a quick node, and it stands in for BOTH lines the other
// road writes — the ceiling's own ([checkpointCeilingNote]) and the started-task
// line under it (route_judge.go's [Agent.launchRouteTask]).
//
// IT IS ONE LINE BECAUSE THERE IS ONE EVENT. The two lines exist on the full
// road because two things happen there that a person has to be told apart: their
// turn was moved, and then a task was started somewhere else with a copy of the
// folder. Here the second half is the interesting half and it is not somewhere
// else — the work carries on in the folder they are standing in — so saying it
// twice would make one small thing sound like two large ones.
//
// IT KEEPS THE REGISTER of the family it joins ([inTheHouseRegister]): an
// observation, a middle dot, a promise, lowercase, no full stop, no machinery.
// The observation is the ceiling's own, because the ceiling is what moved this.
// The promise is the honest one for this node and is deliberately narrower than
// the other two: not "watched", not "can split", but WHERE the work is happening
// and WHAT IT TOOK WITH IT — here, in this folder, holding the transcript — which
// is the whole of what distinguishes a promotion from the task the person might
// otherwise assume started, and the half a person could not otherwise see
// (inherit.go).
const checkpointQuickNote = "this is running long · carrying on here, in this folder, with everything already read: "

// checkpointCeilingFanFull is the ending this road takes when a carry-on INSIDE a
// task would be one child too many for that task ([TaskGraph.claimChild]). It is
// spelled `dropped:` like every other non-moving ending and belongs to
// checkpoint.go's register of them; it is declared beside the only road that takes
// it because it is the only road that can be refused that way — a conversation's
// own carry-on has no parent to be the child of, and the full road takes no fan
// slot at all.
const checkpointCeilingFanFull = "dropped:parent-fan-full"

// checkpointQuickLine is that line with the node's name on the end of it.
func checkpointQuickLine(title string) string {
	return checkpointQuickNote + title
}

// promotedFromTurn answers the quick node this ending should PROMOTE the turn
// into, or nil where this turn cannot be promoted and the brief road below must
// carry it. It answers an ASK and never a spec: what the node is beyond its
// line, its items and `inherit` is decided by the one door every quick node
// comes through ([Agent.admitQuick]), and this road only says what it knows.
//
// EVERY CLAUSE IS A FACT THE HARNESS ALREADY HOLDS, and none of them is a
// reading of anybody's words:
//
//   - THE TURN WROTE NOTHING. [writeMeter.untouched], the write seam's own
//     counter, read for zero. It is the one question that decides between a
//     worker in a copy of the folder and a worker where the person stands, and
//     it is asked in exactly one place in this build — writeseam.go owns the
//     isolation question and fires FIRST at every boundary, so a turn that has
//     been changing things has already been taken down that road. A turn with no
//     counter at all answers NO: a session that never ran an episode cannot prove
//     it left the disk alone, and a doubt is not a proof.
//   - NOTHING OF THIS CONVERSATION'S OWN IS MIXED INTO IT. Where the custody
//     reduction took parts out of the drawing ([checkpointRead.ownRemainder]) or
//     had a ledger to take them against ([checkpointRead.held]), the full road's
//     two gates stand in front of the handover (checkpoint_custody.go). This road
//     has no ladder for those gates to read, so it declines the shape outright
//     and leaves the turn to the road that can weigh it.
//   - AND THERE IS A SENTENCE TO GIVE IT. The line is the person's own words and
//     nobody here writes them; a node started on an empty line is a worker
//     started on a blank page, which is the ending the full road spells
//     [checkpointCeilingNoBrief].
func (a *Agent) promotedFromTurn(read checkpointRead, asked string, meter *checkpointMeter) *quickAsk {
	// AND THE ONE RUNG WHOSE MOVE CANNOT BE A PROMOTION IS ASKED FIRST. A turn
	// the net took because its context could no longer hold another step would
	// hand that same context to a worker on the same model with the same window,
	// which is the identical trouble one step later. That reading was taken when
	// the net fired and is carried rather than re-taken
	// ([checkpointMeter.outOfRoom]).
	if meter != nil && meter.outOfRoom {
		return nil
	}
	if !a.turnWroteNothing() {
		return nil
	}
	// AND WHAT A CONVERSATION DOES TO WORK IT HAS ALREADY HANDED OUT IS NOT WORK
	// TO HAND OUT AGAIN. A drawing of "wait for the second | wait for the third"
	// is three parts by the separator and no pairs of hands at all: every one of
	// them is a verb this conversation owns and a worker cannot do
	// ([checkpointSketch.handsBack], [checkpointHandBack]). The gate used to ride
	// on [checkpointSketch.split], which asked the same question on the way to a
	// different one; the count went with the split and this did not go with it.
	//
	// IT IS NOT COVERED BY THE WATCHING RULE ABOVE IT. A turn that only looked at
	// work already out never climbs a rung at all ([checkpointMeter.round]), but
	// a turn that WORKED and then spent its last rounds watching is a turn the
	// net can take with a hand-back drawing in its hand, and that is the turn
	// this refuses.
	if read.sketch.handsBack {
		return nil
	}
	if len(read.held) > 0 || strings.TrimSpace(read.ownRemainder) != "" {
		return nil
	}
	line := strings.TrimSpace(asked)
	if line == "" {
		line = strings.TrimSpace(a.taskRequest())
	}
	if line == "" {
		return nil
	}
	// AND THE BOUND IS ASKED HERE TOO, though the door asks it again and would
	// refuse for itself. The door's refusal is a sentence for a model to read;
	// this road has no model to read it, and what it needs to know is whether to
	// take the brief road INSTEAD — so it asks the same question, of the same
	// model the door will settle on, before it decides which road the turn goes
	// down ([Agent.workerModelFor], inherit.go).
	if _, fits := a.inheritFits(a.workerModelFor("")); !fits {
		return nil
	}
	// NO FILES ARE CLAIMED, because a turn that wrote nothing has named nothing
	// it is going to write, and no title is given, because the line is the
	// person's own sentence and its first line is the row's name on this road as
	// on the tool's. AND `inherit` IS THE WHOLE OF WHAT THIS ROAD ADDS: the node
	// that carries a turn on is the node the model could have asked for itself.
	//
	// AND AN EMPTY CHECKLIST NO LONGER STOPS THE PROMOTION, WHICH IS THE POINT
	// OF INHERITING. This road used to refuse a drawing with fewer than
	// [checkpointSketchParts] parts in it, and the objection was real while the
	// worker opened on a brief: a node with no items and a paragraph about
	// somebody else's reading had lost the one thing that made this road better
	// than the other one. A promoted node has not. It opens holding the whole
	// transcript — every result, verbatim — so the checklist is a convenience on
	// top of the work rather than the only description of it, and a reader that
	// drew one part has said the remaining work is one job, which is a node with
	// one thing to do and not a node with nothing.
	return &quickAsk{line: line, items: sketchItems(read.sketch), inherit: true}
}

// turnWroteNothing reports that this turn has not landed a single write-shaped
// call under the workspace, and that the harness is in a position to know it.
func (a *Agent) turnWroteNothing() bool {
	return a.writeMeterNow().untouched()
}

// sketchItems reads a drawing out as an ordered list of things to do.
//
// EVERY LETTER IS AN ITEM, WHICH IS NOT HOW THE DIVISION READS THE SAME LINE.
// [drawnDivision.proposal] wants the pieces that could be started SIDE BY SIDE,
// so `A | B | C > D` is three parts to it and the arrow is one part's own
// internal order. A quick node has one worker doing them IN ORDER, so the same
// shape is four items: the arrow is not a boundary it has to respect, it is
// simply where D comes after C. Reading it any other way would drop D on the
// floor or bury it inside C's sentence.
//
// AND THE WORDS ARE THE LEGEND'S ([sketchSaid]), because a list reading
// "1. A  2. B  3. C" is a coordinate system, not a job. Where the legend named
// nothing, the shape's own piece stands — a drawing written as
// `fix the redirect | the flaky fixture` needs no legend and is its own list.
func sketchItems(sketch checkpointSketch) []string {
	reading := readShape(sketch.shape)
	segments := legendSegments(sketch.legend)
	stages := make([]string, 0, len(reading.parts)+len(reading.after))
	for _, piece := range append(append([]string{}, reading.parts...), reading.after...) {
		stages = append(stages, splitAtTopLevel(unbracket(strings.TrimSpace(piece)), '>')...)
	}
	items := make([]string, 0, len(stages))
	for _, stage := range stages {
		item := sketchSaid(stage, segments)
		if item == "" {
			item = strings.TrimSpace(stage)
		}
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

// handOverAsQuick ends the turn and starts a quick node on the drawing.
//
// IT IS [Agent.handOverRunningTurn]'S TAIL WITH THE LADDER TAKEN OUT, and it is
// entered from inside that function once every ending above it has been ruled
// out — the trivial ask, the steward, the await, and the completion claim. What
// it keeps is everything that is about ENDING A TURN and not about writing a
// brief: the gap is spent, the transcript is left with the harness's own line
// where the person's request would otherwise stand unanswered, the turn is
// sealed, and the conversation is named. What it drops is the two model calls
// and the three rungs.
//
// AND THE NODE IS ADMITTED THROUGH THE TOOL'S OWN DOOR ([Agent.admitQuick]),
// not through the route judge's launcher. That launcher starts ORDINARY work a
// judge wrote a goal for — a done-condition, a place on the ground ladder, a
// name asked for, a width to arm — and every one of those is a thing a quick
// node does not have. Borrowing it made the ceiling's quick node a different
// object from the tool's; asking the one door makes it the same one. Nothing is
// armed to divide either: the door never arms a quick node, because the items
// ARE the division and task_divide.go refuses the kind outright.
//
// AND NO NAME IS ASKED FOR ON THIS ROAD AT ALL. A quick node is admitted named
// ([Agent.newQuickSpec]), so [Agent.handOverRunningTurn] asks for the other road's
// name UNDER the branch that comes here rather than over it — for as long as it
// asked above, every write-free carry-on paid a model for a row nothing renames
// (taskname.go's [nameAhead], and
// [TestAQuickCarryOnAsksForNoNameAndTheFullRoadStillDoes] holds the line).
func (a *Agent) handOverAsQuick(ctx context.Context, hub *eventHub, turn *Usage, started time.Time,
	model string, ask quickAsk) checkpointHandover {
	// THE CLOCK COMES OFF FIRST. The briefing stage the person is watching ends
	// here rather than after a writer that is never called, and a phase left
	// standing is the surface drawing work nobody is doing.
	a.endPhase()
	if ctx.Err() != nil {
		return checkpointHandover{decision: checkpointCeilingAbandoned}
	}
	id, spec, refusal := a.admitQuick(ask)
	if refusal.said != "" {
		// THE DOOR DECLINED, AND WHICH REFUSAL IT WAS IS WRITTEN DOWN AS ITSELF.
		//
		// Two of the door's refusals can reach this road and no more:
		// [Agent.promotedFromTurn] hands over neither files nor dependencies nor a
		// model, so what is left is a line with nothing in it — which is the full
		// road's no-brief ending and means the same thing here — and the fan cap.
		//
		// THE FAN CAP IS NOT "NO BRIEF", and saying so was the one dishonest word
		// on this road. A ceiling reached inside a task carries on to a CHILD of
		// that task ([Agent.newQuickSpec] takes the parent from the config), so
		// [TaskGraph.claimChild] can refuse it where a conversation's own carry-on
		// is never refused — and a session file saying `dropped:no-brief` about a
		// turn that had a perfectly good list to hand over sends whoever reads it
		// looking for the wrong bug.
		//
		// EITHER WAY NOTHING IS SEALED and nothing is written into the transcript,
		// and the turn carries on holding its own work.
		if refusal.fanFull {
			return checkpointHandover{decision: checkpointCeilingFanFull}
		}
		return checkpointHandover{decision: checkpointCeilingNoBrief}
	}
	// THE TOLD-AFTER LINE, and it is the one line a person reads about this
	// event — the split's own and the started-task line both stand behind it
	// ([checkpointQuickNote]).
	said := checkpointQuickLine(spec.title)
	hub.send(Event{Kind: EventNotice, Text: said})
	// THE GAP IS SPENT for [Agent.handOverRunningTurn]'s reason: the person has
	// just been interrupted by work appearing over their conversation, and it does
	// not matter to them which door it came through.
	a.mu.Lock()
	a.routeOffered = a.routeTurns
	a.mu.Unlock()
	// AND THE TRANSCRIPT IS NOT LEFT WITH A REQUEST NOBODY REPLIED TO. It is ONE
	// line here where the other road records two, because one is what was said.
	a.record(textMessage("assistant", said))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(*turn, started, model)})
	a.maybeTitle(ctx, hub)
	return checkpointHandover{moved: true, decision: checkpointCeilingMoved, taskID: id, carry: carryRungQuick}
}
