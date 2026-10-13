package collab

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) SetPresence(ctx context.Context, scope svc.Scope, p bridge.SetPresenceParams) (bridge.Empty, error) {
	if err := s.validate(ctx, scope, p.Doc, p.Part, p.Part != ""); err != nil {
		return bridge.Empty{}, err
	}
	if err := s.begin(); err != nil {
		return bridge.Empty{}, err
	}
	defer s.mu.Unlock()
	key := presenceKey{scope.App, p.Doc, scope.Caller.Member}
	if old := s.presence[key]; old != nil {
		old.timer.Stop()
	}
	entry := &present{value: bridge.Presence{Doc: p.Doc, Part: p.Part, Member: scope.Caller.Member, At: stamp(s.clock.Now())}}
	s.presence[key] = entry
	entry.timer = s.clock.AfterFunc(presenceTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.presence[key] == entry {
			s.removePresence(key, entry)
		}
	})
	value := entry.value
	s.publish(key.app, key.doc, bridge.DocEvent{Kind: bridge.DocEventKindPresence, Part: value.Part, Presence: &value, By: key.member})
	return bridge.Empty{}, nil
}

func (s *Service) removePresence(key presenceKey, entry *present) {
	entry.timer.Stop()
	delete(s.presence, key)
	// An absent timestamp withdraws this member's presence without adding a wire field.
	value := bridge.Presence{Doc: key.doc, Member: key.member}
	s.publish(key.app, key.doc, bridge.DocEvent{Kind: bridge.DocEventKindPresence, Presence: &value, By: key.member})
}
