package session

// The bucket's task index is derived: every row is a citation of a node the
// session's checkpoint holds. RebuiltTaskRows says what this session's rows
// would be, for internal/cellindex to write back when a moved chat arrives in a
// bucket that has never heard of it.

// RebuiltTaskRows are the rows the session folder dir owes the bucket index:
// one for every node its checkpoint records as landed. A node still queued or
// running has no row, as it has none while it lives; an interrupted one is
// closed by the session's own reopen.
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
	for _, record := range document.Nodes {
		if landedState(record.State) {
			rows = append(rows, restoreNode(graph, record).indexEntryLocked(id))
		}
	}
	return rows
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
