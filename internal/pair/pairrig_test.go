package pair

// Small pieces every edge-case test of the chat pairing shares, on top of the
// rig in chatrig_test.go: a rig whose relay is hostile, an offering side that is
// always stopped when its test ends, and the checks for "nothing was written".

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/pairbox/pairboxtest"
)

// roomy limits keep the per-network budgets out of the way of tests that pair
// many times through one service.
func roomy() pairbox.Limits {
	l := pairbox.DefaultLimits
	l.CreatePerHour, l.WritePerMinute = 100000, 100000
	return l
}

// hostileRig is the chat rig speaking through a hostile relay over a fresh
// service.
func hostileRig(t *testing.T) (*chatRig, *hostileBox) {
	t.Helper()
	h := newHostile(pairbox.NewMemory(roomy(), nil))
	return newChatRig(t).via(h), h
}

// clockRig is the chat rig over a service whose clock a test moves.
func clockRig(t *testing.T) (*chatRig, *pairboxtest.FakeClock) {
	t.Helper()
	clock := pairboxtest.NewFakeClock()
	return newChatRig(t).via(pairbox.NewMemory(roomy(), clock.Now)), clock
}

// bounded is a context that ends by itself, so a test that waits for something
// that never comes fails instead of hanging.
func bounded(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), d)
	t.Cleanup(stop)
	return ctx
}

// ── the offering side ───────────────────────────────────────────────────────

// running is a device that is offering a code.
type running struct {
	stop context.CancelFunc
	done chan offerEnd
	once sync.Once
	end  offerEnd
}

// startOffer runs the offering side in the background. However the test ends,
// the offer is stopped and waited for, because the offering guard is one flag
// for the whole process and a leaked offer would fail the next test.
func startOffer(t *testing.T, r *chatRig, parent context.Context, grant Grant, ui OfferUI) *running {
	t.Helper()
	ctx, stop := context.WithCancel(parent)
	o := &running{stop: stop, done: make(chan offerEnd, 1)}
	go func() {
		label, err := Offer(ctx, r.route, grant, ui)
		o.done <- offerEnd{label, err}
	}()
	t.Cleanup(func() { stop(); o.wait(t) })
	return o
}

// offerAs is the usual offering side: A's identity, no relay to name.
func offerAs(t *testing.T, r *chatRig, ui OfferUI) *running {
	t.Helper()
	return startOffer(t, r, context.Background(), Grant{Identity: r.a}, ui)
}

// wait is how the offering side ended.
func (o *running) wait(t *testing.T) offerEnd {
	t.Helper()
	o.once.Do(func() {
		select {
		case o.end = <-o.done:
		case <-time.After(20 * time.Second):
			t.Error("the offering side did not finish")
		}
	})
	return o.end
}

// ── screens ─────────────────────────────────────────────────────────────────

// burnWatch is a screen that can look at the mailbox at the instant a code burns,
// which is after the old mailbox was deleted and before the new one is made.
type burnWatch struct {
	*screen
	mu     sync.Mutex
	onBurn func()
}

func (b *burnWatch) Burned() {
	b.screen.Burned()
	b.mu.Lock()
	f := b.onBurn
	b.mu.Unlock()
	if f != nil {
		f()
	}
}

func (b *burnWatch) whenBurned(f func()) { b.mu.Lock(); b.onBurn = f; b.mu.Unlock() }

// burns is how many codes a screen was told had burned.
func burns(s *screen) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.burned
}

// countingJoin is a joining screen that counts how often it was shown words.
type countingJoin struct {
	n     atomic.Int32
	words chan string
}

func newCountingJoin() *countingJoin { return &countingJoin{words: make(chan string, 4)} }

func (c *countingJoin) Waiting(words string) { c.n.Add(1); c.words <- words }

// gone is whether a mailbox no longer answers.
func gone(box pairbox.Box, plate string) bool {
	_, err := box.Poll(context.Background(), plate, pairbox.SideB, 0, 0)
	return errors.Is(err, pairbox.ErrGone)
}

// ── what a code looks like when typed wrong ─────────────────────────────────

// typedWith is the code as the joining device types it, with other digits.
func typedWith(code *Code, digits string) string {
	return code.Plate() + "-" + digits[:3] + "-" + digits[3:]
}

// otherDigits are six digits that are not the code's.
func otherDigits(code *Code) string {
	var n int
	_, _ = fmt.Sscanf(code.secret(), "%d", &n)
	return fmt.Sprintf("%06d", (n+1)%1_000_000)
}

// differs is whether two codes are not the same code.
func differs(a, b *Code) bool { return a.Shown() != b.Shown() }

// ── a home is untouched ─────────────────────────────────────────────────────

// untouched fails the test when a home holds anything at all.
func untouched(t *testing.T, home string) {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("%s was written to this home", e.Name())
	}
}

// filesIn is every file under a home and its bytes.
func filesIn(t *testing.T, home string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		out[path] = raw
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// same fails the test naming the files that differ, and never their contents.
func same(t *testing.T, before, after map[string][]byte) {
	t.Helper()
	for name, was := range before {
		if now, ok := after[name]; !ok || !bytes.Equal(was, now) {
			t.Errorf("%s changed", filepath.Base(name))
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("%s appeared", filepath.Base(name))
		}
	}
}

// joinEnd is how a joining device ended.
type joinEnd struct {
	joined Joined
	err    error
}

// joinInBackground runs the joining side and answers what it ends with.
func joinInBackground(ctx context.Context, r *chatRig, home, typed string, ui JoinUI) <-chan joinEnd {
	done := make(chan joinEnd, 1)
	go func() {
		joined, err := r.join(ctx, home, typed, ui)
		done <- joinEnd{joined, err}
	}()
	return done
}

// joinWithin waits for a joining device to end.
func joinWithin(t *testing.T, done <-chan joinEnd) joinEnd {
	t.Helper()
	select {
	case end := <-done:
		return end
	case <-time.After(20 * time.Second):
		t.Fatal("the joining side did not finish")
		return joinEnd{}
	}
}
