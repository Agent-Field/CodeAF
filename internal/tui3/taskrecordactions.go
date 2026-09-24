package tui3

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// taskActionRows uses the same cached rows as the list. The menu freezes its
// verb; pressing Stop can never turn into Delete because a worker just landed.
func (a *app) taskActionRows(row session.SessionRow, entry session.TaskIndexEntry) []session.TaskIndexEntry {
	rows := append([]session.TaskIndexEntry(nil), row.Tasks.Rows...)
	rows = append(rows, entry)
	for _, item := range a.taskSheet.reading.items {
		if item.entry.SessionID == row.ID {
			rows = append(rows, item.entry)
		}
	}
	if a.convKey(row.Transcript) == a.frontTabKey() {
		for _, plan := range a.planRows {
			rows = append(rows, taskPlanRecord(plan, row.ID))
		}
	}
	return linkTaskPlanRows(rows)
}

func taskPlanRecord(row session.PlanTaskRow, owner string) session.TaskIndexEntry {
	return session.TaskIndexEntry{ID: row.ID, Parent: row.Parent, SessionID: owner, Title: row.Title, Status: row.Status, StartedAt: row.Started, EndedAt: row.Ended}
}

func taskTreeActive(rows []session.TaskIndexEntry, owner, root string) bool {
	for _, row := range session.TaskRecordTree(rows, owner, root) {
		if session.TaskRecordActive(row) {
			return true
		}
	}
	return false
}

func (a *app) taskRecordOwner(row session.SessionRow) Agent {
	key := a.convKey(row.Transcript)
	if key != "" && key == a.frontTabKey() {
		return a.agent
	}
	if held := a.behind[key]; held != nil {
		return held.conv.Agent
	}
	return nil
}

// taskActionReading refreshes the selected owner's graph and plan off the UI
// loop. A task number from another conversation must never reach this one's agent.
func taskActionReading(owner Agent, id string, cached []session.TaskIndexEntry) ([]session.TaskIndexEntry, map[string]bool) {
	rows := append([]session.TaskIndexEntry(nil), cached...)
	if source, ok := owner.(interface {
		TaskIndex() []session.TaskIndexEntry
	}); ok {
		rows = append(rows, source.TaskIndex()...)
	}
	plans := make(map[string]bool)
	if source, ok := owner.(interface{ PlanTasks() []session.PlanTaskRow }); ok {
		for _, row := range source.PlanTasks() {
			rows = append(rows, taskPlanRecord(row, id))
			plans[row.ID] = true
		}
	}
	return linkTaskPlanRows(rows), plans
}

// keepTaskActionOwner retains a newly attached owner without moving the user's
// current conversation or interrupting the owner's unrelated work.
func (a *app) keepTaskActionOwner(conv Conversation) tea.Cmd {
	if conv.Agent == nil {
		return nil
	}
	key := a.convKey(conv.SessionFile)
	if _, visible := chatTabAt(a.tabList(), key); !visible {
		if a.tabShut == nil {
			a.tabShut = make(map[string]bool)
		}
		a.tabShut[key] = true
	}
	return a.stow(conv, &aside{since: a.now()})
}

func (a *app) stopTaskRecord(row session.SessionRow, entry session.TaskIndexEntry) tea.Cmd {
	owner, open := a.taskRecordOwner(row), a.open
	cached := a.taskActionRows(row, entry)
	if owner == nil && (open == nil || a.shared) {
		a.taskRowNotice("open this conversation to stop its tasks")
		return nil
	}
	a.taskRowNotice("stopping…")
	return a.offLoop(func() func(bool) tea.Cmd {
		var attached Conversation
		var err error
		if owner == nil {
			attached, err = open(row.Workspace, row.Transcript)
			if err == nil && filepath.Clean(attached.SessionFile) != filepath.Clean(row.Transcript) {
				err = fmt.Errorf("conversation identity changed; task was not stopped")
			}
			if err == nil {
				owner = attached.Agent
			}
		}
		if err == nil {
			rows, plans := taskActionReading(owner, row.ID, cached)
			for _, task := range session.TaskRecordTree(rows, row.ID, entry.ID) {
				if !session.TaskRecordActive(task) || plans[task.PlanID] {
					continue
				}
				if plans[task.ID] {
					if door, ok := owner.(interface{ PlanCancel(string) error }); ok {
						err = door.PlanCancel(task.ID)
					} else {
						err = fmt.Errorf("task stop is unavailable")
					}
				} else if door, ok := owner.(stopAgent); ok {
					_, err = door.Cancel(session.CancelTask + ":" + task.ID)
				} else {
					err = fmt.Errorf("task stop is unavailable")
				}
				if err != nil {
					break
				}
			}
		}
		return func(bool) tea.Cmd {
			cmd := a.keepTaskActionOwner(attached)
			a.railStamp++
			a.refreshRecordLists()
			if err != nil {
				a.taskRowNotice("could not stop: " + err.Error())
			} else {
				a.taskRowNotice("stop requested · task records kept")
			}
			return cmd
		}
	})
}

// A run started from a numbered task uses that exact number in its plan ID.
// Linking the two records keeps deletion independent of titles and list order.
func linkTaskPlanRows(rows []session.TaskIndexEntry) []session.TaskIndexEntry {
	plans := make(map[tasksKey]bool)
	for _, row := range rows {
		if strings.HasPrefix(row.ID, "t-") {
			plans[tasksKeyOf(row)] = true
		}
	}
	for i := range rows {
		row := &rows[i]
		if row.PlanID == "" && plans[tasksKey{session: row.SessionID, id: "t-" + row.ID}] {
			row.PlanID = "t-" + row.ID
		}
	}
	return rows
}
