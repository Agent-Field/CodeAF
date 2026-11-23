package taskdoor_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers map[*manualTicker]bool
	created chan struct{}
}
type manualTicker struct {
	clock *manualClock
	every time.Duration
	next  time.Time
	ch    chan time.Time
}

func newClock() *manualClock {
	return &manualClock{now: time.Now(), tickers: map[*manualTicker]bool{}, created: make(chan struct{}, 64)}
}
func (c *manualClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *manualClock) NewTicker(d time.Duration) taskdoor.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTicker{clock: c, every: d, next: c.now.Add(d), ch: make(chan time.Time, 64)}
	c.tickers[t] = true
	c.created <- struct{}{}
	return t
}
func (t *manualTicker) C() <-chan time.Time { return t.ch }
func (t *manualTicker) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	delete(t.clock.tickers, t)
}
func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for ticker := range c.tickers {
		for !ticker.next.After(c.now) {
			ticker.ch <- ticker.next
			ticker.next = ticker.next.Add(ticker.every)
		}
	}
}
func (c *manualClock) awaitTicker(t *testing.T) {
	t.Helper()
	select {
	case <-c.created:
	case <-time.After(5 * time.Second):
		t.Fatal("watch never enabled keepalive")
	}
}

// Contract 9's watch boundary must detect a silent peer after 45 seconds and
// send its own probes at 15 and 30, without waiting for wall-clock time.
func TestASilentWatchIsClosedAfterFortyFiveSeconds(t *testing.T) {
	r := newRig(t)
	clock := newClock()
	config := r.config()
	config.Clock = clock
	r.executor.run = func(ctx context.Context, _ *taskdoor.Attempt, _ taskdoor.Journal) error {
		<-ctx.Done()
		return ctx.Err()
	}
	r.serve(config)
	control := r.dial()
	id := attempt(20)
	send(t, control, id, request())
	receive(t, control, "started")
	key, err := taskdoor.DoorKey(r.id, r.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	tunnel := rawTunnel(t, r, key)
	rawHello(t, tunnel, r.a)
	rawSend(t, tunnel, id, taskdoor.Watch{})
	clock.awaitTicker(t)
	for i := 0; i < 2; i++ {
		clock.advance(15 * time.Second)
		rawReceive(t, tunnel, "ping")
	}
	closed := make(chan error, 1)
	go func() { _, err := taskdoor.ReadFrame(tunnel); closed <- err }()
	clock.advance(14 * time.Second)
	select {
	case err := <-closed:
		t.Fatalf("watch closed before 45 seconds: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	clock.advance(time.Second)
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("silent watch sent another frame instead of closing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("silent watch stayed open after 45 seconds")
	}
	if r.executor.calls.Load() != 1 {
		t.Fatal("losing the watch restarted the attempt")
	}
}

// Contract 13 allows a bounded roster cache, never a stale roster extended
// beyond sixty seconds after another computer was removed.
func TestARosterRefreshTurnsAwayAComputerWithinSixtySeconds(t *testing.T) {
	r := newRig(t)
	clock := newClock()
	config := r.config()
	config.Clock = clock
	r.serve(config)
	c := r.dial()
	_ = c.Close()
	r.rosterMu.Lock()
	r.active[r.a.ID()] = false
	r.rosterMu.Unlock()
	clock.advance(59 * time.Second)
	c = r.dial()
	_ = c.Close()
	clock.advance(time.Second)
	c, err := taskdoor.Dial(context.Background(), r.service, r.id, r.a, r.b.ID(), "work computer")
	if c != nil {
		_ = c.Close()
	}
	if err == nil || err.Error() != "this computer was removed from your devices" {
		t.Fatalf("stale roster allowed a removed computer: %v", err)
	}
}

// Contract 9 measures silence from any received frame, rather than rounding a
// fresh arrival to the next fifteen-second probe interval.
func TestEveryWatchFrameRestartsTheFortyFiveSecondSilenceLimit(t *testing.T) {
	r := newRig(t)
	clock := newClock()
	config := r.config()
	config.Clock = clock
	r.executor.run = func(ctx context.Context, _ *taskdoor.Attempt, _ taskdoor.Journal) error {
		<-ctx.Done()
		return ctx.Err()
	}
	r.serve(config)
	control := r.dial()
	id := attempt(21)
	send(t, control, id, request())
	receive(t, control, "started")
	key, err := taskdoor.DoorKey(r.id, r.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	tunnel := rawTunnel(t, r, key)
	rawHello(t, tunnel, r.a)
	rawSend(t, tunnel, id, taskdoor.Watch{})
	clock.awaitTicker(t)
	clock.advance(7 * time.Second)
	rawSend(t, tunnel, "", taskdoor.Ping{})
	rawReceive(t, tunnel, "pong")
	closed := make(chan error, 1)
	go func() {
		for {
			_, err := taskdoor.ReadFrame(tunnel)
			if err != nil {
				closed <- err
				return
			}
		}
	}()
	clock.advance(38 * time.Second)
	select {
	case err := <-closed:
		t.Fatalf("closed only 38 seconds after a frame: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	clock.advance(7 * time.Second)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("silence timeout was rounded beyond 45 seconds since the last frame")
	}
}

// Contract 9 applies the same probes and silence limit at the origin; a
// responder that stops producing frames must not leave the origin waiting.
func TestTheOriginAlsoProbesAndClosesASilentWatch(t *testing.T) {
	r := newRig(t)
	clock := newClock()
	key, err := taskdoor.DoorKey(r.id, r.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registration, err := relay.Register(ctx, r.service, key)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	frames := make(chan string, 8)
	failed := make(chan error, 1)
	go func() {
		stream, err := registration.Accept()
		if err != nil {
			failed <- err
			return
		}
		defer stream.Close()
		tunnel, err := pair.AcceptWithKey(stream, key)
		if err != nil {
			failed <- err
			return
		}
		if _, err := taskdoor.ReadFrame(tunnel); err != nil {
			failed <- err
			return
		}
		hello, err := taskdoor.NewFrame("", taskdoor.Hello{V: 1, Cert: r.b.Cert, Sig: r.b.Sign(tunnel.ChannelBinding())})
		if err != nil {
			failed <- err
			return
		}
		if err := taskdoor.WriteFrame(tunnel, hello); err != nil {
			failed <- err
			return
		}
		for {
			f, err := taskdoor.ReadFrame(tunnel)
			if err != nil {
				return
			}
			frames <- f.Type
		}
	}()
	conn, err := taskdoor.DialWithClock(ctx, r.service, r.id, r.a, r.b.ID(), "work computer", clock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	send(t, conn, attempt(22), taskdoor.Watch{})
	next := func(want string) {
		t.Helper()
		select {
		case got := <-frames:
			if got != want {
				t.Fatalf("origin sent %s, want %s", got, want)
			}
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(5 * time.Second):
			t.Fatalf("origin did not send %s", want)
		}
	}
	next("watch")
	for n := 0; n < 2; n++ {
		clock.advance(15 * time.Second)
		next("ping")
	}
	clock.advance(15 * time.Second)
	closed := make(chan error, 1)
	go func() { _, err := conn.Receive(); closed <- err }()
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("origin returned a task event from a silent peer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("origin did not close its silent watch")
	}
}
