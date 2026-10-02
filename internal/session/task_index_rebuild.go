package session

import (
	"strconv"
	"strings"
)

// The bucket's task index is derived: every row is a citation of a node the
// session's checkpoint holds. RebuiltTaskRows says what this session's rows
// would be, for internal/cellindex to write back when a moved chat arrives in a
// bucket that has never heard of it.

// RebuiltTaskRows are the rows the session folder dir owes the bucket index:
// one for every node its checkpoint records as landed, and one for every run
// row it records as settled: a hand-off's run and an adaptive run's root and
// workers, whose rows the live writers file from the run's own seam and whose
// facts live in the checkpoint's run records. A node or run still queued or
// running has no row, as it has none while it lives; an interrupted one is
// closed by the session's own reopen. A background job never had a row.
//
// A rebuilt row names no folder of the machine that ran the work (Where,
// Ground, the artifact and transcript URIs), because a moved chat reads this on
// another machine where those paths are false.
//
// Fields the checkpoint never carried stay absent, as they do in a row written
// live for such a record: EndedAt is zero for a record that predates the
// landing instant, and a live row's Activity and Phase are never written.
func RebuiltTaskRows(dir string) []TaskIndexEntry {
	document, ok := loadTaskCheckpoint(truthPath(dir, placeTasks))
	if !ok {
		return nil
	}
	graph, id := &TaskGraph{}, Place{Dir: dir}.ID()
	var rows []TaskIndexEntry
	filed := make(map[string]bool)
	for _, record := range document.Nodes {
		if landedState(record.State) {
			row := restoreNode(graph, record).indexEntryLocked(id)
			filed[row.ID] = true
			rows = append(rows, row)
		}
	}
	for _, record := range document.Runs {
		if row, ok := runIndexRow(record, id); ok && !filed[row.ID] {
			filed[row.ID] = true
			rows = append(rows, row)
		}
	}
	return rows
}

// runIndexRow is the row one settled run record owes the index, and false for a
// record that owes none: one still moving, or a background job.
func runIndexRow(record runRecord, session string) (TaskIndexEntry, bool) {
	// A row with no title is never read back ([ReadTaskIndex]), so owing one
	// would leave the index forever short of what it is owed.
	if record.ID == 0 || strings.TrimSpace(record.Title) == "" || record.Kind == TaskKindJob || !landedState(record.State) {
		return TaskIndexEntry{}, false
	}
	notice := runRowNotice(record)
	switch {
	case record.Run == "":
		return crewRunEntry(notice, session), true
	case record.Node == "":
		return adaptiveRootEntry(notice, session), true
	}
	return adaptiveNodeEntry(notice, session), true
}

// adaptiveRootEntry is the closing row of an adaptive run's root, as
// [orchestrateFamily.recordRoot] files it.
func adaptiveRootEntry(notice TaskNotice, session string) TaskIndexEntry {
	entry := adaptiveNodeEntry(notice, session)
	entry.Parent, entry.Model = "", notice.Model
	entry.StartedAt = notice.StartedAt
	if !notice.StartedAt.IsZero() && !notice.EndedAt.IsZero() {
		entry.DurationMS = notice.EndedAt.Sub(notice.StartedAt).Milliseconds()
	}
	return entry
}

// adaptiveNodeEntry is the landing row of one worker of an adaptive run, as
// [orchestrateFamily.recordNode] files it.
func adaptiveNodeEntry(notice TaskNotice, session string) TaskIndexEntry {
	title := strings.TrimSpace(notice.Title)
	return TaskIndexEntry{
		ID:        strconv.FormatUint(notice.ID, 10),
		Parent:    taskIndexParent(notice.Parent),
		Name:      TaskSlug(title),
		Label:     taskLabel(title),
		Title:     title,
		Kind:      TaskKindAdaptive,
		Status:    string(notice.State),
		Outcome:   taskOutcome(notice.Report),
		Cost:      notice.CostUSD,
		EndedAt:   notice.EndedAt,
		SessionID: session,
	}
}

func landedState(s TaskState) bool {
	return s == TaskDone || s == TaskFailed || s == TaskUnverified
}

// AppendTaskRows writes rows into the index at path, the way a live landing
// does.
func AppendTaskRows(path string, rows []TaskIndexEntry) {
	for _, row := range rows {
		appendTaskIndex(path, row)
	}
}

// MissingTaskRows are the rows of owed that the index at path does not yet
// name, by the pair a row is identified by: its session and its id. The file is
// append-only and shared with every other session of the bucket, so a row
// already there is never written twice, whatever it says.
func MissingTaskRows(path string, owed []TaskIndexEntry) []TaskIndexEntry {
	named := make(map[[2]string]bool)
	for _, row := range ReadTaskIndex(path) {
		named[[2]string{row.SessionID, strings.TrimSpace(row.ID)}] = true
	}
	var missing []TaskIndexEntry
	for _, row := range owed {
		if !named[[2]string{row.SessionID, row.ID}] {
			missing = append(missing, row)
		}
	}
	return missing
}
