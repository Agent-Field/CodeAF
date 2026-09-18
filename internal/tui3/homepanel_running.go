package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// runningPanel is `tasks` (docs/design/home-mission-control/DESIGN.md §1, §3
// P2): THE DAY'S TASKS, FLATTENED, AS A PREVIEW OF THE TASKS PLACE. Every task
// and adaptive run the machine ran or is running — whatever conversation or
// project it belongs to — that started or landed inside the last day
// ([homeTasksWindow]), plus every task still running however old, the most
// recent first. A row is a title and a time, like a row of `threads`, and
// enter on it opens the task itself — the same door the tasks place's own list
// takes for the same row ([app.openTaskDoor]).
//
// IT WAS THE RUNNING PANEL: only what was moving right now, with a conversation's
// background jobs and a firing watch beside its tasks. The owner ruled
// (2026-09-17) that the panel should be the tasks place in miniature — a
// flattened list of the last twenty-four hours, up to ten rows and then `N
// more` — so the jobs and the firing watches are gone from it (a watch keeps
// its row on `scheduled`), and what landed today stands beside what is still
// going.
//
// A ROW IS A PIECE OF WORK AND NOT A CONVERSATION. A chat with three tasks out
// is three rows, each with its own title and its own clock, because the question
// this panel answers is "what has the machine done for me today", and a
// conversation's name does not answer it.
//
// TWO AUTHORITIES, THE LIVE ONE FIRST. The presence file says what a session
// has out right now — its running tasks, what the worker is doing, how far a
// run has got — and is fresh within its window ([session.SessionPresence.Fresh]);
// the task index says what landed, and when. A task on both is one row, drawn
// from presence, and it carries the index's own entry so the door knows what to
// open.
//
// THE FIRST RUNNING ROW WEARS THE ONE MOVING CELL (law 8, homespinner.go's
// [homeView.spinAt]) and no other row wears a mark at all.
type runningPanel struct{ homePanelBase }

// homeTasksWindow is how far back the panel looks: a day. A task that landed
// before it is the tasks place's to show; one still running is drawn whatever
// its age, because it is happening now.
const homeTasksWindow = 24 * time.Hour

// The words a task row is drawn with.
const (
	// runningTaskKey prefixes a row's [homeCell.key], so a conversation's task 3
	// is told apart from every other row that names a 3.
	runningTaskKey = "task:"
)

// runningItem is one row before the panel orders it: when it last did
// something, whether it is still going, and the line.
type runningItem struct {
	at      time.Time
	running bool
	line    homeLine
}

func (runningPanel) rows(in *homeGridInput) homePanelRows {
	// The switcher's row for each conversation, for the facts a row of the
	// field wears — `here`, `another window`, its project's word.
	bySession := map[string]switcherRow{}
	for _, row := range in.rows {
		if row.kind == switcherConversation && row.session.ID != "" {
			bySession[row.session.ID] = row
		}
	}
	seen := map[string]bool{}
	var items []runningItem
	// WHAT IS RUNNING, off presence: fresh, and whatever its age.
	for _, row := range in.rows {
		if row.kind != switcherConversation || !row.session.Presence.Fresh(in.now) {
			continue
		}
		for _, task := range row.session.Presence.RunningTasks {
			entry := runningEntry(row, task)
			seen[taskLedgerKey(entry)] = true
			items = append(items, runningItem{at: task.StartedAt, running: true,
				line: runningTaskLine(row, task, entry, in)})
		}
	}
	// WHAT LANDED OR BEGAN INSIDE THE DAY, off the record: every root task the
	// presence pass has not already drawn.
	since := in.now.Add(-homeTasksWindow)
	for _, project := range in.world.Projects {
		for _, session := range project.Sessions {
			row, known := bySession[session.ID]
			if !known {
				row = switcherRow{kind: switcherConversation, session: session, title: homeName(session), project: project.Name}
			}
			for _, entry := range session.Tasks.Rows {
				if entry.Parent != "" {
					continue
				}
				if strings.TrimSpace(entry.SessionID) == "" {
					entry.SessionID = session.ID
				}
				at := runningLastAt(entry)
				if seen[taskLedgerKey(entry)] || at.IsZero() || at.Before(since) {
					continue
				}
				seen[taskLedgerKey(entry)] = true
				items = append(items, runningItem{at: at, line: runningRecordLine(row, entry, in)})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return runningNewer(items[i].at, items[j].at) })
	lines := make([]homeLine, 0, len(items))
	spun := false
	for _, item := range items {
		if item.running && !spun {
			item.line.cell.mark = cellMarkSpin
			spun = true
		}
		lines = append(lines, item.line)
	}
	// THE HEADING IS THE BARE WORD (owner, 2026-09-17): no count and no
	// explainer after it, like `threads`; the rows under it are the count.
	return homePanelCut(in, panelRunning, lines)
}

// runningNewer is the panel's order: what happened last first, and a moment
// nobody recorded — a queued node with no start — after everything that has
// one.
func runningNewer(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return b.IsZero()
	}
	return a.After(b)
}

// runningLastAt is when a task on the record last did something: when it
// landed, else when it began. A row with neither is a row the panel cannot
// place in the day, and it is left to the tasks place.
func runningLastAt(entry session.TaskIndexEntry) time.Time {
	if !entry.EndedAt.IsZero() {
		return entry.EndedAt
	}
	return entry.StartedAt
}

// runningEntry is the record's own row for a task presence says is running,
// joined on the pair that identifies one piece of work ([taskLedgerKey]) — or,
// for a node the record has not written yet, an entry made from what presence
// says, so the door has something to open ([app.openTaskDoor] and the tasks
// place's [readTasks] make the same one for another window's task).
func runningEntry(row switcherRow, task session.PresenceTask) session.TaskIndexEntry {
	for _, entry := range row.session.Tasks.Rows {
		if entry.ID == task.ID {
			if strings.TrimSpace(entry.SessionID) == "" {
				entry.SessionID = row.session.ID
			}
			if strings.TrimSpace(entry.Title) == "" {
				entry.Title = strings.TrimSpace(task.Title)
			}
			if strings.TrimSpace(entry.Label) == "" {
				entry.Label = runningTitle(row, task)
			}
			return entry
		}
	}
	title := runningTitle(row, task)
	return session.TaskIndexEntry{ID: task.ID, SessionID: row.session.ID, Label: title, Title: title,
		Status: task.State, StartedAt: task.StartedAt}
}

// runningTaskLine is one task or adaptive run still going: its title and how
// long it has been going, and under it what it is doing.
func runningTaskLine(row switcherRow, task session.PresenceTask, entry session.TaskIndexEntry, in *homeGridInput) homeLine {
	return runningLine(row, entry, runningTitle(row, task), runningDoing(task), sinceAt(task.StartedAt, in.now))
}

// runningRecordLine is one task off the record — landed inside the day, or
// begun and not yet on presence: its title, when it last did something, and
// under the cursor its project's word and the state it is in, in the words the
// tasks place uses for the same row ([taskStateWord]).
func runningRecordLine(row switcherRow, entry session.TaskIndexEntry, in *homeGridInput) homeLine {
	title := strings.TrimSpace(entry.Label)
	if title == "" {
		title = strings.TrimSpace(entry.Title)
	}
	if title == "" {
		title = row.title
	}
	project := ""
	if homeBucketOf(row.session.Transcript) != in.bucket {
		project = chatProjectTag(row, in.tilde)
	}
	sub := rowClauses(project, taskStateWord(entry, row.session.Runs(entry)))
	return runningLine(row, entry, title, sub, sinceAt(runningLastAt(entry), in.now))
}

// runningLine is one row of the panel: a ledger line whose door is its own
// row — the task — exactly as a landed task's line on `since you left` is
// ([leftLine], [app.leftEnter]), so enter, the pointer and the row's identity
// across rebuilds ([homeLine.sameRow]) are the ones that panel already has.
// The switcher's row rides the cell so the margin can say `here` or `another
// window` ([homeLiveMargin]) and the stop verb can ask whose the work is
// ([app.runningStopTarget]).
func runningLine(row switcherRow, entry session.TaskIndexEntry, title, sub, clock string) homeLine {
	own := row
	own.task = &entry
	cell := &homeCell{panel: panelRunning, title: title, sub: sub, grows: strings.TrimSpace(sub) != "",
		key: runningTaskKey + entry.ID, row: &own}
	homeLiveMargin(cell, own, clock)
	return homeLine{kind: homeLedger, project: pageTasks.word(), dir: taskLedgerKey(entry), cell: cell}
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
// `ctrl+x` reaches it without the strip too, from any column.
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
