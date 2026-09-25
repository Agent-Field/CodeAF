package orchestrate

// ONE TANK, AND A GATE AT THE BOTTOM OF IT.
//
// An adaptive run decides its own size: the planner adds nodes because the
// work turned out to need them, and there is no point in the graph where a
// person was asked "and this much more?". A run bounded by a node count would
// be bounded by the wrong thing — twelve cheap nodes and three opus nodes are
// not the same money — so the bound is DOLLARS, one figure the person approved
// before anything started, and every model call in the run bills against it:
// the nodes, the planner's own thinking, and the synthesis at the end.
//
// The gate is not a kill. At the cap the scheduler stops LAUNCHING, in-flight
// nodes finish (they are already paid for, and killing them buys nothing back),
// and the run parks in a state that can be resumed — top it up, finish with
// what is there, or stop. A budget that silently ended a run at 100% would be
// the same design with the person's decision removed.

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// The three answers to the gate. They are the whole vocabulary, and a surface
// spells them exactly: "topup:2.50", "finish", "stop".
const (
	GateTopup  = "topup"
	GateFinish = "finish"
	GateStop   = "stop"
)

// WarnMark is where the run says the tank is getting low, once. It is early
// enough that a person can top up before anything stops, and late enough that
// it is not a line every run prints out of caution.
const WarnMark = 0.80

// Price is one model's list price in dollars per MILLION tokens, split the way
// every provider splits it. It is per million rather than per token because
// that is the number a person can check against the price page.
type Price struct {
	In  float64
	Out float64
}

// PriceSource is a reader this package does not own, asked before the table
// falls back. It answers with the same pair PriceOf itself returns — one
// model's row in dollars per MILLION tokens — and false means "ask somebody
// else", never "free".
type PriceSource func(model string) (Price, bool)

// priceTariff is the installed source, held behind an atomic because the
// package's meters read it on every call and the installer may swap it while a
// run is in flight. A nil tariff is the unset state: the table alone.
var priceTariff atomic.Value // PriceSource

// UsePrices installs (or, with nil, removes) the source asked before the
// table. Replacing one source with another is the whole point — a tariff can
// move without this package being rebuilt — and a swap is safe to make while
// other goroutines are metering: a call in flight sees either the old source
// or the new one, never half of either.
func UsePrices(src PriceSource) {
	priceTariff.Store(src) // a nil PriceSource is a stored value, not a nil store
}

// CatalogPrices adapts a reader that answers per TOKEN — the shape a model
// catalog publishes — into the per-MILLION PriceSource this package meters
// with, so the unit conversion exists in exactly one place. A reader that
// knows a model carries the answer straight through, a per-token zero
// included; a reader that publishes nothing for it answers false, and the
// table gets its turn. A nil reader adapts to a nil source: nobody published
// anything.
func CatalogPrices(priceNow func(model string) (prompt, completion float64, known bool)) PriceSource {
	if priceNow == nil {
		return nil
	}
	return func(model string) (Price, bool) {
		in, out, known := priceNow(model)
		if !known {
			return Price{}, false
		}
		return Price{In: in * 1e6, Out: out * 1e6}, true
	}
}

// prices is the table this build meters against — the models an adaptive run
// actually rides, at their OpenRouter list price.
//
// IT IS A FALLBACK, NOT THE TRUTH. What bills the tank first is the provider's
// own reported cost, which arrives on the usage of the very call that spent it
// and is right about discounts, caching and per-endpoint pricing this table
// cannot know. [Meter] is what answers when a response carries no figure at
// all, which is common enough that a run metering only reported costs would
// have a tank that never empties.
//
// The rows are the ids the shipped defaults and the crew router's evidence
// table name (internal/crewroute's prior.json), each copied
// from the catalog's published price on the day its row was written; the
// installed tariff ([CatalogPrices]) is what answers when a catalog is present,
// and this table is only the fallback behind it.
var prices = map[string]Price{
	"google/gemini-2.5-flash":         {In: 0.30, Out: 2.50},
	"mistralai/mistral-nemo":          {In: 0.02, Out: 0.03},
	"deepseek/deepseek-v4-flash-0731": {In: 0.06, Out: 0.12},
	"z-ai/glm-5.3-flash":              {In: 0.09, Out: 0.30},
	"z-ai/glm-5.3":                    {In: 1.40, Out: 4.40},
	"moonshotai/kimi-k3":              {In: 3.00, Out: 15.00},
	"qwen/qwen3.8-max-0902":           {In: 2.00, Out: 6.00},
	"anthropic/claude-fable-5.1":      {In: 10.00, Out: 50.00},
	"anthropic/claude-opus-5":         {In: 5.00, Out: 25.00},
}

// unpriced is what a model nobody has a row for costs.
//
// IT IS NOT ZERO, and that is the whole decision. A model this table has never
// heard of is the case where the tank matters most — nobody has checked what
// it costs — and metering it at nothing would leave a run with a cap it can
// never reach. It is the middle of the table rather than its most expensive
// row, because a governor that pauses a cheap run every four nodes is a
// governor people turn off.
var unpriced = Price{In: 1.25, Out: 10.00}

// PriceOf is one model's row, and whether anybody actually holds one. The id
// is matched the way the wire spells it, with suffixes an endpoint adds
// (":free", "@2026-01") cut before the lookup — and the cut, lowercased name
// is what the installed source is asked too, so a source never has to repeat
// this package's normalisation to agree with its own table.
//
// A BARE NAME IS NOT A VENDOR'S ROW. A vendor serving a model on its own base
// does not charge the router's price for it, and a wrong price is worse than no
// price because it looks right.
func PriceOf(model string) (Price, bool) {
	name := strings.ToLower(strings.TrimSpace(model))
	if cut := strings.IndexAny(name, ":@"); cut > 0 {
		name = name[:cut]
	}
	if name == "" {
		return unpriced, false
	}
	if tariff, _ := priceTariff.Load().(PriceSource); tariff != nil {
		if price, known := tariff(name); known {
			return price, true
		}
	}
	if price, known := prices[name]; known {
		return price, true
	}
	return unpriced, false
}

// Meter is what one call's tokens cost when nobody said.
//
// IT CHARGES EVERY TOKEN AT THE OUTPUT RATE, deliberately. The caller that
// reaches this has one number and not two, and the two rates differ by up to
// five times; a tank that empties early stops and asks a person, and a tank
// that empties late has already spent their money. Callers that know both
// halves — which is every caller reading a provider's usage — use [MeterCall]
// and get the honest figure.
func Meter(tokens int, model string) float64 {
	return MeterCall(0, tokens, model)
}

// MeterCall is one call's cost from its two token counts.
func MeterCall(in, out int, model string) float64 {
	price, _ := PriceOf(model)
	if in < 0 {
		in = 0
	}
	if out < 0 {
		out = 0
	}
	return (float64(in)*price.In + float64(out)*price.Out) / 1e6
}

// Charge bills the tank and fires the two things that can happen when it moves.
//
// It is EXPORTED because the run's model calls do not all happen inside this
// package: the planner is the session's, and the law is that every model call
// anywhere in the run meters against one tank. A planner that did not call
// this would be spending money the gauge never sees.
func (o *Orchestrator) Charge(dollars float64) {
	if dollars <= 0 {
		return
	}
	o.mu.Lock()
	o.fuel.Spent += dollars
	fuel := o.fuel
	warn := !o.warned && fuel.Cap > 0 && fuel.Spent >= fuel.Cap*WarnMark
	if warn {
		o.warned = true
	}
	// A RUN ON ITS WAY OUT DOES NOT STOP TO ASK. The gate exists to put a
	// decision in front of somebody before more work is launched, and past the
	// DonePlan — or past a "finish" — there is no more work: pausing there
	// would be a question whose only honest answer is the thing already
	// happening.
	pause := !o.paused && !o.done && !o.finishing && !o.stopped && fuel.Cap > 0 && fuel.Spent >= fuel.Cap
	if pause {
		o.paused = true
	}
	o.mu.Unlock()
	o.publish()

	if warn && o.onFuel != nil {
		o.onFuel(fuel)
	}
	if pause && o.onPause != nil {
		o.onPause(fuel)
	}
}

// openGate is the answer arriving, inside the loop. Every branch leaves the
// run un-paused: the gate is a question that is asked once and answered once,
// and a run still flagged paused after an answer would ask it again.
//
// STOP IS NOT ONE OF THE BRANCHES. It never reaches this channel —
// [Orchestrator.Resolve] routes it to [Orchestrator.Cancel] — because ending a
// run is one act with one code path whether the tank ran it into the gate or a
// person pressed the key on its page.
func (o *Orchestrator) openGate(answer string) {
	var line string
	o.mu.Lock()
	switch {
	case answer == GateFinish:
		o.finishing = true
		line = "finishing on what is already done"
	default:
		amount, _ := topupAmount(answer)
		o.fuel.Cap += amount
		o.warned = o.fuel.Spent >= o.fuel.Cap*WarnMark
		line = fmt.Sprintf("topped up to %s; the run carries on", Dollars(o.fuel.Cap))
	}
	o.paused = false
	o.mu.Unlock()
	o.publish()
	o.note(line)
}

// topupAmount reads "topup:2.50". The dollars are what is ADDED to the cap,
// not a new cap: a person answering a gate is deciding how much more they will
// spend, and they should not have to add up what has already gone.
func topupAmount(answer string) (float64, error) {
	_, figure, found := strings.Cut(answer, ":")
	if !found {
		return 0, fmt.Errorf("orchestrate: a top-up says how much: topup:<dollars>")
	}
	figure = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(figure), "$"))
	amount, err := strconv.ParseFloat(figure, 64)
	if err != nil || amount <= 0 {
		return 0, fmt.Errorf("orchestrate: %q is not an amount of dollars", figure)
	}
	return amount, nil
}

// Dollars is the one way this run writes money, so that the note, the gauge
// and the gate all say it the same way.
func Dollars(amount float64) string { return fmt.Sprintf("$%.2f", amount) }

// Gauge is the spend summary a surface prints: "$1.60 of $2.00", and just the
// spend when the run was never capped.
func (f Fuel) Gauge() string {
	if f.Cap <= 0 {
		return Dollars(f.Spent)
	}
	return Dollars(f.Spent) + " of " + Dollars(f.Cap)
}

// Low reports whether the tank has crossed the warning mark.
func (f Fuel) Low() bool { return f.Cap > 0 && f.Spent >= f.Cap*WarnMark }
