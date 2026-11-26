package filesdoor

import (
	"context"
	"crypto/ecdh"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// C15: with two codeaf processes serving one home, only the one holding the lock
// registers. The other makes no request to the relay while it waits, and takes
// over within a few seconds of the holder ending, which is a take on another
// computer that is in progress during the handover still succeeding inside its
// retry window.
func TestFilesDoorOnlyTheProcessHoldingTheLockRegistersAndTheNextTakesOver(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	frame, objects := w.frameOf(1, 100)
	putFrame(t, disk, frame)
	cfg := w.config(wrap(t, disk))

	first := w.serve(cfg)
	engine, dial := w.relay.engine.Load(), w.relay.dial.Load()

	// Two siblings wait on the same lock. Two Serve calls in one process are a
	// fair stand-in for two processes: a lock belongs to the open file, so a
	// second open of the same path conflicts.
	second := w.serveNoWait(cfg)
	third := w.serveNoWait(cfg)
	time.Sleep(500 * time.Millisecond) // about twenty-five polls each
	if w.relay.engine.Load() != engine || w.relay.dial.Load() != dial {
		t.Fatalf("a process that does not hold the lock made relay requests (%d registrations, %d dials)",
			w.relay.engine.Load()-engine, w.relay.dial.Load()-dial)
	}
	if second.regs.Load() != 0 || third.regs.Load() != 0 {
		t.Fatal("a process that does not hold the lock registered")
	}
	// A sibling that is told to stop while it waits stops, and still asks for nothing.
	third.stop()
	if w.relay.engine.Load() != engine {
		t.Fatal("a waiting process registered while stopping")
	}

	// The holder ends. A taker starts at that moment and has its retry window.
	ended := time.Now()
	first.stop()
	source := w.source()
	got, err := source.Get(context.Background(), objects[0].RID)
	if err != nil {
		t.Fatalf("a take during the handover failed: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("the object that arrived is %d bytes", len(got))
	}
	if took := time.Since(ended); took > 5*time.Second {
		t.Fatalf("the handover took %v, want about a second or two", took)
	}
	select {
	case <-second.ready:
	case <-time.After(time.Second):
		t.Fatal("the waiting process was not registered")
	}
	if second.regs.Load() != 1 {
		t.Fatalf("%d registrations by the process that took over", second.regs.Load())
	}

	// The lock is released when Serve returns, however the holder ended, so the
	// next waiter is served as quickly again.
	second.stop()
	fourth := w.serveNoWait(cfg)
	select {
	case <-fourth.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the lock was not released when its holder returned")
	}
}

// The door's name is held by one process, so a sibling that is not holding the
// lock must also not hold the relay's name, and the process that was
// holding the lock leaves nothing registered behind it.
func TestFilesDoorLeavesNoRegistrationBehindWhenItStops(t *testing.T) {
	w := newWorld(t)
	srv := w.serve(w.config(wrap(t, newDisk(t))))
	srv.stop()
	source := NewSource(SourceConfig{
		Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
		RetryWithin: time.Second, Sleep: func(context.Context, time.Duration) error { return nil },
	})
	defer source.Close()
	if _, err := source.Has(context.Background(), []string{rid(1)}); !errors.Is(err, ErrSourceOff) {
		t.Fatalf("Has after the door stopped = %v", err)
	}
}

// failingRegistrar drives the registration loop with a clock it owns, so the
// pacing of attempts can be measured in minutes without waiting a second.
type failingRegistrar struct {
	clock    time.Time
	attempts []time.Time
	waits    []time.Duration
	said     []string
	readied  int
}

func (f *failingRegistrar) loop(register func(n int) error, hold func(), stopAfterWaits int) (*registrar, context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	return &registrar{
		service: "http://relay.invalid",
		register: func(context.Context, string, *ecdh.PrivateKey) (*relay.Registration, error) {
			f.attempts = append(f.attempts, f.clock)
			if err := register(len(f.attempts)); err != nil {
				return nil, err
			}
			return &relay.Registration{}, nil
		},
		hold:  func(context.Context, *relay.Registration) { hold() },
		ready: func() { f.readied++ },
		say:   func(line string) { f.said = append(f.said, line) },
		now:   func() time.Time { return f.clock },
		wait: func(ctx context.Context, d time.Duration) error {
			f.waits = append(f.waits, d)
			f.clock = f.clock.Add(d)
			if len(f.waits) >= stopAfterWaits {
				cancel()
				return ctx.Err()
			}
			return nil
		},
	}, ctx, cancel
}

// C15: the registration never gives up while the process lives, whatever the relay
// says, and in a steady failure makes at most six attempts in any minute, which
// is what the hosted relay's ten a minute per network leaves room for.
func TestFilesDoorRegistrationNeverGivesUpAndMakesAtMostSixAttemptsAMinute(t *testing.T) {
	failures := []error{
		relay.ErrNameTaken, relay.ErrTooMany, relay.ErrUnreachable, relay.ErrNoDoor,
		errors.New("the relay refused this machine"),
	}
	f := &failingRegistrar{clock: time.Unix(0, 0)}
	r, ctx, _ := f.loop(func(n int) error { return failures[n%len(failures)] }, func() { t.Error("held a registration that never happened") }, 60)
	done := make(chan struct{})
	go func() { r.run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not return when its context ended")
	}
	if len(f.waits) != 60 {
		t.Fatalf("the loop stopped after %d waits, before its context ended", len(f.waits))
	}
	// It starts at a second and doubles up to its ceiling.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	if !reflect.DeepEqual(f.waits[:len(want)], want) {
		t.Fatalf("waits begin %v, want %v", f.waits[:len(want)], want)
	}
	// In steady failure no minute holds more than six attempts.
	steady := f.attempts[len(want):]
	for i, start := range steady {
		n := 0
		for _, at := range steady[i:] {
			if at.Sub(start) < time.Minute {
				n++
			}
		}
		if n > 6 {
			t.Fatalf("%d attempts in the minute from attempt %d", n, i)
		}
	}
	if f.readied != 0 {
		t.Fatal("ready was called without a registration")
	}
}

// C15: a registration that stayed up for a minute starts the backoff over, and
// one that dropped straight after it was accepted does not.
func TestFilesDoorRegistrationBackoffStartsOverOnlyAfterAStableMinute(t *testing.T) {
	for _, c := range []struct {
		name string
		held time.Duration
		want []time.Duration
	}{
		{"stayed up", 90 * time.Second, []time.Duration{time.Second, 2 * time.Second, time.Second}},
		{"dropped at once", 5 * time.Second, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &failingRegistrar{clock: time.Unix(0, 0)}
			r, ctx, _ := f.loop(
				func(n int) error {
					if n <= 2 {
						return relay.ErrUnreachable
					}
					return nil
				},
				func() { f.clock = f.clock.Add(c.held) },
				3,
			)
			r.run(ctx)
			if !reflect.DeepEqual(f.waits, c.want) {
				t.Fatalf("waits %v, want %v", f.waits, c.want)
			}
			if f.readied != 1 {
				t.Fatalf("ready was called %d times for one registration", f.readied)
			}
		})
	}
}

// The door says something only when it lost the sync service after having had
// it, and says the other thing when it is back, once each per spell.
func TestFilesDoorSaysOnlyWhenTheSyncServiceIsLostAndWhenItIsBack(t *testing.T) {
	f := &failingRegistrar{clock: time.Unix(0, 0)}
	r, ctx, _ := f.loop(func(n int) error {
		switch {
		case n == 1: // first start-up attempts that fail are not a loss
			return relay.ErrUnreachable
		case n == 2:
			return nil
		case n <= 5:
			return relay.ErrUnreachable
		}
		return nil
	}, func() {}, 7)
	r.run(ctx)
	if want := []string{LostLine, BackLine}; !reflect.DeepEqual(f.said, want) {
		t.Fatalf("said %q, want %q", f.said, want)
	}
}

// A request that finds the tunnel broken dials again ONCE, through the same
// retry as the first dial, and sends it again: a door that changed process
// between two requests of a take is the case. If it is not there after that, the
// computer is off.
func TestFilesDoorRedialsOnceWhenTheTunnelBreaks(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	frame, objects := w.frameOf(1, 100)
	putFrame(t, disk, frame)
	cfg := w.config(wrap(t, disk))
	first := w.serve(cfg)

	source := NewSource(SourceConfig{
		Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
		RetryWithin: 2 * time.Second, Sleep: func(context.Context, time.Duration) error { return nil },
	})
	defer source.Close()
	ctx := context.Background()
	if _, err := source.Get(ctx, objects[0].RID); err != nil {
		t.Fatal(err)
	}
	if got := source.Stats().Dials; got != 1 {
		t.Fatalf("%d dials for the first request", got)
	}

	// The process holding the door ends and another one takes it.
	first.stop()
	second := w.serve(cfg)
	if _, err := source.Get(ctx, objects[0].RID); err != nil {
		t.Fatalf("the request that found the tunnel broken failed: %v", err)
	}
	if got := source.Stats().Dials; got != 2 {
		t.Fatalf("%d dials after the door changed process, want 2", got)
	}
	if w.relay.dial.Load() != 2 {
		t.Fatalf("%d dials reached the relay", w.relay.dial.Load())
	}

	// And a door that does not come back is a computer that is off.
	second.stop()
	_, err := source.Get(ctx, objects[0].RID)
	if !errors.Is(err, ErrSourceOff) {
		t.Fatalf("a request with no door left = %v, want a source that is off", err)
	}
}
