package pair

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// The rig for pairing a person's chats: one mailbox service in memory, two
// homes, and screens that a test can read and answer.

// screen is one device's UI. It records everything shown, and answers the
// question the way a test decided.
type screen struct {
	mu     sync.Mutex
	codes  chan *Code
	lines  []string
	burned int
	words  chan string
	asked  chan string
	// answer is what the person says; nil means yes.
	answer func(ctx context.Context, label, words string) bool
}

func newScreen() *screen {
	return &screen{codes: make(chan *Code, 8), words: make(chan string, 8), asked: make(chan string, 8)}
}

func (s *screen) Show(code *Code, lines string) {
	s.mu.Lock()
	s.lines = append(s.lines, lines)
	s.mu.Unlock()
	s.codes <- code
}

func (s *screen) Burned() {
	s.mu.Lock()
	s.burned++
	s.mu.Unlock()
}

func (s *screen) Waiting(words string) { s.words <- words }

func (s *screen) Ask(ctx context.Context, label, words string) bool {
	s.asked <- words
	if s.answer == nil {
		return true
	}
	return s.answer(ctx, label, words)
}

// nextCode is the code the screen shows next, or a failure after a while.
func (s *screen) nextCode(t *testing.T) *Code {
	t.Helper()
	select {
	case c := <-s.codes:
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("no code was shown")
		return nil
	}
}

// chatRig is a mailbox service and the two homes that pair through it.
type chatRig struct {
	t     *testing.T
	box   pairbox.Box
	route Mailbox
	homeA string
	homeB string
	a     identity.Identity
}

func newChatRig(t *testing.T) *chatRig {
	t.Helper()
	box := pairbox.NewMemory(pairbox.DefaultLimits, nil)
	r := &chatRig{t: t, box: box, route: Mailbox{Box: box, Host: "relay.test"}, homeA: t.TempDir(), homeB: t.TempDir()}
	id, err := identity.Ensure(r.homeA)
	if err != nil {
		t.Fatal(err)
	}
	r.a = id
	return r
}

// via is the same rig speaking through a different box, such as a hostile one.
func (r *chatRig) via(box pairbox.Box) *chatRig {
	c := *r
	c.box = box
	c.route = Mailbox{Box: box, Host: "relay.test"}
	return &c
}

// offer starts A in the background and answers what it ends with.
func (r *chatRig) offer(ctx context.Context, ui OfferUI) <-chan offerEnd {
	done := make(chan offerEnd, 1)
	go func() {
		label, err := Offer(ctx, r.route, Grant{Identity: r.a}, ui)
		done <- offerEnd{label, err}
	}()
	return done
}

type offerEnd struct {
	label string
	err   error
}

func (r *chatRig) join(ctx context.Context, home, typed string, ui JoinUI) (Joined, error) {
	return Join(ctx, r.route, Joining{Home: home, Label: "laptop"}, typed, ui)
}

func within(t *testing.T, done <-chan offerEnd) offerEnd {
	t.Helper()
	select {
	case end := <-done:
		return end
	case <-time.After(20 * time.Second):
		t.Fatal("the offering side did not finish")
		return offerEnd{}
	}
}
