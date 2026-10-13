package collab

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func request(t *testing.T, s *Service, scope svc.Scope) bridge.Lock {
	t.Helper()
	l, err := s.Takeover(ctx, scope, bridge.TakeoverPartParams(ref))
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func TestTakeoverMatrix(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Service, *fakeClock, *recordedHub)
	}{
		{"no answer hands over at 30 seconds", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			l := request(t, s, member("raj"))
			if l.Holder != "mona" || l.Takeover == nil || l.Takeover.By != "raj" || l.Takeover.Until != stamp(c.Now().Add(takeoverTimeout)) {
				t.Fatalf("request = %#v", l)
			}
			if h.last(t, bridge.DocEventKindLock).Lock.Takeover == nil {
				t.Fatal("request not published")
			}
			c.advance(takeoverTimeout - time.Nanosecond)
			if !s.Holds(member("mona"), ref.Doc, ref.Part) {
				t.Fatal("handover too early")
			}
			c.advance(time.Nanosecond)
			if s.Holds(member("mona"), ref.Doc, ref.Part) || !s.Holds(member("raj"), ref.Doc, ref.Part) {
				t.Fatal("handover missing")
			}
			event := h.last(t, bridge.DocEventKindLock)
			if event.Lock.Holder != "raj" || event.Lock.Takeover != nil {
				t.Fatal("handover event wrong")
			}
		}},
		{"keep cancels handover", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			request(t, s, member("raj"))
			l, err := s.Answer(ctx, member("mona"), bridge.AnswerPartParams{Doc: ref.Doc, Part: ref.Part, Keep: true})
			if err != nil || l.Takeover != nil || l.Holder != "mona" {
				t.Fatalf("keep = %#v, %v", l, err)
			}
			c.advance(takeoverTimeout)
			if !s.Holds(member("mona"), ref.Doc, ref.Part) {
				t.Fatal("keep lost lock")
			}
			if h.events[1].Data.(bridge.DocEvent).Lock.Takeover == nil {
				t.Fatal("keep mutated earlier request event")
			}
		}},
		{"hand over immediately", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			request(t, s, member("raj"))
			l, err := s.Answer(ctx, member("mona"), bridge.AnswerPartParams{Doc: ref.Doc, Part: ref.Part})
			if err != nil || l.Holder != "raj" || l.Takeover != nil {
				t.Fatalf("answer = %#v, %v", l, err)
			}
			c.advance(takeoverTimeout)
			if !s.Holds(member("raj"), ref.Doc, ref.Part) {
				t.Fatal("old handover timer changed new lock")
			}
		}},
		{"non-holder answer refused", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			request(t, s, member("raj"))
			_, err := s.Answer(ctx, member("raj"), bridge.AnswerPartParams{Doc: ref.Doc, Part: ref.Part, Keep: true})
			code(t, err, bridge.CodeConflict)
			c.advance(takeoverTimeout)
			if !s.Holds(member("raj"), ref.Doc, ref.Part) {
				t.Fatal("non-holder cancelled request")
			}
		}},
		{"pending request conflicts", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			request(t, s, member("raj"))
			_, err := s.Takeover(ctx, member("santosh"), bridge.TakeoverPartParams(ref))
			code(t, err, bridge.CodeConflict)
			c.advance(takeoverTimeout)
			if !s.Holds(member("raj"), ref.Doc, ref.Part) {
				t.Fatal("asker replaced")
			}
		}},
		{"offline asker cancels request", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			request(t, s, member("raj"))
			s.Dropped("raj")
			c.advance(takeoverTimeout)
			if !s.Holds(member("mona"), ref.Doc, ref.Part) || h.last(t, bridge.DocEventKindLock).Lock.Takeover != nil {
				t.Fatal("offline asker received lock")
			}
		}},
		{"release cancels request", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			request(t, s, member("raj"))
			if _, err := s.Release(ctx, member("mona"), bridge.ReleasePartParams(ref)); err != nil {
				t.Fatal(err)
			}
			c.advance(takeoverTimeout)
			if len(s.locks) != 0 {
				t.Fatal("released request acquired a lock")
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { s, c, h := setup(t); take(t, s, member("mona")); test.run(t, s, c, h) })
	}
}

func TestAgentTakeoverStopsBeforeReturn(t *testing.T) {
	s, c, h := setup(t)
	agent := member("mona")
	agent.Caller.Agent = true
	l := take(t, s, agent)
	if l.Holder != "agent" || !l.Agent {
		t.Fatalf("agent lock = %#v", l)
	}
	calls := 0
	s.OnAgentStop(func(app, doc, part string) {
		calls++
		if app != "slides" || doc != ref.Doc || part != ref.Part {
			t.Fatalf("stop args = %s/%s/%s", app, doc, part)
		}
		if s.Holds(agent, doc, part) {
			t.Fatal("agent can write unfinished edit")
		}
		if !s.Holds(member("raj"), doc, part) {
			t.Fatal("asker has no lock during stop")
		}
	})
	l = request(t, s, member("raj"))
	if calls != 1 || l.Agent || l.Holder != "raj" || l.Takeover != nil {
		t.Fatalf("takeover = %#v, calls = %d", l, calls)
	}
	c.advance(takeoverTimeout)
	if calls != 1 || h.last(t, bridge.DocEventKindLock).Lock.Holder != "raj" {
		t.Fatal("agent stop repeated or lock not published")
	}
}

func TestLateTimersCannotExtendOwnership(t *testing.T) {
	tests := []struct {
		name    string
		pending bool
		elapsed time.Duration
		want    string
	}{
		{"idle", false, idleTimeout, ""}, {"takeover", true, takeoverTimeout, "raj"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, c, _ := setup(t)
			take(t, s, member("mona"))
			if test.pending {
				request(t, s, member("raj"))
			}
			c.mu.Lock()
			c.now = c.now.Add(test.elapsed)
			c.mu.Unlock()
			if s.Holds(member("mona"), ref.Doc, ref.Part) {
				t.Fatal("late timer extended old holder")
			}
			if test.want != "" && !s.Holds(member(test.want), ref.Doc, ref.Part) {
				t.Fatal("late takeover not applied")
			}
		})
	}
}
