package cellstore

import (
	"context"
	"os"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatfiles"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// SeatFor builds the executor a session owns: every call runs under the class
// the cell declared and each tool call is logged and the
// workspace sealed when it returns. This is the one place a session's
// executor is made.
//
// obs, when set, sees every process the seat ran and every tool a shell call's
// command line started (the inventory's observer).
// report is told of a seal that failed; the call it followed is never failed
// by it.
func SeatFor(class executor.Class, c cell.Cell, workspace string, obs executor.Observer, report func(error)) (executor.Seat, error) {
	return SeatOver(class, c, workspace, obs, report, nil)
}

// SeatOver is SeatFor with the store every seal goes through decorated by
// over, which is handed the cell's engine. A nil over seals on the engine
// itself. It is how a chat that syncs notes each turn it seals.
func SeatOver(class executor.Class, c cell.Cell, workspace string, obs executor.Observer, report func(error), over func(Engine) Store, opts ...SeatOption) (executor.Seat, error) {
	base := executor.Stance{Class: class, Observer: obs, Secrets: vaultEnv{c}}
	if !cell.Available() {
		return executor.Watching(base, obs), nil
	}
	engine := EngineFor(workspace)
	for _, opt := range opts {
		opt(&engine)
	}
	if runtime.GOOS == "windows" && engine.Transport == nil && engine.Run == nil {
		bin, _ := (Spawn{Binary: engine.Binary}).program()
		if socketFor(bin) == "" {
			engine.notifyHistoryOff(c)
			return executor.Watching(base, obs), nil
		}
	}
	if workspace != "" && engine.workspace(c) == "" {
		engine.Guard.notify(homeFolderWord)
	}
	var store Store = engine
	if over != nil {
		store = over(engine)
	}
	rec, err := recorderFor(base.In(workspace), store, c, Options{Report: report, Refresh: refreshOf(obs), Async: engine.pipeline})
	if err != nil {
		return executor.Watching(base, obs), err
	}
	if engine.warm {
		rec.Warm(context.Background())
	}
	return executor.Watching(sealed{Stance: base, rec: rec}, obs), nil
}

// homeFolderWord is said once when a chat opens in a folder that holds the home
// folder: the conversation is saved, the folder's files are not.
const homeFolderWord = "this folder holds your home folder, so its files are not saved with this chat · rewind brings back the conversation only · open codeaf in a project folder to save its files"

// notifyHistoryOff records the sentence on the chat, so reopening a seat
// cannot repeat it. A notice must never make tools wait on saved history.
func (e Engine) notifyHistoryOff(c cell.Cell) {
	f, err := os.OpenFile(rel(c, cell.StateDir+"/saving-engine-notice"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	_ = f.Close()
	e.Guard.notify("rewind is off because this chat cannot save file history securely; restart codeaf to try again.")
}

// SeatOption adjusts the engine a seat seals through.
type SeatOption func(*Engine)

// WithPipeline returns command results while their capture finishes. Every next
// call and close still drains the same recorder, with a bounded wait (D3).
func WithPipeline() SeatOption { return func(e *Engine) { e.pipeline = true } }

// WithWarmCapture screens the folder as soon as the seat opens without adding
// an empty turn. The first command waits for this screen before changing files.
func WithWarmCapture() SeatOption { return func(e *Engine) { e.warm = true } }

// WithNotices sends the guard's one-line notices (a file this machine cannot
// read, a vault that cannot be written) to say instead of to stderr, which a
// full-screen surface cannot show. A file kept out for a secret is not one:
// it goes to the log ([Guard.Kept]). A door with a surface passes where its
// sentences go.
func WithNotices(say func(string)) SeatOption {
	return func(e *Engine) { e.Guard.Notify = say }
}

// EngineFor is the store of a cell whose session works in workspace: it seals
// the whole workspace, with the cell's own .cell/ directory composed in as the
// tree's .cell/ entry, so the receipts and the chain live in the cell and
// nothing is written into the workspace. An empty workspace seals the cell's
// folder itself. It is the one place an Engine is derived for a cell: the
// session's seat and the `cell` verbs both come here, so a rewind restores the
// tree that was sealed.
func EngineFor(workspace string) Engine {
	return Engine{Workspace: workspace, Compose: Composers{taskcopy.Carry{}, chatfiles.Carry{}}, Guard: Guard{Ledger: keys.NewLedger(), index: &captureIndex{}}}
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

// Interrupted implements executor.Interruptible.
func (s sealed) Interrupted() executor.Interrupted { return s.rec.Interrupted() }

// Around implements executor.Seat.
func (s sealed) Around(ctx context.Context, call executor.Call, run func() ([]byte, bool)) error {
	effect := executor.Classify(executor.PolicyFor(s.Class, s.Setup))
	return s.rec.Around(ctx, call, effect, s.trigger(), run)
}

// Drain implements executor.Drainer: process exit keeps the pending capture.
func (s sealed) Drain(ctx context.Context) error { return s.rec.Drain(ctx) }

// Settle implements executor.Settler.
func (s sealed) Settle(ctx context.Context) { s.rec.Settle(ctx) }

func (s sealed) trigger() Trigger {
	if s.Setup {
		return Setup
	}
	return AgentRun
}

// NoteModelCall implements executor.ModelNoter.
func (s sealed) NoteModelCall(m executor.ModelCall) { s.rec.NoteModelCall(m) }

// refresher is an observer that keeps a record of what is running and brings it
// up to date on request.
type refresher interface{ Refresh() }

// refreshOf is the observer's Refresh, and no hook for an observer that has none.
func refreshOf(obs executor.Observer) func() {
	if r, ok := obs.(refresher); ok {
		return r.Refresh
	}
	return nil
}
