package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
	"github.com/Agent-Field/codeaf/internal/session"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// workLogoVisible uses one line only on a real running conversation. The
// accessible, monochrome and small-window tiers retain their existing text,
// and an approval never wears movement that implies work can continue unaided.
func (a *app) workLogoVisible() bool {
	return a.workActivity.Started() && !a.turnBegan.IsZero() && a.state == stateWorking &&
		a.showing() == nil && len(a.questionOpen()) == 0 && !a.asking() && a.room == nil && !a.linear && !a.pal.linear &&
		!a.pal.ascii && a.pal.profile >= tokens.ANSI256 && a.width >= 48 && a.height >= 20 && a.turnAnchor() >= 0
}

// turnAnchor finds the last question or correction submitted in the running
// turn. Work adopted without one keeps the waiting treatment instead of
// attaching movement to a question that was already answered.
func (a *app) turnAnchor() int {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].turn == a.turn && (a.entries[i].kind == entryUser || a.entries[i].kind == entrySteer) {
			return i
		}
	}
	return -1
}

// questionActivity anchors one indicator to the submitted question being
// worked, never to a changing caption or to the transcript's growing tail.
func (a *app) questionActivity(d deck) (tokens.WorkActivity, int, bool) {
	switch {
	case d.lens.clock && a.workLogoVisible():
		return a.workActivity, a.turnAnchor(), true
	case !d.lens.clock && a.roomWorkLogoVisible():
		// A TASK'S REQUEST IS THE THING BEING WORKED THROUGH EVERY RETRY, so
		// its page keeps the latest request or correction without comparing the
		// journal's original turn number with the room's current attempt.
		for i := len(d.entries) - 1; i >= 0; i-- {
			if d.entries[i].kind == entryUser || d.entries[i].kind == entrySteer {
				return a.room.workActivity, i, true
			}
		}
	default:
		return tokens.WorkActivity{}, -1, false
	}
	return tokens.WorkActivity{}, -1, false
}

// activityLabelColumn is a terminal-cell contract, independent of the current
// pose. Future callers put content here rather than measuring visible ink.
const activityLabelColumn = 2 + tokens.WorkLogoWidth + 2
const activityContentColumn = activityLabelColumn + tokens.WorkCaptionWidth + 2

// activityRows is one shared, single-line layout for every activity owner.
func (a *app) activityRows(activity tokens.WorkActivity, trailing string, width int) []row {
	caption := activity.Caption()
	if !a.linear && !a.pal.linear && !a.pal.ascii && a.pal.profile >= tokens.ANSI256 {
		caption = tokens.DecodeWorkCaption(activity.CaptionAt(a.now()), activity.Elapsed(a.now())%tokens.WorkCaptionPeriod)
	}
	caption = a.pal.dim(caption)
	line := "  " + a.activityMark(activity) + "  " + padTo(caption, tokens.WorkCaptionWidth)
	if trailing != "" {
		line += "  " + trailing
	}
	return []row{{text: fit(line, width), entry: -1, activity: true}}
}

func (a *app) activityMark(activity tokens.WorkActivity) string {
	var line strings.Builder
	for _, cell := range activity.Frame(a.now()) {
		line.WriteString(a.pal.workLogoCell(cell))
	}
	return padTo(fit(line.String(), tokens.WorkLogoWidth), tokens.WorkLogoWidth)
}

// roomWorkLogoVisible reads the task being viewed, never its parent chat's turn.
// A held, finished, failed or disconnected task cannot advertise progress.
func (a *app) roomWorkLogoVisible() bool {
	if a.room == nil || !a.room.running() || !a.room.workActivity.Started() || a.showing() != nil || len(a.questionOpen()) > 0 ||
		a.linear || a.pal.linear || a.pal.ascii || a.pal.profile < tokens.ANSI256 ||
		a.width < 48 || a.height < 20 {
		return false
	}
	if _, waiting := a.roomGuest().waiting(); waiting {
		return false
	}
	if run := a.orchOf(); run != nil {
		if !run.known || run.snap.Done || run.snap.Stopped || run.snap.Paused || run.gate != nil {
			return false
		}
		if run.transcript != "" {
			node, ok := orchNodeOf(run.snap, run.transcript)
			return ok && node.State == orchestrate.Running
		}
		return true
	}
	node := a.roomNode()
	if node == nil || node.stopped {
		return false
	}
	status := a.taskStatus(node)
	return status.State == session.TaskRunning && status.On != session.TaskWaitPerson &&
		status.Presence != session.TaskPresenceNeedsLook && !a.roomLandingAsking()
}

func (a *app) anyWorkLogoVisible() bool { return a.workLogoVisible() || a.roomWorkLogoVisible() }
