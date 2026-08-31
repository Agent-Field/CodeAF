package lane

import (
	"math"
	"time"
)

// ── λ: WHAT A SECOND IS WORTH ───────────────────────────────────────────────
//
// One number, in SECONDS PER DOLLAR, and it is not a setting. Three questions
// this build can already answer decide it:
//
//   - Is a person watching this stream right now? Then their attention has a
//     wall value, and at a nominal rate a dollar buys something like ninety
//     seconds of it — in plain terms, spend up to a cent to save a second.
//   - Is this call on the critical path of a task? Then λ is the wall value of
//     the whole task, because the person waits for its end. Off the path with
//     slack to spare, λ falls to zero and price wins outright.
//   - Is there a deadline? Then λ rises as it nears, because a missed deadline
//     is a step cost rather than a slow one.
//
// WHY NOT A SLIDER. A "speed versus cost" control asks the person to state λ
// once, for every future request, when the graph already knows it per request
// and the answer differs by two orders of magnitude between a chat turn and a
// background node. The routing row keeps its three words and gains this
// meaning: `price` sets λ to zero for everything, `latency` computes it, `off`
// stays total.
//
// The other half of this file is the PRICE the λ is spent against, and it is
// cache-aware for the reason Part II §3 of the design gives: the cheapest lane
// on the sheet is not the cheapest lane for a request whose prefix another lane
// already holds, and a router that ignored that would flap between two equal
// lanes forever, paying a cold prefix each time.
//
// Nothing here reads a clock: every moment arrives as an argument.

const (
	// AttentionValue is λ for a person sitting in front of an empty line, in
	// seconds per dollar.
	//
	// NINETY IS AN HOURLY RATE DIVIDED THROUGH. At a nominal $40 an hour a
	// second of somebody's attention is worth about a hundredth of a cent, so a
	// dollar buys ninety seconds of waiting back. It is stated once, here,
	// because every other value of time in this package is expressed as a
	// multiple of it.
	AttentionValue = 90.0

	// TaskWallValue is λ for a call on the critical path of a task.
	//
	// IT IS THE WALL VALUE OF THE WHOLE TASK, because the person waits for the
	// task's end and not for this node's. For v1 it is the attention value: the
	// build does not yet carry a per-task worth, and inventing one would be a
	// number nobody measured deciding what to pay. When the plan learns what a
	// task is worth to whoever asked for it, this is the constant that goes.
	TaskWallValue = AttentionValue

	// DeadlineUrgencyCap bounds what a deadline may do to λ. Four times the
	// task's wall value is a deadline already missed or about to be; above it
	// the arithmetic stops describing a trade and starts describing a panic,
	// and a router that pays any price is a router with no opinion at all.
	DeadlineUrgencyCap = 4.0
)

// Lambda is λ for one request, in seconds per dollar.
//
// The four cases are the design's table, in the order they are asked:
//
//	interactive                     → AttentionValue, somebody is watching
//	on the critical path            → TaskWallValue, the person waits for the end
//	off the path, slack ≥ expected  → zero, and price wins outright
//	off the path, slack < expected  → the wall value, faded in as the slack runs out
//
// A DEADLINE CAN LIFT ANY OF THEM. It is asked last and it takes the larger
// answer, because a missed deadline is a step cost rather than a slow one:
// urgency is how many times over the remaining time the expected work is, and
// once the work no longer fits, λ is at least the task's wall value.
//
// slack, expected and deadline are all durations and all optional: zero means
// "not known", which is why an off-path call that knows nothing about its own
// graph answers zero rather than guessing that it is urgent.
func Lambda(interactive bool, onCriticalPath bool, slack, expected, deadline time.Duration) float64 {
	if interactive {
		return AttentionValue
	}
	value := 0.0
	switch {
	case onCriticalPath:
		value = TaskWallValue
	case expected > 0 && slack < expected:
		// Off the path but nearly on it: the slack is what stands between this
		// node and the critical path, so the value fades in as the slack runs
		// out rather than switching on at the moment it hits zero.
		short := 1 - float64(slack)/float64(expected)
		if short < 0 {
			short = 0
		}
		value = TaskWallValue * short
	}
	if deadline > 0 && expected > 0 {
		urgency := float64(expected) / float64(deadline)
		if urgency > DeadlineUrgencyCap {
			urgency = DeadlineUrgencyCap
		}
		if pressed := TaskWallValue * urgency; pressed > value {
			value = pressed
		}
	}
	return value
}

// valueOfTime is λ for one request, in seconds per dollar.
//
// The request carries it because only the call site knows who is waiting on it
// — the same law [WithRoutingIntent] is written under in the transport — and
// this is the one reader of the field, so that "λ is zero" means one thing
// everywhere: price only, which is the honest reading of "nobody is waiting"
// and never a surprise on a bill.
func valueOfTime(req Request) float64 {
	if req.ValueOfTime < 0 {
		return 0
	}
	return req.ValueOfTime
}

// ── WHAT ONE REQUEST COSTS ON ONE LANE ──────────────────────────────────────

// PriceOf is what this request is expected to cost on a lane, in dollars, with
// nothing of its prompt already cached there.
//
// It is the pessimistic reading and the right default: a lane this session has
// never sent to holds none of its prefix, and assuming otherwise would be the
// router awarding a discount nobody granted.
func PriceOf(facts Facts, req Request) float64 { return PriceWithCache(facts, req, 0) }

// PriceWithCache is the same figure when cached of the prompt's tokens are
// believed to be sitting in this lane's prompt cache:
//
//	$ = in·(prompt − cached) + cache·cached + out·N̂
//
// N̂ IS THE VISIBLE AND HIDDEN SPLIT ADDED BACK UP, because both halves are
// billed at the same rate and only the WAIT they cause differs
// ([PerceivedSeconds]). A request that states neither is priced on its prompt
// alone, which is the honest answer for a call whose answer length nobody has
// estimated.
//
// A lane that publishes no cache tariff bills its cached tokens at the fresh
// rate here. That is not a guess: it is what an endpoint with no cache discount
// charges, and it is what makes the incumbent lane's advantage disappear
// exactly when the incumbent has no cache to hold.
func PriceWithCache(facts Facts, req Request, cached int) float64 {
	prompt := req.PromptTokens
	if prompt < 0 {
		prompt = 0
	}
	if cached < 0 {
		cached = 0
	}
	if cached > prompt {
		cached = prompt
	}
	answer := req.Visible + req.Hidden
	if answer < 0 {
		answer = 0
	}
	price := facts.PriceIn*float64(prompt-cached) + facts.PriceOut*float64(answer)
	if facts.PriceCache > 0 {
		price += facts.PriceCache * float64(cached)
		return price
	}
	return price + facts.PriceIn*float64(cached)
}

// PrefixHold is how long a lane is believed to still hold a prefix it served.
//
// FIVE MINUTES IS THE SHORTEST CACHE WINDOW THE ROUTERS PUBLISH, and being
// wrong in this direction is cheap: believing a warm cache has gone cold costs
// the router one comparison, while believing a cold cache is warm sends a
// request to a lane on a discount it will not get. When a lane's own cache TTL
// is learned from `cached_tokens` this becomes a per-lane number; until then it
// is one constant and it is stated here.
const PrefixHold = 5 * time.Minute

// ── PERCEIVED TIME, SCORED ──────────────────────────────────────────────────

// scoreOf is what the chooser minimises for one lane, in seconds, lower better.
//
//	λ > 0 → $·λ + perceived seconds
//	λ = 0 → the price alone, in dollars
//
// THE MONEY IS MULTIPLIED BY λ AND NEVER DIVIDED BY IT, and getting this
// backwards is the one arithmetic error in this package that leaves every test
// green. λ is SECONDS PER DOLLAR, so dollars times λ is seconds and dollars
// divided by λ is dollars squared per second — a quantity of nothing, about
// eight thousand times too small at the attention value, which makes the price
// term round to zero beside any wait at all. The symptom is a router that says
// it prices money and does not: with [PerceivedSeconds] corrected so that two
// lanes above reading speed feel identical, price is the ONLY thing left to
// separate them on a talk turn, and a price term that cannot be felt turns that
// decision into sampling noise. The direction is fixed by the design's own
// sentence — a dollar buys ninety seconds, so spend up to a cent to save a
// second — and by [underPriceCeiling], which goes the other way (seconds into
// dollars) and divides. See ideation/provider-routing.md, Part III.
//
// THE TWO CASES ARE DIFFERENT UNITS ON PURPOSE. With nobody waiting, a second
// is worth nothing; the honest score is the money, and perceived time is then
// only a tiebreak ([Chooser.Choose] applies it). With somebody waiting, one
// dollar is worth λ seconds by construction, so the two terms are commensurable
// and the sum is a time.
func scoreOf(price, perceived, lambda float64) float64 {
	if lambda <= 0 {
		return price
	}
	if math.IsInf(perceived, 1) {
		return math.Inf(1)
	}
	return price*lambda + perceived
}
