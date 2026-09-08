package session

// UNDER A STEWARD WITH A WALL, INLINE HEAD WORK IS BOUNDED BY A SHARE OF THE WALL.
//
// ── THE RUN THIS WAS WRITTEN FROM (#546) ──
//
// An unattended session in the yolo posture, with a fifteen-minute wall. The fix
// it had been asked for was working in the person's live checkout at about five
// minutes. The turn then kept READING and RUNNING TESTS inline for twelve and a
// half minutes; the write seam fired at round 32, with 147 seconds of the wall
// left; the task it started spent 60 of those opening a worktree, and was still
// running when the wall came down. Three changed files, no commit, no check, no
// landing.
//
// NOTHING DECIDED WRONGLY. Both governors that move inline work onto a task
// answer questions about COUNTS — the mark ladder counts rounds
// ([checkpointMarks]) and the write seam counts what the turn did to the disk
// ([writeAllowanceFiles]) — and a turn that reads and runs tests for twelve
// minutes crosses neither. What nobody was watching is the one thing the session
// was actually running out of.
//
// ── WHY THE CLOCK IS A GOVERNOR ONLY WHERE THERE IS A WALL ──
//
// A person sitting in front of a session has no wall: their turn takes as long as
// it takes, they can read it, and they can stop it. There is nothing for a share
// to be a share OF, so a [Person] session never reaches this at all — the same
// place [Agent.steward] draws every other line that belongs to an unattended run.
//
// AND A CEILING WITH NO WALL IS UNTOUCHED. `--max-cost` alone states a ceiling in
// dollars, and dollars say nothing about whether a task still has time to be set
// up and checked; a share taken off a money ceiling would be a clock invented out
// of a number that is not one.
//
// ── AND IT IS A DOOR, NOT A CAGE ──
//
// It is writeseam.go's shape and takes writeseam.go's road, for writeseam.go's
// reasons: it FIRES ONCE in a turn, it opens the one door
// ([Agent.handOverRunningTurn]), and that road can still DECLINE when the running
// model and the mark's own reader both say nothing remains. Past it the marks and
// the ceiling govern the turn exactly as they always did.

import (
	"context"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// turnWallShare is HOW MUCH OF AN UNATTENDED SESSION'S WALL ONE TURN MAY SPEND
// INLINE before what is left of it moves onto a task. It is the only number this
// law has, and it is stated once.
//
// A THIRD, BECAUSE WHAT THE OTHER TWO THIRDS BUY IS THE THING THE MEASURED RUN
// DID NOT GET. A task is not free the moment it starts: opening its worktree was
// measured at 60 seconds, and after it runs somebody independent has to read the
// result, which is the whole difference between work that happened and work that
// landed. The measured cell spent five sixths of its wall inline and left 147
// seconds — enough for the setup and nothing else.
//
// IT IS A SHARE AND NOT A DURATION so that it means the same thing at every wall
// somebody sets: a third of fifteen minutes and a third of four hours are both
// "the turn has had its go, and there is still most of the run left to finish in".
//
// THE SHARE ITSELF IS SETTLED BY MEASUREMENT AND NOT BY THIS ONE READING. Three
// is what the measured cell argues for; docs/design/turn-wall-share-doe/ is the
// record of the run that decides between it and a half, and a change to this
// number is a change to that record in the same commit.
const turnWallShare = 3

// taskAllowance is WHAT A TASK NEEDS OF THE WALL AFTER A TURN LETS GO OF THE
// WORK: the longest its working copy may wait to open ([gitRootPatience], the
// opening was measured at 60 s) and one full verdict on what it did
// ([auditDeadline], a real repository's own check plus the reading around it).
// It is those two bounds added and not a third number, because the two are
// already what the code holds a task to, and a figure written here beside them
// would be a figure that drifts.
//
// ── THE RUN THIS WAS ADDED FROM (the record in docs/design/turn-wall-share-doe) ──
//
// The share alone is taken off the WHOLE wall from the turn's own start, so a turn
// that began late was given a third the wall no longer had: on a 900 s wall the
// reef cell's turn began with 310 s left, ran its full 300 s, and handed over at
// 894 s — six seconds before the wall, which is #546's own shape with the seam
// that was written to prevent it doing the handing over. A task started then
// cannot be set up, let alone checked, so the handover road had nothing to offer
// and the two model calls behind asking it were a cost with no return. THE SEAM
// HANDS OVER ONLY WHAT CAN STILL BE CHECKED BEFORE THE WALL: it fires when the
// stretch has passed the share AND at least this much of the wall is left, and a
// turn that began with less runs to the wall inline, asked nothing.
const taskAllowance = gitRootPatience + auditDeadline

// turnWallShareNote is the ONE LINE a person reads when a turn that has spent the
// share is moved.
//
// It is the register every line in this house is held to (checkpoint.go's
// [checkpointSplitNote] states it): an observation, a middle dot, a promise, all
// lowercase, no full stop, nothing about machinery.
//
// WHAT IT OBSERVES IS THE CLOCK, because the clock is the only thing this seam
// looked at — no sketch, no counter of files — and a person who has been watching
// a turn grind for minutes does not need a threshold's name. WHAT IT PROMISES is
// the thing the seam exists for: that what is left still has enough of the wall in
// front of it to be set up, run AND CHECKED, which is the half the measured run
// never reached.
//
// THE FRACTION IS A WORD HERE AND A NUMBER IN [turnWallShare], and
// [TestTheShareNoteAndTheConstantSayTheSameFraction] pins the two together. A
// lookup table of ordinals bought to spell one word would be more machinery than
// the drift it prevents, and a number written twice is a number that will drift.
const turnWallShareNote = "this has taken a third of the time · moving it to a task that can be checked before the wall"

// checkpointDecisionRanLong is this seam's own word in the journal. It is spelled
// apart from the write seam's and the ceiling's because the three are different
// facts about a turn — one outran the small edit, one outran the reading it was
// worth paying for, and this one outran the session's own clock — and a bench that
// spelled them the same could not tell them apart afterwards.
const checkpointDecisionRanLong = "ran long"

// pastTurnWallShare reports whether the running turn has spent the session's
// share of the wall, AND CLAIMS THE DOOR when it has.
//
// THE CLAIM IS HERE AND NOT AT THE CALLER, which is [writeMeter.pastAllowance]'s
// reason: two step boundaries can never both be the one that moved the work, and
// a road that DECLINES must not be asked the same question again at every
// boundary after — that would put the two model calls behind it on the bill once
// a round for the rest of the turn.
//
// WHAT IS MEASURED IS THE TURN AND NOT THE SESSION. `SpentWall` is how long the
// whole run has been going, and a run three hours into a four-hour wall would
// move every turn it started on the strength of work that finished hours ago.
// What this bounds is ONE turn's inline stretch, so it is that turn's own start
// that it counts from.
//
// AND IT IS THE STEWARD'S CLOCK. The wall is measured on it ([Steward.Budget]),
// so the share is read off it too: two clocks over one law are two answers to the
// question of how much is left.
//
// THE LAW IS EXCEEDS, SO THE BOUNDARY ITSELF STAYS INLINE. A turn standing
// exactly on the share has not passed it, and the comparison says so rather than
// leaving the one moment the two readings are equal to whichever way an operator
// happened to be typed.
//
// AND IT FIRES ONLY WHERE A TASK STILL FITS. Past the share, the wall's remainder
// is read off the same clock, and under [taskAllowance] the door stays shut: the
// road behind it can only start a task, a task that cannot be set up and checked
// is not an offer, and asking the two minds about it would put their calls on
// the bill for nothing. The allowance's own boundary is inside for the share's
// reason turned around: exactly the allowance left is still enough.
func (a *Agent) pastTurnWallShare(meter *checkpointMeter, started time.Time) bool {
	if meter == nil || meter.shareSpent {
		return false
	}
	steward := a.steward()
	if steward == nil {
		return false
	}
	budget := steward.Budget()
	share := budget.Wall / turnWallShare
	if share <= 0 {
		return false
	}
	if steward.since(started) <= share || budget.Wall-budget.SpentWall < taskAllowance {
		return false
	}
	meter.shareSpent = true
	return true
}

// checkpointOverWallShare moves a turn that has spent the share onto the one
// road, and reports whether the turn is over.
//
// IT IS [Agent.checkpointWriting]'S ROAD WITH ONE THING CHANGED — the LINE, which
// says what was actually noticed — and everything else about it is
// [Agent.handOverRunningTurn]'s and is not restated here. The verdict is NOT
// armed to split, for the write seam's reason: the ceiling arms one because
// outrunning forty rounds is measured evidence of breadth, and a clock that ran
// down is evidence of nothing of the sort.
//
// THE MARK'S OWN READER IS ASKED, and that is what makes a clock safe to fire on.
// The handover can only be declined on TWO MINDS agreeing that nothing remains,
// and the second of them is this reading; without it a turn that was thirty
// seconds from finishing would be converted for having taken a while.
func (a *Agent) checkpointOverWallShare(ctx context.Context, hub *eventHub, turn *Usage, started time.Time, model string, rounds int, verdict routeVerdict, taken *Decision) bool {
	read := a.readMark(ctx)
	a.journalMarkRead(read, 0, rounds, checkpointDecisionRanLong)
	return a.handOverRunningTurn(ctx, hub, turn, started, model,
		turnWallShareNote, checkpointSeamWall, rounds, verdict, read, taken).moved
}

// wallShareIsAsked reports whether this step boundary is one the share may be
// asked at. It is a predicate rather than two conditions at the call site so
// that the ordering, which IS the law, has one place a mechanical reader can
// find it.
//
// A MARK STEP TAKES THE MARK LADDER AND NOTHING ELSE. A boundary that crossed a
// rung already has a reading of the work being taken over it, and asking the
// clock at the same boundary would buy a second sidecar call for one step and
// race the ladder for the same turn.
//
// AND WAITING IS NOT WORKING, which is [checkpointMeter.round]'s law said again
// in the unit this seam counts in. A batch that did nothing but look at work the
// conversation already handed out is not a stretch of inline work, and a turn
// spent watching four running pieces moved onto a task would be the harness
// converting a wait — measured live 2026-09-01, and the reason the ladder does
// not move for one either.
func wallShareIsAsked(mark int, calls []ai.ToolCall) bool {
	return mark == 0 && !roundWasWatching(calls)
}
