package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// Options tune a Recorder; every field has a working zero value.
type Options struct {
	// Async pipelines capture after the durable completion, never the command.
	Async bool
	// Tool names the calls in receipts; "exec" when empty.
	Tool string
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Report receives the outcome of every seal: the error of one that failed,
	// nil for one that held. A failed seal never fails the call it follows: the
	// call's record is durable in the WAL and rides the next seal. [SealWatch]
	// is the holder of what the outcomes add up to.
	Report func(error)
	// Refresh, when set, is called just before each seal so the record the seal
	// carries is true at that moment: the inventory uses it to say which of the
	// chat's commands are still running.
	Refresh func()
}

// Recorder is the one hook (task 0.7): an executor.Executor that logs each
// call's intent, runs it, logs its completion, and seals the cell.
type Recorder struct {
	inner     executor.Executor
	store     Store
	cell      cell.Cell
	wal       *WAL
	opts      Options
	asyncMu   sync.Mutex
	running   chan struct{}
	requested bool
	warming   bool
	errorMu   sync.Mutex
	lastError error

	mu      sync.Mutex // serialises seals; guards pending and models
	pending []Executed
	models  []ModelCall // model calls since the last seal

	// incMu guards incomplete alone. A seal holds mu for the whole capture,
	// which on a fresh home is a screen of every file in the workspace, and
	// the list of calls a crash left unfinished is read while a conversation
	// opens: under mu, an open waited for the first capture to end (seconds
	// on a large folder) only to read a list no seal touches.
	incMu      sync.Mutex
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
	wal.owner = c.Root
	reapOrphans(rec.Incomplete, reapGrace)
	return &Recorder{inner: inner, store: store, cell: c, wal: wal, opts: opts,
		pending: rec.Completed, incomplete: rec.Incomplete}, nil
}

// Wrap leaves the executor unchanged when this build cannot move chats, so
// an unavailable engine adds no recording path to ordinary host execution.
func Wrap(inner executor.Executor, c cell.Cell) (executor.Executor, error) {
	if !cell.Available() {
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
	if err := r.wait(ctx); err != nil {
		return executor.ExecResult{}, err
	}
	intent := r.intent(req)
	if err := r.wal.Begin(intent); err != nil {
		return executor.ExecResult{}, fmt.Errorf("log call intent: %w", err)
	}
	res, err := r.inner.Exec(ctx, req, onOutput)
	r.complete(ctx, intent, executed(intent, res, err, r.now().UnixMilli()), true)
	return res, err
}

// Around records one whole tool call, whatever it did inside: a process through
// Exec, one owned by the tool through Command, or a file written directly. The
// call is the boundary the tree is sealed at, so this is what a session uses;
// Exec is the same record for a caller that has only an executor.
func (r *Recorder) Around(ctx context.Context, call executor.Call, effect executor.SideEffect, trigger Trigger, run func() ([]byte, bool)) error {
	if err := r.wait(ctx); err != nil {
		return err
	}
	intent := Intent{V: walV, Tool: call.Tool, ArgsHash: hashHex(call.Args), Started: r.now().UnixMilli(),
		SideEffect: string(effect), Brief: briefOf(call.Args)}
	if err := r.wal.Begin(intent); err != nil {
		return fmt.Errorf("log call intent: %w", err)
	}
	call.Spawns.Watch(func(pgid int) { r.report(r.wal.Started(intent, groupRec(pgid))) })
	out, failed := run()
	done := executed(intent, executor.ExecResult{Exit: exitOfFailure(failed), Stdout: out}, nil, r.now().UnixMilli())
	done.Changed = call.Changed
	done.Command = commandOf(call.Args)
	done.Trigger = trigger
	r.complete(ctx, intent, done, !call.Deferred)
	return nil
}

func exitOfFailure(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

// NoteModelCall implements executor.ModelNoter: the call rides the next seal,
// in the receipt of the turn it was made in.
func (r *Recorder) NoteModelCall(m executor.ModelCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, modelCallOf(m))
}

// Incomplete lists the calls that were started and never finished, oldest first.
func (r *Recorder) Incomplete() []Intent {
	r.incMu.Lock()
	defer r.incMu.Unlock()
	return append([]Intent(nil), r.incomplete...)
}

// Resolve closes an incomplete call after it has been surfaced and dealt with.
func (r *Recorder) Resolve(i Intent) error {
	r.incMu.Lock()
	defer r.incMu.Unlock()
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

// Interrupted implements executor.Interruptible: what the last run left
// unfinished, as a person and the model are told of it.
func (r *Recorder) Interrupted() executor.Interrupted { return interrupted{r} }

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
	return Intent{V: walV, Tool: r.tool(), ArgsHash: argsHash(req), Started: r.now().UnixMilli(),
		SideEffect: string(executor.Classify(req.Net)), Brief: clip(strings.Join(req.Argv, " "))}
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

// complete logs the call's completion and, unless the caller deferred it,
// seals. It runs on the call's way out whatever the call did, and seals under a
// context the caller cannot cancel: a cancelled call may still have changed
// files. A deferred call is already durable in the WAL, so a death before the
// Settle loses nothing the next start does not carry.
func (r *Recorder) complete(ctx context.Context, i Intent, e Executed, seal bool) {
	r.mu.Lock()
	if err := r.wal.Finish(i, e); err != nil {
		r.report(err)
	}
	r.pending = append(r.pending, e)
	if r.opts.Async {
		r.mu.Unlock()
		if seal {
			r.schedule(ctx)
		}
		return
	}
	defer r.mu.Unlock()
	if seal {
		r.seal(context.WithoutCancel(ctx))
	}
}

// Settle implements executor.Settler. It seals what deferred calls left pending,
// and, with nothing pending, the transcript lines written since the last seal
// (the closing answer of a turn), so that a process killed after either loses
// neither. A seal with no call in it looks at the state directory only: nothing
// but the transcript changed.
func (r *Recorder) Settle(ctx context.Context) {
	if r.opts.Async {
		r.schedule(ctx)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 && len(r.models) == 0 && !r.transcriptGrew() {
		return
	}
	r.seal(context.WithoutCancel(ctx))
}

// transcriptGrew reports whether the transcript is longer than the last seal saw.
func (r *Recorder) transcriptGrew() bool {
	head, err := Head(r.cell)
	if err != nil {
		return true
	}
	return transcriptRange(r.cell, head).End != lastEnd(head)
}

func lastEnd(head *Sealed) int64 {
	if head == nil {
		return 0
	}
	return head.Receipt.Transcript.End
}

// seal takes everything pending into one turn. On failure the batch stays
// pending and the next call's seal carries it. Callers hold r.mu.
func (r *Recorder) seal(ctx context.Context) {
	if r.opts.Refresh != nil {
		r.opts.Refresh()
	}
	batch := r.pending
	if _, err := r.store.Seal(ctx, r.cell, TurnInfo{Trigger: triggerOfBatch(batch), Calls: batch, Models: r.models, Changed: changedOf(batch)}); err != nil {
		r.report(err)
		return
	}
	// Ordered turn and receipt writes survive process death. Group commit and
	// explicit boundaries make both stores durable before work leaves this computer (C28/U1).
	r.pending, r.models = nil, nil
	r.outcome(r.wal.Sealed(batch))
}

// outcome tells Report how a seal ended, nil included.
func (r *Recorder) outcome(err error) {
	r.errorMu.Lock()
	r.lastError = err
	r.errorMu.Unlock()
	if r.opts.Report != nil {
		r.opts.Report(err)
	}
}

func (r *Recorder) report(err error) {
	if err != nil {
		r.errorMu.Lock()
		r.lastError = err
		r.errorMu.Unlock()
	}
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
// could have changed anything (an empty batch changed nothing but the transcript): a promise of "only these" needs every call's word.
func changedOf(batch []Executed) []string {
	if len(batch) == 0 {
		return []string{}
	}
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
