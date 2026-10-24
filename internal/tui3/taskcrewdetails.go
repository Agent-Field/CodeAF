package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

const taskStrongerWord = "r run again with a stronger crew"

// Crew facts belong to the task people open, not to another transcript receipt.
func (a *app) taskCrewRows(entry session.TaskIndexEntry, width int) []string {
	entry = a.currentTaskEntry(entry)
	if entry.Crew == nil {
		return nil
	}
	var rows []string
	if stopped := strings.TrimSpace(entry.Crew.Stopped); stopped != "" {
		for _, line := range wrap(stopped, width) {
			rows = append(rows, a.pal.warn(line))
		}
	}
	for _, line := range wrap("crew · "+a.crewLine(entry.Crew, entry.Cost), width) {
		rows = append(rows, a.pal.dim(line))
	}
	return rows
}

// A stronger run addresses the selected task in its owning conversation, never the latest task elsewhere.
func (a *app) taskCanRedoStronger(entry session.TaskIndexEntry) bool {
	if entry.SessionID == "" || entry.SessionID != a.taskSheetSelfID() || entry.Program != "" || a.taskSheet.awayOwner.on {
		return false
	}
	node := a.taskSheetNodeFor(&entry)
	crew := entry.Crew
	if node != nil && node.crew != nil {
		crew = node.crew
	}
	if crew == nil || strings.TrimSpace(crew.Stopped) != "" || entry.Live() {
		return false
	}
	_, ok := a.agent.(redoAgent)
	return ok
}

func (a *app) redoTaskStronger(entry session.TaskIndexEntry) tea.Cmd {
	if !a.taskCanRedoStronger(entry) {
		return nil
	}
	id, err := strconv.ParseUint(entry.ID, 10, 64)
	if err != nil || id == 0 {
		return nil
	}
	door := a.agent.(redoAgent)
	ctx, conv := a.ctx, a.taskBriefConv()
	a.closeTaskRecord()
	a.closeTaskSheet()
	return a.offLoop(func() func(bool) tea.Cmd {
		next, title, err := door.RedoStronger(ctx, id)
		return foldTaskStarted(taskStartedMsg{kind: "single", id: strconv.FormatUint(next, 10), title: title, err: err, conv: conv})
	})
}

const taskResultOutputWord = "o open full output"

// The result keeps the transcript's existing room one deliberate action away.
func (a *app) taskResultHasOutput(entry session.TaskIndexEntry) bool {
	return a.taskSheetNodeFor(&entry) != nil
}

func (a *app) openTaskResultOutput(entry session.TaskIndexEntry) tea.Cmd {
	node := a.taskSheetNodeFor(&entry)
	if node == nil {
		return nil
	}
	a.closeTaskSheet()
	return a.openRailRoom(node)
}
