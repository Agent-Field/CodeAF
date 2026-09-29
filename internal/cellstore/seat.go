package cellstore

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// SeatFor builds the executor a session owns: every call runs under the class
// the cell declared and, with cells on, each tool call is logged and the
// workspace sealed when it returns. This is the one place a session's
// executor is made.
//
// report is told of a seal that failed; the call it followed is never failed
// by it.
func SeatFor(class executor.Class, c cell.Cell, workspace string, report func(error)) (executor.Seat, error) {
	base := executor.Stance{Class: class}
	if !cell.Enabled() {
		return base, nil
	}
	rec, err := recorderFor(base.In(workspace), sealTree(workspace), c, Options{Report: report})
	if err != nil {
		return base, err
	}
	return sealed{Stance: base, rec: rec}, nil
}

// sealTree is the store that seals the whole workspace. The cell's own .cell/
// directory is composed in as the tree's .cell/ entry, so the receipts and the
// chain live in the cell and nothing is written into the workspace.
func sealTree(workspace string) Engine { return Engine{Workspace: workspace} }

func recorderFor(inner executor.Executor, store Store, c cell.Cell, opts Options) (*Recorder, error) {
	return NewRecorder(inner, store, c, Engine{}.WALPath(c), opts)
}

// sealed is a seat whose tool calls all go through one recorder. Its processes
// are the stance's own; what is added is the record around each call.
type sealed struct {
	executor.Stance
	rec *Recorder
}

// Around implements executor.Seat.
func (s sealed) Around(ctx context.Context, call executor.Call, run func() ([]byte, bool)) error {
	effect := executor.Classify(executor.PolicyFor(s.Class, false))
	return s.rec.Around(ctx, call, effect, run)
}
