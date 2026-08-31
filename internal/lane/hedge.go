package lane

import (
	"sync"
	"time"
)

// ── THE BUDGET: WHAT KEEPS A RESCUE FROM BECOMING A SECOND BILL ─────────────
//
// A hedge is the one mechanism in this package that can cost real money by
// working exactly as designed. Every other part of it is arithmetic over
// numbers somebody else already paid for; this one sends a second request. So
// the budget is not a safety net bolted on afterwards — it is the reason the
// mechanism is allowed to exist at all, and the empty budget refuses
// everything, which is the correct behaviour of an un-wired router.
//
// TWO LIMITS, BECAUSE THERE ARE TWO FAILURE MODES AND ONE LIMIT CANNOT SEE
// BOTH.
//
//   - A SHARE OF REQUESTS bounds a storm: at most two hedges in any twenty
//     requests, which is one in ten once a session is going and two at the very
//     start, when there is no history to judge from.
//
//     IT COUNTS REQUESTS AND NOT MINUTES, and that correction came from the
//     simulator. A per-minute bucket sounds equivalent and is not: a slow batch
//     of `work` requests refills its own allowance while it runs, so the same
//     policy hedged one per cent of requests on one seed and ninety-five per
//     cent on another. The thing being rationed is REQUESTS, so the window has
//     to be measured in them.
//   - A SHARE OF RECENT SPEND bounds the slow leak. A hedge that fires a little
//     too eagerly is invisible on any one request and obvious at the end of the
//     month, and no per-request check can see it: the question is what fraction
//     of the last hour's bill this mechanism added, and only a rolling window
//     can answer that. This one IS in wall-clock time, because a bill is.
//
// The windows are the CALLER'S: the budget is told when a request finished
// ([Budget.NoteRequest]), what a call cost ([Budget.NoteSpend]) and what a hedge
// cost ([Budget.NoteHedge]) by whoever billed them, and holds no clock, no
// ledger and no opinion about prices. It is safe to use from every goroutine
// that is streaming, because that is exactly how it will be used.
//
// WITH NOTHING SPENT YET THE SHARE CANNOT BE EXCEEDED, so a fresh session
// hedges under the bucket alone. The alternative — refusing until some spending
// has accumulated — would switch the mechanism off for precisely the first
// answers a person waits through, which are the ones they remember.

// spendWindow is how far back the SPEND share is measured. An hour is long
// enough that one expensive turn does not licence a spree and short enough that
// a session which has changed what it is doing is judged on what it is doing
// now.
const spendWindow = time.Hour

// requestWindow is how many requests back the RATE share is measured. Twenty
// is short enough that a bad minute is bounded while it is happening and long
// enough that the ratio it holds — one hedge in ten — is a ratio rather than a
// coin toss.
const requestWindow = 20

// billed is one amount at the moment it was charged.
type billed struct {
	usd float64
	at  time.Time
}

// Budget is what stops a rescue mechanism from becoming a second bill.
//
// Two limits, because the two failure modes are different. A TOKEN BUCKET
// bounds a storm — one bad minute in which every stream stalls must not double
// every request in flight. A SHARE OF SPEND bounds the slow leak, where a
// hedge that fires a little too eagerly is invisible per request and obvious at
// the end of the month.
type Budget struct {
	mu sync.Mutex
	// perTwenty is how many hedges are allowed in any twenty requests.
	perTwenty int
	// share is the fraction of recent spend hedging may add, 0 to 1.
	share float64
	// requests is how many have finished, ever, and marks is the request each
	// allowed hedge was counted at. Marks are pruned as the window slides, so
	// what is held is bounded by perTwenty rather than by the session's length.
	requests int
	marks    []int
	// calls and hedges are the rolling spend windows, oldest first. They are
	// slices rather than running totals because a total cannot be un-added when
	// it ages out, and a window that only grows is a budget that loosens
	// forever.
	calls  []billed
	hedges []billed
}

// NewBudget states a budget: how many hedges are allowed in any twenty
// requests, and what share of recent spending they may add. A zero or negative
// allowance is a budget that allows nothing, which is how hedging is switched
// off.
//
// THE FIRST ARGUMENT IS PER TWENTY REQUESTS AND NOT PER MINUTE. It was per
// minute until the simulator showed what that does — see the law at the top of
// this file — and the signature is unchanged because the shape of the answer is
// the same: a small integer allowance and a share.
func NewBudget(perTwenty int, share float64) *Budget {
	return &Budget{perTwenty: perTwenty, share: share}
}

// DefaultBudget is the budget the design argues for: two hedges in any twenty
// requests, a tenth of recent spend. It is stated once here so that a caller
// wiring the transport does not get to pick its own idea of "not very many".
func DefaultBudget() *Budget { return NewBudget(2, 0.10) }

// NoteRequest records that one request has finished. It is the denominator of
// the rate limit and it is called once per request, hedged or not.
func (b *Budget) NoteRequest(_ time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests++
	b.prune()
}

// NoteSpend records what one ordinary call cost. It is the denominator of the
// share and nothing else.
func (b *Budget) NoteSpend(usd float64, now time.Time) {
	if b == nil || usd <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, billed{usd: usd, at: now})
	b.forget(now)
}

// NoteHedge records what one hedge cost — the extra request, whether it won or
// was cancelled — which is the numerator.
//
// A hedge that won is still spending: the answer arrived, the loser was
// cancelled, and the bill for the turn is one stream plus however much of the
// other one was generated before the cancel landed. Counting only the waste
// would let the mechanism spend without limit as long as it kept being right.
func (b *Budget) NoteHedge(usd float64, now time.Time) {
	if b == nil || usd <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hedges = append(b.hedges, billed{usd: usd, at: now})
	b.forget(now)
}

// Allow reports whether one more hedge, expected to cost costEstimate dollars,
// may be sent now — and COUNTS IT when it says yes.
//
// It is a decision and not a question on purpose: two goroutines asking at the
// same moment must not both be told yes on the strength of one allowance, and a
// caller that had to confirm afterwards would be a caller that could forget to.
func (b *Budget) Allow(now time.Time, costEstimate float64) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.affordableLocked(now, costEstimate) {
		return false
	}
	b.marks = append(b.marks, b.requests)
	return true
}

// Affordable is the same question ASKED WITHOUT SPENDING, for the one caller
// that has to know the answer before there is anything to decide: a surface
// drawing "if this does not answer by 4.4s it switches to parasail" is making a
// promise, and a promise the budget was always going to refuse is the surface
// lying about the machinery. It is a reading and not a reservation — the
// decision is still [Budget.Allow]'s, taken at the moment the hedge is really
// wanted — which is the right way round: the promise may be withdrawn between
// the drawing and the deadline, and a person who is never promised anything is
// never let down.
func (b *Budget) Affordable(now time.Time, costEstimate float64) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.affordableLocked(now, costEstimate)
}

// affordableLocked is the arithmetic both of them read, so that the question
// and the decision can never drift apart. It runs with the lock held and it
// prunes, which is a read that ages rather than a write that spends.
func (b *Budget) affordableLocked(now time.Time, costEstimate float64) bool {
	if b.perTwenty <= 0 {
		return false
	}
	b.prune()
	if len(b.marks) >= b.perTwenty {
		return false
	}
	b.forget(now)
	if b.share > 0 {
		var total, hedged float64
		for _, one := range b.calls {
			total += one.usd
		}
		for _, one := range b.hedges {
			hedged += one.usd
		}
		// Nothing spent is nothing to take a share of. See the law at the top:
		// the bucket alone governs a session that has not billed anything yet.
		if total > 0 && hedged+costEstimate > b.share*total {
			return false
		}
	}
	return true
}

// prune drops the hedges that have slid out of the request window.
func (b *Budget) prune() {
	oldest := b.requests - requestWindow
	keep := b.marks[:0]
	for _, mark := range b.marks {
		if mark > oldest {
			keep = append(keep, mark)
		}
	}
	b.marks = keep
}

// forget drops what has aged out of the window. It is called from every write
// so that a session which stops spending stops being judged on what it spent.
func (b *Budget) forget(now time.Time) {
	cut := now.Add(-spendWindow)
	b.calls = sinceCut(b.calls, cut)
	b.hedges = sinceCut(b.hedges, cut)
}

// sinceCut is the window's tail, copied in place rather than resliced so that a
// long session does not hold the whole hour's backing array forever.
func sinceCut(window []billed, cut time.Time) []billed {
	keep := window[:0]
	for _, one := range window {
		if one.at.After(cut) {
			keep = append(keep, one)
		}
	}
	return keep
}
