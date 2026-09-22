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
		a.showing() == nil && len(a.questionOpen()) == 0 && !a.asking() && !a.copy.on && a.room == nil && !a.linear && !a.pal.linear &&
		!a.pal.ascii && a.pal.profile >= tokens.ANSI256 && a.width >= 48 && a.height >= 20
}

// workLogoRows is the transient left-aligned foot of the live reply, never a
// saved transcript entry. All studies occupy the same box, so a moving ball
// cannot reflow the answer or the status words beside it.
func (a *app) workLogoRows(width int, _ string) []row {
	return a.activityRows(a.workActivity, a.pal.narr("Working"), width)
}

// questionActivity anchors one indicator to the last submitted question, never
// to a changing caption or to the transcript's growing tail.
func (a *app) questionActivity(d deck) (tokens.WorkActivity, int, bool) {
	var activity tokens.WorkActivity
	switch {
	case d.lens.clock && a.workLogoVisible():
		activity = a.workActivity
	case !d.lens.clock && a.roomWorkLogoVisible():
		activity = a.room.workActivity
	default:
		return activity, -1, false
	}
	for i := len(d.entries) - 1; i >= 0; i-- {
		if d.entries[i].kind == entryUser || d.entries[i].kind == entrySteer {
			return activity, i, true
		}
	}
	return activity, -1, false
}

// activityLabelColumn is a terminal-cell contract, independent of the current
// pose. Future callers put content here rather than measuring visible ink.
const activityLabelColumn = 2 + tokens.WorkLogoWidth + 2

// activityRows is one shared, single-line layout for every activity owner.
func (a *app) activityRows(activity tokens.WorkActivity, label string, width int) []row {
	return []row{{text: fit("  "+a.activityMark(activity)+"  "+label, width), entry: -1, activity: true}}
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
		a.copy.on || a.linear || a.pal.linear || a.pal.ascii || a.pal.profile < tokens.ANSI256 ||
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

// roomWorkLogoRows shares the renderer but only reads this task's state words.
func (a *app) roomWorkLogoRows(width int) []row {
	node := a.roomNode()
	if node == nil {
		return nil
	}
	label := "Working"
	if live := a.roomOpenCallWord(node); live != "" && len(a.room.entries) == 0 {
		label += " · " + live
	}
	return a.activityRows(a.room.workActivity, a.pal.narr(label), width)
}

func (a *app) anyWorkLogoVisible() bool { return a.workLogoVisible() || a.roomWorkLogoVisible() }
