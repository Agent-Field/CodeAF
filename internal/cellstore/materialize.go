package cellstore

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// Capture snapshots the cell's folder as it stands and answers the snapshot
// id. It is not a turn: nothing is appended to the chain. The disk budget
// takes one just before it removes a working tree, so nothing the tree held
// since the last seal is lost.
func (e Engine) Capture(ctx context.Context, c cell.Cell) (string, error) {
	if err := Await(ctx, c); err != nil {
		return "", err
	}
	if err := e.recoverCompleted(ctx, c); err != nil {
		return "", err
	}
	// Recovery can seal independently; acquire after it to avoid nesting this
	// process lock while still fencing composition through the native snapshot.
	release, err := taskcopy.LockWorkspaceCheckpoint(ctx, c)
	if err != nil {
		return "", err
	}
	defer release()
	if err := e.composeExtra(c); err != nil {
		return "", err
	}
	reply, err := e.plannedSnapshot(ctx, c, "materialization capture", nil, nil)
	if err != nil {
		return "", err
	}
	if err = durable(ctx, c.Root); err != nil {
		return "", err
	}
	return reply.Snapshot, nil
}

// Restore writes the snapshot back into the cell's folder, byte for byte:
// only the named top-level paths, or the whole tree when none are named. A
// composed .cell/ is restored in place, chain files included, so this is for a
// snapshot that is the cell's newest state; a rewind uses restoreKeepingChain.
func (e Engine) Restore(ctx context.Context, c cell.Cell, snapshot string, paths []string) error {
	return e.restore(ctx, c, snapshot, paths, e.cellDir(c))
}

func (e Engine) restore(ctx context.Context, c cell.Cell, snapshot string, paths []string, cellDir string) error {
	if err := e.screenCapture(ctx, c); err != nil {
		return err
	}
	if _, err := e.do(ctx, c, cellDir, restoreOp{Snapshot: snapshot, Paths: paths}); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}

// screenCapture applies the seal's guard before any other operation that may
// snapshot the current folder, including the engine's automatic undo snapshot.
func (e Engine) screenCapture(ctx context.Context, c cell.Cell) error {
	s, err := e.Guard.Look(c, e.tree(c), e.policyDir(c), nil, knownLocks(c))
	if err != nil {
		return err
	}
	return noteLeftOut(c, e.tree(c), s, nil)
}

// recoverCompleted carries a dead process's durable outputs into its next
// capture. The WAL stays local, but a branch or move must include the receipt
// of a completed external call even if death preceded its background seal (D4,
// L9). Incomplete intents remain unresolved and are never executed here.
func (e Engine) recoverCompleted(ctx context.Context, c cell.Cell) error {
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil {
		return err
	}
	if len(recovery.Completed) == 0 {
		return nil
	}
	_, err = e.SealRecovered(ctx, c)
	return err
}

// SealRecovered keeps local edits and a dead process's completed calls in the
// same turn before take-back sets them aside as a branch (D4, L9). A call that
// only returned external output is still work; incomplete intents stay local.
func (e Engine) SealRecovered(ctx context.Context, c cell.Cell) (Sealed, error) {
	wal, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil {
		return Sealed{}, err
	}
	sealed, err := e.Seal(ctx, c, TurnInfo{Calls: recovery.Completed, Changed: changedOf(recovery.Completed)})
	if err != nil || len(recovery.Completed) == 0 {
		return sealed, err
	}
	return sealed, wal.Sealed(recovery.Completed)
}
