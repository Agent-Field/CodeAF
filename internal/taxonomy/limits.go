package taxonomy

import "time"

// Limits are the three numbers the policies read: how many times the wire is
// forgiven, how many measured failures buy a tier, and how much that tier may
// cost one piece of work.
//
// THEY ARE VALUES AND NOT CONSTANTS, and that is the difference between a knob
// and a fact. Every one of them was a literal at a call site once — 3 retries
// here, "escalate on the first refutation" there, no cap anywhere — and a
// person who wanted the harness to be more patient or less expensive had
// nothing to turn. internal/config resolves them from the person's settings and
// hands them down; this package never reads a profile and never names an
// environment variable.
type Limits struct {
	// TransportAttempts is N: how many tries one request gets on its own tier
	// before the transport policy gives up on it. It is the TOTAL, first attempt
	// included, so 3 is one try and two retries.
	TransportAttempts int

	// TransportBackoff is the first wait. Each further attempt doubles it, so
	// this one number is the whole schedule.
	TransportBackoff time.Duration

	// SemanticFailures is K: how many checks must read the finished work and
	// find gaps in it, on the same tier, before a stronger one is bought. It
	// counts REFUTATIONS WITH NOTHING ON THE WIRE UNDER THEM and nothing else.
	SemanticFailures int

	// TierCapUSD is what a lifted tier may cost ONE piece of work. Past it the
	// capability policy stops buying and the verdict comes back as work. 0 is no
	// cap, which is what this build had.
	TierCapUSD float64
}

// The floors. They are what a caller that resolved nothing still gets: three
// attempts on the wire doubling from one second, and a stronger tier bought on
// the first measured failure.
//
// THE WIRE PAIR WAS FOUR AND TWO SECONDS and both came down on 2026-09-10, for
// the reason each of them now states: the fourth attempt was blind and the
// waiting was latency a person paid for a machine that was never going to
// answer. The tier pair is untouched and is still exactly the behaviour this
// build already had.
//
// K FLOORS AT ONE, WHICH IS NOT WHERE THE BILL CAME FROM. The argument for
// buying on the first finding is a good one and it is written out in
// internal/session's repair_role.go: the finding is a MEASURED failure, made
// after the fact, on exactly the work that turned out to need it. What was wrong
// was never the count — it was that a finding about a round whose calls had died
// on the wire was counted at all, and [Tally] is where that is now refused. A
// person who wants the tier held back further raises this; the default changes
// nothing about a harness that is working.
const (
	// DefaultTransportAttempts is three: one try, one repeat, and then the move.
	//
	// IT WAS FOUR, AND THE FOURTH WAS SPENT BLIND. A transport fault is the
	// network on this machine far more often than it is the far end — the call
	// census of 2026-09-10 puts the whole network family at 6.8% of failures —
	// and the chains it produced re-sent into a dead resolver rather than
	// waiting for it: three `no such host` chains ran 1,113s, 1,005s and 787s
	// and answered nothing. The origin being unreachable is already owned by
	// the connectivity gate (internal/provider's connectivity.go), which probes
	// the real origin, says so on the phase pipe while it waits, and gives up
	// after its own window. A fourth blind re-send buys nothing that gate does
	// not buy better, and it spends the turn's ninety seconds
	// ([lane.TurnGiveUp]) doing it.
	DefaultTransportAttempts = 3
	// DefaultTransportBackoff is one second, doubling: 1s, 2s.
	//
	// IT WAS TWO, and at four attempts that was eight seconds of a ninety-second
	// turn spent asleep before anything had moved. The wait exists to let a
	// rate window drain, which is a claim about the machine that refused; when
	// the next move is a DIFFERENT machine there is nothing to drain and the
	// sleep is pure latency the person pays for.
	DefaultTransportBackoff = time.Second
	// DefaultSemanticFailures is one. See above.
	DefaultSemanticFailures = 1
	// DefaultTierCapUSD is twenty-five dollars, and it is nearly inert on a
	// shipped install: one lift is bought per piece of work by default, so the
	// cap has nothing to stop. It earns its keep the moment somebody raises the
	// number of rounds a piece of work may take — which is exactly the
	// configuration where an unbounded lift ran three runs to $9.50 and above.
	//
	// IT WAS $2, WHICH IS UNDER THE PRICE OF ONE LIFT on the models this cap
	// exists to govern: a cap that stops the first purchase it was written to
	// allow is not a cap, it is an off switch wearing a number. Twenty-five
	// dollars is above the run that went wrong and far under a day anybody
	// would defend. 0 is still no cap.
	DefaultTierCapUSD = 25.0
)

// floor fills in whatever the caller left at zero. A policy is handed a floored
// copy so no implementation has to defend itself against an unset field, and a
// caller that forgot to resolve its limits still gets a bounded harness rather
// than one that retries forever or escalates on sight.
//
// ZERO IS AN ABSENCE, NOT AN INSTRUCTION. There is no way to spell "retry zero
// times" or "wait no time at all" here, because a request that is never sent and
// a retry ladder with no pauses in it are not policies anybody wants: a caller
// that wants to stop retrying sets the count to one, and one that wants a short
// ladder sets a short backoff.
func (l Limits) floor() Limits {
	if l.TransportAttempts < 1 {
		l.TransportAttempts = DefaultTransportAttempts
	}
	if l.TransportBackoff <= 0 {
		l.TransportBackoff = DefaultTransportBackoff
	}
	if l.SemanticFailures < 1 {
		l.SemanticFailures = DefaultSemanticFailures
	}
	if l.TierCapUSD < 0 {
		l.TierCapUSD = 0
	}
	return l
}

// Floored is [Limits.floor] for a caller that wants to SHOW the numbers in
// force — a settings row, a log line, a test — rather than act on them.
func (l Limits) Floored() Limits { return l.floor() }
