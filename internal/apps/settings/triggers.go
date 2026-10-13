package settings

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) ListTriggers(ctx context.Context, scope svc.Scope) (bridge.ListTriggersResult, error) {
	def, err := s.definition(ctx, scope.App)
	if err != nil {
		return bridge.ListTriggersResult{}, err
	}
	s.mu.Lock()
	p, err := s.read(scope.App)
	s.mu.Unlock()
	if err != nil {
		return bridge.ListTriggersResult{}, err
	}
	at := s.clock()
	capPaused, err := s.aiPaused(ctx, scope, def, p, at)
	if err != nil {
		return bridge.ListTriggersResult{}, err
	}
	out := bridge.ListTriggersResult{Triggers: []bridge.TriggerInfo{}}
	for _, trigger := range def.App.Triggers {
		out.Triggers = append(out.Triggers, triggerInfo(def, trigger, p.Triggers[trigger.ID], capPaused, at))
	}
	return out, nil
}

func (s *Service) PauseTrigger(ctx context.Context, scope svc.Scope, params bridge.PauseTriggerParams) (bridge.TriggerInfo, error) {
	if scope.Caller.Agent || scope.Caller.Member == "" || svc.RoleOf(scope.Caller) != svc.RoleEditor {
		return bridge.TriggerInfo{}, bridge.Errorf(bridge.CodeDenied, "Editor required")
	}
	def, err := s.definition(ctx, scope.App)
	if err != nil {
		return bridge.TriggerInfo{}, err
	}
	trigger, ok := findTrigger(def, params.ID)
	if !ok {
		return bridge.TriggerInfo{}, bridge.Errorf(bridge.CodeNotFound, "no trigger "+params.ID)
	}
	info, changed, err := s.pauseTrigger(ctx, scope, def, trigger, params.Paused)
	if err != nil || !changed {
		return info, err
	}
	verb := "resumed"
	if params.Paused {
		verb = "paused"
	}
	line := "@" + strings.TrimPrefix(scope.Caller.Member, "@") + " " + verb + " " + triggerStart(def.App, trigger) + " · Undo"
	return info, s.activity(ctx, scope, line)
}

func (s *Service) pauseTrigger(ctx context.Context, scope svc.Scope, def *spec.Bundle, trigger spec.Trigger, paused bool) (bridge.TriggerInfo, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.read(scope.App)
	if err != nil {
		return bridge.TriggerInfo{}, false, err
	}
	at := s.clock()
	capPaused, err := s.aiPaused(ctx, scope, def, p, at)
	if err != nil {
		return bridge.TriggerInfo{}, false, err
	}
	state := p.Triggers[trigger.ID]
	if state.Paused == paused {
		return triggerInfo(def, trigger, state, capPaused, at), false, nil
	}
	state.Paused = paused
	state.PausedBy, state.PausedAt = "", ""
	if paused {
		state.PausedBy, state.PausedAt = scope.Caller.Member, at.Format(time.RFC3339)
	}
	p.Triggers[trigger.ID] = state
	if err := s.write(scope.App, p); err != nil {
		return bridge.TriggerInfo{}, false, err
	}
	return triggerInfo(def, trigger, state, capPaused, at), true, nil
}

func findTrigger(def *spec.Bundle, id string) (spec.Trigger, bool) {
	for _, trigger := range def.App.Triggers {
		if trigger.ID == id {
			return trigger, true
		}
	}
	return spec.Trigger{}, false
}

func (s *Service) aiPaused(ctx context.Context, scope svc.Scope, def *spec.Bundle, p preferences, at time.Time) (bool, error) {
	for _, trigger := range def.App.Triggers {
		if runAI(def.Runs[trigger.Run]) {
			budget, err := s.budget(ctx, scope, def.App, p, at)
			return budget.Paused, err
		}
	}
	return false, nil
}

func (s *Service) Allow(app, id string, at time.Time) bool {
	ctx := context.Background()
	def, err := s.definition(ctx, app)
	if err != nil || s.inactive(ctx, app) {
		return false
	}
	trigger, ok := findTrigger(def, id)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.read(app)
	if err != nil || p.Triggers[id].Paused {
		return false
	}
	if runAI(def.Runs[trigger.Run]) {
		budget, err := s.budget(ctx, svc.Scope{App: app}, def.App, p, at.UTC())
		if err != nil || budget.Paused {
			return false
		}
	}
	state := p.Triggers[id]
	if last, err := time.Parse(time.RFC3339, state.LastFired); err == nil && !at.After(last) {
		return true
	}
	state.LastFired = at.UTC().Format(time.RFC3339)
	p.Triggers[id] = state
	return s.write(app, p) == nil
}

func (s *Service) inactive(ctx context.Context, app string) bool {
	if s.all.Crews.Lifecycle == nil {
		return false
	}
	states, err := s.all.Crews.Lifecycle.List(ctx, svc.Caller{})
	if err != nil {
		// The wave's lifecycle stub has no state yet. Every other failure
		// closes the gate rather than starting work in an unknown app state.
		var typed *bridge.Error
		return !errors.As(err, &typed) || typed.Code != bridge.CodeNotBuilt
	}
	for _, item := range append(states.Archived, states.Deleted...) {
		if item.App.ID == app {
			return true
		}
	}
	return false
}
