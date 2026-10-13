package collab

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var ctx = context.Background()
var ref = bridge.LockPartParams{Doc: "decks/q4", Part: "slides/5.json"}

func member(name string) svc.Scope {
	return svc.Scope{App: "slides", Data: svc.DataReal, Caller: svc.Caller{Member: name, Role: svc.RoleEditor}}
}

type fakeTimer struct {
	clock   *fakeClock
	at      time.Time
	call    func()
	stopped bool
}

func (timer *fakeTimer) Stop() bool {
	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	active := !timer.stopped
	timer.stopped = true
	return active
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func (clock *fakeClock) Now() time.Time { clock.mu.Lock(); defer clock.mu.Unlock(); return clock.now }
func (clock *fakeClock) AfterFunc(d time.Duration, call func()) Timer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	timer := &fakeTimer{clock: clock, at: clock.now.Add(d), call: call}
	clock.timers = append(clock.timers, timer)
	return timer
}
func (clock *fakeClock) advance(d time.Duration) {
	target := clock.Now().Add(d)
	for {
		clock.mu.Lock()
		var next *fakeTimer
		for _, timer := range clock.timers {
			if !timer.stopped && !timer.at.After(target) && (next == nil || timer.at.Before(next.at)) {
				next = timer
			}
		}
		if next == nil {
			clock.now = target
			clock.mu.Unlock()
			return
		}
		clock.now = next.at
		next.stopped = true
		clock.mu.Unlock()
		next.call()
	}
}

type recordedHub struct {
	mu     sync.Mutex
	events []svc.Event
}

func (hub *recordedHub) Publish(topic string, data any) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.events = append(hub.events, svc.Event{Topic: topic, Data: data})
}
func (*recordedHub) Subscribe(string) (<-chan svc.Event, func()) { return nil, func() {} }
func (hub *recordedHub) last(t *testing.T, kind bridge.DocEventKind) bridge.DocEvent {
	t.Helper()
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.events) == 0 {
		t.Fatal("no published event")
	}
	event := hub.events[len(hub.events)-1]
	if event.Topic != bridge.TopicDoc("slides", ref.Doc) {
		t.Fatalf("topic = %q", event.Topic)
	}
	value, ok := event.Data.(bridge.DocEvent)
	if !ok || value.Kind != kind {
		t.Fatalf("event = %#v", event.Data)
	}
	return value
}
func setup(t *testing.T) (*Service, *fakeClock, *recordedHub) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	hub := &recordedHub{}
	service := NewWithClock(svc.Config{Root: t.TempDir()}, &svc.Services{Hub: hub}, clock)
	t.Cleanup(service.Close)
	return service, clock, hub
}
func take(t *testing.T, service *Service, scope svc.Scope) bridge.Lock {
	t.Helper()
	lock, err := service.Lock(ctx, scope, ref)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}
func code(t *testing.T, err error, want bridge.ErrorCode) {
	t.Helper()
	var wire *bridge.Error
	if !errors.As(err, &wire) || wire.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func TestLockMatrix(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Service, *fakeClock, *recordedHub)
	}{
		{"take", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			l := take(t, s, member("mona"))
			if l.Holder != "mona" || l.Agent || l.Doc != ref.Doc || l.Part != ref.Part || l.Since != stamp(c.Now()) || l.IdleUntil != stamp(c.Now().Add(idleTimeout)) {
				t.Fatalf("lock = %#v", l)
			}
			if h.last(t, bridge.DocEventKindLock).Lock.Holder != "mona" {
				t.Fatal("holder not published")
			}
		}},
		{"conflict carries holder", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			l, err := s.Lock(ctx, member("raj"), ref)
			code(t, err, bridge.CodeConflict)
			if l.Holder != "mona" || err.(*bridge.Error).Details["holder"] != "mona" {
				t.Fatalf("lock=%#v, error=%v", l, err)
			}
			if len(h.events) != 1 {
				t.Fatal("conflict changed state")
			}
		}},
		{"lock refresh", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			first := take(t, s, member("mona"))
			c.advance(time.Minute)
			refreshed := take(t, s, member("mona"))
			if refreshed.Since != first.Since || refreshed.IdleUntil != stamp(c.Now().Add(idleTimeout)) {
				t.Fatalf("refreshed = %#v", refreshed)
			}
			c.advance(time.Minute)
			if len(s.locks) != 1 {
				t.Fatal("old idle timer removed refreshed lock")
			}
			c.advance(time.Minute)
			if len(s.locks) != 0 || h.last(t, bridge.DocEventKindLock).Lock != nil {
				t.Fatal("refreshed lock did not expire")
			}
		}},
		{"Holds refresh", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			c.advance(time.Minute)
			if !s.Holds(member("mona"), ref.Doc, ref.Part) {
				t.Fatal("holder refused")
			}
			c.advance(time.Minute)
			if len(s.locks) != 1 {
				t.Fatal("Holds did not refresh")
			}
			c.advance(time.Minute)
			if len(s.locks) != 0 {
				t.Fatal("Holds refresh never expired")
			}
		}},
		{"non-holder cannot refresh", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			c.advance(time.Minute)
			if s.Holds(member("raj"), ref.Doc, ref.Part) {
				t.Fatal("non-holder holds")
			}
			c.advance(time.Minute)
			if len(s.locks) != 0 {
				t.Fatal("non-holder refreshed")
			}
		}},
		{"idle expiry", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			c.advance(idleTimeout - time.Nanosecond)
			if len(s.locks) != 1 {
				t.Fatal("expired before deadline")
			}
			c.advance(time.Nanosecond)
			if s.Holds(member("mona"), ref.Doc, ref.Part) || h.last(t, bridge.DocEventKindLock).Lock != nil {
				t.Fatal("expired lock retained")
			}
			take(t, s, member("raj"))
		}},
		{"release holder", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			if _, err := s.Release(ctx, member("mona"), bridge.ReleasePartParams(ref)); err != nil {
				t.Fatal(err)
			}
			if s.Holds(member("mona"), ref.Doc, ref.Part) || h.last(t, bridge.DocEventKindLock).Lock != nil {
				t.Fatal("released lock retained")
			}
			c.advance(idleTimeout)
			if len(h.events) != 2 {
				t.Fatal("released timer still publishes")
			}
		}},
		{"release non-holder refused", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			_, err := s.Release(ctx, member("raj"), bridge.ReleasePartParams(ref))
			code(t, err, bridge.CodeConflict)
			if len(s.locks) != 1 || len(h.events) != 1 {
				t.Fatal("non-holder release changed state")
			}
		}},
		{"offline drop all apps and parts", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			take(t, s, member("mona"))
			second := member("mona")
			second.App = "docs"
			second.Caller.Agent = true
			if _, err := s.Lock(ctx, second, ref); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Lock(ctx, member("raj"), bridge.LockPartParams{Doc: ref.Doc, Part: "slides/7.json"}); err != nil {
				t.Fatal(err)
			}
			s.Dropped("mona")
			if len(s.locks) != 1 || !s.Holds(member("raj"), ref.Doc, "slides/7.json") {
				t.Fatal("drop did not release exactly Mona's locks")
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { s, c, h := setup(t); test.run(t, s, c, h) })
	}
}

func TestLockKeyIsolation(t *testing.T) {
	s, _, _ := setup(t)
	take(t, s, member("mona"))
	tests := []struct{ app, doc, part string }{{"docs", ref.Doc, ref.Part}, {"slides", "decks/other", ref.Part}, {"slides", ref.Doc, "slides/7.json"}}
	for _, test := range tests {
		t.Run(test.app+"/"+test.doc+"/"+test.part, func(t *testing.T) {
			scope := member("raj")
			scope.App = test.app
			if _, err := s.Lock(ctx, scope, bridge.LockPartParams{Doc: test.doc, Part: test.part}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if len(s.locks) != 4 {
		t.Fatal("keys collapsed")
	}
}

func TestConcurrentLockHasOneHolder(t *testing.T) {
	s, _, _ := setup(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, name := range []string{"mona", "raj"} {
		go func(name string) { <-start; _, err := s.Lock(ctx, member(name), ref); results <- err }(name)
	}
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("results = %v, %v", first, second)
	}
	if first != nil {
		code(t, first, bridge.CodeConflict)
	} else {
		code(t, second, bridge.CodeConflict)
	}
}

func TestClosedServiceCancelsAllDeadlines(t *testing.T) {
	s, c, h := setup(t)
	take(t, s, member("mona"))
	request(t, s, member("raj"))
	looking(t, s, member("raj"), ref.Part)
	count := len(h.events)
	s.Close()
	c.advance(3 * time.Minute)
	if len(s.locks) != 0 || len(s.presence) != 0 || len(h.events) != count {
		t.Fatal("closed service retained state or published a timer event")
	}
	_, err := s.Lock(ctx, member("mona"), ref)
	code(t, err, bridge.CodeConflict)
	_, err = s.Takeover(ctx, member("raj"), bridge.TakeoverPartParams(ref))
	code(t, err, bridge.CodeConflict)
	_, err = s.SetPresence(ctx, member("raj"), bridge.SetPresenceParams{Doc: ref.Doc})
	code(t, err, bridge.CodeConflict)
}

func TestRoleAndAddressGuards(t *testing.T) {
	tests := []struct {
		name  string
		scope svc.Scope
		part  bridge.LockPartParams
		want  bridge.ErrorCode
	}{
		{"viewer", func() svc.Scope { s := member("mona"); s.Caller.Role = svc.RoleViewer; return s }(), ref, bridge.CodeDenied},
		{"missing member", member(""), ref, bridge.CodeInvalid},
		{"app traversal", func() svc.Scope { s := member("mona"); s.App = "../slides"; return s }(), ref, bridge.CodeInvalid},
		{"doc traversal", member("mona"), bridge.LockPartParams{Doc: "../decks", Part: ref.Part}, bridge.CodeInvalid},
		{"part traversal", member("mona"), bridge.LockPartParams{Doc: ref.Doc, Part: "../slide.json"}, bridge.CodeInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _, _ := setup(t)
			_, err := s.Lock(ctx, test.scope, test.part)
			code(t, err, test.want)
			_, err = s.Takeover(ctx, test.scope, bridge.TakeoverPartParams(test.part))
			code(t, err, test.want)
			if len(s.locks) != 0 {
				t.Fatal("invalid caller acquired a lock")
			}
		})
	}
	user := member("mona")
	user.Caller.Role = svc.RoleUser
	s, _, _ := setup(t)
	take(t, s, user)
}
