package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	Running    = "running"
	YourCall   = "your_call"
	Done       = "done"
	Incomplete = "incomplete"
)

var ErrNotFound = errors.New("run not found")
var ErrBudget = errors.New("per-run budget reached")

type Run struct {
	App          string    `json:"app"`
	DefinitionID string    `json:"run"`
	ID           string    `json:"id"`
	State        string    `json:"state"`
	Step         string    `json:"step"`
	CaughtUp     bool      `json:"caught_up,omitempty"`
	Error        string    `json:"error,omitempty"`
	Cost         float64   `json:"cost,omitempty"`
	Approval     *Approval `json:"approval,omitempty"`
	WakeAt       time.Time `json:"wake_at,omitzero"`
	EndedAt      time.Time `json:"ended_at,omitzero"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ChangeEvent struct {
	App   string `json:"app"`
	Run   string `json:"run"`
	ID    string `json:"id"`
	State string `json:"state"`
	Step  string `json:"step"`
}

type Config struct {
	ResolveParams func(app string, declared map[string]Parameter, provided map[string]any) (map[string]any, error)
	RecordInput   func(app string, change Change) (Change, error)
	ReadRecords   func(app string, recordType string) ([]any, error)
	Root          string
	Now           func() time.Time
	Load          func(app, run string) (Definition, error)
	Triggers      func() ([]Trigger, error)
	Records       func(context.Context, RecordCall) (Reply, error)
	Render        func(context.Context, Call, schema.RenderAction) (Reply, error)
	Agent         func(context.Context, AgentCall) (Reply, error)
	Connector     Connector
	// CheckTool authorizes a tool against the current app capabilities on
	// each visit. It can require a send gate based on trusted tool metadata.
	CheckTool func(context.Context, Call) (outbound bool, err error)
	// Callbacks run after the checkpoint commits. They may read run state.
	Changed func(ChangeEvent)
	Ask     func(Run)
}

type Engine struct {
	cfg     Config
	journal *store.Store
	lock    *os.File
	// The clock serializes schedule scanning. Each runnable instance also
	// claims a worker before it can make any effects.
	clock       sync.Mutex
	outbox      sync.Mutex
	mu          sync.Mutex
	checkpoints map[string]json.RawMessage
	active      map[string]Run
	busy        map[string]bool
	workers     sync.WaitGroup
	openedAt    time.Time
	closing     bool
	closed      bool
	lifetime    context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closeErr    error
}

type checkpoint struct {
	Run
	TriggerReceipt *triggerReceipt `json:"trigger_receipt,omitempty"`
	Definition     Definition      `json:"definition"`
	Params         map[string]any  `json:"params"`
	Input          map[string]any  `json:"input,omitempty"`
	Outputs        map[string]any  `json:"outputs"`
	Frames         []frame         `json:"frames"`
	Visit          int             `json:"visit"`
	Attempt        int             `json:"attempt"`
	Prepared       bool            `json:"prepared"`
	Action         *action         `json:"action,omitempty"`
	Approved       string          `json:"approved,omitempty"`
	Declined       string          `json:"declined,omitempty"`
	RaisedBudget   float64         `json:"raised_budget,omitempty"`
	ApprovalSeq    int             `json:"approval_seq"`
}

type frame struct {
	Index int   `json:"index"`
	Round int   `json:"round"`
	Items []any `json:"items,omitempty"`
}

const runPrefix = "run/"

// Open restores runs and schedules and claims the profile's execution lock,
// so two app engines cannot send the same approved step.
func Open(cfg Config) (*Engine, error) {
	if cfg.Root == "" || cfg.Load == nil {
		return nil, errors.New("run journal root and definition loader are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := os.MkdirAll(cfg.Root, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(cfg.Root, "runs.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		lock.Close()
		return nil, fmt.Errorf("claim app runs: %w", err)
	}
	journal, err := store.Open(filepath.Join(cfg.Root, "runs.db"))
	if err != nil {
		lock.Close()
		return nil, err
	}
	entries, err := journal.AppRunCheckpoints()
	if err != nil {
		journal.Close()
		lock.Close()
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	e := &Engine{
		cfg: cfg, journal: journal, lock: lock, checkpoints: entries,
		active: make(map[string]Run), busy: make(map[string]bool),
		lifetime: lifetime, cancel: cancel, openedAt: cfg.Now(),
	}
	for key, raw := range entries {
		if !strings.HasPrefix(key, runPrefix) {
			continue
		}
		run, err := decode[Run](raw)
		if err != nil {
			e.Close()
			return nil, fmt.Errorf("restore app runs: %w", err)
		}
		e.track(run)
	}
	return e, nil
}

func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		e.clock.Lock()
		e.mu.Lock()
		e.closing = true
		e.mu.Unlock()
		e.clock.Unlock()
		e.cancel()
		e.workers.Wait()
		e.mu.Lock()
		defer e.mu.Unlock()
		e.closed = true
		e.closeErr = errors.Join(e.journal.Close(), e.lock.Close())
	})
	return e.closeErr
}

func decode[T any](raw []byte) (T, error) {
	var value T
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&value)
	return value, err
}

func (e *Engine) read(id string) (checkpoint, error) {
	e.mu.Lock()
	raw := e.checkpoints[runPrefix+id]
	e.mu.Unlock()
	if raw == nil {
		return checkpoint{}, ErrNotFound
	}
	c, err := decode[checkpoint](raw)
	if err == nil {
		normalize(c.Params)
		normalize(c.Input)
		normalize(c.Outputs)
		for i := range c.Frames {
			normalize(c.Frames[i].Items)
		}
	}
	return c, err
}

func (e *Engine) Get(id string) (Run, error) {
	c, err := e.read(id)
	return c.Run, err
}

func (e *Engine) List(app, run string) ([]Run, error) {
	e.mu.Lock()
	entries := make([]json.RawMessage, 0, len(e.checkpoints))
	for key, raw := range e.checkpoints {
		if strings.HasPrefix(key, runPrefix) {
			entries = append(entries, raw)
		}
	}
	e.mu.Unlock()
	result := make([]Run, 0, len(entries))
	for _, raw := range entries {
		r, err := decode[Run](raw)
		if err != nil {
			return nil, err
		}
		if (app == "" || r.App == app) && (run == "" || r.DefinitionID == run) {
			result = append(result, r)
		}
	}
	slices.SortFunc(result, func(a, b Run) int { return strings.Compare(b.ID, a.ID) })
	return result, nil
}

func (e *Engine) Start(app, run string, params map[string]any) (Run, error) {
	return e.start(app, run, params, nil, false, "", nil)
}

func (e *Engine) start(app, run string, params, input map[string]any, caughtUp bool, clockKey string, clock any) (Run, error) {
	c, err := e.newCheckpoint(app, run, params, input, caughtUp)
	if err != nil {
		return Run{}, err
	}
	err = e.save(&c, clockKey, clock)
	return c.Run, err
}

func (e *Engine) newCheckpoint(app, run string, params, input map[string]any, caughtUp bool) (checkpoint, error) {
	if !validID(app) || !validID(run) {
		return checkpoint{}, errors.New("valid app and run ids are required")
	}
	def, err := e.cfg.Load(app, run)
	if err != nil {
		return checkpoint{}, err
	}
	if def.Run != run {
		return checkpoint{}, fmt.Errorf("run file %s declares %s", run, def.Run)
	}
	if err := validate(def); err != nil {
		return checkpoint{}, err
	}
	now := e.cfg.Now()
	id, err := cell.NewID(now)
	if err != nil {
		return checkpoint{}, err
	}
	// The plan is captured with the run so an edit to a live app cannot
	// move a checkpoint to a different step after a restart.
	p := make(map[string]any, len(def.Params)+len(params))
	for name, param := range def.Params {
		if param.Default != nil {
			p[name] = param.Default
		}
	}
	for key, value := range params {
		p[key] = value
	}
	if e.cfg.ResolveParams != nil {
		p, err = e.cfg.ResolveParams(app, def.Params, p)
		if err != nil {
			return checkpoint{}, err
		}
	}
	c := checkpoint{
		Run:        Run{App: app, DefinitionID: run, ID: id, State: Running, Step: def.Steps[0].ID, CaughtUp: caughtUp, StartedAt: now},
		Definition: def, Params: p, Input: input, Outputs: make(map[string]any), Frames: []frame{{}},
		Visit: 1,
	}
	return c, nil
}

func (e *Engine) save(c *checkpoint, clockKey string, clock any) error {
	e.mu.Lock()
	err := e.saveLocked(c, clockKey, clock)
	e.mu.Unlock()
	if err == nil {
		e.changed(c.Run)
	}
	return err
}

func (e *Engine) saveLocked(c *checkpoint, clockKey string, clock any) error {
	entries := make(map[string]json.RawMessage, 2)
	if c != nil {
		c.UpdatedAt = e.cfg.Now()
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		entries[runPrefix+c.ID] = raw
	}
	if clockKey != "" {
		raw, err := json.Marshal(clock)
		if err != nil {
			return err
		}
		entries[clockKey] = raw
	}
	if e.closed {
		return errors.New("app runs are closed")
	}
	if err := e.journal.RecordAppRunCheckpoints(entries); err != nil {
		return err
	}
	for key, raw := range entries {
		e.checkpoints[key] = raw
	}
	if c != nil {
		e.track(c.Run)
	}
	return nil
}

func (e *Engine) track(r Run) {
	switch r.State {
	case Running, YourCall:
		e.active[r.ID] = r
	default:
		delete(e.active, r.ID)
	}
}

func (e *Engine) activeRuns() []Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	list := make([]Run, 0, len(e.active))
	for _, r := range e.active {
		list = append(list, r)
	}
	slices.SortFunc(list, func(a, b Run) int { return strings.Compare(a.ID, b.ID) })
	return list
}

func (e *Engine) changed(r Run) {
	if e.cfg.Changed != nil {
		e.cfg.Changed(ChangeEvent{App: r.App, Run: r.DefinitionID, ID: r.ID, State: r.State, Step: r.Step})
	}
}

// Answer commits a decision for the exact approval currently on the run.
// A stale answer cannot release a later approval in the same loop.
func (e *Engine) Answer(id, approval string, allow bool) error {
	return e.answer(id, approval, allow, 0)
}

// RaiseBudget resumes a run with a person-approved new per-run dollar limit.
func (e *Engine) RaiseBudget(id, approval string, dollars float64) error {
	if !validCost(dollars) || dollars == 0 {
		return errors.New("enter a positive budget in dollars")
	}
	return e.answer(id, approval, true, dollars)
}

func (e *Engine) answer(id, approval string, allow bool, dollars float64) error {
	e.mu.Lock()
	raw := e.checkpoints[runPrefix+id]
	if raw == nil {
		e.mu.Unlock()
		return ErrNotFound
	}
	c, err := decode[checkpoint](raw)
	if err == nil && (c.Approval == nil || c.Approval.ID != approval) {
		err = errors.New("this run approval is no longer waiting")
	}
	if err == nil && c.Approval.Kind == "budget" && allow {
		if dollars <= c.Approval.MinBudget {
			err = errors.New("enter a new budget above the current limit and cost")
		} else {
			c.RaisedBudget = dollars
		}
	} else if err == nil && dollars > 0 {
		err = errors.New("this approval does not change the run budget")
	}
	if err == nil {
		if c.Approval.Kind != "budget" {
			c.Approved = c.Approval.Kind
		}
		if !allow {
			c.Declined = "approval declined"
			if c.Approval.Kind == "budget" {
				c.Declined = "stopped at the per-run budget"
			}
		}
		c.Approval = nil
		c.State = Running
		err = e.saveLocked(&c, "", nil)
	}
	e.mu.Unlock()
	if err == nil {
		e.changed(c.Run)
	}
	return err
}

// Tick fires due schedules and advances runnable steps until they block.
// Check and Preview can drive it with a deterministic clock.
func (e *Engine) Tick(ctx context.Context) error {
	ready, err := e.ready(ctx)
	for _, r := range ready {
		if e.claim(r.ID) {
			err = errors.Join(err, e.work(ctx, r.ID))
		}
	}
	return err
}

const maxRunWorkers = 4

// Dispatch fires due schedules and starts ready run workers. The live clock
// can scan again while a connector or model is still answering another run.
func (e *Engine) Dispatch(ctx context.Context, report func(error)) error {
	ready, err := e.ready(ctx)
	for _, r := range ready {
		if e.claim(r.ID) {
			guard.Go("app run", func() {
				if err := e.work(ctx, r.ID); err != nil && report != nil {
					report(err)
				}
			})
		}
	}
	return err
}

func (e *Engine) ready(ctx context.Context) ([]Run, error) {
	e.clock.Lock()
	defer e.clock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.lifetime.Err(); err != nil {
		return nil, err
	}
	err := e.schedules()
	list := e.activeRuns()
	return slices.DeleteFunc(list, func(r Run) bool { return !r.ready(e.cfg.Now()) }), err
}

func (r Run) ready(now time.Time) bool {
	return r.State == Running && (r.WakeAt.IsZero() || !now.Before(r.WakeAt))
}

func (e *Engine) claim(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing || len(e.busy) >= maxRunWorkers || e.busy[id] {
		return false
	}
	e.busy[id] = true
	e.workers.Add(1)
	return true
}

func (e *Engine) work(ctx context.Context, id string) error {
	defer func() {
		e.mu.Lock()
		delete(e.busy, id)
		e.mu.Unlock()
		e.workers.Done()
	}()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.lifetime, cancel)
	defer cancel()
	defer stop()
	c, err := e.read(id)
	if err != nil || !c.Run.ready(e.cfg.Now()) {
		return err
	}
	return e.progress(ctx, &c)
}

func validCost(cost float64) bool {
	return cost >= 0 && !math.IsNaN(cost) && !math.IsInf(cost, 0)
}
