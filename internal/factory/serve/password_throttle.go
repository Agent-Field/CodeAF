package serve

// ── HOW OFTEN A PASSWORD MAY BE TRIED ───────────────────────────────────────
//
// EVERY TRY IS PAID FOR BEFORE IT IS VERIFIED. A sign-in (or a password
// change's current password) is admitted only by spending one try from each
// bucket it counts against, under one lock, and a sign-in only while no
// other sign-in of the same username (or the same trusted device) is being
// verified. A refused guess keeps its tries spent; a right password, or a
// try that never reached a verdict, gives them back. So a burst of concurrent
// guesses cannot slip past a check that spent nothing.
//
// THE BUCKETS are golang.org/x/time/rate limiters refilling over
// throttleWindow (15 minutes):
//
//   - one per (username, client address), userFailures (10). When it runs
//     dry it also backs off exponentially, 30 seconds doubling to at most
//     15 minutes, and that backoff decays once the pair has been quiet for 15
//     minutes. A right password from there forgives the pair at once.
//   - one per username, userFailuresAll (100): a ceiling on how many guesses
//     a name takes from everywhere at once. It never backs off.
//   - one per client address, addrFailures (50), across every username.
//
// A TRUSTED TRY skips the username's ceiling and counts in its own pair
// bucket, keyed by what it is trusted by AND its address, plus the address
// bucket. Two things make a try trusted:
//
//   - a device (tokens.Device: the long-lived secret a machine is handed
//     after a right password, kept by the server only as a hash and so
//     surviving a restart). A stranger draining the ceiling from many
//     addresses slows down the stranger and every machine the owner never
//     used, never the owner's own. The address is part of the key, so a
//     thief guessing with a stolen device token from elsewhere backs off
//     their own pair, not the owner's machine.
//   - a signed-in session changing its password: the session already proves
//     who it is, so a change counts per session.
//
// A brand-new machine during such an attack waits, or the operator makes a
// reset link, which, like an invitation, is never throttled here.
//
// A REFUND IS COUNTED BY HAND. x/time/rate can give a reservation back only
// at the instant it was made, which a verify that takes argon2's time is
// always past; so a refund is kept as credit on the key, spent before the
// limiter and never lifting the key above its burst.
//
// THE ADDRESS is the one the server's own trusted-proxy resolver settled
// (proxy.go), never a header read here; an IPv6 address counts by its /64,
// the block one subscriber is usually given.
//
// THE MEMORY IS BOUNDED. Each set holds at most throttleKeyMost keys. A full
// set forgets keys that have fully recovered first, then the oldest one that
// is not backing off; a key that is backing off is never forgotten, and when
// nothing can be forgotten the try is refused (429) rather than let through,
// with one log line a minute saying so.

import (
	"log"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const (
	throttleWindow  = 15 * time.Minute
	userFailures    = 10
	userFailuresAll = 100
	addrFailures    = 50
	throttleKeyMost = 10000
	backoffFirst    = 30 * time.Second
	backoffMost     = 15 * time.Minute
	backoffForgiven = 15 * time.Minute
	// throttleFullEvery is how often a full set is said in the log.
	throttleFullEvery = time.Minute
)

type throttleEntry struct {
	lim     *rate.Limiter
	credit  float64
	strikes int
	until   time.Time
	seen    time.Time
}

// tokens is the tries the key has now: the limiter's and the credit.
func (e *throttleEntry) tokens(now time.Time) float64 { return e.lim.TokensAt(now) + e.credit }

// throttle is one bounded set of buckets. backoff turns on the exponential
// wait for a key whose bucket runs dry. Its guard's lock covers it.
type throttle struct {
	name    string
	keys    map[string]*throttleEntry
	burst   int
	every   time.Duration
	backoff bool
	most    int
	// full is set when the last refusal was for want of room.
	full bool
}

func newThrottle(name string, failures int, backoff bool) *throttle {
	return &throttle{name: name, keys: map[string]*throttleEntry{}, burst: failures,
		every: throttleWindow / time.Duration(failures), backoff: backoff, most: throttleKeyMost}
}

// take spends one try of key now, or answers false: backing off, empty, or
// no room to remember it.
func (t *throttle) take(key string, now time.Time) bool {
	t.full = false
	e := t.keys[key]
	if e == nil {
		if len(t.keys) >= t.most && !t.evict(now) {
			t.full = true
			return false
		}
		e = &throttleEntry{lim: rate.NewLimiter(rate.Every(t.every), t.burst)}
		t.keys[key] = e
	}
	if now.Sub(e.seen) >= backoffForgiven {
		e.strikes = 0
	}
	e.seen = now
	if now.Before(e.until) {
		return false
	}
	if e.credit >= 1 {
		e.credit--
		return true
	}
	return e.lim.AllowN(now, 1)
}

// refund gives one try of key back, never above its burst.
func (t *throttle) refund(key string, now time.Time) {
	e := t.keys[key]
	if e == nil {
		return
	}
	e.credit = min(e.credit+1, max(0, float64(t.burst)-e.lim.TokensAt(now)))
}

// failed settles a refused try of key: its try stays spent, and a bucket
// left dry starts or lengthens the backoff. It answers true the first time
// the key runs dry in a quiet spell, which is when a record row is worth it.
func (t *throttle) failed(key string, now time.Time) bool {
	e := t.keys[key]
	if e == nil || e.tokens(now) >= 1 {
		return false
	}
	if t.backoff {
		wait := backoffFirst << min(e.strikes, 10)
		e.until = now.Add(min(wait, backoffMost))
	}
	e.strikes++
	return e.strikes == 1
}

// evict makes room for one key, or answers false. Fully recovered keys go
// first, then the oldest one not backing off; one backing off never goes.
func (t *throttle) evict(now time.Time) bool {
	var oldest string
	var oldestSeen time.Time
	for k, e := range t.keys {
		if now.Before(e.until) {
			continue
		}
		if e.tokens(now) >= float64(t.burst) {
			delete(t.keys, k)
			continue
		}
		if oldest == "" || e.seen.Before(oldestSeen) {
			oldest, oldestSeen = k, e.seen
		}
	}
	if len(t.keys) < t.most {
		return true
	}
	if oldest == "" {
		return false
	}
	delete(t.keys, oldest)
	return true
}

// passwordGuard is the server's throttle: the three sets of buckets and the
// names (or devices) being verified now. One lock covers all of it, so
// admitting a try is one step.
type passwordGuard struct {
	mu       sync.Mutex
	pairs    *throttle
	users    *throttle
	addrs    *throttle
	inflight map[string]bool
	saidFull time.Time
	// saidDevicesFull is the people already said in the log to hold every
	// device they may; bounded by throttleKeyMost.
	saidDevicesFull map[string]bool
	logf            func(format string, args ...any)
}

// devicesFull says once per person, in the log, that a right sign-in made no
// new device because the person holds as many live ones as are kept.
func (g *passwordGuard) devicesFull(handle string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.saidDevicesFull[handle] || len(g.saidDevicesFull) >= throttleKeyMost {
		return
	}
	if g.saidDevicesFull == nil {
		g.saidDevicesFull = map[string]bool{}
	}
	g.saidDevicesFull[handle] = true
	g.logf("factory: %s already holds %d trusted devices; a sign-in made no new one", handle, tokens.DevicesPerPerson)
}

func newPasswordGuard() *passwordGuard {
	return &passwordGuard{pairs: newThrottle("username and address", userFailures, true),
		users: newThrottle("username", userFailuresAll, false), addrs: newThrottle("address", addrFailures, false),
		inflight: map[string]bool{}, logf: log.Printf}
}

// spent is one bucket an attempt took a try from.
type spent struct {
	t   *throttle
	key string
}

// attempt is one admitted try: the tries it spent, until it is settled.
type attempt struct {
	g       *passwordGuard
	pair    string
	flight  string
	spent   []spent
	settled bool
}

// throttleAddr is the key a client address counts under: an IPv6 address by
// its /64.
func throttleAddr(addr netip.Addr) string {
	if !addr.IsValid() {
		return "unknown"
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}

// admit spends one try of user from addr, or answers false (429). trusted
// is what the try is trusted by ("device:<id>", "session:<id>"), or "". alone
// is whether it must be the only try of its name (or trusted pair) being
// verified.
func (g *passwordGuard) admit(user, addr, trusted string, now time.Time, alone bool) (*attempt, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	pair := user + "\x00" + addr
	if trusted != "" {
		pair = user + "\x00" + trusted + "\x00" + addr
	}
	flight := ""
	if alone {
		flight = user
		if trusted != "" {
			flight = pair
		}
		if g.inflight[flight] {
			return nil, false
		}
	}
	a := &attempt{g: g, pair: pair, flight: flight}
	refuse := func(t *throttle) (*attempt, bool) {
		for _, s := range a.spent {
			s.t.refund(s.key, now)
		}
		if t.full && now.Sub(g.saidFull) >= throttleFullEvery {
			g.saidFull = now
			g.logf("factory: the password throttle's %s set is full (%d keys, all backing off); new tries are refused until some recover", t.name, t.most)
		}
		return nil, false
	}
	steps := []spent{{g.pairs, pair}}
	if trusted == "" {
		steps = append(steps, spent{g.users, user})
	}
	steps = append(steps, spent{g.addrs, addr})
	for _, s := range steps {
		if !s.t.take(s.key, now) {
			return refuse(s.t)
		}
		a.spent = append(a.spent, s)
	}
	if flight != "" {
		g.inflight[flight] = true
	}
	return a, true
}

// settle ends the attempt once: refund gives every try back.
func (a *attempt) settle(now time.Time, refund bool) bool {
	if a.settled {
		return false
	}
	a.settled = true
	if refund {
		for _, s := range a.spent {
			s.t.refund(s.key, now)
		}
	}
	delete(a.g.inflight, a.flight)
	return true
}

// succeeded settles a right password: every try comes back and the pair (or
// device) is forgiven.
func (a *attempt) succeeded(now time.Time) {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	if a.settle(now, true) {
		delete(a.g.pairs.keys, a.pair)
	}
}

// failed settles a wrong password: the tries stay spent. It answers true the
// first time the person's buckets run dry in a quiet spell.
func (a *attempt) failed(now time.Time) bool {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	if !a.settle(now, false) {
		return false
	}
	dry := false
	for _, s := range a.spent {
		if s.t.failed(s.key, now) && s.t != a.g.addrs {
			dry = true
		}
	}
	return dry
}

// abandon settles a try that never reached a verdict (hashing busy, a server
// fault): the tries come back. After succeeded or failed it does nothing, so
// a handler can defer it.
func (a *attempt) abandon(now time.Time) {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	a.settle(now, true)
}
