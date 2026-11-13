package syncsetup

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

const StorageRefreshEvery = 10 * time.Minute

// One coordinator owns both approval discovery and startup waits in a process.
// Running batchers use their own bounded retry and never poll permission files.
var storageProcesses = struct {
	sync.Mutex
	homes map[string]*storageProcess
}{homes: make(map[string]*storageProcess)}

type storageProcess struct {
	mu      sync.Mutex
	home    string
	users   int
	next    uint64
	waiting map[uint64]func()
	opened  map[uint64]func()
	stop    func() bool
	// ctx belongs to the current lifetime of users; the last user to leave
	// cancels it, so a request in flight cannot outlive everyone who asked.
	ctx    context.Context
	cancel context.CancelFunc
	// inflight is open while a check runs and closed when it ends, so the
	// last user can wait for it instead of returning while it still writes.
	inflight                    chan struct{}
	lastRefresh                 time.Time
	active                      bool
	watchSetup                  bool
	dirty                       bool
	refused                     *Sync
	counts                      blobstore.Counts
	countsRelay, countsIdentity string
	probeRelay, probeIdentity   string
	// These seams count actual scheduled work without relying on elapsed time.
	after func(time.Duration, func()) func() bool
	read  func() (string, string, string)
	fetch func(context.Context) error
	now   func() time.Time
}

func storageProcessFor(home string) *storageProcess {
	storageProcesses.Lock()
	defer storageProcesses.Unlock()
	p := storageProcesses.homes[home]
	if p == nil {
		ctx := context.Background()
		p = &storageProcess{home: home, ctx: ctx, waiting: make(map[uint64]func()), opened: make(map[uint64]func()), now: time.Now}
		p.after = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
		p.read = func() (string, string, string) {
			relay := Resolve(home)
			if !relay.On() || identity.Solo(home) {
				return "", "", ""
			}
			id, err := identity.Load(home)
			if err != nil {
				return "", "", ""
			}
			state, gating, answered := storagePolicy(home, relay.URL, id.ID())
			if !answered && !gating {
				state = ""
			}
			return relay.URL, id.ID(), state
		}
		p.fetch = func(ctx context.Context) error {
			s, ok, err := Open(home)
			if err != nil || !ok {
				return err
			}
			_, err = s.RefreshStorage(ctx)
			return err
		}
		storageProcesses.homes[home] = p
	}
	return p
}

// StartStorageRefresh shares one asynchronous signed reading at startup and
// every ten minutes while a gating relay is unknown or off. Other relays get
// one discovery request until an answer establishes their storage policy.
// Callers join its lifetime independently; chat opening never waits for a network answer.
func StartStorageRefresh(ctx context.Context, home string, initial ...bool) func() {
	p := storageProcessFor(home)
	p.mu.Lock()
	p.users++
	p.watchSetup = true
	first := p.users == 1
	if first && (p.ctx == nil || p.ctx.Err() != nil || p.cancel == nil) {
		p.ctx, p.cancel = context.WithCancel(context.Background())
	}
	p.mu.Unlock()
	if first {
		refresh := len(initial) == 0 || initial[0]
		go p.check(refresh)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			p.mu.Lock()
			p.users--
			var running chan struct{}
			if p.users == 0 && len(p.waiting) == 0 {
				p.disarm()
				if p.cancel != nil {
					p.cancel()
				}
				running = p.inflight
			}
			p.mu.Unlock()
			// A check that already started may still be saving its answer into
			// the home; the caller must not return while that write can land.
			if running != nil {
				<-running
			}
		})
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-done:
		}
	}()
	var joined sync.Once
	return func() { stop(); joined.Do(func() { close(done) }) }
}

// RefreshStorage retains a failed reading and persists only successful stats.
func (s *Sync) RefreshStorage(ctx context.Context) (blobstore.Counts, error) {
	store, ok := s.Store.(interface {
		Stats(context.Context) (blobstore.Counts, error)
	})
	if !ok {
		return blobstore.Counts{}, nil
	}
	p := storageProcessFor(s.Home)
	p.mu.Lock()
	p.probeRelay, p.probeIdentity = normalizeStorageURL(s.Relay), s.Identity.ID()
	p.mu.Unlock()
	counts, err := store.Stats(ctx)
	if p != nil {
		p.mu.Lock()
		p.lastRefresh = p.now()
		if errors.Is(err, wireauth.ErrRevoked) || errors.Is(err, wireauth.ErrGone) {
			p.refused = s
			p.disarm()
		}
		if err == nil {
			p.counts = counts
			p.countsRelay, p.countsIdentity = s.Relay, s.Identity.ID()
		}
		p.mu.Unlock()
	}
	if err == nil {
		err = SaveStorage(s.Home, s.Relay, s.Identity.ID(), counts.Storage)
	}
	return counts, err
}

// WaitStorage registers a stopped chat on one shared local sampling timer.
func WaitStorage(home string, opened func()) func() {
	p := storageProcessFor(home)
	p.mu.Lock()
	p.next++
	id := p.next
	p.waiting[id] = opened
	first := len(p.waiting) == 1
	p.mu.Unlock()
	if first {
		go p.check(false)
	}
	return func() {
		p.mu.Lock()
		_, had := p.waiting[id]
		delete(p.waiting, id)
		empty := had && len(p.waiting) == 0
		if empty {
			p.disarm()
		}
		users := p.users
		p.mu.Unlock()
		if empty && users > 0 {
			go p.check(false)
		}
	}
}

// OnStorageOpened wakes an existing batcher immediately after an in-process
// refresh, without giving it a timer or a separate permission-file reader.
func OnStorageOpened(home string, opened func()) func() {
	p := storageProcessFor(home)
	p.mu.Lock()
	p.next++
	id := p.next
	p.opened[id] = opened
	p.mu.Unlock()
	return func() { p.mu.Lock(); delete(p.opened, id); p.mu.Unlock() }
}

func (p *storageProcess) disarm() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

func (p *storageProcess) check(refresh bool) {
	p.mu.Lock()
	// Checks start on their own goroutines, so one can begin after the last
	// user and waiter left. It then has nobody to answer and must not touch
	// the home, or it outlives the lifetime that started it.
	if p.users == 0 && len(p.waiting) == 0 {
		p.mu.Unlock()
		return
	}
	if p.active {
		p.dirty = true
		p.mu.Unlock()
		return
	}
	p.active = true
	p.inflight = make(chan struct{})
	requests := p.ctx
	p.disarm()
	p.mu.Unlock()
	relay, id, state := p.read()
	p.mu.Lock()
	refused := p.refused
	p.mu.Unlock()
	removed := false
	if refused != nil {
		// Only a different pairing can reopen a refused lifetime. This extra
		// lookup happens after refusal, keeping ordinary waiter samples small.
		current, ok, err := Open(p.home)
		removed = err != nil || !ok || (current.Relay == refused.Relay &&
			current.Identity.ID() == refused.Identity.ID() && current.Device.ID() == refused.Device.ID())
		if !removed {
			p.mu.Lock()
			if p.refused == refused {
				p.refused = nil
				p.probeRelay, p.probeIdentity = "", ""
			}
			p.mu.Unlock()
		}
	}
	p.mu.Lock()
	ask := !removed && (state == "off" || state == "" && (p.probeRelay != normalizeStorageURL(relay) || p.probeIdentity != id))
	if relay != "" && ask && refresh {
		p.probeRelay, p.probeIdentity = normalizeStorageURL(relay), id
	}
	p.mu.Unlock()
	if relay != "" && ask && refresh {
		ctx, cancel := context.WithTimeout(requests, requestTimeout)
		err := p.fetch(ctx)
		cancel()
		removed = errors.Is(err, wireauth.ErrRevoked) || errors.Is(err, wireauth.ErrGone)
		p.mu.Lock()
		p.lastRefresh = p.now()
		p.mu.Unlock()
		relay, _, state = p.read()
	}
	p.mu.Lock()
	p.active = false
	close(p.inflight)
	p.inflight = nil
	removed = removed || p.refused != nil
	again := p.dirty && !removed
	p.dirty = false
	var callbacks []func()
	if !removed && state == "on" {
		for id, fn := range p.waiting {
			callbacks = append(callbacks, fn)
			delete(p.waiting, id)
		}
	}
	// A removed device cannot regain access by polling for storage approval.
	// Before pairing, sample local setup only; the first eligible sample asks once.
	watchingPairing := p.watchSetup && p.users > 0 && relay == "" && Resolve(p.home).On()
	if !removed && (watchingPairing || relay != "" && state == "off") && (p.users > 0 || len(p.waiting) > 0) {
		wait := StorageRefreshEvery
		if p.users > 0 && !p.lastRefresh.IsZero() {
			wait = max(time.Millisecond, StorageRefreshEvery-p.now().Sub(p.lastRefresh))
		}
		if watchingPairing || len(p.waiting) > 0 {
			wait = min(wait, StoragePollEvery)
		}
		p.stop = p.after(wait, func() {
			p.mu.Lock()
			refresh := p.users > 0 && p.now().Sub(p.lastRefresh) >= StorageRefreshEvery
			p.mu.Unlock()
			p.check(refresh)
		})
	}
	p.mu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
	if again {
		p.check(false)
	}
}

func storageSaved(home, relay, id, state string) {
	storageProcesses.Lock()
	p := storageProcesses.homes[home]
	storageProcesses.Unlock()
	if p == nil {
		return
	}
	// The cache may temporarily hold a rotation successor, so callbacks use the
	// current home's binding instead of trusting a different identity's reading.
	p.mu.Lock()
	active := p.active
	if active && state == "off" {
		p.dirty = true
	}
	p.mu.Unlock()
	if state == "on" {
		current, account, reading := p.read()
		if normalizeStorageURL(current) != normalizeStorageURL(relay) || account != id || reading != "on" {
			return
		}
		p.mu.Lock()
		p.disarm()
		callbacks := make([]func(), 0, len(p.waiting)+len(p.opened))
		for key, fn := range p.waiting {
			callbacks = append(callbacks, fn)
			delete(p.waiting, key)
		}
		for _, fn := range p.opened {
			callbacks = append(callbacks, fn)
		}
		p.mu.Unlock()
		for _, fn := range callbacks {
			go fn()
		}
	} else if !active {
		go p.check(false)
	}
}

// StorageCounts retains the last in-process usage reading while approval is on.
// Permission persistence deliberately stores no usage counters.
func StorageCounts(home, relay, identity string) blobstore.Counts {
	storageProcesses.Lock()
	p := storageProcesses.homes[home]
	storageProcesses.Unlock()
	if p == nil {
		return blobstore.Counts{Storage: "on"}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := p.counts
	if normalizeStorageURL(p.countsRelay) != normalizeStorageURL(relay) || p.countsIdentity != identity {
		counts = blobstore.Counts{}
	}
	counts.Storage = "on"
	return counts
}

// StorageNeedsRefresh keeps startup and /status on the same request policy.
// It reserves an unanswered custom relay probe atomically across callers.
// An unanswered custom relay is probed at most once in this process, even if
// that request fails. Hosted unknown/off permissions retain approval discovery.
func StorageNeedsRefresh(home, relay, id string) bool {
	state, gating, answered := storagePolicy(home, relay, id)
	if state == "off" {
		return true
	}
	if answered || gating {
		return false
	}
	p := storageProcessFor(home)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.probeRelay == normalizeStorageURL(relay) && p.probeIdentity == id {
		return false
	}
	p.probeRelay, p.probeIdentity = normalizeStorageURL(relay), id
	return true
}
