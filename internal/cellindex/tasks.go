package cellindex

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

// taskIndex is this session's share of the bucket's task index: one row per
// landed node and per settled run the checkpoint records. The file sits in the
// bucket, outside the cell, shared with every other session there.
type taskIndex struct{}

func (taskIndex) Name() string { return "tasks.jsonl" }

// Present reports whether the bucket already names every row the checkpoint
// owes it. Naming the session in one row is not enough: the rows of a run are
// written live, so a moved chat can arrive with its nodes in the file and its
// runs missing.
func (t taskIndex) Present(c cell.Cell) bool {
	return len(t.missing(c)) == 0
}

// Build appends only the rows the bucket lacks, so rows already there, and the
// rows of other sessions, are left as they are.
func (t taskIndex) Build(c cell.Cell, _ session.Digest) error {
	session.AppendTaskRows(t.path(c), t.missing(c))
	return nil
}

func (t taskIndex) missing(c cell.Cell) []session.TaskIndexEntry {
	return session.MissingTaskRows(t.path(c), session.RebuiltTaskRows(c.Root))
}

func (taskIndex) path(c cell.Cell) string {
	return session.TaskIndexPath(session.Place{Dir: c.Root}.Transcript())
}
