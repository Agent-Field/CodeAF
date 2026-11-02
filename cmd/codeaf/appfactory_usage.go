package main

// ── codeaf app: factory.usage_by_model, WHICH MODELS THE FACTORY'S JOBS USED ─
//
// Every call made for a factory job names the job on this machine's spend
// ledger (session.UsageLine.FactoryJob), so the factory's usage by model is a
// fold of that ledger (session.FactoryUsageByModel), joined to this factory's
// jobs for their numbers. How a model is paid is the calls' own word where
// they said one, else the model catalog's single kind for that id, else
// nothing: a guess here would be a bill.

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app's backend serves factory.usage_by_model.
var _ appwire.FactoryUsageReader = (*appBackend)(nil)

// appFactoryModelKinds is how each model id the catalog knows is paid, for ids
// the catalog reaches by one kind only; a test points it elsewhere.
var appFactoryModelKinds = func() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), taskModelsReadWindow)
	defer cancel()
	_, rows, err := config.TaskModelsAt(ctx, config.ProfileDir())
	if err != nil {
		return nil
	}
	seen := map[string]map[string]bool{}
	for _, row := range rows {
		for _, id := range []string{row.Model.ID, row.Send} {
			if id == "" {
				continue
			}
			if seen[id] == nil {
				seen[id] = map[string]bool{}
			}
			seen[id][string(row.Kind)] = true
		}
	}
	kinds := map[string]string{}
	for id, set := range seen {
		if len(set) == 1 {
			for kind := range set {
				kinds[id] = kind
			}
		}
	}
	return kinds
}

// appLedgerBilling is a ledger row's billing word in the wire's words.
var appLedgerBilling = map[string]string{"metered": "metered", "included": "plan"}

// FactoryUsageByModel answers this machine's factory's usage by model.
func (b *appBackend) FactoryUsageByModel(_ context.Context, p appwire.FactoryUsageParams) (appwire.FactoryUsage, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryUsage{}, err
	}
	items, err := host.floor.List()
	if err != nil {
		return appwire.FactoryUsage{}, appEngineError(err)
	}
	from, _ := time.ParseInLocation(time.DateOnly, p.From, appLedgerZone())
	// A day's row was made within a day of its local midnight in any zone.
	lines, err := session.ReadUsage(appLedgerPath(), from.Add(-24*time.Hour))
	if err != nil {
		return appwire.FactoryUsage{}, appEngineError(err)
	}
	fold := session.FactoryUsageByModel(lines, p.From, p.To)
	kinds := appFactoryModelKinds()
	out := appwire.FactoryUsage{Factory: p.Factory, From: p.From, To: p.To, Scope: appwire.FactoryUsageThisComputer,
		Jobs: factoryJobUses(items, fold.Jobs, p.Job, kinds)}
	switch {
	case p.Job == 0:
		out.Models = factoryModelUses(fold.Models, kinds)
	case len(out.Jobs) == 1:
		out.Models = out.Jobs[0].Models
	}
	return out, nil
}

// factoryJobUses is each job of items with calls in the fold (only job, when
// it is not 0), by job number.
func factoryJobUses(items []factory.Item, used map[string][]session.FactoryModelUsage, job int, kinds map[string]string) []appwire.FactoryJobUse {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	var out []appwire.FactoryJobUse
	for _, it := range items {
		name := factory.LedgerName(it)
		models, ok := used[name]
		if ok && (job == 0 || it.ID == job) {
			out = append(out, appwire.FactoryJobUse{Job: it.ID, Ref: name, Models: factoryModelUses(models, kinds)})
		}
	}
	return out
}

// factoryModelUses is a fold's models on the wire.
func factoryModelUses(models []session.FactoryModelUsage, kinds map[string]string) []appwire.FactoryModelUse {
	out := make([]appwire.FactoryModelUse, 0, len(models))
	for _, m := range models {
		use := appwire.FactoryModelUse{
			Model: m.Model, Paid: appLedgerBilling[m.Billing], Calls: m.Calls, TokensIn: m.Input, TokensOut: m.Output,
			UnpricedCalls: m.Unpriced, UnbilledCalls: m.Unbilled,
		}
		if use.Paid == "" {
			use.Paid = kinds[m.Model]
		}
		if m.Priced > 0 {
			usd := math.Round(m.USD*1e6) / 1e6
			use.CostUSD = &usd
		}
		out = append(out, use)
	}
	return out
}
