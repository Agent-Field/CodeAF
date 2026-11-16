package serve

// ── HOW OFTEN A PASSWORD MAY BE TRIED ───────────────────────────────────────
//
// EVERY TRY IS PAID FOR BEFORE IT IS VERIFIED. A sign-in (or a password
// change's current password) is admitted only by reserving one token from
// each golang.org/x/time/rate bucket it counts against, under one lock, and
// only while no other try of the same username is being verified. A refused
// guess keeps its tokens spent; a right password gives them back. So a burst
// of concurrent guesses cannot all slip past a check that spent nothing: at
// most one per username is ever in flight, and each one was already counted.
// An address the person has signed in from before waits only for its own
// tries, never for a stranger's guess in flight; a password change, which
// needs the person's session already, is never held by a verify in flight.
//
// THE BUCKETS, each refilling over throttleWindow (15 minutes):
//
//   - one per (username, client address), userFailures (10). When it runs
//     dry it also backs off exponentially, 30 seconds doubling to at most
//     15 minutes, and that backoff decays once the pair has been quiet for 15
//     minutes. A right password from that address forgives the pair at once.
//   - one per username, userFailuresAll (100): a ceiling on how many guesses
//     a name takes from everywhere at once. It never backs off, and an address
//     the person has signed in from before does not count against it, so a
//     stranger hammering the name from many addresses slows down the stranger,
//     never the owner on their own machine.
//   - one per client address, addrFailures (50), across every username.
//
// THE ADDRESS is the one the server's own trusted-proxy resolver settled
// (proxy.go), never a header read here; an IPv6 address counts by its /64,
// the block one subscriber is usually given, so rotating within it is one
// address.
//
// THE MEMORY IS BOUNDED. Each set holds at most throttleKeyMost keys. A full
// set forgets keys that have fully recovered first, then the oldest one that
// is not backing off; a key that is backing off is never forgotten, and when
// nothing can be forgotten the try is refused (429) rather than let through.

import (
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
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
	// trustedPerUser and trustedLife bound the addresses remembered as a
	// person's own: the latest few, for as long as a session lives.
	trustedPerUser = 8
	trustedLife    = accessSessionLife
)

type throttleEntry struct {
	lim     *rate.Limiter
	strikes int
	until   time.Time
	seen    time.Time
}

// throttle is one bounded set of buckets. backoff turns on the exponential
// wait for a key whose bucket runs dry. Its guard's lock covers it.
type throttle struct {
	keys    map[string]*throttleEntry
	burst   int
	every   time.Duration
	backoff bool
	most    int
}

func newThrottle(failures int, backoff bool) *throttle {
	return &throttle{keys: map[string]*throttleEntry{}, burst: failures,
		every: throttleWindow / time.Duration(failures), backoff: backoff, most: throttleKeyMost}
}

// reserve takes one try of key now, or answers false: backing off, empty, or
// no room to remember it.
func (t *throttle) reserve(key string, now time.Time) (*rate.Reservation, bool) {
	e := t.keys[key]
	if e == nil {
		if len(t.keys) >= t.most && !t.evict(now) {
			return nil, false
		}
		e = &throttleEntry{lim: rate.NewLimiter(rate.Every(t.every), t.burst)}
		t.keys[key] = e
	}
	if now.Sub(e.seen) >= backoffForgiven {
		e.strikes = 0
	}
	e.seen = now
	if now.Before(e.until) {
		return nil, false
	}
	r := e.lim.ReserveN(now, 1)
	if !r.OK() || r.DelayFrom(now) > 0 {
		r.CancelAt(now)
		return nil, false
	}
	return r, true
}

// failed settles a refused try of key: its token stays spent, and a bucket
// left dry starts or lengthens the backoff. It answers true the first time
// the key runs dry in a quiet spell, which is when a record row is worth it.
func (t *throttle) failed(key string, now time.Time) bool {
	e := t.keys[key]
	if e == nil || e.lim.TokensAt(now) >= 1 {
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
		if e.lim.TokensAt(now) >= float64(t.burst) {
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

// passwordGuard is the server's throttle: the three sets of buckets, the
// usernames being verified now, and the addresses each person has signed in
// from. One lock covers all of it, so admitting a try is one step.
type passwordGuard struct {
	mu       sync.Mutex
	pairs    *throttle
	users    *throttle
	addrs    *throttle
	inflight map[string]bool
	trusted  map[string]map[string]time.Time
	nTrusted int
}

func newPasswordGuard() *passwordGuard {
	return &passwordGuard{pairs: newThrottle(userFailures, true), users: newThrottle(userFailuresAll, false),
		addrs: newThrottle(addrFailures, false), inflight: map[string]bool{}, trusted: map[string]map[string]time.Time{}}
}

// attempt is one admitted try: the tokens it spent, until it is settled.
type attempt struct {
	g          *passwordGuard
	user, addr string
	flight     string
	spent      []*rate.Reservation
	settled    bool
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

// admit reserves one try of user from addr, or answers false (429). alone
// is whether the try must be the only one of its username being verified.
func (g *passwordGuard) admit(user, addr string, now time.Time, alone bool) (*attempt, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	trusted := g.trustedLocked(user, addr, now)
	flight := ""
	if alone {
		flight = user
		if trusted {
			flight = user + "\x00" + addr
		}
		if g.inflight[flight] {
			return nil, false
		}
	}
	a := &attempt{g: g, user: user, addr: addr, flight: flight}
	refuse := func() (*attempt, bool) {
		for _, r := range a.spent {
			r.CancelAt(now)
		}
		return nil, false
	}
	r, ok := g.pairs.reserve(user+"\x00"+addr, now)
	if !ok {
		return refuse()
	}
	a.spent = append(a.spent, r)
	if !trusted {
		if r, ok = g.users.reserve(user, now); !ok {
			return refuse()
		}
		a.spent = append(a.spent, r)
	}
	if r, ok = g.addrs.reserve(addr, now); !ok {
		return refuse()
	}
	a.spent = append(a.spent, r)
	if flight != "" {
		g.inflight[flight] = true
	}
	return a, true
}

func (g *passwordGuard) trustedLocked(user, addr string, now time.Time) bool {
	at, ok := g.trusted[user][addr]
	return ok && now.Sub(at) < trustedLife
}

// trustLocked remembers addr as one user signs in from, within the bounds.
func (g *passwordGuard) trustLocked(user, addr string, now time.Time) {
	mine := g.trusted[user]
	if mine == nil {
		if g.nTrusted >= throttleKeyMost {
			g.dropOldestTrustedLocked()
		}
		mine = map[string]time.Time{}
		g.trusted[user] = mine
	}
	if _, ok := mine[addr]; !ok {
		if len(mine) >= trustedPerUser {
			var oldest string
			for k, at := range mine {
				if oldest == "" || at.Before(mine[oldest]) {
					oldest = k
				}
			}
			delete(mine, oldest)
			g.nTrusted--
		}
		g.nTrusted++
	}
	mine[addr] = now
}

func (g *passwordGuard) dropOldestTrustedLocked() {
	var user, addr string
	var at time.Time
	for u, mine := range g.trusted {
		for a, t := range mine {
			if user == "" || t.Before(at) {
				user, addr, at = u, a, t
			}
		}
	}
	if user == "" {
		return
	}
	delete(g.trusted[user], addr)
	if len(g.trusted[user]) == 0 {
		delete(g.trusted, user)
	}
	g.nTrusted--
}

// succeeded settles a right password: every token comes back, the pair is
// forgiven, and the address is remembered as the person's own.
func (a *attempt) succeeded(now time.Time) {
	g := a.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.settled {
		return
	}
	a.settled = true
	for _, r := range a.spent {
		r.CancelAt(now)
	}
	delete(g.pairs.keys, a.user+"\x00"+a.addr)
	g.trustLocked(a.user, a.addr, now)
	delete(g.inflight, a.flight)
}

// failed settles a wrong password: the tokens stay spent. It answers true the
// first time the person's buckets run dry in a quiet spell.
func (a *attempt) failed(now time.Time) bool {
	g := a.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.settled {
		return false
	}
	a.settled = true
	delete(g.inflight, a.flight)
	g.addrs.failed(a.addr, now)
	pair := g.pairs.failed(a.user+"\x00"+a.addr, now)
	all := g.users.failed(a.user, now)
	return pair || all
}

// abandon settles a try that never reached a verdict (hashing busy, a
// server fault): the tokens come back. After succeeded or failed it does
// nothing, so a handler can defer it.
func (a *attempt) abandon(now time.Time) {
	g := a.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.settled {
		return
	}
	a.settled = true
	for _, r := range a.spent {
		r.CancelAt(now)
	}
	delete(g.inflight, a.flight)
}
