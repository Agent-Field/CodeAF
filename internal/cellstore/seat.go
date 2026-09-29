package cellstore

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// SeatFor builds the executor a session owns: every call runs under the class
// the cell declared and, with cells on, each tool call is logged and the
// workspace sealed when it returns. This is the one place a session's
// executor is made.
//
// obs, when set, sees every process the seat ran and every tool a shell call's
// command line started (the inventory's observer).
// report is told of a seal that failed; the call it followed is never failed
// by it.
func SeatFor(class executor.Class, c cell.Cell, workspace string, obs executor.Observer, report func(error)) (executor.Seat, error) {
	base := executor.Stance{Class: class, Observer: obs, Secrets: vaultEnv{c}}
	if !cell.Enabled() {
		return executor.Watching(base, obs), nil
	}
	rec, err := recorderFor(base.In(workspace), EngineFor(workspace), c, Options{Report: report})
	if err != nil {
		return executor.Watching(base, obs), err
	}
	return executor.Watching(sealed{Stance: base, rec: rec}, obs), nil
}

// EngineFor is the store of a cell whose session works in workspace: it seals
// the whole workspace, with the cell's own .cell/ directory composed in as the
// tree's .cell/ entry, so the receipts and the chain live in the cell and
// nothing is written into the workspace. An empty workspace seals the cell's
// folder itself. It is the one place an Engine is derived for a cell: the
// session's seat and the `cell` verbs both come here, so a rewind restores the
// tree that was sealed.
func EngineFor(workspace string) Engine {
	return Engine{Workspace: workspace, Guard: Guard{Ledger: keys.NewLedger()}}
}

func recorderFor(inner executor.Executor, store Store, c cell.Cell, opts Options) (*Recorder, error) {
	return NewRecorder(inner, store, c, Engine{}.WALPath(c), opts)
}

// sealed is a seat whose tool calls all go through one recorder. Its processes
// are the stance's own; what is added is the record around each call.
type sealed struct {
	executor.Stance
	rec *Recorder
}

// ForSetup implements executor.SetupSeat: the same recorder, and every call is
// a setup turn's, so the seal says so.
func (s sealed) ForSetup() executor.Seat {
	s.Stance.Setup = true
	return s
}

// Around implements executor.Seat.
func (s sealed) Around(ctx context.Context, call executor.Call, run func() ([]byte, bool)) error {
	effect := executor.Classify(executor.PolicyFor(s.Class, s.Setup))
	return s.rec.Around(ctx, call, effect, s.trigger(), run)
}

func (s sealed) trigger() Trigger {
	if s.Setup {
		return Setup
	}
	return AgentRun
}

// NoteModelCall implements executor.ModelNoter.
func (s sealed) NoteModelCall(m executor.ModelCall) { s.rec.NoteModelCall(m) }
