package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// runningPanel is `running` (docs/design/home-mission-control/DESIGN.md §1, §3
// P2): every piece of work out on the machine — each conversation's tasks and
// its background jobs, whatever project it is in — the most recently started
// first.
//
// A ROW IS A PIECE OF WORK AND NOT A CONVERSATION. A chat with three tasks out
// is three rows, each with its own title, its own clock and its own line saying
// what it is doing, because the question this panel answers is "what is the
// machine doing for me", and a conversation's name does not answer it.
//
// EVERY FACT IS THE PRESENCE FILE'S, fresh within its window
// ([session.SessionPresence.Fresh]): the work a session says it has out, what
// its worker is doing (up to one heartbeat old), how far an adaptive run has got,
// and the jobs it has running. A session nobody has refreshed says nothing here.
//
// THE FIRST ROW WEARS THE ONE MOVING CELL (law 8, homespinner.go's
// [homeView.spinAt]) and no other row wears a mark at all.
type runningPanel struct{ homePanelBase }

// The words a running row is drawn with.
const (
	// runningJobWord names what a job row is, after its project.
	runningJobWord = "a background job"
	// runningUpWord leads a job's age: a dev server has been UP for three hours,
	// where a task has been working for four minutes.
	runningUpWord = "up "
	// runningTaskKey and runningJobKey prefix a row's [homeCell.key], so a
	// conversation's task 3 and its job 3 are two rows.
	runningTaskKey = "task:"
	runningJobKey  = "job:"
	// runningItemKey prefixes a firing standing item's key, by its id.
	runningItemKey = "item:"
)

// runningItem is one row before the panel orders it.
type runningItem struct {
	started time.Time
	line    homeLine
}

func (runningPanel) rows(in *homeGridInput) homePanelRows {
	var items []runningItem
	for _, row := range in.rows {
		if row.kind == switcherStanding && row.moving {
			items = append(items, runningItem{row.at, runningStandingLine(row)})
			continue
		}
		if row.kind != switcherConversation || !row.session.Presence.Fresh(in.now) {
			continue
		}
		for _, task := range row.session.Presence.RunningTasks {
			items = append(items, runningItem{task.StartedAt, runningTaskLine(row, task, in.now)})
		}
		for _, job := range row.session.Presence.Jobs {
			items = append(items, runningItem{job.StartedAt, runningJobLine(row, job, chatProjectTag(row, in.tilde), in.now)})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return runningNewer(items[i].started, items[j].started) })
	lines := make([]homeLine, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.line)
	}
	if len(lines) > 0 {
		lines[0].cell.mark = cellMarkSpin
	}
	out := homePanelCut(in, panelRunning, lines)
	out.said = countWord(len(lines))
	return out
}

// runningNewer is the panel's order: the work that started last first, and a
// start nobody recorded — a queued node — after everything that has one.
func runningNewer(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return b.IsZero()
	}
	return a.After(b)
}

// runningTaskLine is one task or adaptive run: its title and how long it has
// been going, and under it what it is doing.
func runningTaskLine(row switcherRow, task session.PresenceTask, now time.Time) homeLine {
	cell := &homeCell{panel: panelRunning, title: runningTitle(row, task), sub: runningDoing(task),
		key: runningTaskKey + task.ID}
	homeLiveMargin(cell, row, sinceAt(task.StartedAt, now))
	return switcherRowLine(row, cell)
}

// runningTitle is a task's own title, and — for a presence row that carries
// none, written by an older build — the label the project's record gives the
// same node, joined on its id ([session.PresenceTask.ID]'s law), and the
// conversation's name when the record has not heard of it either.
func runningTitle(row switcherRow, task session.PresenceTask) string {
	if title := strings.TrimSpace(task.Title); title != "" {
		return title
	}
	for _, entry := range row.session.Tasks.Rows {
		if entry.ID == task.ID && strings.TrimSpace(entry.Label) != "" {
			return strings.TrimSpace(entry.Label)
		}
	}
	return row.title
}

// runningDoing is the line under a task: what its worker is doing, else which
// of its lives it is in, else its state — and how far a run has got.
//
// THE ACTIVITY OUTRANKS THE PHASE HERE, where switcher.go's note has it the
// other way round, because the engine writes the activity ONLY while the worker
// is the node's life ([session.PresenceTask.Activity]) — so when both are on the
// file the activity is the present, and when the node is under a check the
// activity is absent and the phase says so.
func runningDoing(task session.PresenceTask) string {
	doing := switcherFirstLine(task.Activity)
	if doing == "" {
		doing = taskPhaseWords(task.Phase, 0, 0)
	}
	if doing == "" {
		doing = tabSignalWord(tabWorking)
		if task.State == string(session.TaskQueued) {
			doing = roomQueuedWord
		}
	}
	if task.Total > 0 {
		doing += rowSep + itoa(task.Done) + " of " + itoa(task.Total)
	}
	return doing
}

// runningJobLine is one background job: `<command> · a background job`, how
// long it has been up at the margin, and — under the cursor — where it is
// ([chatProjectTag]'s word for its folder), because a dev server has no
// activity to report. The project used to ride inside the title; it is a fact
// like every other row's project and stands where theirs do (owner,
// 2026-09-15: the right margin is a time, the rest is description).
func runningJobLine(row switcherRow, job session.PresenceJob, project string, now time.Time) homeLine {
	clock := ""
	if age := sinceAt(job.StartedAt, now); age != "" {
		clock = runningUpWord + age
	}
	cell := &homeCell{panel: panelRunning, title: strings.TrimSpace(job.Title) + rowSep + runningJobWord,
		key: runningJobKey + job.ID, sub: project, grows: project != ""}
	homeLiveMargin(cell, row, clock)
	return switcherRowLine(row, cell)
}

// runningStandingLine is a watch or a reminder IN THE MIDDLE OF FIRING: work the
// machine started on its own, which is exactly what this panel is about. It
// carries the words the item was set up with, what its pass is doing, and how
// long ago the pass began ([switcherItemAt] reads the running mark).
//
// IT STAYS AN ITEM'S LINE ([switcherRowLine] makes it a [homeItem]), so every
// door an item has — enter to the conversation that asked for it, `→` for its
// pause verb, `ctrl+v` for its own rung — is the door it had on the retired list.
func runningStandingLine(row switcherRow) homeLine {
	cell := &homeCell{panel: panelRunning, title: row.title, sub: row.note,
		key: runningItemKey + row.item.Item.ID}
	homeLiveMargin(cell, row, row.age)
	return switcherRowLine(row, cell)
}

// ── stopping one ────────────────────────────────────────────────────────────

// runningVerbs is `→` on a running row: `s` stops a task THIS WINDOW HOLDS,
// through the one door every other stop takes (stop.go's card, which asks first
// and defaults to keep going).
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. The stop door is this
// window's own engine, and another window's task — or one of this window's
// conversations running behind the front one — has an id that engine does not
// hold, so the row offers no verb rather than one that would end the wrong work
// or fail (DESIGN §6 ruling 5). A row another window holds already says
// `another window`, and enter brings it here, where it can be stopped.
//
// THE VERB IS SPELLED AS THE TASKS PLACE SPELLS IT ([stopActWord]), because it
// is the same act on the same work and reaches the same card; a row that said
// `stop` on home and `stop it` one `tab` away would be two verbs to a person.
// `ctrl+x` reaches it without the strip, which is what a three-column home
// needs: there `running` is the middle column and `→` crosses rather than
// opening the strip. The map names the chord.
func (a *app) runningVerbs(line homeLine) []verb {
	target := a.runningStopTarget(line)
	if target.empty() {
		return nil
	}
	return []verb{{key: 's', word: stopActWord, do: func() tea.Cmd {
		// HOME STEPS ASIDE FOR THE CARD. The block draws every question above the
		// conversation's box (question.go), and home's frame has no such block —
		// so a card raised over home was a question nobody could see, holding the
		// keyboard. The task is this window's own conversation's, which is the
		// frame the card is read in and where the stop's receipt lands.
		a.closeHome()
		a.raiseStop(target)
		return nil
	}}}
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
