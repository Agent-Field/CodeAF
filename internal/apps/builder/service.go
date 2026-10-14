package builder

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ModelEnv names the environment variable that picks the Builder's model.
const ModelEnv = "CODEAF_APPS_BUILDER_MODEL"

// Service is the Builder. It reaches its peers at call time through all, and
// uses only the store, the model, the checker, the sample generator and the
// sample seed (see Seeder). The records query API is never reached from this
// package, which is how rule 01 holds by construction; TestBuilderNeverReadsRecords
// keeps it so.
type Service struct {
	root  string
	ctx   context.Context
	all   *svc.Services
	model string
	now   func() time.Time

	// road is the way builds are made (see road.go); roadName is its name.
	road     road
	roadName string
	// completerFor builds the loop worker's provider seat for a model; a test
	// swaps it for a scripted one.
	completerFor func(model string) (session.Completer, error)

	mu     sync.Mutex
	builds map[string]*build
}

var _ svc.Builder = (*Service)(nil)

// New resolves the Builder's model at server start so saved settings and the
// environment pin choose the same model the settings registry reports.
func New(cfg svc.Config, all *svc.Services) *Service {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	model := config.AppsBuilderModelAt(config.ProfilePath(config.ProfileDir(), ""))
	if model == "" {
		model = DefaultModel
	}
	name, chosen := roadFromEnv()
	return &Service{root: cfg.Root, ctx: ctx, all: all, model: model, now: time.Now, builds: map[string]*build{},
		road: chosen, roadName: name, completerFor: liveCompleter}
}

// Create makes the app in state planning and starts the plan in the background.
func (s *Service) Create(ctx context.Context, caller svc.Caller, p bridge.CreateAppParams) (bridge.AppSummary, error) {
	paragraph := strings.TrimSpace(p.Paragraph)
	if paragraph == "" {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeInvalid, "say what the app is for")
	}
	summary, err := s.all.Store.Create(ctx, caller, deriveName(paragraph), paragraph)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	b := s.newBuild(summary.ID, p)
	b.plan.Ask = paragraph
	s.remember(b)
	err = s.start(b, func() error { return nil },
		func(ctx context.Context) error { return s.road.plan(s, ctx, b, p.JustBuild) })
	return summary, err
}

func (s *Service) newBuild(app string, p bridge.CreateAppParams) *build {
	budget := p.BudgetMonth
	if budget <= 0 {
		budget = defaultBudget
	}
	agentModel := strings.TrimSpace(p.Model)
	if agentModel == "" {
		agentModel = DefaultModel
	}
	return &build{app: app, model: s.model, agentModel: agentModel, budget: budget, connectors: p.Connectors}
}

func (s *Service) remember(b *build) {
	s.mu.Lock()
	s.builds[b.app] = b
	s.mu.Unlock()
}

// buildFor returns the session of an app, rebuilding it from plan.json when the
// server restarted since apps.create.
func (s *Service) buildFor(ctx context.Context, app string) (*build, error) {
	s.mu.Lock()
	b, ok := s.builds[app]
	s.mu.Unlock()
	if ok {
		return b, nil
	}
	detail, err := s.all.Store.Get(ctx, app)
	if err != nil {
		return nil, err
	}
	b = s.newBuild(app, bridge.CreateAppParams{})
	if raw, ok := detail.Files[spec.PlanFile]; ok {
		plan, err := decodePlan(raw)
		if err != nil {
			return nil, bridge.Errorf(bridge.CodeInvalid, "plan.json: "+err.Error())
		}
		b.plan = plan
	}
	if err := s.loadResult(b); err != nil {
		return nil, err
	}
	s.road.restore(s, b)
	s.remember(b)
	return b, nil
}

// start runs fn on the server's context, so closing the browser tab does not
// stop a build, and Cancel can. prepare runs first, and only when no other
// step is running.
func (s *Service) start(b *build, prepare func() error, fn func(context.Context) error) error {
	ctx, err := b.begin(s.ctx)
	if err != nil {
		return err
	}
	if err := prepare(); err != nil {
		b.finish()
		return err
	}
	go func() {
		defer b.finish()
		if err := fn(ctx); err != nil {
			s.report(b, err)
		}
	}()
	return nil
}

// report turns a pipeline error into the final state and event: Cancel goes
// back to planning, anything else is a failed build (Fix it · Cancel).
func (s *Service) report(b *build, err error) {
	ctx := context.WithoutCancel(s.ctx)
	if errors.Is(err, context.Canceled) {
		_ = s.setState(ctx, b, bridge.StatePlanning)
		return
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildFailed, Log: err.Error()})
	if detail, getErr := s.all.Store.Get(ctx, b.app); getErr == nil && detail.State == bridge.StateBuilding {
		_ = s.setState(ctx, b, bridge.StateFailed)
	}
}

// Cancel stops the running step; the app returns to planning.
func (s *Service) Cancel(ctx context.Context, scope svc.Scope) error {
	b, err := s.buildFor(ctx, scope.App)
	if err != nil {
		return err
	}
	if b.stop() {
		return nil
	}
	return s.leaveFailed(ctx, b)
}

// leaveFailed is Cancel on a failed build: nothing is running, and the app goes
// back to planning.
func (s *Service) leaveFailed(ctx context.Context, b *build) error {
	detail, err := s.all.Store.Get(ctx, b.app)
	if err != nil || detail.State != bridge.StateFailed {
		return err
	}
	return s.setState(ctx, b, bridge.StatePlanning)
}

// Write starts the write · check · preview run for a planned or failed app. An
// app still building with no step running was cut off by a restart; it is
// written again.
func (s *Service) Write(ctx context.Context, scope svc.Scope) error {
	b, err := s.buildFor(ctx, scope.App)
	if err != nil {
		return err
	}
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return err
	}
	if !writable[detail.State] {
		return bridge.Errorf(bridge.CodeConflict, "the app is "+string(detail.State)+", not planning")
	}
	if len(b.currentPlan().Screens) == 0 {
		return bridge.Errorf(bridge.CodeConflict, "the plan is not ready yet")
	}
	return s.start(b,
		func() error { return s.setState(ctx, b, bridge.StateBuilding) },
		func(ctx context.Context) error { return s.roadFor(b).write(s, ctx, b) })
}

var writable = map[bridge.AppState]bool{
	bridge.StatePlanning: true, bridge.StateFailed: true, bridge.StateBuilding: true,
}

// Seeder is the one piece of the records service the Builder may touch: it
// replaces the sample rows of an app and reads nothing back.
type Seeder interface {
	Seed(ctx context.Context, app string, rows map[string][]bridge.Record) error
}

func (s *Service) seeder() Seeder { return s.all.Records }
