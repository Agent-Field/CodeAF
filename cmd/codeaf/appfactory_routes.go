package main

// ── codeaf app: THE FACTORY'S ROUTING RULES ─────────────────────────────────
//
// The engine's half of appwire.FactoryRoutesKeeper. The read is one GET of
// /v1/routes; the write is ONE PUT of the same route with a body of exactly
// the table and the caller's expect. Only the operator may write, and the
// refusal is made here, in words, before anything leaves.
//
// A WRITE IS SENT ONCE, never retried. Only a refusal carrying the phase
// marker (api.PhaseHeader) proves nothing was saved; any other failure is
// appwire.CodeUncertain, and factory.routes says what the factory kept.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

var (
	_ appwire.FactoryRoutesKeeper = (*appBackend)(nil)
	_ appwire.FactoryRoutesTester = (*appBackend)(nil)
)

const (
	appFactoryRoutesRoute     = "/v1/routes"
	appFactoryRoutesTestRoute = "/v1/routes/test"
)

func (b *appBackend) FactoryRoutes(ctx context.Context, id string) (appwire.FactoryRoutes, error) {
	var kept store.KeptRoutes
	if err := b.factoryHub().read(ctx, id, appFactoryRoutesRoute, appFactorySmall, &kept); err != nil {
		return appwire.FactoryRoutes{}, err
	}
	rules, err := route.Clean(kept.Rules)
	if err != nil {
		return appwire.FactoryRoutes{}, appwire.Errorf(appwire.CodeEngine, "the factory sent routing rules this app cannot read")
	}
	return appwire.FactoryRoutes{Factory: id, Rules: rules, RulesChange: kept.RulesChange}, nil
}

func (b *appBackend) FactoryRoutesSet(ctx context.Context, p appwire.FactoryRoutesSetParams) (appwire.FactoryRoutes, error) {
	none := appwire.FactoryRoutes{}
	p, err := appwire.CheckRoutesSet(p)
	if err != nil {
		return none, err
	}
	if err := b.routeReasonersKnown(ctx, p.Factory, *p.Rules); err != nil {
		return none, err
	}
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return none, err
	}
	defer release()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return none, err
	}
	if !who.me.Operator {
		return none, appwire.Errorf(appwire.CodeForbidden, "only the operator can change the factory's routing rules")
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	body := struct {
		Rules  []route.Rule  `json:"rules"`
		Expect *[]route.Rule `json:"expect,omitempty"`
	}{*p.Rules, p.Expect}
	r, err := h.linkDo(ctx, who, http.MethodPut, appFactoryRoutesRoute, body, appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	case err != nil:
		return none, routesUncertain()
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeUncertain, "the sign-in changed while this was saving, and the result is not known; read the routing rules before trying again")
	}
	if r.provedNoWrite() {
		if r.status == http.StatusConflict {
			return none, appwire.Errorf(appwire.CodeStale, "the factory's routing rules changed since they were read; nothing was changed")
		}
		return none, linkRefusal(r, "the factory refused these routing rules")
	}
	if r.status != http.StatusOK {
		return none, routesUncertain()
	}
	var saved store.KeptRoutes
	if r.bodyBad || json.Unmarshal(r.body, &saved) != nil {
		return none, routesUncertain()
	}
	rules, err := route.Clean(saved.Rules)
	if err != nil {
		return none, routesUncertain()
	}
	h.touch(p.Factory)
	return appwire.FactoryRoutes{Factory: p.Factory, Rules: rules, RulesChange: saved.RulesChange}, nil
}

// routeReasonersKnown refuses a table whose rule runs jobs on an AgentField
// reasoner the control plane does not know, in words, before anything is
// sent: the same check a person makes picking a reasoner for an agentfield
// step from factory.agentfield_reasoners, made on save so a misspelt one is
// never found by a job that cannot run.
//
// ONLY THIS MACHINE'S FACTORY HOLDS AN AGENTFIELD CONNECTION TO ASK. A table
// for another factory (a team's server) is held to the spelling route.Clean
// already checked, and a job its rule sends to a reasoner that factory cannot
// reach waits with the run's own refusal.
func (b *appBackend) routeReasonersKnown(ctx context.Context, id string, rules []route.Rule) error {
	var named []route.Rule
	for _, r := range rules {
		if r.Then.RunOn != "" {
			named = append(named, r)
		}
	}
	if len(named) == 0 {
		return nil
	}
	host, err := b.localHostOf(id)
	if err != nil || host == nil {
		return nil
	}
	client, _, connected := host.agentFieldConn()
	lister, can := client.(interface {
		Nodes(context.Context) ([]agentfield.NodeReasoners, error)
	})
	if !connected || !can {
		return appwire.Errorf(appwire.CodeConflict, "routing rule %q runs jobs on AgentField, and this organization is not connected to AgentField; connect it first, or take run_on off the rule", named[0].Name)
	}
	nodes, err := lister.Nodes(ctx)
	if err != nil {
		return appwire.Errorf(appwire.CodeConflict, "AgentField did not list its reasoners, so routing rule %q could not be checked; nothing was changed: %s", named[0].Name, err.Error())
	}
	known := map[string]bool{}
	for _, n := range nodes {
		for _, r := range n.Reasoners {
			known[strings.ToLower(n.ID+"."+r.ID)] = true
		}
	}
	for _, r := range named {
		if !known[strings.ToLower(r.Then.RunOn)] {
			return appwire.Errorf(appwire.CodeBadParams, "routing rule %q: AgentField has no reasoner %s; pick one it lists", r.Name, r.Then.RunOn)
		}
	}
	return nil
}

func routesUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the routing rules before trying again")
}

// FactoryRoutesTest is POST /v1/routes/test: what a table would do with the
// newest jobs, which the factory reads without writing or asking a model. As
// nothing can have happened, a failure is never "uncertain": it is said as
// the factory being unreachable, or as its own refusal.
func (b *appBackend) FactoryRoutesTest(ctx context.Context, p appwire.FactoryRoutesTestParams) (appwire.FactoryRoutesTried, error) {
	p, err := appwire.CheckRoutesTest(p)
	if err != nil {
		return appwire.FactoryRoutesTried{}, err
	}
	body := struct {
		Rules []route.Rule `json:"rules"`
		Limit int          `json:"limit"`
	}{*p.Rules, p.Limit}
	raw, err := b.factoryHub().post(ctx, p.Factory, appFactoryRoutesTestRoute, body)
	var wireErr *appwire.Error
	switch {
	case routeMissing(err):
		return appwire.FactoryRoutesTried{}, factoryTooOld()
	case errors.As(err, &wireErr) && wireErr.Code == appwire.CodeUncertain:
		return appwire.FactoryRoutesTried{}, appwire.Errorf(appwire.CodeUnreachable, "the factory did not answer the rules test; try again")
	case err != nil:
		return appwire.FactoryRoutesTried{}, err
	}
	var out struct {
		Jobs []route.Tried `json:"jobs"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return appwire.FactoryRoutesTried{}, appwire.Errorf(appwire.CodeEngine, "the factory sent a rules test this app cannot read")
	}
	if out.Jobs == nil {
		out.Jobs = []route.Tried{}
	}
	return appwire.FactoryRoutesTried{Factory: p.Factory, Jobs: out.Jobs}, nil
}
