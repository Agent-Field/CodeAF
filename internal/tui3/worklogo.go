package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// workLogoVisible uses one line only on a real running conversation. The
// accessible, monochrome and small-window tiers retain their existing text,
// and an approval never wears movement that implies work can continue unaided.
func (a *app) workLogoVisible() bool {
	return a.workActivity.Started() && !a.turnBegan.IsZero() && a.state == stateWorking &&
		a.showing() == nil && len(a.questionOpen()) == 0 && !a.asking() && !a.copy.on && a.room == nil && !a.linear && !a.pal.linear &&
		!a.pal.ascii && a.pal.profile >= tokens.ANSI256 && a.width >= 48 && gutterInner(a.bodyWidth()) >= 40 && a.height >= 20
}

// workLogoRows is the transient left-aligned foot of the live reply, never a
// saved transcript entry. All studies occupy the same box, so a moving ball
// cannot reflow the answer or the status words beside it.
func (a *app) workLogoRows(width int, override string) []row {
	label := a.activityLine(a.pal.narr("Working"))
	if a.running() {
		label = a.pal.narr("Working")
	}
	// Preserve the compact wait's connection and elapsed wording when it knows
	// more than the generic foot. A retry or reported phase retains precedence.
	if words := a.compactWaitWords(a.conversation()); words != "" && !a.running() {
		if _, phase := a.livePhase(); !phase && !a.retrying {
			label = a.pal.narr(words)
		}
	}
	if override != "" {
		label = override
	}
	return a.activityRows(a.workActivity, label, width)
}

// activityRows is one shared, single-line layout for every activity owner.
func (a *app) activityRows(activity tokens.WorkActivity, label string, width int) []row {
	return []row{{text: fit("  "+a.activityMark(activity)+" "+label, width), entry: -1, activity: true}}
}

func (a *app) activityMark(activity tokens.WorkActivity) string {
	var line strings.Builder
	for _, cell := range activity.Frame(a.now()) {
		line.WriteString(a.pal.workLogoCell(cell))
	}
	return line.String()
}

// deckActivity selects the clock belonging to the surface being rendered.
func (a *app) deckActivity(d deck) (tokens.WorkActivity, bool) {
	if d.lens.clock && a.workLogoVisible() {
		return a.workActivity, true
	}
	if !d.lens.clock && a.roomWorkLogoVisible() {
		return a.room.workActivity, true
	}
	return tokens.WorkActivity{}, false
}

// roomWorkLogoVisible reads the task being viewed, never its parent chat's turn.
// A held, finished, failed or disconnected task cannot advertise progress.
func (a *app) roomWorkLogoVisible() bool {
	if a.room == nil || !a.room.running() || !a.room.workActivity.Started() || a.showing() != nil || len(a.questionOpen()) > 0 ||
		a.copy.on || a.linear || a.pal.linear || a.pal.ascii || a.pal.profile < tokens.ANSI256 ||
		a.width < 48 || gutterInner(a.bodyWidth()) < 40 || a.height < 20 {
		return false
	}
	if _, waiting := a.roomGuest().waiting(); waiting {
		return false
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
