package desktopbridge

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// A closed view gets ten quiet minutes before its engine connection is released.
	sessionIdleLimit    = 10 * time.Minute
	sessionReapInterval = time.Minute
)

// sessionLifecycle is protected by Bridge.mu. Views and streams are distinct:
// closing a tab releases one view, while reconnecting SSE releases no views.
type sessionLifecycle struct {
	now        func() time.Time
	views      map[*conversation]*sessionViews
	stop, done chan struct{}
	stopped    bool
}

type sessionViews struct {
	count     int
	idleSince time.Time
	seq       uint64
}

func (b *Bridge) lifecycleLocked() *sessionLifecycle {
	if b.lifecycle == nil {
		b.lifecycle = &sessionLifecycle{now: time.Now, views: make(map[*conversation]*sessionViews)}
	}
	return b.lifecycle
}

// attachViewLocked must be called for both successful POST /sessions paths,
// while the route holds Bridge.mu. Reusing an engine still opens another view.
func (b *Bridge) attachViewLocked(s *conversation) {
	l := b.lifecycleLocked()
	v := l.views[s]
	if v == nil {
		v = &sessionViews{}
		l.views[s] = v
	}
	v.count++
	v.idleSince = time.Time{}
}

// detach runs behind ServeHTTP's authentication gate. It never sends Stop:
// the engine and its outstanding work survive the last tab closing.
func (b *Bridge) detach(w http.ResponseWriter, r *http.Request, s *conversation) {
	if !needPost(w, r) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions[s.id] != s {
		fail(w, http.StatusNotFound, "reattach this conversation")
		return
	}
	l := b.lifecycleLocked()
	v := l.views[s]
	if v == nil {
		// Conversations created before lifecycle wiring already have one view.
		v = &sessionViews{count: 1}
		l.views[s] = v
	}
	if v.count > 0 {
		v.count--
		if v.count == 0 {
			busy := s.lifecycleBusy(l.now())
			s.mu.Lock()
			v.seq = s.seq
			busy = busy || s.running || s.observers != 0 || len(s.queue) != 0
			s.mu.Unlock()
			if !busy {
				v.idleSince = l.now()
			}
		}
	}
	write(w, struct{}{})
}

// reaper starts once from New. The clock and tick channel have a separate door
// so tests advance ten minutes without sleeping or changing production limits.
func (b *Bridge) reaper() {
	b.startReaper(time.Now, nil)
}

func (b *Bridge) startReaper(now func() time.Time, ticks <-chan time.Time) {
	b.mu.Lock()
	l := b.lifecycleLocked()
	if l.done != nil || l.stopped {
		b.mu.Unlock()
		return
	}
	l.now = now
	l.stop, l.done = make(chan struct{}), make(chan struct{})
	b.mu.Unlock()
	go func() {
		defer close(l.done)
		if ticks == nil {
			ticker := time.NewTicker(sessionReapInterval)
			defer ticker.Stop()
			ticks = ticker.C
		}
		for {
			select {
			case <-l.stop:
				return
			case _, ok := <-ticks:
				if !ok {
					return
				}
				b.reapIdle(now())
			}
		}
	}()
}

// stopReaper runs before Close holds Bridge.mu or closes any conversation.
// Waiting here prevents shutdown and a sweep from closing the same engine.
func (b *Bridge) stopReaper() {
	b.mu.Lock()
	l := b.lifecycleLocked()
	if !l.stopped {
		l.stopped = true
		if l.stop != nil {
			close(l.stop)
		}
	}
	done := l.done
	b.mu.Unlock()
	if done != nil {
		<-done
	}
}

// reapIdle serializes removal with attachment. A failed task read is unknown
// work, so it keeps the child alive rather than treating uncertainty as idle.
func (b *Bridge) reapIdle(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.lifecycleLocked()
	if l.stopped {
		return
	}
	for id, s := range b.sessions {
		v := l.views[s]
		if v == nil || v.count != 0 {
			continue
		}
		busy := s.lifecycleBusy(now)
		s.mu.Lock()
		busy = busy || s.running || s.observers != 0 || len(s.queue) != 0
		if busy {
			v.idleSince = time.Time{}
		} else if v.idleSince.IsZero() || s.seq != v.seq {
			v.idleSince = now
		}
		v.seq = s.seq
		if busy || v.idleSince.IsZero() || now.Sub(v.idleSince) < sessionIdleLimit {
			s.mu.Unlock()
			continue
		}
		delete(b.sessions, id)
		delete(l.views, s)
		close(s.done)
		s.mu.Unlock()
		s.terminals().closeAll()
		if s.conn.Close != nil {
			s.conn.Close()
		}
	}
}

func (s *conversation) lifecycleBusy(now time.Time) bool {
	if s.conn.Agent.NeedsPerson() {
		return true
	}
	if door, ok := s.conn.Agent.(interface{ OpenQuestions() []session.Question }); ok && len(door.OpenQuestions()) != 0 {
		return true
	}
	rows, err := s.planRows()
	if err != "" {
		return true
	}
	for _, row := range rows {
		if row.Status == "running" || row.Status == "claimed" || row.Running > 0 {
			return true
		}
	}
	for _, term := range s.terminals().list() {
		if term.State == "running" {
			return true
		}
	}
	// Local children already publish their canonical jobs and running tasks in
	// presence. Remote connections do not share this disk and cannot use it.
	if s.conn.Local && s.conn.Welcome.SessionFile != "" {
		if p, ok := session.ReadSessionPresence(filepath.Dir(s.conn.Welcome.SessionFile), now); ok {
			return len(p.Jobs) != 0 || len(p.RunningTasks) != 0 || p.State == session.PresenceWorking || p.NeedsPerson()
		}
	}
	return false
}
