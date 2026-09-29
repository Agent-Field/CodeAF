package cellindex

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

// taskIndex is this session's share of the bucket's task index: one row per
// landed node the checkpoint records. The file sits in the bucket, outside the
// cell, shared with every other session there.
type taskIndex struct{}

func (taskIndex) Name() string { return "tasks.jsonl" }

// Present reports whether the bucket already names the session in any row, or
// the session has no landed node to name.
func (t taskIndex) Present(c cell.Cell) bool {
	rows := session.RebuiltTaskRows(c.Root)
	return len(rows) == 0 || named(session.ReadTaskIndex(t.path(c)), c.ID)
}

func (t taskIndex) Build(c cell.Cell, _ session.Digest) error {
	session.AppendTaskRows(t.path(c), session.RebuiltTaskRows(c.Root))
	return nil
}

func (taskIndex) path(c cell.Cell) string {
	return session.TaskIndexPath(session.Place{Dir: c.Root}.Transcript())
}

func named(rows []session.TaskIndexEntry, id string) bool {
	for _, r := range rows {
		if r.SessionID == id {
			return true
		}
	}
	return false
}
