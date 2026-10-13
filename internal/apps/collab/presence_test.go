package collab

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func looking(t *testing.T, s *Service, scope svc.Scope, part string) {
	t.Helper()
	if _, err := s.SetPresence(ctx, scope, bridge.SetPresenceParams{Doc: ref.Doc, Part: part}); err != nil {
		t.Fatal(err)
	}
}
func TestPresenceMatrix(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Service, *fakeClock, *recordedHub)
	}{
		{"viewer looks without a lock", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			viewer := member("mona")
			viewer.Caller.Role = svc.RoleViewer
			looking(t, s, viewer, ref.Part)
			if len(s.locks) != 0 || len(s.presence) != 1 {
				t.Fatal("looking took a lock or lost presence")
			}
			p := h.last(t, bridge.DocEventKindPresence).Presence
			if p.Member != "mona" || p.Doc != ref.Doc || p.Part != ref.Part || p.At != stamp(c.Now()) {
				t.Fatalf("presence = %#v", p)
			}
		}},
		{"expires after 60 seconds", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			looking(t, s, member("mona"), ref.Part)
			c.advance(presenceTimeout - time.Nanosecond)
			if len(s.presence) != 1 {
				t.Fatal("early presence expiry")
			}
			c.advance(time.Nanosecond)
			if len(s.presence) != 0 {
				t.Fatal("presence did not expire")
			}
			p := h.last(t, bridge.DocEventKindPresence).Presence
			if p.Member != "mona" || p.At != "" {
				t.Fatalf("withdrawal = %#v", p)
			}
		}},
		{"refresh replaces part and expiry", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			looking(t, s, member("mona"), ref.Part)
			c.advance(30 * time.Second)
			looking(t, s, member("mona"), "slides/7.json")
			c.advance(30 * time.Second)
			if len(s.presence) != 1 {
				t.Fatal("old timer removed refreshed presence")
			}
			if h.last(t, bridge.DocEventKindPresence).Presence.Part != "slides/7.json" {
				t.Fatal("part not updated")
			}
			c.advance(30 * time.Second)
			if len(s.presence) != 0 {
				t.Fatal("refreshed presence did not expire")
			}
		}},
		{"document presence has no part", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			looking(t, s, member("mona"), "")
			if h.last(t, bridge.DocEventKindPresence).Presence.Part != "" {
				t.Fatal("invented a part")
			}
		}},
		{"offline clears only that member", func(t *testing.T, s *Service, c *fakeClock, h *recordedHub) {
			looking(t, s, member("mona"), ref.Part)
			otherApp := member("mona")
			otherApp.App = "docs"
			looking(t, s, otherApp, "")
			looking(t, s, member("raj"), ref.Part)
			s.Dropped("mona")
			if len(s.presence) != 1 || s.presence[presenceKey{"slides", ref.Doc, "raj"}] == nil {
				t.Fatal("drop cleared wrong presence")
			}
			before := len(h.events)
			c.advance(presenceTimeout)
			if len(h.events) != before+1 {
				t.Fatal("dropped timers still publish")
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { s, c, h := setup(t); test.run(t, s, c, h) })
	}
}
