package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/crewroute"
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
// ok is false for a model nobody prices; a cap already reached still stops it.
type SpendPrice func(model string) (prompt, completion, cacheRead float64, ok bool)

// SpendDay is today's spend as this process knows it: what the ledger said
// when the process looked, and every guarded call since.
type SpendDay struct {
	mu    sync.Mutex
	usd   float64
	since float64
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

// Total is what the day has spent.
func (d *SpendDay) Total() float64 {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.usd + d.since
}

// hold prices one call to model at est — raised to the model's last actual
// cost — against the cap (none when capUSD is zero) and, when it fits, holds
// it until [SpendDay.settle]. It answers what was held.
func (d *SpendDay) hold(model string, est, capUSD float64) (float64, bool) {
	if d == nil {
		return 0, true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if last := d.last[model]; last > est {
		est = last
	}
	if capUSD > 0 && d.usd+d.since+d.held+est > capUSD {
		return 0, false
	}
	d.held += est
	return est, true
}

// settle releases a call's hold and books what it actually cost.
func (d *SpendDay) settle(model string, held, usd float64) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
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
}

// SpendTask is what one task has spent and holds in flight, across every
// guarded call made for it.
type SpendTask struct {
	mu    sync.Mutex
	spent float64
	held  float64
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
	if !ok {
		// A CALL NOBODY PRICES HAS NO ESTIMATE, BUT A LINE ALREADY REACHED
		// STOPS IT: a day at its cap, a task at its limit and a checker at its
		// ceiling make no more calls of any kind — a model with no catalog
		// price, a free pool and a local model included — on the same sentence
		// a priced call ends on. Below every line it goes as it always has, and
		// what it cost is counted after when the provider says.
		if g.Cap > 0 && g.Day.Total() >= g.Cap {
			return 0, ErrSpendStopped{Action: g.CapAction}
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
	if last := g.Day.lastCost(model); last > est {
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
	held, fits := g.Day.hold(model, est, g.Cap)
	if !fits {
		task.settle(est, 0)
		if ceiling > 0 {
			g.seatTally(seat).settle(est, 0)
		}
		return 0, ErrSpendStopped{Action: g.CapAction}
	}
	if g.Day == nil {
		held = est
	}
	return held, nil
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
	if response == nil || response.Usage == nil {
		g.Day.settle(model, held, 0)
		task.settle(held, 0)
		if seat := crewSeatOf(ctx); g.SeatCeilings[seat] > 0 {
			g.seatTally(seat).settle(held, 0)
		}
		return
	}
	usd := 0.0
	if response.Usage.Cost != nil {
		usd = *response.Usage.Cost
	} else if g.Price != nil {
		if prompt, completion, cacheRead, ok := g.Price(model); ok {
			cached := float64(response.Usage.CacheReadTokens())
			usd = (float64(response.Usage.PromptTokens)-cached)*prompt + cached*cacheRead +
				float64(response.Usage.CompletionTokens)*completion
		}
	}
	g.Day.settle(model, held, usd)
	task.settle(held, usd)
	if seat := crewSeatOf(ctx); g.SeatCeilings[seat] > 0 {
		g.seatTally(seat).settle(held, usd)
	}
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
	response, err := c.next.CompleteWithMessages(ctx, messages, options...)
	c.guard.after(ctx, model, response, held)
	return response, err
}

// guardedChain keeps the model chain of a completer that has one.
type guardedChain struct {
	guardedCompleter
	chain modelChain
}

func (c guardedChain) FallbackModels(model string) []string { return c.chain.FallbackModels(model) }
