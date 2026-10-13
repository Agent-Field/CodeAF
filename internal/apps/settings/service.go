package settings

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service keeps app settings separate from the Builder's definition files.
type Service struct {
	root string
	all  *svc.Services
	mu   sync.Mutex
	now  atomic.Value
}

var (
	_ svc.Settings    = (*Service)(nil)
	_ svc.TriggerGate = (*Service)(nil)
	_ svc.BudgetCap   = (*Service)(nil)
)

func New(cfg svc.Config, all *svc.Services) *Service {
	s := &Service{root: cfg.Root, all: all}
	s.now.Store(time.Now)
	return s
}

// SetClock supplies the space's clock without racing calls already in flight.
func (s *Service) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	s.now.Store(now)
}

func (s *Service) clock() time.Time { return s.now.Load().(func() time.Time)().UTC() }

func (s *Service) definition(ctx context.Context, app string) (*spec.Bundle, error) {
	if !validAppID(app) {
		return nil, bridge.Errorf(bridge.CodeInvalid, "invalid app id")
	}
	if s.all == nil || s.all.Store == nil {
		return nil, bridge.Errorf(bridge.CodeInternal, "app store unavailable")
	}
	// Settings need the runs engine to report spending and AI trigger pauses.
	// An unwired engine leaves the whole settings surface unavailable.
	if s.all.Runs == nil {
		return nil, bridge.NotBuilt("runs")
	}
	def, err := s.all.Store.Definition(ctx, app)
	if err != nil {
		return nil, err
	}
	if def == nil || def.App == nil {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return def, nil
}

func (s *Service) activity(ctx context.Context, scope svc.Scope, line string) error {
	if s.all.Crews.Activity == nil {
		return nil
	}
	return s.all.Crews.Activity.Write(ctx, scope, line)
}
