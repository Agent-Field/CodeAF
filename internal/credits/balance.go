package credits

// ── THE BALANCE BOOK: what the service last reported, per account, in memory ─
//
// A [Book] answers "what does the provider say this account has left" without
// letting a screen decide how often the provider is asked. It keeps ONE entry
// per account identity, never on disk, and enforces four laws:
//
//   - ONE REQUEST IN FLIGHT PER ACCOUNT. A second caller waits for it (at most
//     [WaitersMax] of them, each leaving on its own context) or, past that, is
//     told the book is busy and handed what is already known. Nothing queues
//     without bound and nothing runs outside the caller's context.
//   - AT MOST ONE REQUEST A MINUTE PER ACCOUNT. An attempt, failed or not, is
//     not repeated until [MinGap] has passed; an earlier ask is handed the
//     entry with how long to wait. There is no way to force one.
//   - A CHANGED CREDENTIAL IS A DIFFERENT ACCOUNT. An identity names the
//     provider, its address and a digest of the key as read at the call. The
//     moment a provider is asked about under another identity the old entry is
//     dropped and its request cancelled, so a balance is never shown for an
//     account it did not come from, and the minute does not protect a new key.
//   - A TRANSIENT FAILURE KEEPS THE LAST GOOD FIGURES. A rejected key, an
//     expired key or a reply with no figures clears them; an unreachable
//     service, a timeout or a malformed answer does not.

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"sync"
	"time"
)

// The book's bounds.
const (
	// MinGap is the least time between two requests for one account.
	MinGap = 60 * time.Second
	// BookMax is how many accounts a book holds at once.
	BookMax = 8
	// WaitersMax is how many callers may wait on one request in flight.
	WaitersMax = 8
)

// Identity names an account without holding its key: the key is kept only as a
// digest.
type Identity struct {
	Provider string
	Address  string
	key      [sha256.Size]byte
}

// NewIdentity digests key into an identity. Pass the key as read at the call.
func NewIdentity(provider, address, key string) Identity {
	return Identity{Provider: provider, Address: address, key: sha256.Sum256([]byte(key))}
}

// Kind is the stable name of why a read failed.
type Kind string

// The kinds Classify answers. They are wire words: do not respell them.
const (
	KindKeyRejected Kind = "key_rejected"
	KindKeyExpired  Kind = "key_expired"
	KindTimeout     Kind = "timeout"
	KindUnreachable Kind = "unreachable"
	KindProvider    Kind = "provider_error"
	KindBadResponse Kind = "bad_response"
	KindUnexpected  Kind = "unexpected_status"
	KindCanceled    Kind = "canceled"
)

// Classify names a failed read's cause from its type alone: the words of an
// error may hold an address and are never inspected or passed on.
func Classify(err error) Kind {
	var status *StatusError
	var netErr net.Error
	switch {
	case err == nil:
		return ""
	case errors.As(err, &status):
		return status.Kind
	case errors.Is(err, ErrTooLarge), errors.Is(err, ErrInvalid), errors.Is(err, errDecode):
		return KindBadResponse
	case errors.Is(err, context.Canceled):
		return KindCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	case errors.As(err, &netErr) && netErr.Timeout():
		return KindTimeout
	}
	return KindUnreachable
}

// Snapshot is what the book knows about one account when it answered.
type Snapshot struct {
	// Attempted is false until a request has been made for this account.
	Attempted bool
	// Detail is the last attempt's reading when Err is nil.
	Detail Detail
	// Err is the last attempt's failure; its words are never shown.
	Err error
	// Good holds the figures of the last success that carried any; HasGood says
	// whether it does, and GoodAt when.
	Good    Detail
	HasGood bool
	GoodAt  time.Time
	// LastSuccess and LastError are the times of the last successful and the
	// last failed attempt, zero when there has been none.
	LastSuccess, LastError time.Time
	// Busy says a request is in flight that this caller did not wait for, the
	// book is full of requests in flight, or the request it waited on was
	// abandoned: what is held is all there is for now.
	Busy bool
	// RetryAfter is how long until the account may be asked again, zero when it
	// may be asked now.
	RetryAfter time.Duration
}

type flight struct {
	done    chan struct{}
	aborted bool
}

type entry struct {
	seq     uint64
	flight  *flight
	waiting int
	cancel  context.CancelFunc

	attempted   bool
	attemptAt   time.Time
	detail      Detail
	err         error
	good        Detail
	hasGood     bool
	goodAt      time.Time
	lastSuccess time.Time
	lastError   time.Time
}

func (e *entry) snapshot(now time.Time) Snapshot {
	s := Snapshot{
		Attempted: e.attempted, Detail: e.detail, Err: e.err, Good: e.good, HasGood: e.hasGood,
		GoodAt: e.goodAt, LastSuccess: e.lastSuccess, LastError: e.lastError,
	}
	if e.attempted {
		if left := MinGap - now.Sub(e.attemptAt); left > 0 {
			s.RetryAfter = left
		}
	}
	return s
}

// Book holds the entries. The zero value is not usable; see [NewBook].
type Book struct {
	base context.Context
	now  func() time.Time

	mu      sync.Mutex
	entries map[Identity]*entry
	seq     uint64
}

// NewBook makes a book whose requests are all cancelled when base is. A nil
// now is the wall clock.
func NewBook(base context.Context, now func() time.Time) *Book {
	if now == nil {
		now = time.Now
	}
	return &Book{base: base, now: now, entries: map[Identity]*entry{}}
}

// Drop forgets every entry for a provider and cancels its requests, as when
// its key is gone.
func (b *Book) Drop(provider string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dropOthersLocked(Identity{Provider: provider}, false)
}

func (b *Book) dropOthersLocked(keep Identity, keepIt bool) {
	for id, e := range b.entries {
		if id.Provider != keep.Provider || (keepIt && id == keep) {
			continue
		}
		if e.cancel != nil {
			e.cancel()
		}
		delete(b.entries, id)
	}
}

// Len is how many accounts the book holds.
func (b *Book) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// Get answers what is known about id, asking the service through fetch only
// when no request is in flight and the minute since the last has passed. fetch
// runs on the caller's goroutine under ctx, so a caller that gives up takes its
// request with it; that attempt is then not counted. The error is ctx's, and
// only that.
func (b *Book) Get(ctx context.Context, id Identity, fetch func(context.Context) (Detail, error)) (Snapshot, error) {
	b.mu.Lock()
	b.dropOthersLocked(id, true)
	e := b.entries[id]
	if e == nil {
		if len(b.entries) >= BookMax && !b.evictLocked() {
			b.mu.Unlock()
			return Snapshot{Busy: true}, nil
		}
		e = &entry{}
		b.entries[id] = e
	}
	b.seq++
	e.seq = b.seq
	if f := e.flight; f != nil {
		if e.waiting >= WaitersMax {
			s := e.snapshot(b.now())
			s.Busy = true
			b.mu.Unlock()
			return s, nil
		}
		e.waiting++
		b.mu.Unlock()
		select {
		case <-f.done:
		case <-ctx.Done():
		}
		b.mu.Lock()
		e.waiting--
		s := Snapshot{Busy: true}
		if b.entries[id] == e {
			s = e.snapshot(b.now())
			s.Busy = f.aborted
		}
		b.mu.Unlock()
		return s, ctx.Err()
	}
	now := b.now()
	if e.attempted && now.Sub(e.attemptAt) < MinGap {
		s := e.snapshot(now)
		b.mu.Unlock()
		return s, nil
	}
	fctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(b.base, cancel)
	f := &flight{done: make(chan struct{})}
	e.flight, e.cancel = f, cancel
	b.mu.Unlock()

	detail, err := fetch(fctx)
	stop()
	abandoned := fctx.Err() != nil
	cancel()

	b.mu.Lock()
	defer b.mu.Unlock()
	e.flight, e.cancel = nil, nil
	defer close(f.done)
	if b.entries[id] != e || abandoned {
		// The credential changed, or the caller or the process left: this
		// answer is for nobody and the attempt does not count.
		f.aborted = true
		if b.entries[id] != e {
			return Snapshot{Busy: true}, ctx.Err()
		}
		s := e.snapshot(b.now())
		s.Busy = true
		return s, ctx.Err()
	}
	done := b.now()
	e.attempted, e.attemptAt = true, now
	e.detail, e.err = detail, err
	switch {
	case err != nil:
		e.lastError = done
		if Classify(err) == KindKeyRejected {
			e.hasGood = false
		}
	default:
		e.lastSuccess = done
		if _, ok := detail.Remaining(); ok {
			e.good, e.hasGood, e.goodAt = detail, true, done
		} else {
			e.hasGood = false
		}
	}
	return e.snapshot(done), nil
}

// evictLocked frees one slot by dropping the least recently asked entry with
// nothing in flight, and says whether it could.
func (b *Book) evictLocked() bool {
	var oldest Identity
	var found *entry
	for id, e := range b.entries {
		if e.flight == nil && (found == nil || e.seq < found.seq) {
			oldest, found = id, e
		}
	}
	if found == nil {
		return false
	}
	delete(b.entries, oldest)
	return true
}
