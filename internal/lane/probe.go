package lane

import (
	"context"
	"sync"
	"time"
)

// ── THE PROBE: FRESHNESS BOUGHT TWO SECONDS EARLY ───────────────────────────
//
// The sheet is a half-hour aggregate and our own belief may be minutes old, and
// both of those are the best available right up until the moment a person
// starts typing — at which point we know, seconds in advance, that a request is
// coming and roughly where it will go.
//
// So on the first keystroke of a turn, debounced, a one-token request goes to
// the top two lanes of the frontier with `provider.only` naming each, and its
// first-token wait is recorded. It costs about two hundredths of a cent for a
// turn, it warms the connection so that a TLS handshake is out of the real
// measurement, and it lands in the ledger with a SMALL observation noise
// because it measured exactly our path to that lane, right now, rather than
// everybody's average over half an hour. [Sighting.Probe] is how the ledger
// knows which kind it is holding.
//
// It is a first-token measurement and never a rate one: an answer one token
// long rates the handshake and nothing else.
//
// Its budget is the point at which it stays cheap: at most one pair per twenty
// seconds per model, none at all when the gate says nobody is waiting — λ of
// zero, or a pool that is already pacing — and never more than two lanes at
// once. NOTHING EVER WAITS FOR A PROBE. [Prober.Probe] returns before anything
// has been sent, and a probe that fails teaches the ledger what a failure
// teaches it and nothing more, because a keystroke is not a request and must
// never be able to make one slower.
//
// The transport half lives in `internal/provider/probe.go`, which is the only
// layer that knows a base URL, a key and the router's dialect. It is injected
// here as one function, so this file can be tested without a network and the
// package keeps its law: nothing here opens a connection.

// probeEvery is the shortest interval between probe pairs for one model.
const probeEvery = 20 * time.Second

// probeLanes is the most lanes one probe pair touches. Two is the frontier's
// head — the lane the request is going to and the one a hedge would go to —
// and a third would be paying to measure a lane the choice was never going to
// make.
const probeLanes = 2

// ProbeSend is the transport half: it sends a one-token request to exactly one
// lane and reports how long the first token took.
//
// It is a function rather than an interface because there is one thing to do
// and no state to hold. An error is a real answer — a lane that refuses is a
// lane worth knowing about — and the wait it reports on the way to that error
// is not recorded, because a refusal times a refusal.
type ProbeSend func(ctx context.Context, model, lane string) (time.Duration, error)

// ProbeGate answers whether a probe is worth sending at all right now. False is
// "nobody is waiting, or the pool is already pacing", and it is asked before
// every pair rather than configured once, because both facts change by the
// second.
type ProbeGate func(model string) bool

// ProberConfig is everything the real prober needs from the layers around it.
//
// Every field is optional and the zero value of each is the safe reading: no
// send is a prober that sends nothing, no gate is a prober that is always
// allowed, no clock is the wall clock, and no ledger is the registry's own.
type ProberConfig struct {
	Send   ProbeSend
	Gate   ProbeGate
	Ledger Ledger
	Now    func() time.Time
	// Every is the shortest interval between pairs, [probeEvery] when zero.
	Every time.Duration
}

// prober is the real prober: rate-limited, gated, and fire-and-forget.
type prober struct {
	config ProberConfig
	mu     sync.Mutex
	// last is when each model was last probed. It is keyed by model rather
	// than by lane because the budget is about the person's typing, not about
	// any one endpoint: a pair is a pair however it was spread.
	last map[string]time.Time
}

// newProber builds the empty prober: no send, so nothing is ever sent. It is
// called from the registry and nowhere else.
func newProber() *prober { return &prober{last: map[string]time.Time{}} }

// NewProber builds a prober around a transport. It is what the transport calls
// to wire itself in; the registry is still the only place the EMPTY one is
// built, because a caller must never get a half-wired prober by accident.
func NewProber(config ProberConfig) Prober {
	return &prober{config: config, last: map[string]time.Time{}}
}

// Probe sends a one-token request to at most two lanes and returns at once.
//
// The context is the CALLER'S and it is used for one thing only: a session that
// is shutting down stops probing. Nothing in this method blocks on it.
func (p *prober) Probe(ctx context.Context, model string, lanes []string) {
	if p.config.Send == nil || model == "" || len(lanes) == 0 {
		return
	}
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if p.config.Gate != nil && !p.config.Gate(model) {
		return
	}
	now := p.now()
	if !p.claim(model, now) {
		return
	}
	if len(lanes) > probeLanes {
		lanes = lanes[:probeLanes]
	}
	for _, lane := range lanes {
		if lane == "" {
			continue
		}
		id := ID{Model: model, Lane: lane}
		go p.one(ctx, id)
	}
}

// one is a single lane's probe, on its own goroutine, recovering its own
// panics: it has no caller left to report a fault to.
func (p *prober) one(ctx context.Context, id ID) {
	defer func() { _ = recover() }()
	began := p.now()
	ttft, err := p.config.Send(ctx, id.Model, id.Lane)
	if err != nil || ttft <= 0 {
		// A refusal times a refusal and not a lane. What a lane that would not
		// answer is worth is the quality axis's question, and the probe has no
		// answer to hand it: an unusable ANSWER is an outcome, and no answer at
		// all is nothing.
		return
	}
	p.ledger().Note(Sighting{
		ID:    id,
		TTFT:  ttft,
		Probe: true,
		At:    began.Add(ttft),
	})
}

// claim is the rate limit: it reports whether a pair may go out now for this
// model, and records that it did.
func (p *prober) claim(model string, now time.Time) bool {
	every := p.config.Every
	if every <= 0 {
		every = probeEvery
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if last, seen := p.last[model]; seen && now.Sub(last) < every {
		return false
	}
	if p.last == nil {
		p.last = map[string]time.Time{}
	}
	p.last[model] = now
	return true
}

// now is the prober's clock. It is the one place in this package that reads a
// wall clock by default, and it is allowed to because a probe is not a choice:
// nothing about an answer depends on it, only whether a measurement was bought
// too recently to buy again.
func (p *prober) now() time.Time {
	if p.config.Now != nil {
		return p.config.Now()
	}
	return time.Now()
}

// ledger is where a probe's sighting goes: the one it was built with, or the
// registry's own.
func (p *prober) ledger() Ledger {
	if p.config.Ledger != nil {
		return p.config.Ledger
	}
	return Default().Ledger()
}
