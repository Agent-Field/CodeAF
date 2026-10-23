package main

// ── codeaf app: BUDGETS, THE DAY CAP AND THE ASKS ABOUT THEM ────────────────
//
// TWO LIMITS, EACH CHECKED WHERE IT CAN HOLD. An agent's budget is its
// conversation's own spend limit, which the engine checks before every turn
// (internal/session/rail.go); the app keeps the figure in its file, because the
// engine's is the open conversation's only, and binds it again every time the
// conversation opens or a turn starts. The day cap is the app's own and is
// checked here, before a turn is handed to the engine: a turn the cap refuses
// is never sent, so it costs nothing and its words are kept for a retry.
//
// NEITHER CUTS A TURN IN HALF. A turn that crosses a line finishes, as the
// engine's own limit does, and the line is looked at again when it ends.
//
// AND EVERY LINE IS AN ASK, NOT A WALL. Near or at its budget, an agent asks
// whether to raise it, stop, or be told something; at the day cap it asks
// whether to raise the cap for today or wait. A raise that unblocks a refused
// turn runs that turn again with the same words.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appNearBudget is the share of its budget at which an agent first asks.
const appNearBudget = 0.8

// appMoneyAsks numbers the budget asks this process opens.
var appMoneyAsks atomic.Uint64

// appMoneyAsk is one open budget ask: which cap it is about.
type appMoneyAsk struct {
	scope string
}

// appUSD writes a figure as the asks say it: to the cent, and to a hundredth
// of a cent under one, so a tiny budget is never written as nothing.
func appUSD(usd float64) string {
	if usd > 0 && usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// appParseUSD reads a figure a person typed: "8", "8.00" or "$8.00".
func appParseUSD(text string) (float64, bool) {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "$"))
	usd, err := strconv.ParseFloat(text, 64)
	if err != nil || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0, false
	}
	return usd, true
}

// agentBudget is the budget in force for one agent at now: the app's figure
// with any raise for today over it, zero for an agent the person said has no
// limit, or the profile's own conversation limit when the app holds neither.
// set says the figure is the app's.
func (b *appBackend) agentBudget(id string, now time.Time) (usd float64, set bool) {
	var record appwire.AgentBudget
	_ = b.state.read(func(f *appStateFile) { record = f.agentBudgetAt(id, now) })
	switch record.Source {
	case appwire.BudgetSet:
		return *record.USD, true
	case appwire.BudgetUnlimited:
		return 0, true
	}
	return config.SpendRailUSDAt(config.ProfileDir()), false
}

// spentTodayChecked is admission's reader; display callbacks never turn a
// failed ledger read into permission to send.
func (b *appBackend) spentTodayChecked(now time.Time) (float64, error) {
	if b.today != nil {
		return b.today(now), nil
	}
	return session.DailySpendUSD(session.UsageLedgerPath(), now)
}

// bindBudget gives an open conversation the app's budget for it, when the app
// has one. The engine reads it before the next turn.
func (b *appBackend) bindBudget(id string, conv appConversation) error {
	if usd, set := b.agentBudget(id, b.now()); set {
		return conv.SetSpendRail(usd)
	}
	return nil
}

// dayRefused is a turn the day cap would not let start: it is answered like a
// turn that failed before it began, its words kept for a retry, and the agent
// asks about the cap.
func (b *appBackend) dayRefused(a *appAgent, live *appLive, text, clientID string, spent, limit float64) uint64 {
	turn := b.turns.Add(1)
	words := "Day cap reached · " + appUSD(spent) + " of " + appUSD(limit)
	var id string
	b.locked(func() {
		id = a.view.ID
		a.failed, a.failedKind, a.failedClient = text, appwire.ErrorBudget, clientID
		a.view.Retryable = text != ""
	})
	live.box.Event(appwire.EventTurnStarted, appwire.TurnStarted{Agent: id, Turn: turn})
	live.box.Event(appwire.EventTurnDone, appwire.TurnDone{Agent: id, Turn: turn, OK: false, Error: words, ErrorKind: appwire.ErrorBudget})
	b.askDay(a, live, spent, limit, text)
	return turn
}

// askDay opens the day cap's ask on one agent, unless one is open there.
func (b *appBackend) askDay(a *appAgent, live *appLive, spent, limit float64, waiting string) {
	detail := "Spent " + appUSD(spent) + " today across every agent"
	if line := appDetail(waiting); line != "" {
		detail += " · waiting to send: " + line
	}
	b.openMoneyAsk(a, live, appwire.CapDay, appwire.Ask{
		Title:  "Day cap reached · " + appUSD(spent) + " of " + appUSD(limit),
		Detail: detail,
		Options: []appwire.AskOption{
			{ID: appwire.OptionRaiseToday, Label: "Raise for today"},
			{ID: appwire.OptionWait, Label: "Wait until tomorrow"},
		},
	})
}

// askBudget opens an agent's budget ask: close to it, or at it.
func (b *appBackend) askBudget(a *appAgent, live *appLive, spent, budget float64) {
	title := "It's close to its " + appUSD(budget) + " budget"
	if spent >= budget {
		title = "It reached its " + appUSD(budget) + " budget"
	}
	detail := "Spent " + appUSD(spent) + " so far"
	var last string
	b.locked(func() { last = live.lastAnswer })
	if line := appDetail(last); line != "" {
		detail += " · " + line
	}
	b.openMoneyAsk(a, live, appwire.CapAgent, appwire.Ask{
		Title:  title,
		Detail: detail,
		Options: []appwire.AskOption{
			{ID: appwire.OptionRaise, Label: "Raise budget"},
			{ID: appwire.OptionStop, Label: "Stop"},
			{ID: appwire.OptionTell, Label: "Tell it something"},
		},
	})
}

// openMoneyAsk raises one budget ask on a held agent, unless one about the
// same cap is already open there.
func (b *appBackend) openMoneyAsk(a *appAgent, live *appLive, scope string, ask appwire.Ask) {
	ask.ID = "budget:" + scope + ":" + strconv.FormatUint(appMoneyAsks.Add(1), 10)
	ask.Kind, ask.Subkind = appwire.AskApproval, appwire.SubkindBudget
	ask.Subject = &appwire.AskSubject{Kind: appwire.SubjectBudget, Ref: scope}
	opened, moved := false, false
	var view appwire.Agent
	b.locked(func() {
		if a.live != live {
			return
		}
		for _, open := range live.money {
			if open.scope == scope {
				return
			}
		}
		ask.Agent = a.view.ID
		live.money[ask.ID] = appMoneyAsk{scope: scope}
		live.asks = append(live.asks, ask)
		opened = true
		view, moved = b.restateLocked(a, live)
	})
	if !opened {
		return
	}
	live.box.Event(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
	if moved {
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

// closeMoneyAsks closes the open budget asks about one cap on one agent, as
// answered by the app with option, and answers whether any was open.
func (b *appBackend) closeMoneyAsks(a *appAgent, scope, option, receipt string) bool {
	var live *appLive
	var closed []string
	moved := false
	var view appwire.Agent
	b.locked(func() {
		if live = a.live; live == nil {
			return
		}
		for askID, open := range live.money {
			if open.scope == scope {
				closed = append(closed, askID)
				delete(live.money, askID)
				b.dropAskLocked(live, askID)
			}
		}
		if len(closed) > 0 {
			view, moved = b.restateLocked(a, live)
		}
	})
	if len(closed) == 0 {
		return false
	}
	at := b.now()
	for _, askID := range closed {
		live.box.Event(appwire.EventAskClosed, appwire.AskClosed{Agent: view.ID, ID: askID, By: appwire.ClosedByApp, Option: option, At: at})
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: appReceiptEntryID(askID), Agent: view.ID, Kind: appwire.EntryReceipt, At: at, Text: receipt + " · you · " + at.Local().Format("15:04"),
		}})
	}
	if moved {
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return true
}

// retryRefused runs an agent's turn again when the last one failed for want of
// money, which is what a raise was for.
func (b *appBackend) retryRefused(a *appAgent) {
	var id string
	retry := false
	b.locked(func() {
		id = a.view.ID
		retry = a.failed != "" && a.failedKind == appwire.ErrorBudget
	})
	if retry {
		guard.Go("app/retry", func() { _, _ = b.Retry(id) })
	}
}

// moneyAfter looks at both lines once a turn's spending has reached the
// ledger, and asks about the one that was crossed or is close.
func (b *appBackend) moneyAfter(a *appAgent, live *appLive, today float64) {
	now := b.now()
	var id string
	held := false
	b.locked(func() { id, held = a.view.ID, a.live == live })
	if !held {
		return
	}
	// An unreadable limit asks nothing here: the next turn is refused on it.
	if limit, err := b.dayCap(now); err == nil && limit > 0 && today >= limit {
		b.askDay(a, live, today, limit, "")
	}
	budget, _ := b.agentBudget(id, now)
	if budget <= 0 {
		return
	}
	spent := live.conn.Conv.Usage().CostUSD
	if spent < budget*appNearBudget {
		return
	}
	// ONCE PER FIGURE: an agent that was asked at this budget and kept going
	// is not asked again until its budget moves or it reaches it.
	level := "near"
	if spent >= budget {
		level = "reached"
	}
	mark := level + ":" + strconv.FormatFloat(budget, 'f', -1, 64)
	ask := false
	b.locked(func() {
		if live.budgetAsked != mark {
			live.budgetAsked, ask = mark, true
		}
	})
	if ask {
		b.askBudget(a, live, spent, budget)
	}
}

// answerMoney answers one of this process's budget asks, and says whether the
// ask was one.
func (b *appBackend) answerMoney(a *appAgent, askID, option, text string) (bool, error) {
	var live *appLive
	var open appMoneyAsk
	var id string
	found, running := false, false
	b.locked(func() {
		live, id = a.live, a.view.ID
		if live != nil {
			open, found = live.money[askID]
			running = live.running
		}
	})
	if !found {
		return false, nil
	}
	now := b.now()
	switch open.scope {
	case appwire.CapAgent:
		switch option {
		case appwire.OptionRaise:
			usd, ok := appParseUSD(text)
			if !ok {
				return true, appwire.Errorf(appwire.CodeBadParams, "raise needs the new budget as text, such as \"8.00\"")
			}
			return true, b.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: id, ToUSD: usd, For: appwire.RaiseGood})
		case appwire.OptionStop:
			if running {
				b.locked(func() { live.stopping = true })
				live.conn.Conv.Interrupt()
			}
			b.closeMoneyAsks(a, appwire.CapAgent, appwire.OptionStop, "Stopped")
			return true, nil
		case appwire.OptionTell:
			words := strings.TrimSpace(text)
			if words == "" {
				return true, appwire.Errorf(appwire.CodeBadParams, "tell needs the words to tell it")
			}
			if running {
				return true, appwire.Errorf(appwire.CodeBusy, "this agent is still working on a turn; tell it when the turn ends")
			}
			b.closeMoneyAsks(a, appwire.CapAgent, appwire.OptionTell, "Told it")
			guard.Go("app/tell", func() { _, _ = b.Send(id, words, "") })
			return true, nil
		}
		return true, appwire.Errorf(appwire.CodeBadParams, "the ask %s offers raise, stop and tell", askID)
	case appwire.CapDay:
		switch option {
		case appwire.OptionRaiseToday:
			usd, ok := appParseUSD(text)
			if !ok {
				// Nothing typed raises the cap by itself again: twice the
				// cap, for the rest of today.
				limit, err := b.dayCap(now)
				if err != nil {
					return true, appEngineError(err)
				}
				usd = 2 * limit
				if usd <= 0 {
					usd = 2 * appDayCapDefault
				}
			}
			return true, b.RaiseCap(appwire.CapRaise{Scope: appwire.CapDay, ToUSD: usd, For: appwire.RaiseToday})
		case appwire.OptionWait:
			b.closeMoneyAsks(a, appwire.CapDay, appwire.OptionWait, "Waiting until tomorrow")
			return true, nil
		}
		return true, appwire.Errorf(appwire.CodeBadParams, "the ask %s offers raise_today and wait", askID)
	}
	return true, appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", askID)
}

// RaiseCap raises an agent's budget or the day cap, for today or for good,
// closes the asks it answers and runs again any turn the old figure refused.
func (b *appBackend) RaiseCap(raise appwire.CapRaise) error {
	now := b.now()
	switch raise.Scope {
	case appwire.CapAgent:
		a, err := b.findCrewOwner(raise.ID)
		if err != nil {
			return err
		}
		if err := b.lentFence(a, "budget"); err != nil {
			return err
		}
		err = b.state.write(func(f *appStateFile) error {
			if f.Budgets == nil {
				f.Budgets = map[string]float64{}
			}
			if _, none := f.Unlimited[raise.ID]; none {
				// AN AGENT WITH NO LIMIT HAS NOTHING TO RAISE for today: a
				// figure until midnight would be a cap it does not have now.
				// For good, the person has chosen a figure.
				if raise.For == appwire.RaiseToday {
					return appwire.Errorf(appwire.CodeBadParams, "this agent has no budget to raise; set one with agents.budget.set")
				}
				delete(f.Unlimited, raise.ID)
			}
			if raise.For == appwire.RaiseToday {
				if _, ok := f.Budgets[raise.ID]; !ok {
					f.Budgets[raise.ID] = config.SpendRailUSDAt(config.ProfileDir())
				}
				if f.Raises == nil {
					f.Raises = map[string]appRaise{}
				}
				f.Raises["agent:"+raise.ID] = appRaise{USD: raise.ToUSD, Until: appMidnight(now)}
				return nil
			}
			f.Budgets[raise.ID] = raise.ToUSD
			delete(f.Raises, "agent:"+raise.ID)
			return nil
		})
		if err != nil {
			return appEngineError(err)
		}
		if live := b.liveOf(a); live != nil {
			if err := b.bindBudgetConn(raise.ID, live.conn); err != nil {
				return appEngineError(err)
			}
		}
		view := b.stampAgentBudget(a)
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		b.closeMoneyAsks(a, appwire.CapAgent, appwire.OptionRaise, "Budget raised to "+appUSD(raise.ToUSD))
		b.retryRefused(a)
		return nil
	case appwire.CapDay:
		// ONE AUTHORITY, ONE WRITE: the profile's limit, or its raise for
		// today, and nothing is acknowledged that the environment or a
		// failed write kept from taking effect.
		var err error
		if raise.For == appwire.RaiseToday {
			err = b.raiseDayToday(raise.ToUSD, now)
		} else {
			err = b.setDayBase(raise.ToUSD)
		}
		if err != nil {
			return err
		}
		receipt := "Day cap raised to " + appUSD(raise.ToUSD)
		if raise.For == appwire.RaiseToday {
			receipt += " for today"
		} else {
			b.settingsMoved()
		}
		var agents []*appAgent
		b.locked(func() {
			for _, a := range b.agents {
				agents = append(agents, a)
			}
		})
		for _, a := range agents {
			if b.closeMoneyAsks(a, appwire.CapDay, appwire.OptionRaiseToday, receipt) {
				b.retryRefused(a)
			}
		}
		b.spendSay()
		return nil
	}
	return appwire.Errorf(appwire.CodeBadParams, "scope is agent or day")
}

// spendSay says spend.changed with what today reads now.
func (b *appBackend) spendSay() {
	spend, err := b.SpendToday()
	if err != nil {
		return
	}
	b.locked(func() { b.spent = spend.TodayUSD })
	b.emit.Emit(appwire.EventSpendChanged, appwire.SpendChanged{Spend: spend})
}
