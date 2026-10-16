package main

// ── codeaf app: ONE AGENT'S OWN BUDGET, READ AND SET ────────────────────────
//
// THE RECORD IS THE APP'S, THE CEILING IS THE ENGINE'S. An agent's own figure
// is kept in the app's file (appstate.go's Budgets, and Unlimited for an agent
// the person said has none), because the engine's figure lives only as long as
// the open conversation; bindBudget hands it to the engine at every open and
// every turn (appmoney.go). So a reopened agent, after a quit or a restart, is
// bound the same figure it had — never today's default in its place.
//
// AN AGENT WITH NOTHING OF ITS OWN IS NOT GUESSED AT. It runs on the
// conversation limit its engine read when it opened, which only that engine can
// say; with nothing holding the conversation, or an engine too old to answer,
// it is unknown and is said as unknown. settings.budget_usd is the default a
// conversation is built with, not this.
//
// A SET IS WRITTEN BEFORE IT IS BOUND, AND UNDONE IF IT CANNOT BE. A write the
// disk refuses changes nothing anywhere. A write the open conversation then
// refuses is rolled back on disk, so the record and the ceiling in force never
// disagree about a figure the person was told did not land.

import (
	"math"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// The app's backend serves agents.budget.*.
var _ appwire.Budgeter = (*appBackend)(nil)

// appSpendRailReader is an open conversation that can say the limit it
// checks its next turn against; remote.Agent is one, and an engine too old to
// know the call answers an error.
type appSpendRailReader interface {
	SpendRail() (float64, error)
}

// agentBudgetAt is what the app's file says one agent's budget is at now.
// It is the one reading of the record: bindBudget binds what this says.
func (f *appStateFile) agentBudgetAt(id string, now time.Time) appwire.AgentBudget {
	out := appwire.AgentBudget{Scope: appwire.BudgetScopeAgent, From: appwire.BudgetFromRecord}
	if _, none := f.Unlimited[id]; none {
		out.Source, out.Limit = appwire.BudgetUnlimited, appwire.LimitUnlimited
		return out
	}
	// A figure of zero or less is not a figure: an older file may hold one
	// a raise for today wrote over a profile with no limit, and it was never
	// bound, so it is read as nothing of the agent's own.
	if base, ok := f.Budgets[id]; ok && base > 0 {
		usd := f.raised("agent:"+id, base, now)
		out.Source, out.Limit = appwire.BudgetSet, appwire.LimitLimited
		out.BaseUSD, out.USD = &base, &usd
		if usd > base {
			until := f.Raises["agent:"+id].Until
			out.RaisedUntil = &until
		}
		return out
	}
	out.Source, out.Limit = appwire.BudgetInherited, appwire.LimitUnknown
	return out
}

// budgetRecord is one agent's budget as the app's file says it now.
func (b *appBackend) budgetRecord(id string) (appwire.AgentBudget, error) {
	var record appwire.AgentBudget
	now := b.now()
	err := b.state.read(func(f *appStateFile) { record = f.agentBudgetAt(id, now) })
	return record, err
}

// budgetOf is the record with the open conversation's own reading beside it,
// when the conversation is held and can answer. An inherited budget is then
// the engine's figure, said as the engine's.
func (b *appBackend) budgetOf(id string, live *appLive) (appwire.AgentBudget, error) {
	budget, err := b.budgetRecord(id)
	if err != nil {
		return appwire.AgentBudget{}, appEngineError(err)
	}
	if live == nil {
		return budget, nil
	}
	reader, ok := live.conn.Conv.(appSpendRailReader)
	if !ok {
		return budget, nil
	}
	rail, err := reader.SpendRail()
	if err != nil || rail < 0 || math.IsNaN(rail) || math.IsInf(rail, 0) {
		return budget, nil
	}
	engine := &appwire.AgentBudgetEngine{Limit: appwire.LimitUnlimited, SpentUSD: live.conn.Conv.Usage().CostUSD}
	if rail > 0 {
		engine.Limit, engine.USD = appwire.LimitLimited, &rail
	}
	budget.Engine = engine
	if budget.Source == appwire.BudgetInherited {
		budget.Limit, budget.USD, budget.From = engine.Limit, engine.USD, appwire.BudgetFromEngine
	}
	return budget, nil
}

// stampAgentBudget carries a changed persisted cap on the normal agent fact.
// No engine RPC runs on this event path; all windows see the same change once.
func (b *appBackend) stampAgentBudget(a *appAgent) appwire.Agent {
	var id string
	b.locked(func() { id = a.view.ID })
	budget, err := b.budgetRecord(id)
	var view appwire.Agent
	b.locked(func() {
		if err == nil {
			a.view.Budget = &budget
		}
		view = a.view
	})
	return view
}

// AgentBudget answers agents.budget.get.
func (b *appBackend) AgentBudget(id string) (appwire.AgentBudget, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.AgentBudget{}, err
	}
	return b.budgetOf(id, b.liveOf(a))
}

// SetAgentBudget answers agents.budget.set: a positive figure for good, or
// nil for none. It is written first, bound to the open conversation second,
// and rolled back when the conversation refuses it.
func (b *appBackend) SetAgentBudget(set appwire.AgentBudgetSet) (appwire.AgentBudget, error) {
	if set.USD != nil && (*set.USD <= 0 || math.IsNaN(*set.USD) || math.IsInf(*set.USD, 0)) {
		return appwire.AgentBudget{}, appwire.Errorf(appwire.CodeBadParams, "usd must be a positive amount, or null for none")
	}
	a, err := b.find(set.ID)
	if err != nil {
		return appwire.AgentBudget{}, err
	}
	if b.isClosing() {
		return appwire.AgentBudget{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	// The gate keeps a conversation from opening between the write and the
	// bind with the figure it read before the write.
	a.gate.Lock()
	defer a.gate.Unlock()
	id, scope := set.ID, "agent:"+set.ID
	var oldRail *float64
	if live := b.liveOf(a); live != nil {
		if reader, ok := live.conn.Conv.(appSpendRailReader); ok {
			if value, err := reader.SpendRail(); err == nil && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
				oldRail = &value
			}
		}
	}
	var before appBudgetEntries
	err = b.state.write(func(f *appStateFile) error {
		before = appBudgetEntriesOf(f, id)
		if f.Budgets == nil {
			f.Budgets = map[string]float64{}
		}
		if f.Unlimited == nil {
			f.Unlimited = map[string]time.Time{}
		}
		// A FIGURE SET IS SET FOR GOOD, as caps.raise for good is: a raise
		// for today over the old figure ends with it.
		delete(f.Raises, scope)
		if set.USD == nil {
			delete(f.Budgets, id)
			f.Unlimited[id] = b.now()
			return nil
		}
		delete(f.Unlimited, id)
		f.Budgets[id] = *set.USD
		return nil
	})
	if err != nil {
		return appwire.AgentBudget{}, appEngineError(err)
	}
	live := b.liveOf(a)
	if live != nil {
		if err := b.bindBudget(id, live.conn.Conv); err != nil {
			// The record goes back to what the engine still holds, and the
			// old figure is bound again in case the refusal came after it
			// was taken.
			if undo := b.state.write(func(f *appStateFile) error { before.restore(f, id); return nil }); undo != nil {
				return appwire.AgentBudget{}, appwire.Errorf(appwire.CodeEngine, "the budget update could not be confirmed and its saved value could not be restored: %s; read this agent's budget again", appEngineError(undo).Error())
			}
			var restore error
			if oldRail != nil {
				restore = live.conn.Conv.SetSpendRail(*oldRail)
			} else {
				if _, set := b.agentBudget(id, b.now()); !set {
					return appwire.AgentBudget{}, appwire.Errorf(appwire.CodeEngine, "the saved budget was restored but the conversation's prior limit could not be confirmed; read this agent's budget again")
				}
				restore = b.bindBudget(id, live.conn.Conv)
			}
			if restore != nil {
				return appwire.AgentBudget{}, appwire.Errorf(appwire.CodeEngine, "the saved budget was restored but its live limit could not be confirmed: %s; read this agent's budget again", appEngineError(restore).Error())
			}
			return appwire.AgentBudget{}, appwire.Errorf(appwire.CodeEngine, "the conversation did not take the new budget, so it keeps the one it had: %s", appEngineError(err).Error())
		}
	}
	receipt := "Budget set to no limit"
	if set.USD != nil {
		receipt = "Budget set to " + appUSD(*set.USD)
	}
	if b.closeMoneyAsks(a, appwire.CapAgent, appwire.OptionRaise, receipt) {
		b.retryRefused(a)
	}
	view := b.stampAgentBudget(a)
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return b.budgetOf(id, live)
}

// appBudgetEntries is one agent's three entries in the app's file as they
// stood, so a set the conversation refused puts them back exactly.
type appBudgetEntries struct {
	budget    float64
	hasBudget bool
	none      time.Time
	hasNone   bool
	raise     appRaise
	hasRaise  bool
}

func appBudgetEntriesOf(f *appStateFile, id string) appBudgetEntries {
	var e appBudgetEntries
	e.budget, e.hasBudget = f.Budgets[id]
	e.none, e.hasNone = f.Unlimited[id]
	e.raise, e.hasRaise = f.Raises["agent:"+id]
	return e
}

func (e appBudgetEntries) restore(f *appStateFile, id string) {
	delete(f.Budgets, id)
	delete(f.Unlimited, id)
	delete(f.Raises, "agent:"+id)
	if e.hasBudget {
		if f.Budgets == nil {
			f.Budgets = map[string]float64{}
		}
		f.Budgets[id] = e.budget
	}
	if e.hasNone {
		if f.Unlimited == nil {
			f.Unlimited = map[string]time.Time{}
		}
		f.Unlimited[id] = e.none
	}
	if e.hasRaise {
		if f.Raises == nil {
			f.Raises = map[string]appRaise{}
		}
		f.Raises["agent:"+id] = e.raise
	}
}
