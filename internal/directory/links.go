package directory

import (
	"context"
	"crypto/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// LinkLimits are the numbers a client may read (contract 3.6).
type LinkLimits struct {
	TTLMs           int64 `json:"ttl_ms"`
	CreatePerHour   int   `json:"create_per_hour"`
	PendingPerIP    int   `json:"pending_per_ip"`
	ReadPerMinute   int   `json:"read_per_minute"`
	MissPerMinute   int   `json:"miss_per_minute"`
	ConcurrentPolls int   `json:"concurrent_polls"`
	MaxGrant        int   `json:"max_grant"`
	MaxLive         int   `json:"max_live"`
}

// DefaultLinkLimits are the contract's numbers.
var DefaultLinkLimits = LinkLimits{
	TTLMs: RequestTTL.Milliseconds(), CreatePerHour: 10, PendingPerIP: 3, ReadPerMinute: 60,
	MissPerMinute: 20, ConcurrentPolls: 4, MaxGrant: MaxGrant, MaxLive: 2000,
}

// Decider is the decision side of the request store, as an identity's
// directory sees it. apply runs while the request is still undecided and
// before the decision is kept, so a refused write leaves the request pending
// and two approvers cannot both write.
type Decider interface {
	Decide(code string, d Decision, apply func(Request) error) error
}

// Links is the global store of pending requests, one per relay (the Worker's
// LinkGate and LinkRequest objects). It is in memory: a request lives ten
// minutes, so a restart costs a person one new code.
type Links struct {
	clock  func() time.Time
	limits LinkLimits

	mu    sync.Mutex
	reqs  map[string]*entry
	peers map[string]*use
}

// entry is a request, the moment it stops being readable, and a channel that
// closes when it is decided.
type entry struct {
	rec   Request
	peer  string
	until int64
	done  chan struct{}
}

// NewLinks returns an empty store that reads time from clock.
func NewLinks(clock func() time.Time, limits LinkLimits) *Links {
	return &Links{clock: clock, limits: limits, reqs: map[string]*entry{}, peers: map[string]*use{}}
}

var _ Decider = (*Links)(nil)

// Limits are the numbers this store enforces.
func (l *Links) Limits() LinkLimits { return l.limits }

func (l *Links) now() int64 { return l.clock().UnixMilli() }

// Open stores a request made by peer.
func (l *Links) Open(peer string, in NewRequest) (Opened, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	code, err := newCode()
	if err != nil {
		return Opened{}, err
	}
	o, rec, err := in.opened(code, now)
	if err != nil {
		return Opened{}, err
	}
	if err := l.admitCreate(peer, now); err != nil {
		return Opened{}, err
	}
	l.reqs[code] = &entry{rec: rec, peer: peer, until: rec.ExpiresAt, done: make(chan struct{})}
	return o, nil
}

// admitCreate applies the per-network and global limits to one create.
func (l *Links) admitCreate(peer string, now int64) error {
	switch {
	case len(l.reqs) >= l.limits.MaxLive:
		return ErrFull
	case l.pendingOf(peer) >= l.limits.PendingPerIP:
		return slow(time.Minute)
	}
	return l.use(peer).creates.take(now, time.Hour.Milliseconds(), l.limits.CreatePerHour)
}

func (l *Links) pendingOf(peer string) (n int) {
	for _, e := range l.reqs {
		if e.peer == peer && e.rec.State == RequestPending {
			n++
		}
	}
	return n
}

// Read answers a request to peer; see Requests.GetRequest.
func (l *Links) Read(ctx context.Context, peer, code string, wait time.Duration) (Request, error) {
	e, release, err := l.find(peer, code, wait > 0)
	if err != nil {
		return Request{}, err
	}
	defer release()
	if wait > 0 {
		l.await(ctx, e, min(wait, MaxWait))
	}
	return l.snapshot(e, wait > 0)
}

// find looks a code up under peer's read budget and, when the caller will
// wait, takes one of its poll places. release gives the place back.
func (l *Links) find(peer, code string, polling bool) (*entry, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	u := l.use(peer)
	if err := u.reads.take(now, time.Minute.Milliseconds(), l.limits.ReadPerMinute); err != nil {
		return nil, nil, err
	}
	e, ok := l.reqs[NormalizeCode(code)]
	if !ok {
		if err := u.misses.take(now, time.Minute.Milliseconds(), l.limits.MissPerMinute); err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrRequestGone
	}
	if !polling || e.rec.State != RequestPending {
		return e, func() {}, nil
	}
	if u.polls >= l.limits.ConcurrentPolls {
		return nil, nil, slow(time.Second)
	}
	u.polls++
	return e, func() { l.mu.Lock(); u.polls--; l.mu.Unlock() }, nil
}

// await blocks until e is decided, the wait ends or ctx ends.
func (l *Links) await(ctx context.Context, e *entry, wait time.Duration) {
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-e.done:
	case <-t.C:
	case <-ctx.Done():
	}
}

// snapshot copies e's record; a polled request that is still pending is
// ErrStillPending, so the caller can tell a quiet wait from an answer.
func (l *Links) snapshot(e *entry, polled bool) (Request, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if polled && e.rec.State == RequestPending {
		return Request{}, ErrStillPending
	}
	return e.rec, nil
}

// Decide moves the request named code out of pending; see Decider.
func (l *Links) Decide(code string, d Decision, apply func(Request) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	e, ok := l.reqs[NormalizeCode(code)]
	if !ok {
		return ErrRequestGone
	}
	switch err := Settle(e.rec, d); err {
	case errRepeat:
		return nil
	case nil:
	default:
		return err
	}
	if err := apply(e.rec); err != nil {
		return err
	}
	e.rec, e.until = Decided(e.rec, d), now+DecidedKeep.Milliseconds()
	close(e.done)
	return nil
}

// sweep forgets what can no longer be read, and the networks that have used
// nothing lately. The caller holds the lock.
func (l *Links) sweep(now int64) {
	for code, e := range l.reqs {
		if now >= e.until {
			delete(l.reqs, code)
		}
	}
	for peer, u := range l.peers {
		if u.idle(now) {
			delete(l.peers, peer)
		}
	}
}

// From is the Requests one network sees; it is how a caller in the same
// process (a test, a local relay) uses the store without HTTP.
func (l *Links) From(peer string) Requests { return linkView{l, peer} }

type linkView struct {
	l    *Links
	peer string
}

func (v linkView) CreateRequest(_ context.Context, in NewRequest) (Opened, error) {
	return v.l.Open(v.peer, in)
}

func (v linkView) GetRequest(ctx context.Context, code string, wait time.Duration) (Request, error) {
	return v.l.Read(ctx, v.peer, code, wait)
}

// newCode is 40 random bits in Crockford base32.
func newCode() (string, error) {
	var raw [5]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	var bits uint64
	for _, b := range raw {
		bits = bits<<8 | uint64(b)
	}
	out := make([]byte, 8)
	for i := range out {
		out[i] = codeAlphabet[bits>>(5*(7-i))&31]
	}
	return string(out), nil
}

// use is what one network has spent.
type use struct {
	creates, reads, misses window
	polls                  int
}

func (l *Links) use(peer string) *use {
	if l.peers[peer] == nil {
		l.peers[peer] = &use{}
	}
	return l.peers[peer]
}

func (u *use) idle(now int64) bool {
	return u.polls == 0 && u.creates.empty(now, time.Hour.Milliseconds()) &&
		u.reads.empty(now, time.Minute.Milliseconds()) && u.misses.empty(now, time.Minute.Milliseconds())
}

// window counts uses inside a span, so a limit is "no more than n in the last
// span" and not a bucket that refills in lumps.
type window struct{ at []int64 }

func (w *window) trim(now, span int64) {
	keep := 0
	for keep < len(w.at) && now >= w.at[keep]+span {
		keep++
	}
	w.at = w.at[keep:]
}

func (w *window) empty(now, span int64) bool {
	w.trim(now, span)
	return len(w.at) == 0
}

// take records a use unless max already fall inside span; then it answers a
// rate-limit refusal that says when the oldest use leaves the window.
func (w *window) take(now, span int64, max int) error {
	w.trim(now, span)
	if len(w.at) >= max {
		return slow(time.Duration(w.at[0]+span-now) * time.Millisecond)
	}
	w.at = append(w.at, now)
	return nil
}

// slow is ErrRateLimited carrying how long to wait, rounded up to a second.
func slow(d time.Duration) error {
	secs := int((d + time.Second - 1) / time.Second)
	return wireauth.Wait(ErrRateLimited, http.Header{"Retry-After": {strconv.Itoa(max(secs, 1))}})
}
