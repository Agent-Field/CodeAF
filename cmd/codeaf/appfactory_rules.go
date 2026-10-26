package main

// ── codeaf app: THE FACTORY'S OWN RULES ─────────────────────────────────────
//
// The engine's half of appwire.FactoryRulesKeeper. The read is one GET of
// /v1/rules; the write is ONE PUT of the same route, spelled here as a
// constant, with a body of exactly the three lists, so nothing else can be
// sent. Only the operator may write, and the refusal is made here, in words,
// before anything leaves (the factory would answer 403, which can read as
// "signed out").
//
// A WRITE IS SENT ONCE, never retried. Only a refusal carrying the phase
// marker (api.PhaseHeader) proves nothing was saved; any other failure is
// appwire.CodeUncertain, and factory.rules says what the factory kept.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

var _ appwire.FactoryRulesKeeper = (*appBackend)(nil)

const appFactoryRulesRoute = "/v1/rules"

func (b *appBackend) FactoryRules(ctx context.Context, id string) (appwire.FactoryRules, error) {
	var rules factory.HouseRules
	if err := b.factoryHub().read(ctx, id, appFactoryRulesRoute, appFactorySmall, &rules); err != nil {
		return appwire.FactoryRules{}, err
	}
	clean, err := rules.Clean()
	if err != nil {
		return appwire.FactoryRules{}, appwire.Errorf(appwire.CodeEngine, "the factory sent rules this app cannot read")
	}
	return appwire.FactoryRules{Factory: id, Rules: clean}, nil
}

func (b *appBackend) FactoryRulesSet(ctx context.Context, p appwire.FactoryRulesSetParams) (appwire.FactoryRules, error) {
	none := appwire.FactoryRules{}
	p, clean, err := appwire.CheckRulesSet(p)
	if err != nil {
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
		return none, appwire.Errorf(appwire.CodeForbidden, "only the operator can change the factory's rules")
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	// THE ADAPT WORD IS SENT ONLY WHEN IT WAS ASKED, "" included: omitted, the
	// factory keeps its word as it is.
	body := struct {
		Rules    []string  `json:"rules"`
		AskFirst []string  `json:"ask_first"`
		Never    []string  `json:"never"`
		Adapt    *string   `json:"adapt,omitempty"`
		Start    *bool     `json:"start_on_its_own,omitempty"`
		Labels   *[]string `json:"start_labels,omitempty"`
	}{clean.Rules, clean.AskFirst, clean.Never, p.Adapt, p.StartOnItsOwn, nil}
	if p.StartLabels != nil {
		labels := append([]string{}, clean.StartLabels...)
		body.Labels = &labels
	}
	r, err := h.linkDo(ctx, who, http.MethodPut, appFactoryRulesRoute, body, appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	case err != nil:
		return none, rulesUncertain()
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeUncertain, "the sign-in changed while this was saving, and the result is not known; read the rules before trying again")
	}
	if r.provedNoWrite() {
		return none, linkRefusal(r, "the factory refused these rules")
	}
	if r.status != http.StatusOK {
		return none, rulesUncertain()
	}
	var saved factory.HouseRules
	if r.bodyBad || json.Unmarshal(r.body, &saved) != nil {
		return none, rulesUncertain()
	}
	if saved, err = saved.Clean(); err != nil {
		return none, rulesUncertain()
	}
	h.touch(p.Factory)
	return appwire.FactoryRules{Factory: p.Factory, Rules: saved}, nil
}

func rulesUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the rules before trying again")
}
