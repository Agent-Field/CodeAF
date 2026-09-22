package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// workLogoVisible spends four rows only on a real running conversation. The
// accessible, monochrome and small-window tiers retain their existing text,
// and an approval never wears movement that implies work can continue unaided.
func (a *app) workLogoVisible() bool {
	return a.workActivity.Started() && !a.turnBegan.IsZero() && a.state == stateWorking &&
		a.page == pageNone && !a.asking() && !a.copy.on && a.room == nil && !a.linear && !a.pal.linear &&
		!a.pal.ascii && a.pal.profile >= tokens.ANSI256 && a.width >= 48 && gutterInner(a.bodyWidth()) >= 32 && a.height >= 20
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

// activityRows is the shared layout door for chat, task and run pages. A caller
// supplies only the operation's own activity, its truthful label and the width.
func (a *app) activityRows(activity tokens.WorkActivity, label string, width int) []row {
	frame := activity.Frame(a.now())
	out := make([]row, 0, tokens.WorkLogoHeight)
	for i, cells := range frame {
		var line strings.Builder
		line.WriteString("  ")
		for _, cell := range cells {
			line.WriteString(a.pal.workLogoCell(cell))
		}
		if i == 1 {
			line.WriteString("  ")
			line.WriteString(label)
		}
		out = append(out, row{text: fit(line.String(), width), entry: -1, activity: true})
	}
	return out
}

// roomWorkLogoVisible reads the task being viewed, never its parent chat's turn.
// A held, finished, failed or disconnected task cannot advertise progress.
func (a *app) roomWorkLogoVisible() bool {
	if a.room == nil || !a.room.running() || !a.room.workActivity.Started() || a.page != pageNone ||
		a.copy.on || a.linear || a.pal.linear || a.pal.ascii || a.pal.profile < tokens.ANSI256 ||
		a.width < 48 || gutterInner(a.bodyWidth()) < 32 || a.height < 20 {
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
	label := a.roomStateWord(node)
	if live := a.roomOpenCallWord(node); live != "" {
		label += " · " + live
	}
	return a.activityRows(a.room.workActivity, a.pal.narr(label), width)
}

func (a *app) anyWorkLogoVisible() bool { return a.workLogoVisible() || a.roomWorkLogoVisible() }
