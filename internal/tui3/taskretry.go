package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

const taskRetryWord = "enter retry"

type taskRetryDoor interface{ RetryTask(uint64) error }

type taskRetryState struct {
	conv    string
	id      uint64
	pending bool
	problem string
}
type taskRetriedMsg struct {
	conv string
	id   uint64
	err  error
}

// Retry belongs to the owning conversation and only to work the engine can resume.
func (a *app) taskCanRetry(entry session.TaskIndexEntry) bool {
	node := a.taskSheetNodeFor(&entry)
	if node == nil || node.state != session.TaskFailed || node.run != "" || a.taskSheet.awayOwner.on {
		return false
	}
	// A PROGRAM'S TASK IS NEVER RETRIED: the engine's retry reopens one of its
	// own nodes, and a program's run is not one (session's programNotCarriedOn).
	if strings.TrimSpace(entry.Program) != "" || node.program != "" {
		return false
	}
	switch node.kind {
	case session.TaskKindJob, session.TaskKindAdaptive:
		return false
	}
	if _, ok := a.agent.(taskRetryDoor); !ok {
		return false
	}
	if host, ok := a.agent.(interface{ TaskRetrySupported() bool }); ok && !host.TaskRetrySupported() {
		return false
	}
	return true
}

func (a *app) retryTask(entry session.TaskIndexEntry) tea.Cmd {
	if !a.taskCanRetry(entry) || a.taskRetry.pending && a.taskRetry.conv == a.taskBriefConv() {
		return nil
	}
	node := a.taskSheetNodeFor(&entry)
	door := a.agent.(taskRetryDoor)
	conv, id := a.taskBriefConv(), node.id
	file := a.file
	a.taskRetry = taskRetryState{conv: conv, id: id, pending: true}
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		var err error
		if bound, ok := door.(interface{ RetryTaskIn(uint64, string) error }); ok {
			err = bound.RetryTaskIn(id, file)
		} else {
			err = door.RetryTask(id)
		}
		return func(bool) tea.Cmd {
			a.taskRetried(taskRetriedMsg{conv: conv, id: id, err: err})
			return nil
		}
	})
}

func (a *app) taskRetried(msg taskRetriedMsg) {
	if a.taskRetry.conv != msg.conv || a.taskRetry.id != msg.id {
		return
	}
	a.taskRetry.pending = false
	if msg.conv != a.taskBriefConv() {
		return
	}
	if msg.err != nil {
		a.taskRetry.problem = "could not retry · " + msg.err.Error()
	}
	a.touch()
}

// The live graph overrides a saved landing while preserving its transcript and identity.
func (a *app) currentTaskEntry(entry session.TaskIndexEntry) session.TaskIndexEntry {
	node := a.taskSheetNodeFor(&entry)
	if node == nil || !node.retried {
		return entry
	}
	entry.Status, entry.Ending = string(node.state), node.ending
	entry.Parent, entry.Outcome = node.parent, firstProseLine(node.report)
	entry.StartedAt, entry.EndedAt = node.started, taskNodeEnded(node)
	entry.Cost, entry.Model = node.spent(), node.model
	return entry
}

// A new attempt cannot inherit a stopped clock or a failure from its predecessor.
func (a *app) resetRetriedTask(node *taskNode) {
	if a.taskRetry.conv == a.taskBriefConv() && a.taskRetry.id == node.id {
		a.taskRetry.problem = ""
	}
	node.retried = true
	node.stopped, node.restored = false, false
	node.ending, node.report, node.merge = "", "", ""
	node.produced, node.producedWhole = "", ""
	node.producedCut, node.producedHeld = false, false
	node.began, node.ended = time.Time{}, time.Time{}
	node.elapsed = 0
}

func (a *app) taskRetryHint(entry session.TaskIndexEntry) string {
	if a.taskRetry.conv == a.taskBriefConv() && a.taskRetry.id == taskSheetEntryID(entry.ID) {
		if a.taskRetry.pending {
			return "retrying"
		}
		if a.taskRetry.problem != "" {
			return a.taskRetry.problem
		}
	}
	return ""
}

func (a *app) taskRetryFoot(keys string, entry session.TaskIndexEntry) string {
	if a.taskCanRetry(entry) && a.taskRetryHint(entry) != "retrying" {
		return taskRetryWord + " · " + strings.TrimPrefix(keys, "↑↓ scroll · ")
	}
	return keys
}

// The room and the record card address the same node through the same retry door.
func (a *app) roomRetryEntry() session.TaskIndexEntry {
	if a.room == nil {
		return session.TaskIndexEntry{}
	}
	return session.TaskIndexEntry{ID: itoa(int(a.room.id)), Title: a.room.title, SessionID: a.taskSheetSelfID()}
}

// A task page remains open, but its finished subscription belongs to the old attempt.
func (a *app) resumeRetriedRoom(node *taskNode) tea.Cmd {
	if a.room == nil || a.roomIsGuest() || a.room.id != node.id {
		return nil
	}
	room := a.room
	if room.stop != nil {
		room.stop()
	}
	a.roomGen++
	room.gen = a.roomGen
	room.setDone(false)
	room.workActivity.Start(a.now(), tokens.WorkLogoRandom)
	room.lane, room.stop = nil, nil
	if doors, ok := a.roomDoors(); ok {
		lane, stop, err := roomLaneOf(doors, node.id)
		if err == nil {
			room.lane, room.stop = lane, stop
			return waitRoom(lane, room.gen)
		}
	}
	return a.readRoomRecord()
}
