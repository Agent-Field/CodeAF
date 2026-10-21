package cellstore

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Capture snapshots the cell's folder as it stands and answers the snapshot
// id. It is not a turn: nothing is appended to the chain. The disk budget
// takes one just before it removes a working tree, so nothing the tree held
// since the last seal is lost.
func (e Engine) Capture(ctx context.Context, c cell.Cell) (string, error) {
	out, err := e.do(ctx, c, e.cellDir(c), snapOp{Message: "materialization capture"})
	if err != nil {
		return "", fmt.Errorf("capture: %w", err)
	}
	return parseSnapshot(out)
}

// Restore writes the snapshot back into the cell's folder, byte for byte:
// only the named top-level paths, or the whole tree when none are named. A
// composed .cell/ is restored in place, chain files included, so this is for a
// snapshot that is the cell's newest state; a rewind uses restoreKeepingChain.
func (e Engine) Restore(ctx context.Context, c cell.Cell, snapshot string, paths []string) error {
	return e.restore(ctx, c, snapshot, paths, e.cellDir(c))
}

func (e Engine) restore(ctx context.Context, c cell.Cell, snapshot string, paths []string, cellDir string) error {
	if _, err := e.do(ctx, c, cellDir, restoreOp{Snapshot: snapshot, Paths: paths}); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}
