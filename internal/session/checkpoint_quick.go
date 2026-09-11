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
// (task_divide_sketch.go's [Agent.divideFromSketch]).

import (
	"context"
	"strings"
	"time"
)

// checkpointQuickNote is the ONE line a person reads when a write-free turn's
// parts are taken by a quick node, and it stands in for BOTH lines the other
// road writes — the split's own ([checkpointSplitNote]) and the started-task
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
// The observation is the same one the split makes, because the same reader saw
// the same thing. The promise is the honest one for this node and is deliberately
// narrower than the other two: not "watched", not "can split", but WHERE the
// work is happening — here, in this folder — because that is the whole of what
// distinguishes a quick node from the task the person might otherwise assume
// started.
const checkpointQuickNote = "this has parts · a quick task is taking them here, in this folder: "

// checkpointQuickLine is that line with the node's name on the end of it.
func checkpointQuickLine(title string) string {
	return checkpointQuickNote + title
}

// quickFromDrawing answers the quick node this handover should ask for, or nil
// where this is not that road. It answers an ASK and never a spec: what the
// node is beyond its line and its items is decided by the one door every quick
// node comes through ([Agent.admitQuick]), and this road only says what it
// knows.
//
// EVERY CLAUSE IS A FACT THE HARNESS ALREADY HOLDS, and none of them is a reading
// of anybody's words:
//
//   - THE DRAWING HAS PARTS. The same reading that decided to hand the turn over
//     at all ([checkpointSketch.split]), asked once so that the two cannot
//     disagree.
//   - THE TURN WROTE NOTHING. [writeMeter.untouched], the write seam's own
//     counter, read for zero. A turn with no counter at all answers NO: a
//     session that never ran an episode cannot prove it left the disk alone, and
//     a doubt is not a proof (writeseam.go says the same of a delivery it cannot
//     establish).
//   - NOTHING OF THIS CONVERSATION'S OWN IS MIXED INTO IT. Where the custody
//     reduction took parts out of the drawing ([checkpointRead.ownRemainder]) or
//     had a ledger to take them against ([checkpointRead.held]), the full road's
//     two gates stand in front of the handover — the reduced drawing, and the
//     bare ask that cannot tell the halves apart (checkpoint_custody.go). This
//     road has no ladder for those gates to read, so it declines the shape
//     outright and leaves the turn to the road that can weigh it.
//   - AND THERE IS A SENTENCE TO GIVE IT. The line is the person's own words and
//     nobody here writes them; a node started on an empty line is a worker
//     started on a blank page, which is the ending the full road spells
//     [checkpointCeilingNoBrief].
func (a *Agent) quickFromDrawing(read checkpointRead, asked string) *quickAsk {
	if !read.sketch.split() {
		return nil
	}
	if !a.turnWroteNothing() {
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
	items := sketchItems(read.sketch)
	// THE PARTS ARE COUNTED AGAIN AFTER THEY ARE READ OUT, because the split was
	// decided on the shape and the items are the shape AND the legend together. A
	// drawing whose letters nothing could be made of leaves a node with a line
	// and no list, which is a quick node that has lost the very thing that made
	// this road better than the other one.
	if len(items) < checkpointSketchParts {
		return nil
	}
	// NO FILES ARE CLAIMED, because a turn that wrote nothing has named nothing
	// it is going to write, and no title is given, because the line is the
	// person's own sentence and its first line is the row's name on this road as
	// on the tool's.
	return &quickAsk{line: line, items: items}
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
// THE NAME ASKED FOR AHEAD IS NOT USED. A quick node is never named
// ([Agent.newQuickSpec]), so the call [Agent.handOverRunningTurn] started for the
// other road is let go unclaimed when that function returns ([nameAhead.release]).
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
	if refusal != "" {
		// THE DOOR DECLINED THE ASK, and on this road the only ask it can decline
		// is a line with nothing in it — [Agent.quickFromDrawing] never hands over
		// files or dependencies, and a conversation's fan is not capped. That is
		// the full road's no-brief ending, and it is taken the same way: nothing
		// started, so nothing is sealed and nothing is written into the
		// transcript, and the turn carries on.
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
