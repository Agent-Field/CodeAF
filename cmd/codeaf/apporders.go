package main

// ── codeaf app: STANDING ORDERS AND REMINDERS (wave 6) ──────────────────────
//
// AN ORDER IS AN AUTOMATION. There is one store and one clock, the ones the
// terminal's /automations list reads (internal/automation), and the app keeps
// nothing of its own about an order: every method here is a door onto that
// store, and every event is read back out of it.
//
// What the app adds lives in the store too (internal/automation's trust.go):
// an order an agent proposes is saved as proposed and runs nothing until the
// person ratifies it, from the card in its conversation or from the orders
// list; work that is not yet trusted asks before each run; and every order
// says where its results are said.
//
// THE APP HOLDS AN OPEN WINDOW FOR AS LONG AS IT RUNS, the same hold a
// terminal window takes (chatv3_clock.go), so the clock runs while the app is
// open, and what fell due while the Mac was asleep catches up once, late,
// which the app says as order.missed.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appOrdersEvery is how often the app reads the store for what changed: a
// run that started, a run waiting for the person, an order changed elsewhere.
const appOrdersEvery = 2 * time.Second

// appOrderStore is the automations store, or nil where the process has none.
type appOrderStore func() *automation.Store

// appOrdersStore opens the store under this home the first time it is asked
// for, and answers the same handle after; close lets it go when the app leaves.
// IT IS THE APP'S OWN HANDLE, not the process-wide one conversations propose
// through: that one is opened once for the life of the process, and a test
// binary that starts the app under a throwaway home must not fix it there.
func appOrdersStore() (open appOrderStore, close func()) {
	var once sync.Once
	var store *automation.Store
	open = func() *automation.Store {
		once.Do(func() {
			opened, err := automation.Open(automationsRoot())
			if err == nil {
				store = opened
			}
		})
		return store
	}
	close = func() {
		once.Do(func() {})
		if store != nil {
			_ = store.Close()
		}
	}
	return open, close
}

// ordersOf is the store, or the refusal that says there is none.
func (b *appBackend) ordersOf() (*automation.Store, error) {
	if b.orderStore == nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "standing orders are off")
	}
	store := b.orderStore()
	if store == nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "standing orders are off")
	}
	return store, nil
}

// orderError turns the store's refusals into the app's.
func orderError(err error) error {
	if err == nil {
		return nil
	}
	var wire *appwire.Error
	if errors.As(err, &wire) {
		return err
	}
	if errors.Is(err, automation.ErrNotFound) {
		return appwire.Errorf(appwire.CodeNotFound, "there is no such order")
	}
	return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
}

// orderView is one automation as the app draws an order.
func orderView(asking map[string]automation.Run, a automation.Automation, now time.Time) appwire.Order {
	o := appwire.Order{
		ID:           a.ID,
		Crew:         a.Crew,
		Title:        a.Title,
		Workspace:    a.Workspace,
		When:         automation.Describe(a.Schedule, now),
		WhenExact:    automation.Exact(a.Schedule),
		Wont:         a.Wont,
		AnswersIn:    string(a.AnswersIn),
		Trust:        string(a.Trust),
		ApprovedRuns: a.Approved,
		TrustAfter:   automation.TrustAfter,
		CreatedAt:    a.Created,
	}
	o.Agent = orderAgent(a)
	switch {
	case a.Look != nil:
		o.Kind = appwire.OrderKindWatch
	case a.Action.Do != "":
		o.Kind = appwire.OrderKindOrder
	default:
		o.Kind = appwire.OrderKindReminder
	}
	o.Does = a.Action.Do
	if o.Does == "" {
		o.Does = a.Action.Say
	}
	if o.AnswersIn == "" {
		o.AnswersIn = appwire.AnswersChat
	}
	if o.Trust == "" {
		o.Trust = appwire.TrustTrusted
	}
	limits := a.Limits.Effective()
	o.BudgetPerRunUSD, o.TimePerRunS = limits.USD, int(limits.Time/time.Second)
	switch a.Status {
	case automation.StatusProposed:
		o.State = appwire.OrderProposed
	case automation.StatusPaused:
		o.State = appwire.OrderPaused
	case automation.StatusStopped:
		o.State = appwire.OrderStopped
	case automation.StatusFinished:
		o.State = appwire.OrderDone
	default:
		o.State = appwire.OrderActive
	}
	if !a.Next.IsZero() && o.State == appwire.OrderActive {
		at := a.Next
		o.FiresAt = &at
	}
	if a.Last != nil {
		last := orderRunView(*a.Last)
		o.LastRun = &last
	}
	if run, ok := asking[a.ID]; ok {
		view := orderRunView(run)
		o.Asking = &view
	}
	return o
}

func orderAgent(a automation.Automation) string {
	if a.Origin.Transcript != "" {
		return appAgentID(a.Origin.Transcript)
	}
	return a.Origin.SessionID
}

// orderAsking keeps the last run in the store's oldest-first snapshot.
func orderAsking(runs []automation.Run) map[string]automation.Run {
	asking := make(map[string]automation.Run, len(runs))
	for _, run := range runs {
		asking[run.AutomationID] = run
	}
	return asking
}

// orderRunView is one run as an order's row says it.
func orderRunView(run automation.Run) appwire.OrderRun {
	v := appwire.OrderRun{ID: run.ID, DueAt: run.Due, Late: run.Why == automation.WhyLate, Line: run.Line, USD: run.USD, Transcript: run.Transcript}
	if !run.Started.IsZero() {
		at := run.Started
		v.At = &at
	}
	if run.Phase == automation.PhaseAsking {
		v.AskID = appwire.OrderAskPrefix + strconv.FormatInt(run.ID, 10)
	}
	if run.Phase != automation.PhaseOver {
		return v
	}
	if v.At == nil && !run.Finished.IsZero() {
		at := run.Finished
		v.At = &at
	}
	switch run.Outcome {
	case automation.OutcomeSkipped:
		v.Result = appwire.ResultSkipped
		if run.Line == automation.LineNotRight {
			v.Result = appwire.ResultNotRight
		}
	case automation.OutcomeQuiet:
		v.Result = appwire.ResultQuiet
	case automation.OutcomeDone:
		v.Result = appwire.ResultOK
	default:
		v.Result = appwire.ResultIncomplete
	}
	return v
}

// Orders is every order, or one agent's.
func (b *appBackend) Orders(agent string) ([]appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return nil, err
	}
	all, err := store.List()
	if err != nil {
		return nil, appEngineError(err)
	}
	runs, _ := store.Asking()
	asking := orderAsking(runs)
	now := b.now()
	var out []appwire.Order
	for _, a := range all {
		if agent != "" && orderAgent(a) != agent {
			continue
		}
		out = append(out, orderView(asking, a, now))
	}
	return out, nil
}

// OrderRuns is one order's history, newest first.
func (b *appBackend) OrderRuns(id string, limit int) ([]appwire.OrderRun, error) {
	store, err := b.ordersOf()
	if err != nil {
		return nil, err
	}
	runs, err := store.Runs(id, limit)
	if err != nil {
		return nil, orderError(err)
	}
	out := make([]appwire.OrderRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, orderRunView(run))
	}
	return out, nil
}

// sayOrder reads an order back and says it as order.changed.
func (b *appBackend) sayOrder(store *automation.Store, id string) (appwire.Order, error) {
	a, err := store.Get(id)
	if err != nil {
		return appwire.Order{}, orderError(err)
	}
	runs, _ := store.Asking()
	view := orderView(orderAsking(runs), a, b.now())
	b.emit.Emit(appwire.EventOrderChanged, appwire.OrderChanged{Order: view})
	return view, nil
}

// proposalCard finds the card in a conversation that is asking about a
// proposed order, so the list's answer and the card's are one answer.
func (b *appBackend) proposalCard(id string) (agent, ask string, q session.Question, found bool) {
	b.locked(func() {
		for agentID, a := range b.agents {
			if a.live == nil {
				continue
			}
			for askID, question := range a.live.questions {
				if question.Kind == session.QuestionAutomation && question.Subject.Ref == id {
					if _, done := a.live.answered[askID]; done {
						continue
					}
					agent, ask, q, found = agentID, askID, question, true
					return
				}
			}
		}
	})
	return
}

// RatifyOrder is the person's yes. Edits made on the way are saved first;
// the card in the conversation, if it is still up, is answered with the
// same yes, so the conversation knows.
func (b *appBackend) RatifyOrder(id string, edits *appwire.OrderEdits) (appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return appwire.Order{}, err
	}
	if edits != nil {
		if _, err := b.changeOrder(store, id, *edits); err != nil {
			return appwire.Order{}, err
		}
	}
	if _, err := store.Ratify(id); err != nil {
		return appwire.Order{}, orderError(err)
	}
	if agent, ask, q, found := b.proposalCard(id); found {
		_ = b.AnswerAsk(agent, ask, appOptionOfKey(q, session.AutomationSaveKey), "")
	}
	return b.sayOrder(store, id)
}

// NotNowOrder is the person's no to a proposed order: it is taken away, and
// the card, if it is still up, is answered with the same no.
func (b *appBackend) NotNowOrder(id string) error {
	store, err := b.ordersOf()
	if err != nil {
		return err
	}
	a, err := store.Get(id)
	if err != nil {
		return orderError(err)
	}
	if a.Status != automation.StatusProposed {
		return appwire.Errorf(appwire.CodeBadParams, "%s is already agreed to; pause or stop it instead", a.Title)
	}
	if agent, ask, q, found := b.proposalCard(id); found {
		_ = b.AnswerAsk(agent, ask, appOptionOfKey(q, session.AutomationNoKey), "")
	}
	if err := store.Delete(id); err != nil && !errors.Is(err, automation.ErrNotFound) {
		return orderError(err)
	}
	b.emit.Emit(appwire.EventOrderRemoved, appwire.OrderRemoved{ID: id})
	return nil
}

// ChangeOrder saves the person's edits.
func (b *appBackend) ChangeOrder(id string, edits appwire.OrderEdits) (appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return appwire.Order{}, err
	}
	if _, err := b.changeOrder(store, id, edits); err != nil {
		return appwire.Order{}, err
	}
	return b.sayOrder(store, id)
}

// changeOrder lays edits over the order and saves it through the store's own
// update, which works a changed schedule out again from now.
func (b *appBackend) changeOrder(store *automation.Store, id string, edits appwire.OrderEdits) (automation.Automation, error) {
	a, err := store.Get(id)
	if err != nil {
		return a, orderError(err)
	}
	now := b.now()
	if edits.When != nil {
		line, err := orderWhenLine(id, *edits.When)
		if err != nil {
			return a, err
		}
		cmd, err := automation.ParseCommand(line)
		if err != nil {
			return a, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		}
		if a, err = cmd.Apply(a, now); err != nil {
			return a, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		}
	}
	if edits.Title != nil {
		a.Title = strings.TrimSpace(*edits.Title)
	}
	if edits.Does != nil {
		does := strings.TrimSpace(*edits.Does)
		if a.Action.Do != "" || a.Action.Say == "" {
			a.Action.Do = does
		} else {
			a.Action.Say = does
		}
	}
	if edits.Wont != nil {
		a.Wont = strings.TrimSpace(*edits.Wont)
	}
	if edits.Crew != nil {
		a.Crew = strings.TrimSpace(*edits.Crew)
	}
	if edits.AnswersIn != nil {
		a.AnswersIn = automation.Scope(*edits.AnswersIn)
	}
	if edits.BudgetPerRunUSD != nil {
		if *edits.BudgetPerRunUSD <= 0 {
			return a, appwire.Errorf(appwire.CodeBadParams, "a budget per run is above zero")
		}
		a.Limits.USD = *edits.BudgetPerRunUSD
	}
	if edits.TimePerRunS != nil {
		if *edits.TimePerRunS <= 0 {
			return a, appwire.Errorf(appwire.CodeBadParams, "a time per run is above zero")
		}
		a.Limits.Time = time.Duration(*edits.TimePerRunS) * time.Second
	}
	saved, err := store.Update(a)
	if err != nil {
		return saved, orderError(err)
	}
	if edits.Trust != nil {
		if saved, err = store.SetTrust(id, automation.Trust(*edits.Trust)); err != nil {
			return saved, orderError(err)
		}
	}
	return saved, nil
}

// orderWhenLine is the typed line that gives an order a new schedule: `every
// 15m`, `every 0 9 * * 1`, `at tomorrow 6pm` or `in 2h`, as the person wrote
// it, with the rhythm quoted so a cron line is one value.
func orderWhenLine(id, when string) (string, error) {
	word, rest, _ := strings.Cut(strings.TrimSpace(when), " ")
	rest = strings.TrimSpace(rest)
	word = strings.ToLower(word)
	if rest == "" {
		return "", appwire.Errorf(appwire.CodeBadParams, `when is "every 15m", "every 0 9 * * 1", "at 18:00" or "in 2h"`)
	}
	switch word {
	case "at":
		return "edit " + id + " at " + rest, nil
	case "every", "in":
		return "edit " + id + " " + word + " " + strconv.Quote(rest), nil
	}
	return "", appwire.Errorf(appwire.CodeBadParams, `when starts with every, at or in`)
}

// SetOrderState pauses, resumes or stops an order. Pausing keeps its trust.
func (b *appBackend) SetOrderState(id, state string) (appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return appwire.Order{}, err
	}
	switch state {
	case appwire.OrderStopped:
		_, err = store.Stop(id)
	case appwire.OrderPaused:
		_, err = store.SetStatus(id, automation.StatusPaused)
	case appwire.OrderActive:
		_, err = store.SetStatus(id, automation.StatusActive)
	default:
		err = fmt.Errorf("an order cannot be put %s", state)
	}
	if err != nil {
		return appwire.Order{}, orderError(err)
	}
	return b.sayOrder(store, id)
}

// RunOrderNow asks for a run outside the schedule. The person asking for it
// is its go: it does not ask again.
func (b *appBackend) RunOrderNow(id string) (appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return appwire.Order{}, err
	}
	a, err := store.Get(id)
	if err != nil {
		return appwire.Order{}, orderError(err)
	}
	if a.Status == automation.StatusProposed || a.Status == automation.StatusStopped {
		return appwire.Order{}, appwire.Errorf(appwire.CodeBadParams, "%s is not agreed to, so it does not run", a.Title)
	}
	if !a.NeedsAsk() {
		if _, err := store.QueueNow(id); err != nil {
			return appwire.Order{}, orderError(err)
		}
		return b.sayOrder(store, id)
	}
	run, err := store.QueueAsking(id)
	if err != nil {
		return appwire.Order{}, orderError(err)
	}
	if _, err := store.Answer(run.ID, automation.ReplyGo); err != nil {
		return appwire.Order{}, orderError(err)
	}
	return b.sayOrder(store, id)
}

// OrderVerdict is the person's word on a run that asked first. A run of zero
// is whichever run of the order is asking.
func (b *appBackend) OrderVerdict(id string, run int64, verdict string) (appwire.Order, error) {
	store, err := b.ordersOf()
	if err != nil {
		return appwire.Order{}, err
	}
	if run == 0 {
		asking, err := store.Asking()
		if err != nil {
			return appwire.Order{}, appEngineError(err)
		}
		for _, r := range asking {
			if r.AutomationID == id {
				run = r.ID
			}
		}
		if run == 0 {
			return appwire.Order{}, appwire.Errorf(appwire.CodeNotFound, "no run of this order is waiting for you")
		}
	}
	if r, err := store.Run(run); err != nil || r.AutomationID != id {
		return appwire.Order{}, appwire.Errorf(appwire.CodeNotFound, "that run is not this order's")
	}
	if _, err := store.Answer(run, automation.Reply(verdict)); err != nil {
		return appwire.Order{}, orderError(err)
	}
	return b.sayOrder(store, id)
}

// answerOrderAsk is asks.answer on a run's ask, "order:<run>".
func (b *appBackend) answerOrderAsk(askID, option string) (bool, error) {
	raw, ok := strings.CutPrefix(askID, appwire.OrderAskPrefix)
	if !ok {
		return false, nil
	}
	run, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return true, appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", askID)
	}
	store, err := b.ordersOf()
	if err != nil {
		return true, err
	}
	r, err := store.Run(run)
	if err != nil {
		return true, appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", askID)
	}
	if err := appValidateOrderAnswer(askID, option); err != nil {
		return true, err
	}
	_, err = b.OrderVerdict(r.AutomationID, run, option)
	if err == nil {
		appOrderAnswered.Store(run, option)
	}
	return true, err
}

func appValidateOrderAnswer(id, option string) error {
	switch option {
	case appwire.VerdictGo, appwire.VerdictSkip, appwire.VerdictNotRight:
		return nil
	}
	return appwire.Errorf(appwire.CodeBadParams, "the ask %s offers go, skip or not_right", id)
}

// appOrderAnswered is the runs this app answered, with the option, so the
// ask's closing says it was answered here.
var appOrderAnswered sync.Map

// appOrderAskID is a run's ask id. A RUN, NOT AN ORDER: an order asks once per
// run, a run's number is never used twice in the store, and an answer is to
// that run and no other.
func appOrderAskID(run int64) string {
	return appwire.OrderAskPrefix + strconv.FormatInt(run, 10)
}

// appOrderRunAsk is a run waiting for the person as an ask, so it is in
// asks.list and the Inbox like every other thing waiting on them.
func appOrderRunAsk(view appwire.Order, run automation.Run) appwire.Ask {
	ask := appwire.Ask{
		ID: appOrderAskID(run.ID), Agent: view.Agent, Kind: appwire.AskQuestion, Subkind: appwire.SubkindOrderRun,
		Title: "Run " + view.Title + " now?", Detail: view.Does,
		Subject: &appwire.AskSubject{Kind: appwire.SubjectAutomation, Ref: view.ID},
		Options: []appwire.AskOption{
			{ID: appwire.VerdictGo, Label: "Go"},
			{ID: appwire.VerdictSkip, Label: "Skip this one"},
			{ID: appwire.VerdictNotRight, Label: "Not right"},
		},
	}
	if view.Trust == appwire.TrustAsksFirst || view.Trust == appwire.TrustEarning {
		ask.Reason = "it asks before each run until it has earned trust"
	}
	return ask
}

// orderAsksOf is the runs waiting on the person for an agent's orders, as
// asks.
func (b *appBackend) orderAsksOf(agent string) []appwire.Ask {
	store, err := b.ordersOf()
	if err != nil {
		return nil
	}
	runs, err := store.Asking()
	if err != nil {
		return nil
	}
	asking := orderAsking(runs)
	now := b.now()
	var out []appwire.Ask
	for _, run := range runs {
		a, err := store.Get(run.AutomationID)
		if err != nil {
			continue
		}
		if orderAgent(a) == agent {
			out = append(out, appOrderRunAsk(orderView(asking, a, now), run))
		}
	}
	return out
}

// sayOrderAsks opens an ask for every run newly waiting on the person and
// closes the ask of every run that stopped waiting, against what was open.
func (b *appBackend) sayOrderAsks(store *automation.Store, open map[int64]appwire.Ask) bool {
	runs, err := store.Asking()
	if err != nil {
		return false
	}
	b.sayOrderAsksFrom(store, open, runs)
	return true
}

func (b *appBackend) sayOrderAsksFrom(store *automation.Store, open map[int64]appwire.Ask, runs []automation.Run) {
	asking := orderAsking(runs)
	now := b.now()
	waiting := map[int64]bool{}
	for _, run := range runs {
		waiting[run.ID] = true
		if _, known := open[run.ID]; known {
			continue
		}
		a, err := store.Get(run.AutomationID)
		if err != nil {
			continue
		}
		ask := appOrderRunAsk(orderView(asking, a, now), run)
		open[run.ID] = ask
		b.emit.Emit(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
	}
	for id, ask := range open {
		if waiting[id] {
			continue
		}
		delete(open, id)
		closed := appwire.AskClosed{Agent: ask.Agent, ID: ask.ID, By: appwire.ClosedByElsewhere, At: now}
		if option, ok := appOrderAnswered.LoadAndDelete(id); ok {
			closed.By, closed.Option = appwire.ClosedByApp, option.(string)
		}
		b.emit.Emit(appwire.EventAskClosed, closed)
	}
}

// sayOrderResult puts a finished run's line in the chat it answers in.
func (b *appBackend) sayOrderResult(view appwire.Order, run automation.Run) {
	if view.Agent == "" || !run.News() {
		return
	}
	if view.AnswersIn != appwire.AnswersChat && view.AnswersIn != appwire.AnswersEverywhere {
		return
	}
	result := orderRunView(run)
	words := view.Title + " · " + result.Result
	if line := strings.TrimSpace(run.Line); line != "" {
		words += ": " + line
	}
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: "order-run-" + strconv.FormatInt(run.ID, 10), Agent: view.Agent, Kind: appwire.EntryNotice,
		At: b.now(), Text: redact.Secrets(words), Ref: appOrderAskID(run.ID),
	}})
}

// watchOrders reads the store until stop closes and says what changed:
// order.changed for an order that is new or different, order.removed for one
// that is gone, order.asking for a run waiting for the person, order.fired
// for a run that started, and order.missed for one that started late.
func (b *appBackend) watchOrders(stop <-chan struct{}) {
	store := b.orderStore()
	if store == nil {
		return
	}
	stamp := appOrdersStamp(store) + "|" + b.now().Format("2006-01-02")
	cursor, err := store.Cursor()
	if err != nil {
		return
	}
	seen := map[string]string{}
	runs, askingErr := store.Asking()
	asking := orderAsking(runs)
	if all, err := store.List(); err == nil {
		for _, a := range all {
			seen[a.ID] = orderPrint(orderView(asking, a, b.now()))
		}
	}
	// THE RUNS ALREADY WAITING ARE SAID AT ONCE, so the Inbox holds them from
	// the moment the app opens.
	open := map[int64]appwire.Ask{}
	if askingErr == nil {
		b.sayOrderAsksFrom(store, open, runs)
	}
	ticker := time.NewTicker(appOrdersEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		cursor, _ = b.ordersRefresh(store, &stamp, cursor, seen, open)
	}
}

// orderPrint is what makes an order different enough to say again.
func orderPrint(view appwire.Order) string {
	raw, _ := json.Marshal(view)
	return string(raw)
}

// ordersOnce is one look at the store.
func (b *appBackend) ordersOnce(store *automation.Store, cursor int64, seen map[string]string) int64 {
	next, _, _ := b.ordersRead(store, cursor, seen)
	return next
}

// ordersRead leaves the input stamp unconsumed after a transient database error.
func (b *appBackend) ordersRead(store *automation.Store, cursor int64, seen map[string]string) (int64, []automation.Run, bool) {
	now := b.now()
	runs, next, err := store.Changes(cursor)
	if err != nil {
		return cursor, nil, false
	}
	all, err := store.List()
	if err != nil {
		return cursor, nil, false
	}
	waiting, err := store.Asking()
	if err != nil {
		return cursor, nil, false
	}
	asking := orderAsking(waiting)
	for _, run := range runs {
		a, err := store.Get(run.AutomationID)
		if err != nil {
			continue
		}
		view := orderView(asking, a, now)
		fired := appwire.OrderFired{Order: view, Run: orderRunView(run)}
		switch run.Phase {
		case automation.PhaseAsking:
			b.emit.Emit(appwire.EventOrderAsking, fired)
		case automation.PhaseRunning:
			b.emit.Emit(appwire.EventOrderFired, fired)
			if run.Why == automation.WhyLate {
				b.emit.Emit(appwire.EventOrderMissed, appwire.OrderChanged{Order: view})
			}
		case automation.PhaseOver:
			b.sayOrderResult(view, run)
		}
	}
	cursor = next
	present := map[string]bool{}
	for _, a := range all {
		present[a.ID] = true
		view := orderView(asking, a, now)
		print := orderPrint(view)
		if seen[a.ID] == print {
			continue
		}
		seen[a.ID] = print
		b.emit.Emit(appwire.EventOrderChanged, appwire.OrderChanged{Order: view})
	}
	for id := range seen {
		if !present[id] {
			delete(seen, id)
			b.emit.Emit(appwire.EventOrderRemoved, appwire.OrderRemoved{ID: id})
		}
	}
	return cursor, waiting, true
}
