package main

// The presence file is evidence, not a lock. Snapshot it before any opener can
// replace it; at expiry re-read it, and never resume a conversation joined on
// another holder. The journal's own lock remains the final ownership check.

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// Two attempts tolerate a transient failure without repeatedly running a step
	// that kills its own app. Three affected starts in ten minutes stop all such
	// work; an hour-old answer is left for the person to choose to carry on.
	appCloseResumeLimit   = 2
	appCloseStartLimit    = 3
	appCloseBreakerWithin = 10 * time.Minute
	appCloseResumeWithin  = time.Hour
	// The margin avoids deciding on the exact boundary of the heartbeat window.
	appClosePresenceMargin = 100 * time.Millisecond
	appCloseResumedWord    = "Resumed after the app closed"
	appCloseStoppedWord    = "The app closed while this was working · send a message to carry on"
)

type appCloseClaim struct {
	presence session.SessionPresence
	stop     func()
}

// appCloseRecovery separates the timer registry from work that may open a
// conversation. No registry lock is held while calling an opener or emitter.
type appCloseRecovery struct {
	mu        sync.Mutex
	work      sync.Mutex
	started   time.Time
	claims    map[string]*appCloseClaim
	quit      map[string]bool
	corrected map[string]time.Time
	watching  bool
	stopped   bool
	counted   bool
	breaker   bool
}

// snapshotCloseClaims is synchronous: crews and quit resumes must not get the
// journal before this reads the old holder's last claim, including stale ones.
func (b *appBackend) snapshotCloseClaims() {
	r := &b.closeRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = b.now()
	r.claims = map[string]*appCloseClaim{}
	r.quit = map[string]bool{}
	r.corrected = map[string]time.Time{}
	existing := map[string]bool{}
	buckets, _ := os.ReadDir(session.PlacesRoot())
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		dirs, _ := os.ReadDir(filepath.Join(session.PlacesRoot(), bucket.Name()))
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			existing[dir.Name()] = true
			path := filepath.Join(session.PlacesRoot(), bucket.Name(), dir.Name())
			if p, ok := session.ReadSessionPresenceAnyAge(path); ok && appBusyClaim(p) {
				r.claims[path] = &appCloseClaim{presence: p}
			}
		}
	}
	// A quit that did not finish still belongs solely to the quit road. Keep
	// that decision after resumeAfterQuit consumes its pending list.
	_ = b.state.write(func(f *appStateFile) error {
		f.pruneCloseHistory(existing, r.started)
		for _, id := range f.ResumeAfterQuit {
			r.quit[id] = true
		}
		for path, c := range r.claims {
			if r.quit[filepath.Base(path)] {
				f.CloseHandled[filepath.Base(path)] = c.presence.UpdatedAt
			}
		}
		return nil
	})
}

func (f *appStateFile) pruneCloseHistory(existing map[string]bool, now time.Time) {
	stamps := f.CloseStarts[:0]
	for _, at := range f.CloseStarts {
		if !at.Before(now.Add(-appCloseBreakerWithin)) {
			stamps = append(stamps, at)
		}
	}
	f.CloseStarts = stamps
	if f.CloseResumes == nil {
		f.CloseResumes = map[string]int{}
	}
	if f.CloseHandled == nil {
		f.CloseHandled = map[string]time.Time{}
	}
	for id := range f.CloseResumes {
		if !existing[id] {
			delete(f.CloseResumes, id)
		}
	}
	for id := range f.CloseHandled {
		if !existing[id] {
			delete(f.CloseHandled, id)
		}
	}
}

func appBusyClaim(p session.SessionPresence) bool {
	return p.PID > 0 && p.PID != os.Getpid() && !p.UpdatedAt.IsZero() &&
		(p.State == session.PresenceWorking || p.State == session.PresenceWaiting)
}

func (b *appBackend) startCloseWatch() {
	r := &b.closeRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.watching = true
	for path, c := range r.claims {
		b.scheduleCloseLocked(path, c)
	}
}

// watchCloseClaim also observes holders first seen after startup, so a death
// while this engine stays open has the same expiry as a death before startup.
func (b *appBackend) watchCloseClaim(dir string) {
	if dir == "" {
		return
	}
	p, ok := session.ReadSessionPresenceAnyAge(dir)
	if !ok || !appBusyClaim(p) {
		return
	}
	r := &b.closeRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	if r.claims == nil {
		r.claims = map[string]*appCloseClaim{}
		r.quit = map[string]bool{}
		r.corrected = map[string]time.Time{}
		r.started, r.watching = b.now(), true
	}
	if _, exists := r.claims[dir]; exists {
		return
	}
	c := &appCloseClaim{presence: p}
	r.claims[dir] = c
	if r.watching {
		b.scheduleCloseLocked(dir, c)
	}
}

// scheduleCloseLocked only installs a timer; even a due callback runs outside
// the registry lock. A stopped backend cancels every outstanding timer.
func (b *appBackend) scheduleCloseLocked(path string, c *appCloseClaim) {
	delay := max(time.Duration(0), c.presence.UpdatedAt.Add(session.PresenceWindow+appClosePresenceMargin).Sub(b.now()))
	c.stop = b.after(delay, func() {
		defer guard.Recover("app/close-claim")
		b.checkCloseClaim(path, c)
	})
}

// closeClaimDue rechecks the evidence under the registry lock. Its three
// results say whether to correct a view, whether the old claim died, and
// whether the quit road owns that death.
func (b *appBackend) closeClaimDue(path string, c *appCloseClaim) (refresh, dead, quit bool) {
	r := &b.closeRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.claims[path] != c {
		return false, false, false
	}
	current, exists := session.ReadSessionPresenceAnyAge(path)
	switch {
	case !exists:
		delete(r.claims, path)
		return true, false, false
	case current.PID == c.presence.PID && current.UpdatedAt.After(c.presence.UpdatedAt):
		// A heartbeat, including a move to idle, is proof the writer is alive.
		if current.State != session.PresenceWorking && current.State != session.PresenceWaiting {
			delete(r.claims, path)
			return true, false, false
		}
		c.presence = current
		b.scheduleCloseLocked(path, c)
		return false, false, false
	case current.PID != c.presence.PID && current.PID != os.Getpid():
		delete(r.claims, path)
		return true, false, false
	case current.PID != os.Getpid() && b.now().Sub(c.presence.UpdatedAt) <= session.PresenceWindow:
		b.scheduleCloseLocked(path, c)
		return false, false, false
	}
	delete(r.claims, path)
	return true, true, r.quit[filepath.Base(path)]
}

func (b *appBackend) checkCloseClaim(path string, c *appCloseClaim) {
	r := &b.closeRecovery
	refresh, dead, quit := b.closeClaimDue(path, c)
	if !refresh || b.isClosing() {
		return
	}
	if !dead {
		// Correct a cached view without enrolling the replacement holder as this
		// snapshot's work. A later client read can observe that holder separately.
		var a *appAgent
		b.locked(func() { a = b.agents[filepath.Base(path)] })
		if a != nil {
			b.refreshElsewhereWith(a, false)
		}
		return
	}
	a, err := b.find(filepath.Base(path))
	if err != nil {
		return
	}
	b.correctCloseState(a, c.presence.UpdatedAt)
	if quit {
		return
	}
	// A slow opener may hold up automatic work, but never another claim's
	// correction. Only the paid-capable work and its start counter serialize.
	r.work.Lock()
	defer r.work.Unlock()
	if !b.isClosing() {
		b.carryAfterClose(a, c.presence)
	}
}

// refreshElsewhere makes cached reads obey freshness too. Its correction is
// emitted once per claim even if a list or open beats the expiry callback.
func (b *appBackend) refreshElsewhere(a *appAgent) {
	b.refreshElsewhereWith(a, true)
}

// refreshElsewhereWith can correct an observed claim without enrolling a new
// owner discovered by the expiry callback itself.
func (b *appBackend) refreshElsewhereWith(a *appAgent, watch bool) {
	var dir string
	b.locked(func() {
		if a.live == nil && a.transcript != "" {
			dir = filepath.Dir(a.transcript)
		}
	})
	if dir == "" {
		return
	}
	p, live := session.ReadSessionPresence(dir, b.now())
	if live && (p.State == session.PresenceWorking || p.State == session.PresenceWaiting) {
		if watch {
			b.watchCloseClaim(dir)
		}
		state := appwire.StateWorking
		if p.State == session.PresenceWaiting {
			state = appwire.StateNeedsYou
		}
		var view appwire.Agent
		changed := false
		b.locked(func() {
			if a.live == nil && a.view.State != state {
				a.view.State = state
				view, changed = a.view, true
			}
		})
		if changed {
			b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
		return
	}
	stamp := time.Time{}
	if old, ok := session.ReadSessionPresenceAnyAge(dir); ok {
		stamp = old.UpdatedAt
	}
	b.correctCloseState(a, stamp)
}

func (b *appBackend) correctCloseState(a *appAgent, stamp time.Time) {
	r := &b.closeRecovery
	var view appwire.Agent
	emit := false
	r.locked(func() {
		if r.corrected == nil {
			r.corrected = map[string]time.Time{}
		}
		b.locked(func() {
			if a.live != nil || a.view.State == appwire.StateClosed || a.view.State == appwire.StateStopped {
				return
			}
			busy := a.view.State == appwire.StateWorking || a.view.State == appwire.StateNeedsYou
			previous, seen := r.corrected[a.view.ID]
			if !busy && (stamp.IsZero() || (seen && previous.Equal(stamp))) {
				return
			}
			a.view.State = appwire.StateIdle
			view, emit = a.view, true
			r.corrected[a.view.ID] = stamp
		})
	})
	if emit {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

// locked scopes a registry operation so an absorbed panic cannot strand it.
func (r *appCloseRecovery) locked(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

func (b *appBackend) carryAfterClose(a *appAgent, p session.SessionPresence) {
	id := filepath.Base(p.Dir)
	handled := false
	if err := b.state.read(func(f *appStateFile) { handled = !f.CloseHandled[id].Before(p.UpdatedAt) }); err != nil || handled {
		return
	}
	// Settle the profile before any paid-capable opener or task continuation.
	// A failed migration keeps the claim unhandled so a later start can try it.
	if err := b.settleDayLimit(); err != nil {
		return
	}
	r := &b.closeRecovery
	resume := r.started.Sub(p.UpdatedAt) <= appCloseResumeWithin
	count := 0
	if err := b.state.read(func(f *appStateFile) { count = f.CloseResumes[id] }); err != nil {
		return
	}
	// Count affected starts at this start's timestamp, not at the delayed
	// expiry callback: waiting out a fresh claim must not evade the breaker.
	// Once tripped it stays tripped for this entire start, even as stamps age.
	breaker := r.breaker
	if err := b.state.write(func(f *appStateFile) error {
		if !r.counted {
			starts := 1
			for _, at := range f.CloseStarts {
				if !at.Before(r.started.Add(-appCloseBreakerWithin)) {
					starts++
				}
			}
			breaker = starts >= appCloseStartLimit
			f.CloseStarts = append(f.CloseStarts, r.started)
		}
		stamps := f.CloseStarts[:0]
		for _, at := range f.CloseStarts {
			if !at.Before(b.now().Add(-appCloseBreakerWithin)) {
				stamps = append(stamps, at)
			}
		}
		f.CloseStarts = stamps
		return nil
	}); err != nil {
		return
	}
	r.counted, r.breaker = true, breaker

	resume = resume && count < appCloseResumeLimit && !r.breaker
	kind, words := appwire.FooterStoppedByClose, appCloseStoppedWord
	if resume {
		kind, words = appwire.FooterResumedAfterClose, appCloseResumedWord
	}
	tasks := b.closeRunningTasks(a, p)
	if resume {
		_, _, err := b.ensure(a)
		live := b.liveOf(a)
		if err != nil || live == nil {
			resume = false
			kind, words = appwire.FooterStoppedByClose, appCloseStoppedWord
		} else if !live.conn.Assembled {
			return
		}
	}
	// Keep the notice before the handled stamp. Its deterministic id makes a
	// death between these two writes idempotent; no turn starts before both.
	key := "close-" + p.UpdatedAt.UTC().Format("20060102T150405.000000000Z")
	if err := b.keepAppFooter(a, key, kind, words); err != nil {
		return
	}
	if err := b.state.write(func(f *appStateFile) error {
		if f.CloseHandled == nil {
			f.CloseHandled = map[string]time.Time{}
		}
		if f.CloseResumes == nil {
			f.CloseResumes = map[string]int{}
		}
		f.CloseHandled[id] = p.UpdatedAt
		if resume {
			f.CloseResumes[id]++
		}
		return nil
	}); err != nil {
		return
	}
	if resume {
		b.resumeOneWith(id, true, tasks, appwire.FooterResumedAfterClose, appCloseResumedWord, key)
	} else {
		b.emitAppFooter(a, key)
	}
}

func (b *appBackend) closeRunningTasks(a *appAgent, p session.SessionPresence) []string {
	seen := map[string]bool{}
	for _, task := range p.RunningTasks {
		if task.State == string(session.TaskRunning) {
			seen[appIndexTaskID(filepath.Base(p.Dir), task.ID)] = true
		}
	}
	for _, task := range session.ReadSessionTasks(a.transcript) {
		if task.State == session.TaskRunning || task.State == session.TaskInterrupted {
			seen[appTaskID(filepath.Base(p.Dir), task.ID)] = true
		}
	}
	if path := session.TaskIndexPath(a.transcript); path != "" {
		for _, task := range session.ReadTaskIndex(path) {
			if task.SessionID == filepath.Base(p.Dir) && task.Status == string(session.TaskRunning) {
				seen[appIndexTaskID(task.SessionID, task.ID)] = true
			}
		}
	}
	tasks := make([]string, 0, len(seen))
	for id := range seen {
		tasks = append(tasks, id)
	}
	return tasks
}

func (b *appBackend) resetCloseResumes(id string) {
	hasCount := false
	if err := b.state.read(func(f *appStateFile) { hasCount = f.CloseResumes[id] != 0 }); err != nil || !hasCount {
		return
	}
	_ = b.state.write(func(f *appStateFile) error { delete(f.CloseResumes, id); return nil })
}

func (b *appBackend) stopCloseWatch() {
	r := &b.closeRecovery
	r.locked(func() {
		r.stopped = true
		for _, c := range r.claims {
			if c.stop != nil {
				c.stop()
			}
		}
		r.claims = nil
	})
	r.work.Lock()
	defer r.work.Unlock()
}
