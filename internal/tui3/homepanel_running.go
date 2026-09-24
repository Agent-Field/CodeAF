package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// runningTaskKey identifies task actions reached through Home's task records.
const runningTaskKey = "task:"

// ── the door ────────────────────────────────────────────────────────────────

// openTaskDoor is enter on a row of home named after one piece of work: THE
// DOOR THE TASKS PLACE'S OWN LIST TAKES FOR THE SAME ROW ([tasksPlace.enter]),
// so a task pressed on home arrives where it would have arrived one `tab`
// away. Work this window is running opens its live room; everything else
// opens the record inside the tasks place, parked on that row.
func (a *app) openTaskDoor(entry *session.TaskIndexEntry) tea.Cmd {
	if entry == nil {
		return nil
	}
	if node := a.taskSheetNodeFor(entry); node != nil {
		a.closeHome()
		if node.run != "" {
			a.openOrchRoom(node.run, node.node)
		} else {
			a.openRoomFor(node.id, node.title)
		}
		return a.takeRoomPump()
	}
	return a.openTaskRecord(entry)
}

// ── stopping one ────────────────────────────────────────────────────────────

// runningVerbs gives task rows the same Stop/Delete actions as Sessions.
// Other running work retains its existing stop confirmation.
func (a *app) runningVerbs(line homeLine) []verb {
	var verbs []verb
	if line.cell != nil && line.cell.row != nil && line.cell.row.task != nil {
		return a.taskRowVerbs(line.cell.row.session, *line.cell.row.task)
	}
	target := a.runningStopTarget(line)
	if target.empty() {
		return verbs
	}
	return append(verbs, verb{key: 's', word: stopActWord, do: func() tea.Cmd {
		// HOME STEPS ASIDE FOR THE CARD. The block draws every question above the
		// conversation's box (question.go), and home's frame has no such block —
		// so a card raised over home was a question nobody could see, holding the
		// keyboard. The task is this window's own conversation's, which is the
		// frame the card is read in and where the stop's receipt lands.
		a.closeHome()
		a.raiseStop(target)
		return nil
	}})
}

// runningStopTarget is the task a running row names, when this window's engine
// is the one running it.
//
// WHICH CONVERSATION THIS WINDOW HOLDS IS THE READING'S OWN FACT
// ([switcherRow.here], the fact the row's `here` margin is drawn from), and
// never a path resolved again here. A row's verbs can be read while drawing,
// and a draw may not
// walk the disk to answer it — resolving the transcript's symlinks would be a
// syscall a frame.
func (a *app) runningStopTarget(line homeLine) stopTarget {
	key := line.cellKey()
	if !strings.HasPrefix(key, runningTaskKey) || line.cell.row == nil || !line.cell.row.here {
		return stopTarget{}
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(key, runningTaskKey), 10, 64)
	if err != nil {
		return stopTarget{}
	}
	return a.stopTaskTarget(a.tasks[id])
}
