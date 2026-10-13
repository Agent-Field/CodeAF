package collab

import (
	"context"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const (
	idleTimeout     = 2 * time.Minute
	takeoverTimeout = 30 * time.Second
	presenceTimeout = time.Minute
)

// Timer allows expired work to be cancelled without depending on a wall clock.
type Timer interface{ Stop() bool }

// Clock keeps lock and presence deadlines deterministic in tests.
type Clock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) Timer
}

type wallClock struct{}

func (wallClock) Now() time.Time                            { return time.Now() }
func (wallClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

type partKey struct{ app, doc, part string }
type presenceKey struct{ app, doc, member string }
type heldLock struct {
	value        bridge.Lock
	owner        svc.Caller
	asker        svc.Caller
	idle, answer Timer
}
type present struct {
	value bridge.Presence
	timer Timer
}

// Service holds transient collaboration state; only comments survive a restart.
type Service struct {
	cfg       svc.Config
	all       *svc.Services
	clock     Clock
	mu        sync.Mutex
	locks     map[partKey]*heldLock
	presence  map[presenceKey]*present
	agentStop func(app, doc, part string)
	closed    bool
}

var _ svc.Collab = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return NewWithClock(cfg, all, wallClock{}) }

func NewWithClock(cfg svc.Config, all *svc.Services, clock Clock) *Service {
	s := &Service{cfg: cfg, all: all, clock: clock, locks: make(map[partKey]*heldLock), presence: make(map[presenceKey]*present)}
	if cfg.Ctx != nil {
		context.AfterFunc(cfg.Ctx, s.Close)
	}
	return s
}

// Close cancels deadlines when the server or a test ends.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for key := range s.locks {
		s.removeLock(key)
	}
	for key, p := range s.presence {
		p.timer.Stop()
		delete(s.presence, key)
	}
}

// OnAgentStop registers the app agent's cancellation door.
func (s *Service) OnAgentStop(stop func(app, doc, part string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentStop = stop
}

func (s *Service) begin() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return bridge.Errorf(bridge.CodeConflict, "collaboration is closed")
	}
	return nil
}

func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }
func holder(c svc.Caller) string {
	if c.Agent {
		return "agent"
	}
	return c.Member
}
func sameHolder(a, b svc.Caller) bool {
	if a.Agent || b.Agent {
		return a.Agent && b.Agent
	}
	return a.Member == b.Member
}
func snapshot(l *heldLock) bridge.Lock {
	value := l.value
	if value.Takeover != nil {
		request := *value.Takeover
		value.Takeover = &request
	}
	return value
}
func conflict(l *heldLock) error {
	value := snapshot(l)
	return &bridge.Error{Code: bridge.CodeConflict, Message: "part is held by " + value.Holder, Details: map[string]any{"holder": value.Holder, "lock": value}}
}
func writable(scope svc.Scope) error {
	switch scope.Caller.Role {
	case svc.RoleEditor, svc.RoleUser:
		return nil
	default:
		return bridge.Errorf(bridge.CodeDenied, "an Editor or User is required")
	}
}
func relative(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !strings.HasPrefix(name, "/") && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00")
}
func (s *Service) validate(ctx context.Context, scope svc.Scope, doc, part string, needPart bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.cfg.Ctx != nil {
		if err := s.cfg.Ctx.Err(); err != nil {
			return err
		}
	}
	if !relative(scope.App) || strings.Contains(scope.App, "/") || scope.Caller.Member == "" || !relative(doc) || (needPart && !relative(part)) {
		return bridge.Errorf(bridge.CodeInvalid, "app, member, document and part must be valid")
	}
	return nil
}
func (s *Service) publish(app, doc string, event bridge.DocEvent) {
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicDoc(app, doc), event)
	}
}
func (s *Service) publishLock(key partKey, l *heldLock, by string) {
	event := bridge.DocEvent{Kind: bridge.DocEventKindLock, Part: key.part, By: by}
	if l != nil {
		value := snapshot(l)
		event.Lock = &value
	}
	s.publish(key.app, key.doc, event)
}
