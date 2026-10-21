package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// cellSweepEvery is how often a start looks at the disk budget. The look is
// cheap (sizes are cached), so this only bounds how often it is made.
const cellSweepEvery = time.Hour

// cellBusy is the one way the harness knows a session holds a cell: the flock
// on its journal.
func cellBusy(c cell.Cell) bool {
	path, err := c.Path(cell.TranscriptPath)
	return err == nil && session.InUse(path)
}

// budgetOnOpen readies the cell for its session: an evicted cell is written
// back, and the open is stamped for the least-recently-used order. It then
// starts the periodic sweep in the background. A failure to restore is
// returned by the caller's own open, which cannot read the missing files, so
// nothing here stops the open.
func budgetOnOpen(dir string) {
	c, err := cell.OpenAt(dir, filepath.Base(dir))
	if err != nil {
		return
	}
	m := cellBudget()
	if m.Open(context.Background(), c) != nil {
		return
	}
	guard.Go("chatv3/cell-budget", func() { _ = m.Auto(context.Background(), cellSweepEvery) })
}

// heldByEngineHint says why a cell can still read as held after every window
// closed: the resident engine keeps a chat's lock while it waits for someone to
// come back, and lets go on its own.
func heldByEngineHint() string {
	return fmt.Sprintf("an engine lets an idle chat go %s after the last window closes; `codeaf engine --stop` lets go now",
		roughAge(enginehost.SessionIdleAfter()))
}
