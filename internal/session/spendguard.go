package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// A SEAT'S NEXT CALL IS PRICED BEFORE IT IS MADE.
//
// A run's dollar limit is read off what its workers have already spent
// (internal/run's countLiveSpend), so a limit is reached only by the call that
// crosses it — and one long checker round on a dear model is many calls, each
// of which started under the line, so one checker round can overrun a small
// day's cap several times over before the limit is read. The guard answers the
// question BEFORE each call instead: what the day has spent, plus what this
// call is expected to cost, against the day's cap; and, for a seat given one,
// what the seat has spent on this task, plus this call, against its own
// ceiling. A call that would cross either is not made, and the call ends on
// the one sentence that says which line it met.
//
// The estimate is the call's prompt read at a token per four bytes, priced at
// the model's prompt price (its cache-read price once the seat has called it
// before, when the provider publishes one), and its completion ceiling priced
// at the completion price. What the call actually cost — the provider's own
// figure when it gives one, the tokens it reports priced otherwise — is added
// after, so the next estimate starts from the truth.

// SpendPrice is a model's price per token: prompt, completion and cache read.
// ok is false for a model whose price nobody knows; a line already reached
// still stops it. A known price of nothing is never stopped.
type SpendPrice func(model string) (prompt, completion, cacheRead float64, ok bool)

// SpendDay is today's spend as this process knows it: what the ledger said
// when the process looked, and every guarded call since.
type SpendDay struct {
	// Live days also observe spending booked by other conversations. A
	// calendar boundary resets completed spending, never in-flight holds.
	ledger        func() float64
	ledgerChecked func() (float64, error)
	ledgerErr     error
	clock         func() time.Time
	date          string
	observed      float64
	mu            sync.Mutex
	usd           float64
	since         float64
	// held is what calls in flight were estimated at: a call is priced
	// against the day AND every call already on its way, so seats asked at
	// the same moment cannot all pass on one figure.
	held float64
	// last is each model's most recent actual call cost today. A call is
	// estimated at no less: a price sheet that says a call costs a tenth of a
	// cent is not believed after the provider charged four tenths for one.
	last map[string]float64
}

// NewSpendDay is a day that had spent base when it was read.
func NewSpendDay(base float64) *SpendDay { return &SpendDay{usd: base} }

// newLedgerSpendDay keeps the shared ledger visible throughout a conversation.
func newLedgerSpendDay(read func() float64, now func() time.Time) *SpendDay {
	return &SpendDay{ledger: read, clock: now}
}

func newCheckedLedgerSpendDay(read func() (float64, error), now func() time.Time) *SpendDay {
	return &SpendDay{ledgerChecked: read, clock: now}
}

// refreshLocked preserves locally settled costs while the asynchronous ledger
// catches up, without adding the same response twice. Cross-process calls in
// flight are not reservations in this process; their receipts are seen here.
func (d *SpendDay) refreshLocked() {
	if d.ledger == nil && d.ledgerChecked == nil {
		return
	}
	date := d.clock().Format("2006-01-02")
	var spent float64
	if d.ledgerChecked != nil {
		var err error
		spent, err = d.ledgerChecked()
		d.ledgerErr = err
		if err != nil {
			return
		} // Keep all known money and holds until the read succeeds.
	} else {
		spent = d.ledger()
	}
	if date != d.date {
		d.date, d.usd, d.since = date, spent, 0
		d.last = nil
	}
	d.observed = spent
}

func (d *SpendDay) totalLocked() float64 {
	return max(d.usd+d.since, d.observed)
}

// Total is what the day has spent.
func (d *SpendDay) Total() float64 {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	return d.totalLocked()
}

func (d *SpendDay) totalChecked() (float64, error) {
	if d == nil {
		return 0, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	return d.totalLocked(), d.ledgerErr
}

// hold prices one call to model at est — raised to the model's last actual
// cost — against the cap (none when capUSD is zero) and, when it fits, holds
// it until [SpendDay.settle]. It answers what was held.
func (d *SpendDay) hold(model string, est, capUSD float64) (float64, bool) {
	held, fits, _ := d.holdChecked(model, est, capUSD)
	return held, fits
}

func (d *SpendDay) holdChecked(model string, est, capUSD float64) (float64, bool, error) {
	if d == nil {
		return 0, true, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	if capUSD > 0 && d.ledgerErr != nil {
		return 0, false, d.ledgerErr
	}
	if last := d.last[model]; last > est {
		est = last
	}
	if capUSD > 0 && d.totalLocked()+d.held+est > capUSD {
		return 0, false, nil
	}
	d.held += est
	return est, true, nil
}

// settle releases a call's hold and books what it actually cost.
func (d *SpendDay) settle(model string, held, usd float64) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	if d.ledgerErr != nil && d.clock != nil {
		date := d.clock().Format("2006-01-02")
		if date != d.date {
			// Old-day totals may expire, but a call settling now must survive
			// the first successful read of this new day. In-flight holds remain.
			d.date, d.usd, d.since, d.observed = date, 0, 0, 0
			d.last = nil
		}
	}
	d.held -= held
	if d.held < 0 {
		d.held = 0
	}
	if usd <= 0 {
		return
	}
	d.since += usd
	if d.last == nil {
		d.last = map[string]float64{}
	}
	d.last[model] = usd
}

// lastCost is model's most recent actual call cost today.
func (d *SpendDay) lastCost(model string) float64 {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.last[model]
}

// SpendGuard holds a task's seat calls to the day's cap and to each seat's
// own ceiling. The zero parts are off: no Day or no Cap is no day cap, and a
// seat with no SeatCeilings entry has no ceiling of its own.
type SpendGuard struct {
	Price SpendPrice
	Day   *SpendDay
	// Cap is the day's limit in dollars and CapAction the sentence a call it
	// stops ends on.
	Cap       float64
	CapAction string
	// DayLimit, when set, is the person's daily limit as it stands NOW, read
	// before every call that could cost money: a limit raised, lowered or
	// removed after this guard was built is the one that holds. The lower of
	// it and Cap stops a call. An error is a limit nobody could read, and
	// refuses every call that might be paid — never a default, never none.
	// It must not take locks of its own that a guarded call can hold.
	DayLimit func() (usd float64, action string, err error)
	// SeatCeilings are a seat's own spend ceiling on this task, and
	// CeilingAction the sentence (with the ceiling's dollars) a call it stops
	// ends on.
	SeatCeilings  map[crewroute.Seat]float64
	CeilingAction string
	// TaskCap is the most the task may spend in dollars, across every model
	// this guard prices, and TaskAction the sentence a call it stops ends on.
	// Zero is no per-task limit.
	TaskCap    float64
	TaskAction string
	// Task is the tally TaskCap is read against. Guards that share one hold
	// the seats and the helpers of one task to one limit; nil is a tally of
	// the guard's own, made on its first call.
	Task *SpendTask

	mu         sync.Mutex
	modelSpent map[string]float64
	seatSpent  map[crewroute.Seat]*SpendTask
	// bookMu is held across one call's task and role booking, and by a
	// checkpoint reading them, so the two always agree.
	bookMu sync.Mutex
}

// SpendTask is what one task has spent and holds in flight, across every
// guarded call made for it.
type SpendTask struct {
	mu    sync.Mutex
	spent float64
	held  float64
	// calls is every answered call booked here, and unknown those whose cost
	// nobody could say — no price for the route and no cost from the provider.
	// A known nothing (a free pool, a plan, a provider's own cost of 0) is a
	// call with a price; an unknown one is NOT counted as $0 spent.
	calls, unknown int
}

// Counts is how many answered calls were booked and how many of them had no
// known cost.
func (t *SpendTask) Counts() (calls, unknown int) {
	if t == nil {
		return 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls, t.unknown
}

// book releases a hold and books one admitted call in one step: its cost
// and whether that cost is known, so a reader never sees the dollars of a
// call without its certainty or the other way round.
func (t *SpendTask) book(held, usd float64, known bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.held -= held
	if t.held < 0 {
		t.held = 0
	}
	if usd > 0 {
		t.spent += usd
	}
	t.calls++
	if !known {
		t.unknown++
	}
}

// snapshot is what was spent and how many calls, and how many of unknown
// cost, read together.
func (t *SpendTask) snapshot() (spent float64, calls, unknown int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.spent, t.calls, t.unknown
}

// Total is what the task has spent.
func (t *SpendTask) Total() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.spent
}

// hold holds est against capUSD (none when zero) and answers whether it fit.
func (t *SpendTask) hold(est, capUSD float64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if capUSD > 0 && t.spent+t.held+est > capUSD {
		return false
	}
	t.held += est
	return true
}

// settle releases a hold and books what the call cost.
func (t *SpendTask) settle(held, usd float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.held -= held
	if t.held < 0 {
		t.held = 0
	}
	if usd > 0 {
		t.spent += usd
	}
}

// tally is the guard's task tally, made on first use.
func (g *SpendGuard) tally() *SpendTask {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Task == nil {
		g.Task = &SpendTask{}
	}
	return g.Task
}

// seatTally holds one seat's spend and in-flight estimates across model changes.
func (g *SpendGuard) seatTally(seat crewroute.Seat) *SpendTask {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seatSpent == nil {
		g.seatSpent = make(map[crewroute.Seat]*SpendTask)
	}
	if g.seatSpent[seat] == nil {
		g.seatSpent[seat] = &SpendTask{}
	}
	return g.seatSpent[seat]
}

// ErrSpendStopped is a call the guard did not make. Its text is the one
// sentence that says which line it met.
type ErrSpendStopped struct{ Action string }

func (e ErrSpendStopped) Error() string { return e.Action }

// spendCompletionGuess is the completion a call that names no ceiling is
// priced at.
const spendCompletionGuess = 2000

// Wrap is next with the guard in front of it, for calls to model.
func (g *SpendGuard) Wrap(model string, next Completer) Completer {
	if g == nil {
		return next
	}
	guarded := guardedCompleter{guard: g, model: model, next: next}
	if chain, ok := next.(modelChain); ok {
		return guardedChain{guardedCompleter: guarded, chain: chain}
	}
	return guarded
}

// spendLimitUnread is the sentence a potentially paid call ends on when the
// daily limit could not be read.
const spendAccountingUnread = "spending records could not be read, so nothing that could cost money was sent · check the spending ledger"

const spendLimitUnread = "the daily spending limit could not be read, so nothing that could cost money was sent · check /budget"

// dayCap is the cap one call is held to: Cap and the daily limit read now,
// whichever is lower and positive. The Day tally exists whenever either can
// bind, so a limit switched on later has a day to be measured on.
func (g *SpendGuard) dayCap() (float64, string, error) {
	capUSD, action := g.Cap, g.CapAction
	if g.DayLimit == nil {
		return capUSD, action, nil
	}
	limit, said, err := g.DayLimit()
	if err != nil {
		return 0, "", ErrSpendStopped{Action: spendLimitUnread}
	}
	if limit > 0 && (capUSD <= 0 || limit < capUSD) {
		capUSD, action = limit, said
	}
	if capUSD > 0 {
		g.mu.Lock()
		if g.Day == nil {
			g.Day = newCheckedLedgerSpendDay(spentTodayOnLedgerChecked, time.Now)
		}
		g.mu.Unlock()
	}
	return capUSD, action, nil
}

// before is whether a call to model with these messages may be made, and
// what it holds on the day until [SpendGuard.after] settles it.
func (g *SpendGuard) before(ctx context.Context, model string, messages []ai.Message, options []ai.Option) (float64, error) {
	if g == nil {
		return 0, nil
	}
	var prompt, completion, cacheRead float64
	ok := false
	if g.Price != nil {
		prompt, completion, cacheRead, ok = g.Price(model)
	}
	if ok && prompt <= 0 && completion <= 0 {
		// A CALL THAT COSTS NOTHING IS NEVER STOPPED BY A DOLLAR LINE, so it
		// does not even ask for the daily limit: a limit nobody can read
		// stops what might be paid, not what is known to be free.
		return 0, nil
	}
	capUSD, capAction, err := g.dayCap()
	if err != nil {
		return 0, err
	}
	if !ok {
		// A CALL WHOSE PRICE NOBODY KNOWS HAS NO ESTIMATE, BUT A LINE ALREADY
		// REACHED STOPS IT: a day at its cap, a task at its limit and a checker
		// at its ceiling make no more such calls, on the same sentence a priced
		// call ends on. Below every line it goes as it always has, and what it
		// cost is counted after when the provider says.
		if capUSD > 0 {
			spent, err := g.dayTally().totalChecked()
			if err != nil {
				return 0, ErrSpendStopped{Action: spendAccountingUnread}
			}
			if spent >= capUSD {
				return 0, ErrSpendStopped{Action: capAction}
			}
		}
		if g.TaskCap > 0 && g.tally().Total() >= g.TaskCap {
			return 0, ErrSpendStopped{Action: g.TaskAction}
		}
		if seat := crewSeatOf(ctx); g.SeatCeilings[seat] > 0 && g.seatTally(seat).Total() >= g.SeatCeilings[seat] {
			ceiling := g.SeatCeilings[seat]
			return 0, ErrSpendStopped{Action: fmt.Sprintf(g.CeilingAction, ceiling)}
		}
		return 0, nil
	}
	g.mu.Lock()
	modelSpent := g.modelSpent[model]
	g.mu.Unlock()
	est := g.estimate(messages, options, prompt, completion, cacheRead, modelSpent > 0)
	day := g.dayTally()
	if last := day.lastCost(model); last > est {
		est = last
	}
	seat := crewSeatOf(ctx)
	ceiling := g.SeatCeilings[seat]
	if ceiling > 0 && !g.seatTally(seat).hold(est, ceiling) {
		return 0, ErrSpendStopped{Action: fmt.Sprintf(g.CeilingAction, ceiling)}
	}
	task := g.tally()
	if !task.hold(est, g.TaskCap) {
		if ceiling > 0 {
			g.seatTally(seat).settle(est, 0)
		}
		return 0, ErrSpendStopped{Action: g.TaskAction}
	}
	held, fits, readErr := day.holdChecked(model, est, capUSD)
	if !fits {
		task.settle(est, 0)
		if ceiling > 0 {
			g.seatTally(seat).settle(est, 0)
		}
		if readErr != nil {
			return 0, ErrSpendStopped{Action: spendAccountingUnread}
		}
		return 0, ErrSpendStopped{Action: capAction}
	}
	if day == nil {
		held = est
	}
	return held, nil
}

// dayTally is the guard's day, read under the lock a late-made one is set by.
func (g *SpendGuard) dayTally() *SpendDay {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.Day
}

// estimate is one call's expected cost.
func (g *SpendGuard) estimate(messages []ai.Message, options []ai.Option, prompt, completion, cacheRead float64, warm bool) float64 {
	raw, _ := json.Marshal(messages)
	tokens := float64(len(raw)) / 4
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	out := float64(spendCompletionGuess)
	if request.MaxTokens != nil && *request.MaxTokens > 0 {
		out = float64(*request.MaxTokens)
	}
	in := prompt
	if warm && cacheRead > 0 {
		in = cacheRead
	}
	return tokens*in + out*completion
}

// after releases what before held and records what a call to model cost.
func (g *SpendGuard) after(ctx context.Context, model string, response *ai.Response, held float64) {
	if g == nil {
		return
	}
	task := g.tally()
	// A SEAT'S SPEND IS BOOKED WHETHER OR NOT IT HAS A CEILING: the ceiling
	// decides what a call may hold beforehand, never whether what it cost is
	// counted against the role that made it.
	seat := crewSeatOf(ctx)
	seatHeld := 0.0
	if g.SeatCeilings[seat] > 0 {
		seatHeld = held
	}
	// WHAT A CALL COST IS KNOWN ONLY WHEN SOMEBODY SAID IT: the provider's own
	// cost (0 included), or this route's published price with the usage it
	// reported. A route priced at nothing is known free with or without usage.
	// An answer with no usage on a priced or unpriced route, and a call that
	// failed, cost something nobody said; it is counted as unknown, never $0.
	usd, known := 0.0, false
	prompt, completion, cacheRead, priced := 0.0, 0.0, 0.0, false
	if g.Price != nil {
		prompt, completion, cacheRead, priced = g.Price(model)
	}
	free := priced && prompt <= 0 && completion <= 0 && cacheRead <= 0
	switch {
	case response != nil && response.Usage != nil && response.Usage.Cost != nil:
		usd, known = *response.Usage.Cost, true
	case response != nil && response.Usage != nil && priced:
		cached := float64(response.Usage.CacheReadTokens())
		usd = (float64(response.Usage.PromptTokens)-cached)*prompt + cached*cacheRead +
			float64(response.Usage.CompletionTokens)*completion
		known = true
	case free:
		known = true
	}
	g.dayTally().settle(model, held, usd)
	// The seat's tally is fetched before bookMu: a checkpoint takes g.mu and
	// then bookMu, so this side never takes them the other way round.
	var role *SpendTask
	if seat != "" {
		role = g.seatTally(seat)
	}
	g.bookMu.Lock()
	task.book(held, usd, known)
	if role != nil {
		role.book(seatHeld, usd, known)
	}
	g.bookMu.Unlock()
	if usd <= 0 {
		return
	}
	g.mu.Lock()
	if g.modelSpent == nil {
		g.modelSpent = map[string]float64{}
	}
	g.modelSpent[model] += usd
	g.mu.Unlock()
}

// guardedCompleter is one model's completer behind the guard.
type guardedCompleter struct {
	guard *SpendGuard
	model string
	next  Completer
}

func (c guardedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	model := c.model
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if request.Model != "" {
		model = request.Model
	}
	held, err := c.guard.before(ctx, model, messages, options)
	if err != nil {
		return nil, err
	}
	callCtx := provider.WithDiscardedUsage(ctx, func(_ string, _ string, response *ai.Response) {
		c.guard.after(ctx, model, response, 0)
	})
	response, err := c.next.CompleteWithMessages(callCtx, messages, options...)
	c.guard.after(ctx, model, response, held)
	return response, err
}

// guardedChain keeps the model chain of a completer that has one.
type guardedChain struct {
	guardedCompleter
	chain modelChain
}

func (c guardedChain) FallbackModels(model string) []string { return c.chain.FallbackModels(model) }
