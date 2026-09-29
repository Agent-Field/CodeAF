package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// Options tune a Recorder; every field has a working zero value.
type Options struct {
	// Tool names the calls in receipts; "exec" when empty.
	Tool string
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Report receives a seal failure. A failed seal never fails the call it
	// follows: the call's record is durable in the WAL and rides the next seal.
	Report func(error)
}

// Recorder is the one hook (task 0.7): an executor.Executor that logs each
// call's intent, runs it, logs its completion, and seals the cell.
type Recorder struct {
	inner executor.Executor
	store Store
	cell  cell.Cell
	wal   *WAL
	opts  Options

	mu         sync.Mutex // serialises seals; guards pending and incomplete
	pending    []Executed
	incomplete []Intent
}

var _ executor.Executor = (*Recorder)(nil)

// NewRecorder reopens the WAL at walPath. Calls that completed but never
// sealed ride the next seal; calls with an intent and no completion are kept
// for Incomplete and are never run again here (L9).
func NewRecorder(inner executor.Executor, store Store, c cell.Cell, walPath string, opts Options) (*Recorder, error) {
	wal, rec, err := OpenWAL(walPath)
	if err != nil {
		return nil, fmt.Errorf("open call log: %w", err)
	}
	return &Recorder{inner: inner, store: store, cell: c, wal: wal, opts: opts,
		pending: rec.Completed, incomplete: rec.Incomplete}, nil
}

// Wrap is the seam callers use. With CODEAF_CELLS off it returns inner itself,
// so the flag-off path is not a different code path, it is no path.
func Wrap(inner executor.Executor, c cell.Cell) (executor.Executor, error) {
	if !cell.Enabled() {
		return inner, nil
	}
	r, err := recorderFor(inner, Engine{}, c, Options{})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Exec implements executor.Executor. A call whose intent cannot be made
// durable does not run: an unlogged external call could never be recovered.
func (r *Recorder) Exec(ctx context.Context, req executor.ExecRequest, onOutput func(executor.Chunk)) (executor.ExecResult, error) {
	intent := r.intent(req)
	if err := r.wal.Begin(intent); err != nil {
		return executor.ExecResult{}, fmt.Errorf("log call intent: %w", err)
	}
	res, err := r.inner.Exec(ctx, req, onOutput)
	r.complete(ctx, intent, executed(intent, res, err, r.now().UnixMilli()))
	return res, err
}

// Around records one whole tool call, whatever it did inside: a process through
// Exec, one owned by the tool through Command, or a file written directly. The
// call is the boundary the tree is sealed at, so this is what a session uses;
// Exec is the same record for a caller that has only an executor.
func (r *Recorder) Around(ctx context.Context, call executor.Call, effect executor.SideEffect, trigger Trigger, run func() ([]byte, bool)) error {
	intent := Intent{V: schemaV, Tool: call.Tool, ArgsHash: hashHex(call.Args), Started: r.now().UnixMilli(), SideEffect: string(effect)}
	if err := r.wal.Begin(intent); err != nil {
		return fmt.Errorf("log call intent: %w", err)
	}
	out, failed := run()
	done := executed(intent, executor.ExecResult{Exit: exitOfFailure(failed), Stdout: out}, nil, r.now().UnixMilli())
	done.Changed = call.Changed
	done.Trigger = trigger
	r.complete(ctx, intent, done)
	return nil
}

func exitOfFailure(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

// Incomplete lists the calls that were started and never finished, oldest first.
func (r *Recorder) Incomplete() []Intent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Intent(nil), r.incomplete...)
}

// Resolve closes an incomplete call after it has been surfaced and dealt with.
func (r *Recorder) Resolve(i Intent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.wal.Resolve(i); err != nil {
		return err
	}
	r.incomplete = without(r.incomplete, i)
	return nil
}

func without(list []Intent, gone Intent) []Intent {
	var out []Intent
	for _, i := range list {
		if i.key() != gone.key() {
			out = append(out, i)
		}
	}
	return out
}

func (r *Recorder) now() time.Time {
	if r.opts.Now == nil {
		return time.Now()
	}
	return r.opts.Now()
}

func (r *Recorder) tool() string {
	if r.opts.Tool == "" {
		return "exec"
	}
	return r.opts.Tool
}

func (r *Recorder) intent(req executor.ExecRequest) Intent {
	return Intent{V: schemaV, Tool: r.tool(), ArgsHash: argsHash(req), Started: r.now().UnixMilli(),
		SideEffect: string(executor.Classify(req.Net))}
}

// argsHash hashes the request's canonical arguments. The environment is left
// out: it carries secrets, and the receipt is not the place for them.
func argsHash(req executor.ExecRequest) string {
	raw, _ := json.Marshal(struct {
		Argv []string `json:"argv"`
		Dir  string   `json:"dir"`
	}{req.Argv, req.Dir})
	return hashHex(raw)
}

// complete logs the call's completion and seals. It runs on the call's way
// out whatever the call did, and seals under a context the caller cannot
// cancel: a cancelled call may still have changed files.
func (r *Recorder) complete(ctx context.Context, i Intent, e Executed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.wal.Finish(i, e); err != nil {
		r.report(err)
	}
	r.pending = append(r.pending, e)
	r.seal(context.WithoutCancel(ctx))
}

// seal takes everything pending into one turn. On failure the batch stays
// pending and the next call's seal carries it. Callers hold r.mu.
func (r *Recorder) seal(ctx context.Context) {
	batch := r.pending
	if _, err := r.store.Seal(ctx, r.cell, TurnInfo{Trigger: triggerOfBatch(batch), Calls: batch, Changed: changedOf(batch)}); err != nil {
		r.report(err)
		return
	}
	r.pending = nil
	r.report(r.wal.Sealed(batch))
}

func (r *Recorder) report(err error) {
	if err != nil && r.opts.Report != nil {
		r.opts.Report(err)
	}
}

// executed folds a finished call into its record. Output is kept only for an
// external call (SCHEMAS.md ruling 3): a local one can be re-run.
func executed(i Intent, res executor.ExecResult, callErr error, ended int64) Executed {
	e := Executed{
		Call: Call{Tool: i.Tool, ArgsHash: i.ArgsHash, Started: i.Started, Ended: ended, Exit: exitOf(res, callErr),
			StdoutHash: hashHex(res.Stdout), StderrHash: hashHex(res.Stderr), SideEffect: i.SideEffect},
		Exact:    executor.SnapshotExact(res),
		Services: serviceRecs(res.Services),
	}
	if !i.MayRerun() {
		e.Stdout, e.Stderr = res.Stdout, res.Stderr
	}
	return e
}

func exitOf(res executor.ExecResult, callErr error) int {
	if callErr != nil {
		return -1
	}
	return res.Exit
}

func serviceRecs(list []executor.Service) []ServiceRec {
	var out []ServiceRec
	for _, s := range list {
		out = append(out, serviceRec(s.PGID, s.Argv, s.Ports))
	}
	return out
}

// changedOf is the paths a batch of calls changed, or nil when any one of them
// could have changed anything: a promise of "only these" needs every call's word.
func changedOf(batch []Executed) []string {
	var all []string
	for _, e := range batch {
		if e.Changed == nil {
			return nil
		}
		all = append(all, e.Changed...)
	}
	return all
}

// triggerOfBatch is why the batch's turn exists: Setup when any call in it was a
// setup turn's, so a setup call's seal is never labelled an ordinary run.
func triggerOfBatch(batch []Executed) Trigger {
	for _, e := range batch {
		if e.Trigger == Setup {
			return Setup
		}
	}
	return AgentRun
}
