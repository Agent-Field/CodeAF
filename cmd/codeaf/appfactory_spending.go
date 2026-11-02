package main

// ── codeaf app: THE FACTORY'S SPENDING DEFAULTS ────────────────────────────
//
// The engine's half of appwire.FactorySpendingKeeper. The read is one GET of
// /v1/spending; the write is ONE PATCH of the same route with exactly the
// fields asked for. Only the operator may write, and the refusal is made here,
// in words, before anything leaves. A default model must be one models.list
// offers now, so a factory is never told to run jobs on a model this app
// cannot name.
//
// A WRITE IS SENT ONCE, never retried. Only a refusal carrying the phase
// marker (api.PhaseHeader) proves nothing was saved; any other failure is
// appwire.CodeUncertain, and factory.spending says what the factory kept.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

var _ appwire.FactorySpendingKeeper = (*appBackend)(nil)

const appFactorySpendingRoute = "/v1/spending"

func (b *appBackend) FactorySpending(ctx context.Context, id string) (appwire.FactorySpending, error) {
	var saved factory.Spending
	if err := b.factoryHub().read(ctx, id, appFactorySpendingRoute, appFactorySmall, &saved); err != nil {
		return appwire.FactorySpending{}, err
	}
	if saved.Check() != nil {
		return appwire.FactorySpending{}, appwire.Errorf(appwire.CodeEngine, "the factory sent spending settings this app cannot read")
	}
	return appwire.FactorySpendingOf(id, saved), nil
}

func (b *appBackend) FactorySpendingSet(ctx context.Context, p appwire.FactorySpendingSet) (appwire.FactorySpending, error) {
	none := appwire.FactorySpending{}
	if m := p.Patch.DefaultModel; m != nil && *m != "" {
		offered, err := b.offeredModel(*m)
		if err != nil {
			return none, err
		}
		p.Patch.DefaultModel = &offered
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
		return none, appwire.Errorf(appwire.CodeForbidden, "only the operator can change the factory's spending settings")
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	r, err := h.linkDo(ctx, who, http.MethodPatch, appFactorySpendingRoute, p.Patch, appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	case err != nil:
		return none, spendingUncertain()
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeUncertain, "the sign-in changed while this was saving, and the result is not known; read the spending settings before trying again")
	}
	return spendingReceipt(p.Factory, r, h)
}

// spendingReceipt reads the factory's answer to the one PATCH.
func spendingReceipt(id string, r linkReply, h *appFactoryHub) (appwire.FactorySpending, error) {
	none := appwire.FactorySpending{}
	if r.provedNoWrite() {
		if r.status == http.StatusConflict {
			return none, appwire.Errorf(appwire.CodeStale, "the factory's spending settings changed since they were read; nothing was changed")
		}
		return none, linkRefusal(r, "the factory refused these spending settings")
	}
	var saved factory.Spending
	if r.status != http.StatusOK || r.bodyBad || json.Unmarshal(r.body, &saved) != nil || saved.Check() != nil {
		return none, spendingUncertain()
	}
	h.touch(id)
	return appwire.FactorySpendingOf(id, saved), nil
}

// offeredModel is model as models.list names it, or bad_params when this app
// does not offer it now.
func (b *appBackend) offeredModel(model string) (string, error) {
	offered, err := b.offeredStepModels(map[string]string{"": strings.TrimSpace(model)})
	if err != nil {
		return "", err
	}
	return offered[""], nil
}

func spendingUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the spending settings before trying again")
}
