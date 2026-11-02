package session

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── WHICH MODEL ANSWERS A ROLE ───────────────────────────────────────────────
//
// Every role this session resolves — an errand, the reflex, a task's seats, the
// auditor, the shaper — asks HERE, and nowhere reads [Config.RolesSource] on its
// own (rolemodel_law_test.go fails the build that does).
//
// It exists because `--one-model` used to be kept by the doors alone: they
// withheld the profile's rows and the session trusted that they had. One seam
// (auxiliary.go's errand door) also moved its floor, but the rungs ABOVE the
// floor still answered whatever a source said, and the reflex never looked at
// the flag at all. So a one-model session that was handed a source anyway sent
// its mark reader to the mastermind tier and its reflex to the reflex tier
// (run d6d968960c795fc7: four and two calls off the one model out of 1896).
// The promise is now kept where the question is answered, so a door that
// forgets to withhold a row can no longer break it.

// roleReader answers role and tier questions for one session.
type roleReader struct {
	source roles.Source
	// one is set under one model; model is then the only text model.
	one   bool
	model string
}

// readerFor is the reader a config and its live model give.
func readerFor(cfg Config, model string) roleReader {
	return roleReader{source: roles.Source(cfg.RolesSource), one: cfg.OneModel, model: model}
}

// roleModels is this conversation's reader, on the live model so a /model typed
// mid-conversation moves every role with the work.
func (a *Agent) roleModels() roleReader {
	a.mu.Lock()
	defer a.mu.Unlock()
	return readerFor(a.config, a.model)
}

// roleLadder is [roleReader.Ladder] on this conversation's reader.
func (a *Agent) roleLadder(role roles.Role, floor string) ([]roles.Call, error) {
	return a.roleModels().Ladder(role, floor)
}

// Source is the roles source every lookup reads. Under one model it answers
// the one model for every tier and every text pin. A media pin is still the
// person's explicit choice and stays as written: media is chosen by what a
// model can do ([roles.IsMedia]).
func (r roleReader) Source() roles.Source {
	if !r.one {
		return r.source
	}
	media := map[string]bool{}
	for _, role := range roles.Vocabulary() {
		if roles.IsMedia(role) {
			media[roles.PinKey(role)] = true
		}
	}
	inner, model := r.source, r.model
	return func(key string) (string, bool) {
		if media[key] {
			if inner == nil {
				return "", false
			}
			return inner(key)
		}
		return model, model != ""
	}
}

// Ladder is every rung that may answer role, most specific first. Under one
// model a text role has exactly one rung, the one model, whatever floor the
// caller passed; a media role has only its explicit pin.
func (r roleReader) Ladder(role roles.Role, floor string) ([]roles.Call, error) {
	if !r.one {
		return roles.Ladder(r.source, role, floor)
	}
	if roles.IsMedia(role) {
		if pin, ok := roles.Pinned(r.source, role); ok {
			model, effort := roles.SplitEffort(pin)
			return []roles.Call{{Model: model, Effort: effort}}, nil
		}
		return nil, fmt.Errorf("%w for role %q under one model", roles.ErrNoModel, role)
	}
	if r.model == "" {
		return nil, fmt.Errorf("%w for role %q under one model", roles.ErrNoModel, role)
	}
	if _, ok := roles.TierOf(role); !ok {
		return nil, fmt.Errorf("%w: %q", roles.ErrUnknownRole, role)
	}
	return []roles.Call{{Model: r.model}}, nil
}

// ResolveCall is the first rung of [roleReader.Ladder].
func (r roleReader) ResolveCall(role roles.Role, floor string) (roles.Call, error) {
	rungs, err := r.Ladder(role, floor)
	if err != nil {
		return roles.Call{}, err
	}
	return rungs[0], nil
}

// Resolve is [roleReader.ResolveCall] without the effort.
func (r roleReader) Resolve(role roles.Role, floor string) (string, error) {
	call, err := r.ResolveCall(role, floor)
	return call.Model, err
}

// TierModel is the model a tier names; under one model, the one model.
func (r roleReader) TierModel(tier roles.Tier) (string, bool) {
	return roles.TierModel(r.Source(), tier)
}
