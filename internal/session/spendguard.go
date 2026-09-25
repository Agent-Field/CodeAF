package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
// ok is false for a model nobody prices, whose calls the guard does not stop.
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
// model with no Ceilings entry has no ceiling of its own.
type SpendGuard struct {
	Price SpendPrice
	Day   *SpendDay
	// Cap is the day's limit in dollars and CapAction the sentence a call it
	// stops ends on.
	Cap       float64
	CapAction string
	// Ceilings are a model's own spend ceiling on this task, and
	// CeilingAction the sentence (with the ceiling's dollars) a call it stops
	// ends on.
	Ceilings      map[string]float64
	CeilingAction string

	mu    sync.Mutex
	spent map[string]float64
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
func (g *SpendGuard) before(model string, messages []ai.Message, options []ai.Option) (float64, error) {
	if g == nil || g.Price == nil {
		return 0, nil
	}
	prompt, completion, cacheRead, ok := g.Price(model)
	if !ok {
		return 0, nil
	}
	g.mu.Lock()
	seatSpent := g.spent[model]
	g.mu.Unlock()
	est := g.estimate(messages, options, prompt, completion, cacheRead, seatSpent > 0)
	if last := g.Day.lastCost(model); last > est {
		est = last
	}
	if ceiling := g.Ceilings[model]; ceiling > 0 && seatSpent+est > ceiling {
		return 0, ErrSpendStopped{Action: fmt.Sprintf(g.CeilingAction, ceiling)}
	}
	held, fits := g.Day.hold(model, est, g.Cap)
	if !fits {
		return 0, ErrSpendStopped{Action: g.CapAction}
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
func (g *SpendGuard) after(model string, response *ai.Response, held float64) {
	if g == nil {
		return
	}
	if response == nil || response.Usage == nil {
		g.Day.settle(model, held, 0)
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
	if usd <= 0 {
		return
	}
	g.mu.Lock()
	if g.spent == nil {
		g.spent = map[string]float64{}
	}
	g.spent[model] += usd
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
	held, err := c.guard.before(model, messages, options)
	if err != nil {
		return nil, err
	}
	response, err := c.next.CompleteWithMessages(ctx, messages, options...)
	c.guard.after(model, response, held)
	return response, err
}

// guardedChain keeps the model chain of a completer that has one.
type guardedChain struct {
	guardedCompleter
	chain modelChain
}

func (c guardedChain) FallbackModels(model string) []string { return c.chain.FallbackModels(model) }
