package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	Running    = "running"
	Finishing  = "finishing"
	YourCall   = "your_call"
	Done       = "done"
	Incomplete = "incomplete"
)

var ErrNotFound = errors.New("run not found")
var ErrStopped = errors.New("the run was stopped")
var ErrAIUnavailable = errors.New("this app AI capability is unavailable")
var ErrBudget = errors.New("per-run budget reached")

type Run struct {
	App          string          `json:"app"`
	DefinitionID string          `json:"run"`
	ID           string          `json:"id"`
	State        string          `json:"state"`
	Step         string          `json:"step"`
	Steps        []StepDetail    `json:"steps,omitempty"`
	CaughtUp     bool            `json:"caught_up,omitempty"`
	Error        string          `json:"error,omitempty"`
	Cost         float64         `json:"cost,omitempty"`
	Proposal     *Proposal       `json:"proposal,omitempty"`
	Subject      *Subject        `json:"subject,omitempty"`
	Screen       string          `json:"screen,omitempty"`
	CanSteer     bool            `json:"can_steer,omitempty"`
	FirstStep    string          `json:"first_step,omitempty"`
	Principal    string          `json:"principal,omitempty"`
	Harness      string          `json:"harness,omitempty"`
	AppVersion   int             `json:"app_version,omitempty"`
	Title        string          `json:"title,omitempty"`
	Queue        string          `json:"review_queue,omitempty"`
	Effects      []EffectReceipt `json:"effects,omitempty"`
	Approval     *Approval       `json:"approval,omitempty"`
	WakeAt       time.Time       `json:"wake_at,omitzero"`
	EndedAt      time.Time       `json:"ended_at,omitzero"`
	StartedAt    time.Time       `json:"started_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ChangeEvent struct {
	App   string `json:"app"`
	Run   string `json:"run"`
	ID    string `json:"id"`
	State string `json:"state"`
	Step  string `json:"step"`
	Agent bool   `json:"agent,omitempty"`
	Queue string `json:"review_queue,omitempty"`
}

type Config struct {
	// OwnerName supplies the name only when a run template binds owner.name.
	OwnerName     string
	ResolveParams func(app string, declared map[string]Parameter, provided map[string]any) (map[string]any, error)
	RecordInput   func(app string, change Change) (Change, error)
	ReadRecords   func(app string, recordType string) ([]any, error)
	Root          string
	Now           func() time.Time
	Load          func(app, run string) (Definition, error)
	Triggers      func() ([]Trigger, error)
	GooglePoll    func(context.Context, connections.GoogleTrigger, connections.GoogleCheckpoint, time.Time) (connections.GoogleCheckpoint, []connections.GoogleEvent, error)
	// Records applies local writes. The live and sample engines use Data.Write,
	// which commits a receipt for each declared write's stable Call.Key.
	Records    func(context.Context, RecordCall) (Reply, error)
	Render     func(context.Context, Call, schema.RenderAction) (Reply, error)
	Agent      func(context.Context, AgentCall) (Reply, error)
	CheckAI    func(*schema.AIStep) error
	AppVersion func(string) (int, error)
	Connector  Connector
	// CheckTool authorizes a tool against the current app capabilities on
	// each visit. It can require a send gate based on trusted tool metadata.
	CheckTool        func(context.Context, Call) (outbound bool, err error)
	CheckAgentAction func(app string, origin AgentActionOrigin) error
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
	workCancels map[string]context.CancelFunc
	workers     sync.WaitGroup
	openedAt    time.Time
	closing     bool
	closed      bool
	lifetime    context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closeErr    error
}

// AgentActionOrigin is the app grant that authorized this run.
type AgentActionOrigin struct {
	Principal data.AgentPrincipal `json:"principal"`
	Action    string              `json:"action"`
	Cause     string              `json:"cause"`
	Version   int                 `json:"version"`
}

type checkpoint struct {
	Run
	TriggerReceipt *triggerReceipt    `json:"trigger_receipt,omitempty"`
	IntakeAI       bool               `json:"intake_ai,omitempty"`
	Definition     Definition         `json:"definition"`
	Params         map[string]any     `json:"params"`
	Input          map[string]any     `json:"input,omitempty"`
	Outputs        map[string]any     `json:"outputs"`
	Frames         []frame            `json:"frames"`
	Visit          int                `json:"visit"`
	Attempt        int                `json:"attempt"`
	Prepared       bool               `json:"prepared"`
	Action         *action            `json:"action,omitempty"`
	Approved       string             `json:"approved,omitempty"`
	Declined       string             `json:"declined,omitempty"`
	RaisedBudget   float64            `json:"raised_budget,omitempty"`
	ApprovalSeq    int                `json:"approval_seq"`
	Steering       []string           `json:"steering,omitempty"`
	Stopped        bool               `json:"stopped,omitempty"`
	AgentOrigin    *AgentActionOrigin `json:"agent_origin,omitempty"`
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
		active: make(map[string]Run), busy: make(map[string]bool), workCancels: make(map[string]context.CancelFunc),
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
	if err == nil {
		c.projectDetails()
	}
	return c.Run, err
}

// AgentProjection reads the question's address and the answer committed by the agent step.
func (e *Engine) AgentProjection(id string) (map[string]any, string, error) {
	c, err := e.read(id)
	if err != nil {
		return nil, "", err
	}
	step, _ := c.Outputs[c.FirstStep].(map[string]any)
	if step == nil {
		step, _ = c.Outputs["answer"].(map[string]any)
	}
	answer, text := step["output"].(string)
	if !text && step["output"] != nil {
		raw, err := json.Marshal(step["output"])
		if err != nil {
			return nil, "", err
		}
		answer = string(raw)
	}
	return c.Params, answer, nil
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

// StartAgentAction starts a declared automation under the app agent's grant.
func (e *Engine) StartAgentAction(app, run string, params map[string]any, origin AgentActionOrigin) (Run, error) {
	if origin.Principal.ID == "" || origin.Action == "" || origin.Cause == "" || origin.Version < 1 || e.cfg.CheckAgentAction == nil {
		return Run{}, errors.New("the agent action grant is unavailable")
	}
	if err := e.cfg.CheckAgentAction(app, origin); err != nil {
		return Run{}, err
	}
	c, err := e.newCheckpoint(app, run, params, nil, false)
	if err != nil {
		return Run{}, err
	}
	c.AgentOrigin, c.Principal = &origin, origin.Principal.ID
	err = e.save(&c, "", nil)
	return c.Run, err
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
	if err := e.checkAI(def.Steps); err != nil {
		return checkpoint{}, err
	}
	return e.buildCheckpoint(app, def, params, input, caughtUp)
}

func (e *Engine) buildCheckpoint(app string, def Definition, params, input map[string]any, caughtUp bool) (checkpoint, error) {
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
		Run:        Run{App: app, DefinitionID: def.Run, ID: id, State: Running, Step: def.Steps[0].ID, CaughtUp: caughtUp, StartedAt: now},
		Definition: def, Params: p, Input: input, Outputs: make(map[string]any), Frames: []frame{{}},
		Visit: 1,
	}
	if e.cfg.AppVersion != nil {
		c.AppVersion, err = e.cfg.AppVersion(app)
		if err != nil {
			return checkpoint{}, err
		}
	}
	c.bindAgentSubject()

	return c, nil
}

// bindAgentSubject records the target before the worker runs, so presence and
// record panels can show work while the first step is waiting.
func (c *checkpoint) bindAgentSubject() {
	def := c.Definition
	step := firstAgentStep(def.Steps)
	if step == nil {
		return
	}
	c.Harness, c.Title, c.FirstStep, c.CanSteer = HarnessCodeaf, def.Title, step.ID, true
	if step.AI != nil {
		c.Harness = harnessID(step.AI)
	}
	switch {
	case step.AI != nil && step.AI.Propose != "":
		c.bindSubject(c.Params[strings.TrimPrefix(step.AI.Propose, "$params.")])
	case AgentConversation(def.Run):
		c.Screen, _ = c.Params["screen"].(string)
		c.bindSubject(map[string]any{"id": c.Params["record"], "type": c.Params["type"]})
	default:
		for _, key := range slices.Sorted(maps.Keys(def.Params)) {
			if def.Params[key].Type == "ref" {
				c.bindSubject(c.Params[key])
				break
			}
		}
	}
}

func firstAgentStep(steps []Step) *Step {
	for _, step := range steps {
		if step.AI != nil || step.Agent != "" {
			return &step
		}
		if found := firstAgentStep(step.Steps); found != nil {
			return found
		}
	}
	return nil
}

func (c *checkpoint) bindSubject(value any) {
	row, _ := value.(map[string]any)
	id, _ := row["id"].(string)
	typ, _ := row["type"].(string)
	if id != "" && typ != "" {
		c.Subject = &Subject{Type: typ, ID: id}
	}
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
		if previous := e.checkpoints[runPrefix+c.ID]; previous != nil && !c.Stopped {
			stopped, err := decode[struct{ Stopped bool }](previous)
			if err != nil {
				return err
			}
			if stopped.Stopped {
				return ErrStopped
			}
		}
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
	case Running, Finishing, YourCall:
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
		e.cfg.Changed(ChangeEvent{App: r.App, Run: r.DefinitionID, ID: r.ID, State: r.State, Step: r.Step, Agent: r.Harness != "", Queue: r.Queue})
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
	err := e.schedules(ctx)
	list := e.activeRuns()
	return slices.DeleteFunc(list, func(r Run) bool { return !r.ready(e.cfg.Now()) }), err
}

func (r Run) ready(now time.Time) bool {
	return (r.State == Running || r.State == Finishing) && (r.WakeAt.IsZero() || !now.Before(r.WakeAt))
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
		delete(e.workCancels, id)
		e.mu.Unlock()
		e.workers.Done()
	}()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.lifetime, cancel)
	defer cancel()
	defer stop()
	e.mu.Lock()
	e.workCancels[id] = cancel
	e.mu.Unlock()
	c, err := e.read(id)
	if err != nil || !c.Run.ready(e.cfg.Now()) {
		return err
	}
	err = e.progress(ctx, &c)
	if errors.Is(err, ErrStopped) || errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func validCost(cost float64) bool {
	return cost >= 0 && !math.IsNaN(cost) && !math.IsInf(cost, 0)
}
