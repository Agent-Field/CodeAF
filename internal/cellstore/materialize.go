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
	args := append([]string{"--json", "snap", "-m", "materialization capture"}, e.cellDirArgs(c)...)
	out, err := e.engine(ctx, c, args...)
	if err != nil {
		return "", fmt.Errorf("capture: %w", err)
	}
	return parseSnapshot(out)
}

// Restore writes the snapshot back into the tree, byte for byte: only the
// named top-level paths, or the whole tree when none are named. Named paths
// are the tree's own, so the composed .cell/ is left as it is. Whole, it is
// restored in place, chain files included, so that is for a snapshot that is
// the cell's newest state; a rewind uses restoreKeepingChain.
func (e Engine) Restore(ctx context.Context, c cell.Cell, snapshot string, paths []string) error {
	if len(paths) > 0 {
		return e.restore(ctx, c, snapshot, paths, nil)
	}
	return e.restore(ctx, c, snapshot, nil, e.cellDirArgs(c))
}

func (e Engine) restore(ctx context.Context, c cell.Cell, snapshot string, paths, cellDir []string) error {
	args := append([]string{"--json", "rewind", snapshot, "--yes"}, cellDir...)
	for _, p := range paths {
		args = append(args, "--paths="+p)
	}
	if _, err := e.engine(ctx, c, args...); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}
