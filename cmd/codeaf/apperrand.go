package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// errandDoor is the errand on a held conversation: a local session agent, or
// a remote owner that runs it on its own engine.
type errandDoor interface {
	RunErrand(context.Context, session.ErrandAsk) (session.ErrandAnswer, error)
}

// errandRouter is the routes half, which only a local agent can answer.
type errandRouter interface {
	ErrandRoutes() ([]session.ErrandRoute, string)
}

// Errand runs an errand on an agent this engine already holds. Like a reply
// draft it never opens a conversation, sends a turn or invokes a tool; an
// unheld agent answers not_open, and an owner without the door unavailable.
// An explicit model must be one this engine can serve now, or the agent's own.
func (b *appBackend) Errand(ctx context.Context, id string, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	a, err := b.find(id)
	if err != nil {
		return session.ErrandAnswer{}, err
	}
	live := b.liveOf(a)
	if live == nil {
		return session.ErrandAnswer{Reason: appwire.ErrandNotOpen}, nil
	}
	door, ok := live.conn.Conv.(errandDoor)
	if !ok {
		return session.ErrandAnswer{Reason: appwire.ErrandUnavailable}, nil
	}
	if ask.Model != "" && ask.Model != live.conn.Conv.Model() {
		if err := b.errandModelServed(ask.Model); err != nil {
			return session.ErrandAnswer{}, err
		}
	}
	answer, err := door.RunErrand(ctx, ask)
	if err == nil || errors.Is(err, session.ErrErrandAsk) || errors.Is(err, session.ErrErrandRoute) {
		return answer, err
	}
	// ANY OTHER FAILURE IS THE OWNER'S, NOT THE CALLER'S: a remote owner that
	// closed under the errand, or a link that dropped. It is a reason like
	// every other way of not answering, never a wire error.
	if b.liveOf(a) != live {
		return session.ErrandAnswer{Reason: session.ErrandClosed}, nil
	}
	return session.ErrandAnswer{Reason: appwire.ErrandUnavailable}, nil
}

// errandModelServed refuses a model no connected provider offers, by the
// same list models.list answers: the engine's existing model policy.
func (b *appBackend) errandModelServed(model string) error {
	models, err := b.ListModels()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(models, func(m appwire.Model) bool { return m.ID == model }) {
		return nil
	}
	return fmt.Errorf("%w: %s is not a model this engine can serve now; agents.errand.models lists them", session.ErrErrandRoute, model)
}

// ErrandModels answers the roles and models an errand can use right now, from
// the real configuration: the held agent's own routes when one is named, else
// the profile's with the default model as floor.
func (b *appBackend) ErrandModels(p appwire.ErrandModelsParams) (appwire.ErrandModels, error) {
	listed, err := b.ListModels()
	if err != nil {
		return appwire.ErrandModels{}, err
	}
	known := b.errandParameters()
	out := appwire.ErrandModels{}
	providers := map[string]string{}
	for _, m := range listed {
		providers[m.ID] = m.Provider
		out.Models = append(out.Models, appwire.ErrandModel{ID: m.ID, Provider: m.Provider, ProviderName: m.ProviderName,
			JSON: known(m.ID, "response_format"), JSONSchema: known(m.ID, "structured_outputs"),
			PriceInPerM: m.PriceInPerM, PriceOutPerM: m.PriceOutPerM, ContextLength: m.ContextLength})
	}
	routes, oneModel, err := b.errandRoutes(p.Agent)
	if err != nil {
		return appwire.ErrandModels{}, err
	}
	out.OneModel = oneModel
	for _, r := range routes {
		out.Roles = append(out.Roles, appwire.ErrandRoute{Role: r.Role, Model: r.Model, Provider: providers[r.Model],
			Effort: r.Effort, JSON: known(r.Model, "response_format")})
	}
	return out, nil
}

func (b *appBackend) errandRoutes(id string) ([]session.ErrandRoute, string, error) {
	if id != "" {
		a, err := b.find(id)
		if err != nil {
			return nil, "", err
		}
		if live := b.liveOf(a); live != nil {
			if router, ok := live.conn.Conv.(errandRouter); ok {
				routes, one := router.ErrandRoutes()
				return routes, one, nil
			}
		}
	}
	return b.profileErrandRoutes()
}

// profileErrandRoutes is the profile's role settings with no agent behind them.
func (b *appBackend) profileErrandRoutes() ([]session.ErrandRoute, string, error) {
	proc, err := b.process()
	if err != nil {
		return nil, "", appEngineError(err)
	}
	home, _ := os.UserHomeDir()
	source, err := v3RolesSource(home, proc.ProfileDir)
	if err != nil {
		return nil, "", appEngineError(err)
	}
	return session.ErrandRoutesFrom(roles.Source(source), appDefaultModel()), "", nil
}

// errandParameters answers whether the catalog says a model accepts a
// request field: true, false, or nil when no catalog knows.
func (b *appBackend) errandParameters() func(model, parameter string) *bool {
	proc, err := b.process()
	if err != nil || proc.Models == nil {
		return func(string, string) *bool { return nil }
	}
	return func(model, parameter string) *bool {
		supported, known := proc.Models.SupportsParameter(model, parameter)
		if !known {
			return nil
		}
		return &supported
	}
}
