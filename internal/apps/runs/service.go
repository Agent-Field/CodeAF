package runs

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the runs engine. It publishes bridge.RunState on app/<id>/runs.
type Service struct {
	eng *engine
}

var _ svc.Runs = (*Service)(nil)

// New returns the runs engine for this apps root.
func New(cfg svc.Config, all *svc.Services) *Service {
	return &Service{eng: shared(cfg, all)}
}

// Close stops triggers and in-flight runs before closing their shared journal.
// Owners must call it before removing the apps root, including on server shutdown.
func (s *Service) Close() {
	s.eng.close()
}

// Start checks the per-run budget, then executes until the run finishes or waits.
func (s *Service) Start(ctx context.Context, scope svc.Scope, p bridge.StartRunParams) (bridge.RunState, error) {
	return s.eng.start(ctx, scope, p, "manual")
}

// Get returns one run.
func (s *Service) Get(ctx context.Context, scope svc.Scope, p bridge.GetRunParams) (bridge.RunState, error) {
	if p.RunID == "" {
		return bridge.RunState{}, bridge.Errorf(bridge.CodeInvalid, "run_id is required")
	}
	job, err := s.eng.loadJob(scope.App, p.RunID)
	if err != nil {
		return bridge.RunState{}, bridge.Errorf(bridge.CodeInternal, "could not read the run")
	}
	if job == nil {
		return bridge.RunState{}, bridge.Errorf(bridge.CodeNotFound, "no run "+p.RunID)
	}
	_ = ctx
	return job.State, nil
}

// List returns runs newest first. For keeps a run whose params name that record.
func (s *Service) List(ctx context.Context, scope svc.Scope, p bridge.ListRunsParams) (bridge.ListRunsResult, error) {
	jobs, err := s.eng.loadJobs(scope.App)
	if err != nil {
		return bridge.ListRunsResult{}, bridge.Errorf(bridge.CodeInternal, "could not list runs")
	}
	out := []bridge.RunState{}
	for _, job := range jobs {
		if p.Run != "" && job.State.Run != p.Run {
			continue
		}
		if p.For != "" && !mentions(job.State.Params, p.For) {
			continue
		}
		out = append(out, job.State)
	}
	_ = ctx
	return bridge.ListRunsResult{Runs: out}, nil
}

func mentions(value any, id string) bool {
	switch typed := value.(type) {
	case string:
		return typed == id
	case map[string]any:
		for _, item := range typed {
			if mentions(item, id) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if mentions(item, id) {
				return true
			}
		}
	}
	return false
}
