package tui3

import (
	"path/filepath"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/session"
)

// taskRecordDeleted gives every projection the same owner-scoped decision.
// Permanent deletion wins over snapshots and notices already in flight.
func (a *app) taskRecordDeleted(owner string, ids ...string) bool {
	if a.deletedRecords[tasksKey{session: owner}] {
		return true
	}
	for _, id := range ids {
		if id != "" && a.deletedRecords[tasksKey{session: owner, id: id}] {
			return true
		}
	}
	return false
}

func (a *app) taskNoticeDeleted(notice *session.TaskNotice) bool {
	return a.taskRecordDeleted(filepath.Base(filepath.Dir(a.file)), strconv.FormatUint(notice.ID, 10), strconv.FormatUint(notice.Parent, 10), notice.PlanID)
}

func (a *app) keepPlanTaskRecords(rows []session.PlanTaskRow) []session.PlanTaskRow {
	if len(a.deletedRecords) == 0 {
		return rows
	}
	owner := filepath.Base(filepath.Dir(a.file))
	kept := make([]session.PlanTaskRow, 0, len(rows))
	for _, row := range rows {
		if !a.taskRecordDeleted(owner, row.ID, row.Parent) {
			kept = append(kept, row)
		}
	}
	return kept
}

// reconcileDeletedTasks updates the live projections together, before another
// frame is drawn. Renderers never invent their own definition of a deleted task.
func (a *app) reconcileDeletedTasks() {
	if len(a.deletedRecords) == 0 {
		return
	}
	a.comp.tasks = a.keepTaskRecords(a.comp.tasks)
	if a.planRowsFront == a.frontGen {
		kept := a.keepPlanTaskRecords(a.planRows)
		if len(kept) != len(a.planRows) {
			a.planRows = kept
			a.planRowsGen++
		}
	}
	owner := filepath.Base(filepath.Dir(a.file))
	changed := false
	for id, node := range a.tasks {
		if !a.taskRecordDeleted(owner, strconv.FormatUint(id, 10), node.parent, node.planID) {
			continue
		}
		if a.room != nil && a.room.guest == nil && a.room.id == id {
			a.closeRoom()
		}
		if a.task != nil && a.task.id == id {
			a.task = nil
		}
		if pilot := a.pilots[id]; pilot != nil && pilot.stop != nil {
			pilot.stop()
		}
		delete(a.pilots, id)
		delete(a.tasks, id)
		delete(a.taskSeen, id)
		delete(a.railOpen, id)
		delete(a.typedTaskBriefs, id)
		changed = true
	}
	if !changed {
		return
	}
	kept := a.taskOrder[:0]
	for _, id := range a.taskOrder {
		if a.tasks[id] != nil {
			kept = append(kept, id)
		}
	}
	a.taskOrder = kept
	if !a.railWhere.onJobs() && a.tasks[a.railWhere.id] == nil {
		a.railWhere = railSpot{}
		a.railTop = 0
		if len(kept) > 0 {
			a.railWhere.id = kept[0]
		} else {
			a.railHold = false
		}
	}
	a.railStamp++
	a.touch()
}
