package cellstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// ErrNotSealable says a workspace was left out of sealing: it is somebody's
// folder and not a repository, and the seal would have to make it one.
var ErrNotSealable = errors.New("this folder is not a git repository, so its work is not sealed")

// SeatFor builds the executor a session owns: every call runs under the class
// the cell declared and, with cells on, the recorder logs it and seals the
// workspace when it returns. This is the one place a session's executor is made.
//
// report is told of a seal that failed; the call it followed is never failed by it.
//
// An unsealable workspace still gets a working seat, class and all, and the
// error says why its calls leave no receipts; the caller shows that once.
func SeatFor(class executor.Class, c cell.Cell, workspace string, report func(error)) (executor.Seat, error) {
	base := executor.Stance{Class: class}
	if !cell.Enabled() {
		return base, nil
	}
	tree, err := sealTree(c, workspace)
	if err != nil {
		return base, err
	}
	if err := os.MkdirAll(filepath.Join(tree.Root, cell.StateDir), 0o700); err != nil {
		return base, fmt.Errorf("prepare the sealed tree: %w", err)
	}
	rec, err := recorderFor(base.In(tree.Root), tree, Options{Report: report})
	if err != nil {
		return base, err
	}
	return sealed{Stance: base, rec: rec}, nil
}

// sealTree is the handle whose root is the tree a seal snapshots: the whole
// workspace. THE COMPOSED MODE IS THIS FUNCTION: when the engine can take the
// cell's .cell/ folded into the workspace from elsewhere, it returns c whole
// and the workspace becomes a field of the seal.
func sealTree(c cell.Cell, workspace string) (cell.Cell, error) {
	if err := sealable(c, workspace); err != nil {
		return c, err
	}
	c.Root = workspace
	return c, nil
}

// sealable admits a workspace the session owns (it lives inside the cell) and
// any repository. A user's plain folder is never turned into one.
func sealable(c cell.Cell, workspace string) error {
	if workspace == "" {
		return errors.New("seal: no workspace")
	}
	if inside(c.Root, workspace) || isRepository(workspace) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotSealable, workspace)
}

func inside(parent, dir string) bool {
	rel, err := filepath.Rel(parent, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isRepository(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, repositoryMarker))
	return err == nil
}

func recorderFor(inner executor.Executor, tree cell.Cell, opts Options) (*Recorder, error) {
	engine := Engine{}
	return NewRecorder(inner, engine, tree, engine.WALPath(tree), opts)
}

// sealed is a seat whose tool calls all go through one recorder. Its processes
// are the stance's own; what is added is the record around each call.
type sealed struct {
	executor.Stance
	rec *Recorder
}

// Around implements executor.Seat.
func (s sealed) Around(ctx context.Context, tool string, args []byte, run func() ([]byte, bool)) error {
	effect := executor.Classify(executor.PolicyFor(s.Class, false))
	return s.rec.Around(ctx, tool, args, effect, run)
}
