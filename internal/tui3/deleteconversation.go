package tui3

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

const deletePermanentWord = "delete is permanent, are you sure?"

// askRecordDelete captures the selected owner before replacing its four actions
// with yes/no. Cancelling restores the same actions without moving the cursor.
func (a *app) askRecordDelete(row session.SessionRow, task *session.TaskIndexEntry) tea.Cmd {
	normal := verbStrip{open: true, row: a.rowIdentity(), verbs: a.rowVerbs()}
	target := row
	var entry *session.TaskIndexEntry
	if task != nil {
		copy := *task
		entry = &copy
	}
	detail := "stops this conversation and deletes all its tasks"
	if entry != nil {
		detail = "deletes this task and all its subtasks; keeps the conversation"
	}
	a.strip = verbStrip{open: true, row: normal.row, prompt: deletePermanentWord + "\n" + detail, verbs: []verb{
		{key: 'y', word: "yes", do: func() tea.Cmd { return a.deleteRecord(target, entry) }},
		{key: 'n', word: "no", do: func() tea.Cmd { a.strip = normal; return nil }},
	}}
	return nil
}

type recordDeletedMsg struct {
	key         string
	front       bool
	row         session.SessionRow
	task        *session.TaskIndexEntry
	replacement Conversation
	stopped     bool
	err         error
	sessions    bool
	taskIDs     []string
	attached    Conversation
}

// deleteRecord runs shutdown and disk removal off the update loop. The selected
// row cannot change while this explicit deletion is finishing, and no other
// conversation is stopped to delete a task's saved record.
func (a *app) deleteRecord(row session.SessionRow, task *session.TaskIndexEntry) tea.Cmd {
	if a.deleteBusy {
		return nil
	}
	key := a.convKey(row.Transcript)
	front := key != "" && key == a.frontTabKey()
	var owner Agent
	if front {
		owner = a.agent
	} else if held := a.behind[key]; held != nil {
		owner = held.conv.Agent
	}
	start, open := a.start, a.open
	where := row.Workspace
	if row.Owned {
		where = row.ProjectDir
	}
	sessions := a.at(pageTasks)
	var taskRows []session.TaskIndexEntry
	if task != nil {
		taskRows = a.taskActionRows(row, *task)
	}
	a.deleteBusy = true
	a.taskRowNotice("deleting…")
	return a.offLoop(func() func(bool) tea.Cmd {
		out := recordDeletedMsg{key: key, front: front, row: row, task: task, sessions: sessions}
		done := func(bool) tea.Cmd { return a.recordDeleted(out) }
		if task != nil {
			if owner == nil && session.InUse(row.Transcript) {
				if open == nil || a.shared {
					out.err = fmt.Errorf("open this conversation before deleting its task records")
					return done
				}
				out.attached, out.err = open(row.Workspace, row.Transcript)
				if out.err != nil {
					return done
				}
				if filepath.Clean(out.attached.SessionFile) != filepath.Clean(row.Transcript) {
					out.err = fmt.Errorf("conversation identity changed; task was not deleted")
					return done
				}
				owner = out.attached.Agent
			}
			rows, _ := taskActionReading(owner, row.ID, taskRows)
			out.taskIDs, out.err = session.DeleteTaskTree(row.Dir, row.ID, task.ID, rows)
			return done
		}
		// A replacement is created before ending the foreground agent, so a failed
		// new-conversation door cannot strand the surface without a usable agent.
		if front {
			if start == nil {
				out.err = fmt.Errorf("cannot prepare a new conversation")
				return done
			}
			out.replacement, out.err = start(where)
			if out.err != nil {
				return done
			}
			if out.replacement.Agent == nil {
				out.err = fmt.Errorf("new conversation has no agent")
				return done
			}
		}
		if owner == nil && session.InUse(row.Transcript) {
			if open == nil {
				out.err = fmt.Errorf("conversation is held by another window")
				return done
			}
			conv, err := open(row.Workspace, row.Transcript)
			if err != nil {
				out.err = err
				return done
			}
			owner = conv.Agent
		}
		if owner != nil {
			out.err = endConversationAgent(owner, session.StopByLeaving)
			if out.err != nil {
				if out.replacement.Agent != nil {
					_ = out.replacement.Agent.Close()
					out.replacement = Conversation{}
				}
				return done
			}
			out.stopped = true
		}
		out.err = session.DeleteConversation(row.Dir, row.ID)
		return done
	})
}

func (a *app) recordDeleted(msg recordDeletedMsg) tea.Cmd {
	a.deleteBusy = false
	cmd := a.keepTaskActionOwner(msg.attached)
	if msg.err == nil {
		if a.deletedRecords == nil {
			a.deletedRecords = make(map[tasksKey]bool)
		}
		id := ""
		if msg.task != nil {
			id = msg.task.ID
		}
		a.deletedRecords[tasksKey{session: msg.row.ID, id: id}] = true
		for _, taskID := range msg.taskIDs {
			a.deletedRecords[tasksKey{session: msg.row.ID, id: taskID}] = true
		}
		a.reconcileDeletedTasks()
	}
	if msg.task == nil && (msg.stopped || msg.err == nil) {
		key := msg.key
		if msg.front && msg.replacement.Agent != nil {
			a.forgetSteerOwner(draftOwnerOf(a.host, a.workspace, a.file))
			a.detachConversation()
			cmd = a.attachConversation(msg.replacement, nil)
		} else if held := a.behind[key]; held != nil {
			a.letGoKept(key, held, func(Agent) {})
		}
		a.forget(key)
		delete(a.tabShut, key)
		delete(a.unreadChats, key)
		tabs := a.closedTabs[:0]
		for _, tab := range a.closedTabs {
			if tab.key != key {
				tabs = append(tabs, tab)
			}
		}
		a.closedTabs = tabs
		visible := a.chatTabs[:0]
		for _, tab := range a.chatTabs {
			if tab.key != key {
				visible = append(visible, tab)
			}
		}
		a.chatTabs = visible
		a.chatTabBar = tabBar{}
	}
	// Re-read both authorities so no cached task, close-stack entry or preview
	// can bring a deleted conversation back to either list.
	if msg.sessions && !a.at(pageTasks) {
		cmd = tea.Batch(cmd, a.showPage(pageTasks))
	}
	a.refreshRecordLists()
	if msg.err != nil {
		a.taskRowNotice("could not delete: " + msg.err.Error())
	} else if msg.task != nil {
		a.taskRowNotice("task record deleted")
	} else {
		a.taskRowNotice("conversation deleted")
	}
	return cmd
}

func (a *app) refreshRecordLists() {
	if a.at(pageHome) {
		a.refreshHome()
		return
	}
	p := &a.taskSheet
	p.world = a.readWorld()
	p.mine = a.taskSheetMine()
	p.reading = readTasks(p.world, p.mine, p.reading.win, p.order, p.reading.seen, a.now())
	p.cursor = a.tasksSettle(p.cursor)
}

// conversationRowVerbs is shared by Home search and the Sessions tree. Enter
// already reopens, so the closed row's destructive action is explicitly Delete.
func (a *app) conversationRowVerbs(row session.SessionRow) []verb {
	read := switcherRow{kind: switcherConversation, session: row, place: row.Workspace}
	read.session.Archived = a.homeConversationClosed(row)
	read.gone = a.home.gone[filepath.Clean(row.Workspace)]
	return a.homeReadingVerbs(homeLine{kind: homeSession, row: row, dir: row.Workspace}, read)
}

// keepTaskRecords rejects in-flight snapshots taken before a confirmed delete.
func (a *app) keepTaskRecords(rows []session.TaskIndexEntry) []session.TaskIndexEntry {
	if len(a.deletedRecords) == 0 {
		return rows
	}
	kept := make([]session.TaskIndexEntry, 0, len(rows))
	for _, row := range rows {
		if !a.taskRecordDeleted(row.SessionID, row.ID, row.Parent, row.PlanID) {
			kept = append(kept, row)
		}
	}
	return kept
}

// withoutDeletedConversations applies confirmed deletion to every world reading,
// including an engine snapshot captured before the delete finished. Copying the
// lists keeps the engine's shared cache immutable while both pages update now.
func (a *app) withoutDeletedConversations(world session.World) session.World {
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			for id, gone := range row.DeletedTasks {
				if gone {
					if a.deletedRecords == nil {
						a.deletedRecords = make(map[tasksKey]bool)
					}
					a.deletedRecords[tasksKey{session: row.ID, id: id}] = true
				}
			}
		}
	}
	a.reconcileDeletedTasks()
	if len(a.deletedRecords) == 0 {
		return world
	}
	projects := make([]session.Project, 0, len(world.Projects))
	for _, project := range world.Projects {
		rows := make([]session.SessionRow, 0, len(project.Sessions))
		for _, row := range project.Sessions {
			if !a.deletedRecords[tasksKey{session: row.ID}] {
				row.Tasks.Rows = a.keepTaskRecords(row.Tasks.Rows)
				// Plan rows also read these tombstones when a stale world is returned.
				deleted := make(map[string]bool)
				for id, gone := range row.DeletedTasks {
					deleted[id] = gone
				}
				for key, gone := range a.deletedRecords {
					if key.session == row.ID && key.id != "" {
						deleted[key.id] = gone
					}
				}
				row.DeletedTasks = deleted
				rows = append(rows, row)
			}
		}
		project.Sessions = rows
		projects = append(projects, project)
	}
	world.Projects = projects
	return world
}
