package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/session"
)

// cellBusy is the one way the harness knows a session holds a cell: the flock
// on its journal.
func cellBusy(c cell.Cell) bool {
	path, err := c.Path(cell.TranscriptPath)
	return err == nil && session.InUse(path)
}

// budgetOnOpen restores files removed by an explicit gc and records the open.
// C25 forbids starting collection from an open, an idle pass or a timer.
func budgetOnOpen(dir string) {
	c, err := cell.OpenAt(dir, filepath.Base(dir))
	if err != nil {
		return
	}
	m := cellBudget()
	if m.Open(context.Background(), c) != nil {
		return
	}
}

// heldByEngineHint says why a cell can still read as held after every window
// closed: the resident engine keeps a chat's lock while it waits for someone to
// come back, and lets go on its own.
func heldByEngineHint() string {
	return fmt.Sprintf("an engine lets an idle chat go %s after the last window closes; `codeaf engine --stop` lets go now",
		roughAge(enginehost.SessionIdleAfter()))
}
