package syncsetup

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
)

const StorageRefreshEvery = 10 * time.Minute

// One coordinator owns both approval discovery and startup waits in a process.
// Running batchers use their own bounded retry and never poll permission files.
var storageProcesses = struct {
	sync.Mutex
	homes map[string]*storageProcess
}{homes: make(map[string]*storageProcess)}

type storageProcess struct {
	mu                          sync.Mutex
	home                        string
	users                       int
	next                        uint64
	waiting                     map[uint64]func()
	opened                      map[uint64]func()
	stop                        func() bool
	ctx                         context.Context
	lastRefresh                 time.Time
	active                      bool
	dirty                       bool
	counts                      blobstore.Counts
	countsRelay, countsIdentity string
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
			if !relay.On() || !IsHostedRelay(relay.URL) || identity.Solo(home) {
				return "", "", ""
			}
			id, err := identity.Load(home)
			if err != nil {
				return "", "", ""
			}
			return relay.URL, id.ID(), StorageState(home, relay.URL, id.ID())
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
// every ten minutes while hosted storage is unknown or off. Callers join its
// lifetime independently; chat opening never waits for a network answer.
func StartStorageRefresh(ctx context.Context, home string, initial ...bool) func() {
	p := storageProcessFor(home)
	p.mu.Lock()
	p.users++
	first := p.users == 1
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
			if p.users == 0 && len(p.waiting) == 0 {
				p.disarm()
			}
			p.mu.Unlock()
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
	counts, err := store.Stats(ctx)
	storageProcesses.Lock()
	p := storageProcesses.homes[s.Home]
	storageProcesses.Unlock()
	if p != nil {
		p.mu.Lock()
		p.lastRefresh = p.now()
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
	if p.active {
		p.dirty = true
		p.mu.Unlock()
		return
	}
	p.active = true
	p.disarm()
	p.mu.Unlock()
	relay, _, state := p.read()
	if relay != "" && state != "on" && refresh {
		ctx, cancel := context.WithTimeout(p.ctx, requestTimeout)
		_ = p.fetch(ctx)
		cancel()
		p.mu.Lock()
		p.lastRefresh = p.now()
		p.mu.Unlock()
		relay, _, state = p.read()
	}
	p.mu.Lock()
	p.active = false
	again := p.dirty
	p.dirty = false
	var callbacks []func()
	if state == "on" {
		for id, fn := range p.waiting {
			callbacks = append(callbacks, fn)
			delete(p.waiting, id)
		}
	}
	if relay != "" && state != "on" && (p.users > 0 || len(p.waiting) > 0) {
		wait := StorageRefreshEvery
		if p.users > 0 && !p.lastRefresh.IsZero() {
			wait = max(time.Millisecond, StorageRefreshEvery-p.now().Sub(p.lastRefresh))
		}
		if len(p.waiting) > 0 {
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
