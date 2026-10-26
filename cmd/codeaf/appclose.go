package main

// The presence file is evidence, not a lock. Snapshot it before any opener can
// replace it; at expiry re-read it, and never resume a conversation joined on
// another holder. The journal's own lock remains the final ownership check.

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
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

type appClosePending struct {
	id, dir, key string
	at           time.Time
}

// appCloseRecovery separates the timer registry from work that may open a
// conversation. No registry lock is held while calling an opener or emitter.
type appCloseRecovery struct {
	mu          sync.Mutex
	work        sync.Mutex
	started     time.Time
	claims      map[string]*appCloseClaim
	quit        map[string]bool
	corrected   map[string]time.Time
	watching    bool
	stopped     bool
	counted     bool
	breaker     bool
	attempted   map[string]bool
	pending     []appClosePending
	stopPending []func()
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
	r.attempted = map[string]bool{}
	existing := map[string]bool{}
	paths := map[string]string{}
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
			path := filepath.Join(session.PlacesRoot(), bucket.Name(), dir.Name())
			if !appCloseConversationExists(path) {
				continue
			}
			existing[dir.Name()] = true
			paths[dir.Name()] = path
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
		for id, key := range f.ClosePending {
			if _, busy := r.claims[paths[id]]; !busy && !r.quit[id] {
				r.pending = append(r.pending, appClosePending{id: id, dir: paths[id], key: key, at: f.ClosePendingAt[id]})
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
	for id := range f.CloseResumedAt {
		if !existing[id] {
			delete(f.CloseResumedAt, id)
		}
	}
	for id := range f.ClosePending {
		if !existing[id] {
			delete(f.ClosePending, id)
			delete(f.ClosePendingAt, id)
		}
	}
	for id := range f.ClosePendingAt {
		if !existing[id] {
			delete(f.ClosePendingAt, id)
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
	for _, pending := range r.pending {
		// An idle heartbeat can be what the opener wrote before dying. Wait
		// out its claim too, so a live foreign holder is never driven.
		delay := time.Duration(0)
		if p, ok := session.ReadSessionPresenceAnyAge(pending.dir); ok && p.PID != os.Getpid() {
			delay = max(delay, p.UpdatedAt.Add(session.PresenceWindow+appClosePresenceMargin).Sub(b.now()))
		}
		r.stopPending = append(r.stopPending, b.after(delay, func() {
			defer guard.Recover("app/close-pending")
			b.replayClosePending(pending)
		}))
	}
	r.pending = nil
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
		r.attempted = map[string]bool{}
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
	defer b.allowLazyOpen(filepath.Base(path))
	if !dead {
		// A new busy claim needs its own expiry watch even when the heartbeat
		// read above was idle. No subsequent client read is guaranteed.
		var a *appAgent
		b.locked(func() { a = b.agents[filepath.Base(path)] })
		if a != nil {
			b.refreshElsewhereWith(a, true)
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
			dir = session.DirOf(a.transcript)
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

// appCloseWork keeps a pending replay's original key and age, independently of
// whatever idle heartbeat the interrupted opener may have left behind.
type appCloseWork struct {
	presence session.SessionPresence
	key      string
	at       time.Time
	replay   bool
}

func (b *appBackend) carryAfterClose(a *appAgent, p session.SessionPresence) {
	id := filepath.Base(p.Dir)
	var handled bool
	var pending appClosePending
	if err := b.state.read(func(f *appStateFile) {
		handled = !f.CloseHandled[id].Before(p.UpdatedAt)
		pending = appClosePending{id: id, dir: p.Dir, key: f.ClosePending[id], at: f.ClosePendingAt[id]}
	}); err != nil {
		return
	}
	if pending.key != "" {
		work := appPendingCloseWork(pending)
		work.presence = p
		b.carryCloseWork(a, work)
		return
	}
	if handled {
		return
	}
	b.carryCloseWork(a, appCloseWork{presence: p, key: "close-" + p.UpdatedAt.UTC().Format("20060102T150405.000000000Z"), at: p.UpdatedAt})
}

func appPendingCloseWork(p appClosePending) appCloseWork {
	claim := session.SessionPresence{Dir: p.dir}
	if current, ok := session.ReadSessionPresenceAnyAge(p.dir); ok && appBusyClaim(current) {
		claim = current
	}
	return appCloseWork{presence: claim, key: p.key, at: p.at, replay: true}
}

func (b *appBackend) replayClosePending(p appClosePending) {
	defer b.allowLazyOpen(p.id)
	r := &b.closeRecovery
	r.work.Lock()
	defer r.work.Unlock()
	if b.isClosing() {
		return
	}
	current := false
	_ = b.state.read(func(f *appStateFile) { current = f.ClosePending[p.id] == p.key })
	if !current {
		return
	}
	if a, err := b.find(p.id); err == nil {
		b.carryCloseWork(a, appPendingCloseWork(p))
	}
}

// carryCloseWork admits both roads before an opener can restart checkpointed
// tasks. The work lock serializes admission, including rollback on a lost hold.
func (b *appBackend) carryCloseWork(a *appAgent, work appCloseWork) {
	id := filepath.Base(work.presence.Dir)
	r := &b.closeRecovery
	attempt := id + "/" + work.key
	if r.attempted[attempt] || (work.replay && r.attempted["pending/"+id]) || b.abandonCloseToHolder(a) {
		return
	}
	// A failed migration keeps the work owed for a later start.
	if err := b.settleDayLimit(); err != nil {
		return
	}
	reset, err := b.resetCloseAfterPerson(a)
	if err != nil || (reset && work.replay) {
		return
	}
	resume, undo, err := b.admitCloseWork(id, work)
	if err != nil {
		return
	}
	r.attempted[attempt] = true
	alreadyHandled := r.attempted["pending/"+id]
	r.attempted["pending/"+id] = true
	tasks := b.closeRunningTasks(a, work.presence)
	if resume {
		_, _, err = b.ensure(a)
		live := b.liveOf(a)
		if appCloseOwnershipConflict(err) || (live != nil && !live.conn.Assembled) {
			undo()
			delete(r.attempted, attempt)
			r.attempted["pending/"+id] = alreadyHandled
			b.abandonCloseToHolder(a)
			return
		}
		resume = err == nil && live != nil
	}
	if b.abandonCloseToHolder(a) {
		undo()
		delete(r.attempted, attempt)
		r.attempted["pending/"+id] = alreadyHandled
		return
	}
	if resume {
		if err := b.keepAppFooter(a, work.key, appwire.FooterResumedAfterClose, appCloseResumedWord); err != nil {
			return
		}
		b.resumeOneWith(id, true, tasks, appwire.FooterResumedAfterClose, appCloseResumedWord, work.key)
		return
	}
	// Only a person's explicit agents.open opens a refused conversation; the
	// app never opens it by itself. That action retains the existing paused
	// conversation behaviour: opening it resumes its checkpointed tasks.
	if b.keepCloseStopped(a, work) {
		undo()
		delete(r.attempted, attempt)
		r.attempted["pending/"+id] = alreadyHandled
	}
}

// admitCloseWork writes the attempt, handled claim and pending continuation in
// ONE transaction before opening anything that can run a task or a reply.
func (b *appBackend) admitCloseWork(id string, work appCloseWork) (bool, func(), error) {
	r := &b.closeRecovery
	resume := !work.at.IsZero() && r.started.Sub(work.at) <= appCloseResumeWithin
	wasCounted, wasBreaker := r.counted, r.breaker
	breaker := r.breaker
	var previous appStateFile
	at := b.now()
	err := b.state.write(func(f *appStateFile) error {
		// Selection is only a hint. Person actions and deletion can invalidate
		// it while the day limit or journal read is in progress.
		if !appCloseConversationExists(work.presence.Dir) ||
			(work.replay && f.ClosePending[id] != work.key) ||
			(!work.replay && f.ClosePending[id] != "") {
			return errAppCloseCancelled
		}
		previous = f.clone()
		if !r.counted {
			starts := 1
			for _, stamp := range f.CloseStarts {
				if !stamp.Before(r.started.Add(-appCloseBreakerWithin)) {
					starts++
				}
			}
			breaker = starts >= appCloseStartLimit
			f.CloseStarts = append(f.CloseStarts, r.started)
		}
		stamps := f.CloseStarts[:0]
		for _, stamp := range f.CloseStarts {
			if !stamp.Before(at.Add(-appCloseBreakerWithin)) {
				stamps = append(stamps, stamp)
			}
		}
		f.CloseStarts = stamps
		resume = resume && f.CloseResumes[id] < appCloseResumeLimit && !breaker
		if resume {
			f.CloseResumes[id]++
			f.CloseResumedAt[id] = at
			f.ClosePending[id] = work.key
			if work.replay && f.ClosePendingAt[id].IsZero() {
				f.ClosePendingAt[id] = work.at
			}
			if !work.replay {
				f.ClosePendingAt[id] = at
			}
		}
		// The outstanding promise consumes a newer claim without acquiring a
		// new identity or age. A delayed claim callback then owes no other work.
		if (resume || work.replay) && work.presence.UpdatedAt.After(f.CloseHandled[id]) {
			f.CloseHandled[id] = work.presence.UpdatedAt
		}
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	r.counted, r.breaker = true, breaker
	undo := func() {
		if err := b.state.write(func(f *appStateFile) error {
			// A person may have acted while open was waiting. Preserve that
			// reset rather than restoring a streak they already broke.
			if f.ClosePending[id] == work.key && f.CloseResumedAt[id].Equal(at) {
				appRestoreCloseAgent(f, previous, id)
			}
			// A person's reset clears pending but deliberately keeps handled
			// claims. A lost hold must undo this admission's stamp either way.
			if f.CloseHandled[id].Equal(work.presence.UpdatedAt) {
				delete(f.CloseHandled, id)
				if stamp, ok := previous.CloseHandled[id]; ok {
					f.CloseHandled[id] = stamp
				}
			}
			if !wasCounted {
				f.CloseStarts = previous.CloseStarts
			}
			return nil
		}); err == nil {
			r.counted, r.breaker = wasCounted, wasBreaker
		}
	}
	return resume, undo, nil
}

// appRestoreCloseAgent restores only this admission, leaving other agents and
// unrelated settings untouched while a slow opener was in flight.
func appRestoreCloseAgent(f *appStateFile, old appStateFile, id string) {
	delete(f.CloseResumes, id)
	delete(f.CloseResumedAt, id)
	delete(f.CloseHandled, id)
	delete(f.ClosePending, id)
	delete(f.ClosePendingAt, id)
	if v, ok := old.CloseResumes[id]; ok {
		f.CloseResumes[id] = v
	}
	if v, ok := old.CloseResumedAt[id]; ok {
		f.CloseResumedAt[id] = v
	}
	if v, ok := old.CloseHandled[id]; ok {
		f.CloseHandled[id] = v
	}
	if v, ok := old.ClosePending[id]; ok {
		f.ClosePending[id] = v
	}
	if v, ok := old.ClosePendingAt[id]; ok {
		f.ClosePendingAt[id] = v
	}
}

func appCloseOwnershipConflict(err error) bool {
	var wire *appwire.Error
	return errors.Is(err, session.ErrSessionLocked) || (errors.As(err, &wire) && wire.Code == appwire.CodeConflict)
}

// abandonCloseToHolder rechecks ownership before either outcome is published.
// Freshness alone decides, including an idle holder; no pid liveness is used.
func (b *appBackend) abandonCloseToHolder(a *appAgent) bool {
	var transcript string
	b.locked(func() { transcript = a.transcript })
	p, fresh := session.ReadSessionPresence(session.DirOf(transcript), b.now())
	if !fresh || p.PID == os.Getpid() {
		return false
	}
	state := appwire.StateIdle
	switch p.State {
	case session.PresenceWorking:
		state = appwire.StateWorking
	case session.PresenceWaiting:
		state = appwire.StateNeedsYou
	}
	var view appwire.Agent
	changed := false
	b.locked(func() {
		changed = a.view.State != state
		a.view.State = state
		view = a.view
	})
	if changed {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	b.watchCloseClaim(session.DirOf(transcript))
	return true
}

func (b *appBackend) keepCloseStopped(a *appAgent, work appCloseWork) (abandoned bool) {
	if b.abandonCloseToHolder(a) {
		return true
	}
	key := work.key + "-stopped"
	if err := b.keepAppFooter(a, key, appwire.FooterStoppedByClose, appCloseStoppedWord); err != nil {
		return false
	}
	if !work.replay {
		if err := b.state.write(func(f *appStateFile) error {
			f.CloseHandled[filepath.Base(work.presence.Dir)] = work.presence.UpdatedAt
			return nil
		}); err != nil {
			return false
		}
	}
	b.emitAppFooter(a, key)
	return false
}

// resetCloseAfterPerson also sees directions written while this backend was
// absent. Only the person's own messages stamp LastUserAt; compaction, rewind
// and session-authored notes cannot change that witness.
//
// The stamp is written behind the path through offpath.Write. That is acceptable
// here because this app's send, steer, queue and answer doors reset the guard
// synchronously; only another window's stamp is read from meta.
func (b *appBackend) resetCloseAfterPerson(a *appAgent) (bool, error) {
	var id, transcript string
	b.locked(func() { id, transcript = a.view.ID, a.transcript })
	var last time.Time
	if err := b.state.read(func(f *appStateFile) {
		last = f.CloseResumedAt[id]
		if pending := f.ClosePendingAt[id]; !pending.IsZero() {
			last = pending
		}
	}); err != nil {
		return false, err
	}
	if last.IsZero() {
		return false, nil
	}
	meta, err := session.LoadMeta(session.DirOf(transcript))
	if err != nil {
		return false, err
	}
	// LoadMeta treats missing and malformed identity files as blank. Neither is
	// evidence that the person has stayed silent, so automatic work must wait.
	if meta.ID == "" {
		return false, errors.New("conversation metadata is unavailable")
	}
	if meta.LastUserAt.After(last) {
		b.resetCloseResumes(id)
		return true, nil
	}
	return false, nil
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
	hasWork := false
	if err := b.state.read(func(f *appStateFile) {
		hasWork = f.CloseResumes[id] != 0 || f.ClosePending[id] != "" || !f.CloseResumedAt[id].IsZero()
	}); err != nil || !hasWork {
		return
	}
	_ = b.state.write(func(f *appStateFile) error {
		delete(f.CloseResumes, id)
		delete(f.CloseResumedAt, id)
		delete(f.ClosePending, id)
		delete(f.ClosePendingAt, id)
		return nil
	})
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
		for _, stop := range r.stopPending {
			stop()
		}
		r.stopPending = nil
	})
	r.work.Lock()
	defer r.work.Unlock()
}

// appCloseWake is process-local evidence for clearing a durable pending marker.
// Queueing alone is insufficient: a death before turn-start must leave it owed.
type appCloseWake struct {
	key               string
	accepted, started bool
}

func (b *appBackend) closeWakeAccepted(id string, live *appLive, wake *appCloseWake, accepted bool) {
	key := ""
	b.locked(func() {
		if live.closeWake != wake {
			return
		}
		if !accepted {
			live.closeWake = nil
			return
		}
		wake.accepted = true
		if wake.started {
			key, live.closeWake = wake.key, nil
		}
	})
	b.completeClosePending(id, key)
}

func (b *appBackend) closeWakeStarted(id string, live *appLive, consumed string) {
	key := ""
	b.locked(func() {
		if wake := live.closeWake; wake != nil && wake.key == consumed {
			wake.started = true
			if wake.accepted {
				key, live.closeWake = wake.key, nil
			}
		}
	})
	b.completeClosePending(id, key)
}

func (b *appBackend) completeClosePending(id, key string) {
	if key == "" {
		return
	}
	_ = b.state.write(func(f *appStateFile) error {
		if f.ClosePending[id] == key {
			delete(f.ClosePending, id)
			delete(f.ClosePendingAt, id)
		}
		return nil
	})
}

// A checkpoint can precede the heartbeat that names its newly started task.
// Absence proves interruption only after the writer had a heartbeat to see it.
func appTaskWasInterrupted(p session.SessionPresence, fresh bool, id string, started time.Time) bool {
	if !fresh {
		return true
	}
	return !p.Holds(id) && p.UpdatedAt.Sub(started) >= session.PresenceHeartbeat
}

// heldTaskViews reads after the disk so a start arriving during the listing
// wins. Locally owned tasks come from the engine, even ahead of its event lane.
func (b *appBackend) heldTaskViews(a *appAgent) map[string]appwire.Task {
	heard := map[string]appwire.Task{}
	var live *appLive
	var id string
	b.locked(func() {
		live, id = a.live, a.view.ID
		if live != nil {
			for key, task := range live.tasks {
				heard[key] = task
			}
		}
	})
	if live != nil && live.conn.Assembled {
		if tasks, err := live.conn.Conv.AppTasks(); err == nil {
			for _, view := range tasks {
				task := appTaskOfView(id, view)
				task.UpdatedAt = heard[task.ID].UpdatedAt
				heard[task.ID] = task
			}
		}
	}
	return heard
}

// Deleted journals keep their directory for deliverables. Only their lifetime
// marker, not directory existence alone, can establish that work is still owed.
func appCloseConversationExists(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, teams.ConversationDeletedFile)); !os.IsNotExist(err) {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

var errAppCloseCancelled = errors.New("close continuation no longer owed")

// A completed reply breaks the streak but cannot acknowledge a different
// queued continuation. Only keyed consumption or a person's action clears it.
func (b *appBackend) resetCloseAfterReply(id string) {
	hasWork := false
	if err := b.state.read(func(f *appStateFile) {
		hasWork = f.CloseResumes[id] != 0 || !f.CloseResumedAt[id].IsZero()
	}); err != nil || !hasWork {
		return
	}
	_ = b.state.write(func(f *appStateFile) error {
		delete(f.CloseResumes, id)
		delete(f.CloseResumedAt, id)
		return nil
	})
}
