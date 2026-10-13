package collab

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) Lock(ctx context.Context, scope svc.Scope, p bridge.LockPartParams) (bridge.Lock, error) {
	if err := s.validate(ctx, scope, p.Doc, p.Part, true); err != nil {
		return bridge.Lock{}, err
	}
	if err := writable(scope); err != nil {
		return bridge.Lock{}, err
	}
	if err := s.begin(); err != nil {
		return bridge.Lock{}, err
	}
	defer s.mu.Unlock()
	key := partKey{scope.App, p.Doc, p.Part}
	l := s.current(key)
	if l != nil && !sameHolder(l.owner, scope.Caller) {
		return snapshot(l), conflict(l)
	}
	if l == nil {
		l = s.acquire(key, scope.Caller)
	} else {
		s.refresh(key, l)
		s.publishLock(key, l, holder(scope.Caller))
	}
	return snapshot(l), nil
}

func (s *Service) acquire(key partKey, caller svc.Caller) *heldLock {
	l := &heldLock{owner: caller, value: bridge.Lock{Doc: key.doc, Part: key.part, Holder: holder(caller), Agent: caller.Agent, Since: stamp(s.clock.Now())}}
	s.locks[key] = l
	s.refresh(key, l)
	s.publishLock(key, l, holder(caller))
	return l
}

func (s *Service) refresh(key partKey, l *heldLock) {
	if l.idle != nil {
		l.idle.Stop()
	}
	until := s.clock.Now().Add(idleTimeout)
	l.value.IdleUntil = stamp(until)
	l.idle = s.clock.AfterFunc(idleTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.locks[key] == l && l.value.IdleUntil == stamp(until) {
			s.current(key)
		}
	})
}

// current also checks deadlines because a timer can run after an incoming call.
func (s *Service) current(key partKey) *heldLock {
	l := s.locks[key]
	if l == nil || s.closed {
		return nil
	}
	now := s.clock.Now()
	if l.value.Takeover != nil && due(l.value.Takeover.Until, now) {
		asker := l.asker
		s.removeLock(key)
		return s.acquire(key, asker)
	}
	if due(l.value.IdleUntil, now) {
		s.removeLock(key)
		s.publishLock(key, nil, l.value.Holder)
		return nil
	}
	return l
}
func due(deadline string, now time.Time) bool {
	at, _ := time.Parse(time.RFC3339Nano, deadline)
	return !now.Before(at)
}
func (s *Service) removeLock(key partKey) {
	l := s.locks[key]
	if l == nil {
		return
	}
	if l.idle != nil {
		l.idle.Stop()
	}
	if l.answer != nil {
		l.answer.Stop()
	}
	delete(s.locks, key)
}

func (s *Service) Holds(scope svc.Scope, doc, part string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := partKey{scope.App, doc, part}
	l := s.current(key)
	if l == nil || !sameHolder(l.owner, scope.Caller) {
		return false
	}
	s.refresh(key, l)
	s.publishLock(key, l, holder(scope.Caller))
	return true
}

func (s *Service) Release(ctx context.Context, scope svc.Scope, p bridge.ReleasePartParams) (bridge.Empty, error) {
	if err := s.validate(ctx, scope, p.Doc, p.Part, true); err != nil {
		return bridge.Empty{}, err
	}
	if err := s.begin(); err != nil {
		return bridge.Empty{}, err
	}
	defer s.mu.Unlock()
	key := partKey{scope.App, p.Doc, p.Part}
	l := s.current(key)
	if l == nil {
		return bridge.Empty{}, bridge.Errorf(bridge.CodeNotFound, "part has no lock")
	}
	if !sameHolder(l.owner, scope.Caller) {
		return bridge.Empty{}, conflict(l)
	}
	s.removeLock(key)
	s.publishLock(key, nil, holder(scope.Caller))
	return bridge.Empty{}, nil
}

func (s *Service) Takeover(ctx context.Context, scope svc.Scope, p bridge.TakeoverPartParams) (bridge.Lock, error) {
	if err := s.validate(ctx, scope, p.Doc, p.Part, true); err != nil {
		return bridge.Lock{}, err
	}
	if err := writable(scope); err != nil {
		return bridge.Lock{}, err
	}
	if err := s.begin(); err != nil {
		return bridge.Lock{}, err
	}
	value, stop, err := s.takeover(partKey{scope.App, p.Doc, p.Part}, scope.Caller)
	s.mu.Unlock()
	// Cancellation may call back into collaboration; ownership is already withdrawn.
	if stop != nil {
		stop(scope.App, p.Doc, p.Part)
	}
	return value, err
}
func (s *Service) takeover(key partKey, caller svc.Caller) (bridge.Lock, func(string, string, string), error) {
	l := s.current(key)
	if l == nil {
		return snapshot(s.acquire(key, caller)), nil, nil
	}
	if sameHolder(l.owner, caller) {
		s.refresh(key, l)
		s.publishLock(key, l, holder(caller))
		return snapshot(l), nil, nil
	}
	if l.value.Takeover != nil {
		return snapshot(l), nil, conflict(l)
	}
	if l.owner.Agent {
		s.removeLock(key)
		return snapshot(s.acquire(key, caller)), s.agentStop, nil
	}
	l.asker = caller
	l.value.Takeover = &bridge.Takeover{By: holder(caller), Until: stamp(s.clock.Now().Add(takeoverTimeout))}
	l.answer = s.clock.AfterFunc(takeoverTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.locks[key] == l {
			s.current(key)
		}
	})
	s.publishLock(key, l, holder(caller))
	return snapshot(l), nil, nil
}

func (s *Service) Answer(ctx context.Context, scope svc.Scope, p bridge.AnswerPartParams) (bridge.Lock, error) {
	if err := s.validate(ctx, scope, p.Doc, p.Part, true); err != nil {
		return bridge.Lock{}, err
	}
	if err := s.begin(); err != nil {
		return bridge.Lock{}, err
	}
	defer s.mu.Unlock()
	key := partKey{scope.App, p.Doc, p.Part}
	l := s.current(key)
	if l == nil {
		return bridge.Lock{}, bridge.Errorf(bridge.CodeNotFound, "part has no lock")
	}
	if !sameHolder(l.owner, scope.Caller) {
		return snapshot(l), conflict(l)
	}
	if l.value.Takeover == nil {
		return snapshot(l), bridge.Errorf(bridge.CodeInvalid, "part has no takeover request")
	}
	if !p.Keep {
		asker := l.asker
		s.removeLock(key)
		return snapshot(s.acquire(key, asker)), nil
	}
	l.answer.Stop()
	l.answer = nil
	l.value.Takeover = nil
	l.asker = svc.Caller{}
	s.refresh(key, l)
	s.publishLock(key, l, holder(scope.Caller))
	return snapshot(l), nil
}

func (s *Service) Dropped(member string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, l := range s.locks {
		if l.owner.Member == member {
			s.removeLock(key)
			s.publishLock(key, nil, l.value.Holder)
		} else if l.value.Takeover != nil && l.asker.Member == member {
			l.answer.Stop()
			l.answer = nil
			l.value.Takeover = nil
			l.asker = svc.Caller{}
			s.publishLock(key, l, member)
		}
	}
	for key, p := range s.presence {
		if key.member == member {
			s.removePresence(key, p)
		}
	}
}
