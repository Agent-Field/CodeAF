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
	out, err := e.engine(ctx, c, "--json", "snap", "-m", "materialization capture")
	if err != nil {
		return "", fmt.Errorf("capture: %w", err)
	}
	return parseSnapshot(out)
}

// Restore writes the snapshot back into the cell's folder, byte for byte:
// only the named top-level paths, or the whole tree when none are named.
func (e Engine) Restore(ctx context.Context, c cell.Cell, snapshot string, paths []string) error {
	args := []string{"--json", "rewind", snapshot, "--yes"}
	for _, p := range paths {
		args = append(args, "--paths="+p)
	}
	if _, err := e.engine(ctx, c, args...); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}
