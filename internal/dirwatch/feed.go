package dirwatch

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State is what the feed knows right now.
type State struct {
	// Version is the newest directory version the socket has announced.
	Version uint64
	// Up is true while the socket is open and has spoken: a socket that
	// upgraded and says nothing is not believed.
	Up bool
	// Vouching is true while the open socket was answered with the server's
	// word that it vouches for the leases the socket names. It is false
	// whenever Up is.
	Vouching bool
}

// Follower is one surface's view of the feed.
type Follower interface {
	// State is the feed's state now.
	State() State
	// Changes signals, without blocking the feed, that State is no longer what
	// it was. Signals coalesce: a burst of frames is one signal, so the reader
	// sees the newest state and never a backlog. It is closed by Close.
	Changes() <-chan struct{}
	// Probe asks the feed to find out now whether the socket is alive, for a
	// window that has just been returned to.
	Probe()
	// Close stops following. The last follower to close takes the socket down.
	Close()
}

// Feed is one identity's connection and everything that follows it.
type Feed struct {
	dial  Dialer
	clock Clock
	retry backoff
	kick  chan struct{}
	// redial asks the serving loop to end the socket it has and dial again.
	redial chan struct{}

	mu     sync.Mutex
	state  State
	heard  time.Time // when the open socket last showed a sign of life
	subs   map[*sub]struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

// newFeed builds a feed that is not yet connected.
func newFeed(dial Dialer, clock Clock, jitter func() float64) *Feed {
	return &Feed{
		dial: dial, clock: clock,
		retry:  backoff{rand: jitter},
		kick:   make(chan struct{}, 1),
		redial: make(chan struct{}, 1),
		subs:   map[*sub]struct{}{},
	}
}

// start opens the connection in the background.
func (f *Feed) start() {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel, f.done = cancel, make(chan struct{})
	go f.run(ctx)
}

// stop closes the connection and waits until it is closed.
func (f *Feed) stop() {
	f.cancel()
	<-f.done
}

// follow adds a follower.
func (f *Feed) follow(key string) *sub {
	s := &sub{feed: f, key: key, changes: make(chan struct{}, 1)}
	f.mu.Lock()
	f.subs[s] = struct{}{}
	f.mu.Unlock()
	return s
}

// leave removes a follower and reports whether it was the last.
func (f *Feed) leave(s *sub) (last bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, s)
	close(s.changes)
	return len(f.subs) == 0
}

func (f *Feed) snapshot() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

// set replaces the state and wakes the followers when it differs.
func (f *Feed) set(edit func(*State)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	before := f.state
	edit(&f.state)
	if f.state == before {
		return
	}
	for s := range f.subs {
		select {
		case s.changes <- struct{}{}:
		default: // a signal is already waiting; the reader will see the newest state
		}
	}
}

// run connects, serves and reconnects until it is stopped or refused.
func (f *Feed) run(ctx context.Context) {
	defer close(f.done)
	for ctx.Err() == nil {
		wait, again := f.retryIn(f.session(ctx))
		if !again || !f.sleep(ctx, wait) {
			return
		}
	}
}

// session is one connection from dial to its end, and why it ended.
func (f *Feed) session(ctx context.Context) error {
	// A redial asked for before this dial is answered by it, because the dial
	// reads what it names after this point; one asked for later still ends it.
	drain(f.redial)
	s, err := f.dial(ctx)
	if err != nil {
		return err
	}
	defer s.Close()
	return f.serve(ctx, s)
}

// retryIn says how long to wait after a connection ended for err, and whether
// to come back at all. A refusal is final, a relay without the route is asked
// again only rarely, and anything else is a dropped socket: back off, return.
func (f *Feed) retryIn(err error) (wait time.Duration, again bool) {
	switch {
	case errors.Is(err, ErrRevoked), errors.Is(err, ErrRotated):
		return 0, false
	case errors.Is(err, ErrNoRoute):
		return ProbeEvery, true
	default:
		return f.retry.next(), true
	}
}

// sleep waits d on the feed's clock and reports whether the feed is still wanted.
func (f *Feed) sleep(ctx context.Context, d time.Duration) bool {
	t := f.clock.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return true
	case <-ctx.Done():
		return false
	}
}

// reply is what the reader goroutine found.
type reply struct {
	frame Frame
	err   error
}

// pump reads the socket on its own goroutine, so the serve loop can wait on
// frames and its timer at once. It ends with the context or the first error.
func pump(ctx context.Context, s Stream) <-chan reply {
	out := make(chan reply)
	go func() {
		for {
			f, err := s.Next(ctx)
			select {
			case out <- reply{f, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

// serve runs one open socket until it fails, and returns why.
func (f *Feed) serve(ctx context.Context, s Stream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer f.set(func(st *State) { st.Up, st.Vouching = false, false })
	f.drainKick()
	in := pump(ctx, s)
	k := newKeeper(f.clock)
	defer k.stop()
	vouching := s.Vouching()
	for {
		select {
		case r := <-in:
			if r.err != nil {
				return r.err
			}
			f.hear(r.frame, vouching)
			k.heard()
		case <-k.C():
			if k.dead() {
				return errDead
			}
			if err := f.ping(ctx, s, k); err != nil {
				return err
			}
		case <-f.kick:
			if err := f.ping(ctx, s, k); err != nil && !k.outstanding {
				return err
			}
		case <-f.redial:
			return errRedial
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

var (
	errDead = errors.New("dirwatch: the socket stopped answering")
	// errRedial ends a healthy socket because what it was dialled with changed.
	errRedial = errors.New("dirwatch: the socket is dialled again")
)

// ping sends one ping unless one is already waiting for its answer.
func (f *Feed) ping(ctx context.Context, s Stream, k *keeper) error {
	if k.outstanding {
		return nil
	}
	if err := s.Ping(ctx); err != nil {
		return err
	}
	k.pinged()
	return nil
}

// hear files a frame. A socket that speaks is up, and a socket that has spoken
// is one worth reconnecting to at the first pace again.
func (f *Feed) hear(fr Frame, vouching bool) {
	f.retry.reset()
	f.mu.Lock()
	f.heard = f.clock.Now()
	f.mu.Unlock()
	f.set(func(st *State) {
		st.Up, st.Vouching = true, vouching
		if !fr.Pong {
			st.Version = fr.Version
		}
	})
}

// lastHeard is when the open socket last showed a sign of life.
func (f *Feed) lastHeard() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.heard
}

// redialNow asks the serving loop to end the socket and dial again; one waiting
// request is enough. The wait before the dial is the ladder's, so a burst of
// changes does not become a burst of dials.
func (f *Feed) redialNow() {
	select {
	case f.redial <- struct{}{}:
	default:
	}
}

// drainKick forgets a probe asked for while there was no socket to probe.
func (f *Feed) drainKick() { drain(f.kick) }

// drain empties a one-slot request channel.
func drain(c chan struct{}) {
	select {
	case <-c:
	default:
	}
}
