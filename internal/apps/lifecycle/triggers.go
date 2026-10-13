package lifecycle

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Archived is the settings gate's optional lifecycle check through svc.Crews.Lifecycle.
func (s *Service) Archived(app string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.load(app)
	return err != nil || state != nil && state.State == bridge.StateArchived
}

// Allow also blocks deleted apps; it does not interrupt runs already in flight.
func (s *Service) Allow(app, trigger string, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.load(app)
	return err == nil && state == nil
}

var _ svc.TriggerGate = (*Service)(nil)

// Gate composes settings pauses with lifecycle without changing the runs engine.
func Gate(all *svc.Services, settings svc.TriggerGate) svc.TriggerGate {
	return triggerGate{all: all, settings: settings}
}

type triggerGate struct {
	all      *svc.Services
	settings svc.TriggerGate
}

func (g triggerGate) Allow(app, trigger string, at time.Time) bool {
	if gate, ok := g.all.Crews.Lifecycle.(svc.TriggerGate); ok && !gate.Allow(app, trigger, at) {
		return false
	}
	return g.settings == nil || g.settings.Allow(app, trigger, at)
}
