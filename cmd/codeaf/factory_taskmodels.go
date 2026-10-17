package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE FACTORY HONOURS THE PERSON'S TASK MODELS POLICY. `models.tasks.mode=one`
// with `models.tasks.one=model@provider` is the profile's answer to "which
// model does work that leaves a conversation run on", and a factory job is
// exactly that work. A worker that built its parent with the launch flag off
// ignored the row and routed the job's roles through tiers, pins and fallbacks
// nobody chose for it.
//
// THIS READS THE SAME SEAM A TASK DOES ([config.RouteCrew]) and then reuses the
// existing [session.Config.OneModel] mechanism, so there is no second policy to
// drift: one model on one route for every text role, no roles ladder, no crew
// router, no fallback chain, no catalog neighbours. It applies to factory jobs
// ONLY — an interactive conversation keeps its own model.
//
// THE POLICY IS SETTLED ON WHATEVER PARENT A WORKER USES, at the one place every
// factory conversation is opened from ([factoryParentSource]): the parent a
// worker built for itself, the launch config a window or the engine handed it,
// and the empty config `codeaf runner join` starts with all go through
// [factoryApplyTaskPolicy]. An explicit --one-model launch pin still wins.
//
// THE DAILY CREW CAP IS NOT BYPASSED. Under one model a day at its cap refuses
// factory admission ([config.ErrCrewAtCap]) instead of running on: the stages
// are not crew-routed, so the factory's own money budget is a different
// rail and does not enforce the person's /crew cap. This is deliberately
// conservative; raising or waiting out the cap is the one fix.

// factoryOneSources is the service set a one-model parent is checked against and
// dispatches through: the parent's own, else the profile's, exactly what a
// launch would have built.
func factoryOneSources(have modelsource.Set, profileDir string) modelsource.Set {
	if !have.Empty() {
		return have
	}
	return config.ResolveSources(profileDir, config.APIKeyAt(profileDir), config.DefaultBaseURL)
}

// factoryModelID is the whole model id a person pinned or the catalog answered,
// with the thinking level removed, case folded and the default service's own
// `openrouter/` route word dropped, which is exactly what the crew resolver
// strips before it asks the catalog. The vendor segment STAYS: `vendor-a/shared`
// and `vendor-b/shared` are two models.
func factoryModelID(id string) string {
	base, _ := roles.SplitEffort(strings.TrimSpace(id))
	return strings.TrimPrefix(strings.ToLower(base), modelsource.DefaultID+"/")
}

// factoryModelTail is the last path segment of a model id with any thinking
// level removed and case folded: `deepseek/deepseek-v4.1-flash:high` and the
// bare send of a connection that drops the vendor segment share one tail, and
// a dated snapshot or a `:free` pool does not.
func factoryModelTail(id string) string {
	base, _ := roles.SplitEffort(strings.TrimSpace(id))
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	return strings.ToLower(base)
}

// factoryPinnedProvider is the connection the saved one model must run on: the
// one its @provider names, else the one its own id selects.
func factoryPinnedProvider(pin config.CrewPin, profileDir string, sources modelsource.Set) string {
	if named := strings.TrimSpace(pin.Provider); named != "" {
		for _, p := range config.CrewProvidersAt(profileDir) {
			if strings.EqualFold(p.ID, named) || strings.EqualFold(p.Written, named) {
				return strings.ToLower(p.ID)
			}
		}
		return strings.ToLower(named)
	}
	return factorySourceOf(sources, pin.Model)
}

// factorySourceOf is the id of the service a send is dispatched through.
func factorySourceOf(sources modelsource.Set, send string) string {
	connected, _ := sources.For(send)
	if id := strings.TrimSpace(connected.Source.ID); id != "" {
		return strings.ToLower(id)
	}
	return modelsource.DefaultID
}

// factoryTaskOneModel is the model every factory text role must run on when the
// profile is in one-model mode, as the string to send (its @provider route
// already resolved), and whether the profile is in that mode at all. Auto
// answers ("", false, nil) and the caller keeps exactly what it did before.
//
// A one model that cannot be reached, is malformed or is missing is an error,
// NEVER another model: the refusal names the one fix. So is a route that is
// not the pinned one: the provider AND the model the person saved are what
// the send must reach, checked against the service set the send is dispatched
// through. A Lineage, dated-snapshot or :free substitution, or an unprefixed
// send that would fall to the default service, refuses.
func factoryTaskOneModel(profileDir string, sources modelsource.Set) (send string, one bool, err error) {
	if config.TaskModelsModeAt(profileDir) != crewroute.ModeOne {
		return "", false, nil
	}
	d, err := config.RouteCrew(profileDir, config.CrewAsk{})
	// THE PERSON'S DAILY CREW CAP IS NOT SILENTLY BYPASSED: the stages are not
	// crew-routed, so nothing else would enforce it. Every other refusal keeps
	// its own action text (reconnect, add credit, choose the one model).
	if err != nil {
		return "", true, err
	}
	pin, haveOne := config.TaskOneModelAt(profileDir)
	if !haveOne {
		return "", true, errors.New("task models are set to one model, but none is chosen; choose the one model, or switch Task models back to Auto")
	}
	sources = factoryOneSources(sources, profileDir)
	provider := factoryPinnedProvider(pin, profileDir, sources)
	for _, seat := range crewroute.Seats {
		pick := d.Seat(seat)
		got := strings.TrimSpace(pick.Send)
		if got == "" {
			return "", true, fmt.Errorf("task models are set to one model, but the %s role has none; choose the one model, or switch Task models back to Auto", seat)
		}
		if send != "" && got != send {
			return "", true, fmt.Errorf("task models are set to one model, but the %s role would run %s and not %s", seat, got, send)
		}
		send = got
		if !strings.EqualFold(strings.TrimSpace(pick.Provider), provider) || factorySourceOf(sources, got) != provider {
			return "", true, fmt.Errorf("task models are set to one model on %s, but the %s role would reach it through %s; reconnect %s with /connect, or choose another one model", provider, seat, factorySourceOf(sources, got), provider)
		}
		if err := factoryCheckIdentity(sources, provider, pin.Model, pick.Model, got); err != nil {
			return "", true, fmt.Errorf("task models are set to %s, but the %s role would run %s (%v); choose the canonical model id with /models, or switch Task models back to Auto", pin.Model, seat, got, err)
		}
	}
	return send, true, nil
}

// factoryCheckIdentity proves the send reaches the very model the person pinned,
// by full canonical id and never by basename. The catalog id the resolver
// answered must equal the pin; on the default service the bare send must equal
// it too (only a service prefix the connected set itself recognised was
// removed from the send). A direct connection may drop the catalog's vendor
// segment on the wire, so there the resolver's full-id match carries the
// vendor and the bare send must still end in the pinned model's own name. An
// alias this cannot prove the same model is refused, not allowed.
func factoryCheckIdentity(sources modelsource.Set, provider, pinned, picked, send string) error {
	want := factoryModelID(pinned)
	if got := factoryModelID(picked); got != want {
		return fmt.Errorf("the catalog id %s is not %s", got, want)
	}
	_, bare := sources.For(send)
	if provider == modelsource.DefaultID {
		if got := factoryModelID(bare); got != want {
			return fmt.Errorf("the send %s is not %s", got, want)
		}
		return nil
	}
	if factoryModelTail(bare) != factoryModelTail(pinned) {
		return fmt.Errorf("the send %s does not name %s", bare, want)
	}
	return nil
}

// factoryApplyTaskPolicy settles cfg on the saved Task models policy. Auto
// returns cfg EXACTLY as it came (the old path); an explicit --one-model launch
// pin wins untouched; a saved one model replaces the model and clears every
// ladder, router, judge and launch-model figure that could name another model.
// A policy that cannot be met returns [factoryRefusedParent] and the reason.
func factoryApplyTaskPolicy(cfg session.Config, profileDir string) (session.Config, error) {
	if cfg.OneModel {
		return cfg, nil
	}
	sources := factoryOneSources(cfg.Sources, profileDir)
	send, one, err := factoryTaskOneModel(profileDir, cfg.Sources)
	switch {
	case !one:
		return cfg, nil
	case err != nil:
		return factoryRefusedParent(cfg), err
	}
	cfg.Sources = sources
	cfg.Model, cfg.OneModel, cfg.TaskModel = send, true, ""
	cfg.RolesSource, cfg.RouteCrew, cfg.ModelFallbacks, cfg.NearestModels = nil, nil, nil, nil
	// NO SECOND TEXT MODEL: the pool judge scores landings with a crew model,
	// and the launch model's window is not the one model's.
	cfg.TaskLanded = nil
	cfg.ContextWindow = 0
	return cfg, nil
}

// factoryGovernedParent is the parent config a factory worker builds for
// itself: the ordinary governed config settled on the saved policy.
func factoryGovernedParent(cfg session.Config, profileDir string) (session.Config, error) {
	base, err := applyV3Governance(cfg, profileDir, false, false)
	if err != nil {
		return cfg, err
	}
	return factoryApplyTaskPolicy(base, profileDir)
}

// factoryRefusedParent is what a conversation opens from when the one-model
// policy cannot be met NOW. It carries no model, which [session.NewAgent]
// refuses, and none of the ladders that could stand in for one: a refusal, and
// never a quiet fall back to the last config or to Auto.
func factoryRefusedParent(cfg session.Config) session.Config {
	cfg.Model, cfg.OneModel, cfg.TaskModel = "", true, ""
	cfg.RolesSource, cfg.RouteCrew, cfg.ModelFallbacks, cfg.NearestModels = nil, nil, nil, nil
	cfg.TaskLanded = nil
	cfg.ContextWindow = 0
	return cfg
}

// factoryPolicyRefused says whether a parent is the refused one, and the
// reason is [factoryParentSource.Refusal]'s.
func factoryPolicyRefused(cfg session.Config) bool {
	return cfg.OneModel && strings.TrimSpace(cfg.Model) == ""
}

// errFactoryPolicyRefused is the refusal a refused parent carries when no
// source recorded a more specific reason.
var errFactoryPolicyRefused = errors.New("task models are set to one model, but it cannot be used right now; choose the one model, or switch Task models back to Auto")

// factoryParentSource is the parent every manager and stage conversation of one
// worker opens from: raw (the worker's own governed config, or the launch's)
// settled on the saved policy at each call, with the last refusal kept beside
// it so a conversation is refused BEFORE a session folder is minted, and with
// the person's own words.
type factoryParentSource struct {
	raw     func() session.Config
	profile string
	mu      sync.Mutex
	last    error
}

// Config is the parent for the conversation about to open.
func (f *factoryParentSource) Config() session.Config {
	cfg, err := factoryApplyTaskPolicy(f.raw(), f.profile)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = err
	return cfg
}

// Refusal is why the last [factoryParentSource.Config] was refused, nil when it
// was not.
func (f *factoryParentSource) Refusal() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// refusalOf is the error a refused parent opens with.
func refusalOf(cfg session.Config, why func() error) error {
	if !factoryPolicyRefused(cfg) {
		return nil
	}
	if why != nil {
		if err := why(); err != nil {
			return err
		}
	}
	return errFactoryPolicyRefused
}
