package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE WORKER'S DOOR INTO THE FACTORY ─────────────────────────────────────
//
// The worker loop asks for its factory every time it dials, through the same
// order the page does ([resolveFactory]), so a worker whose serving window
// closed follows the factory to whoever serves it next (or serves it itself).
// Its credentials are cached in worker.json, one per slot per factory url: a
// factory that forgot them (it restarted) answers 401 and the slot attaches
// again.

// factoryWorkerDial is one process's worker door: where the factory is now,
// and the credentials this machine holds there.
type factoryWorkerDial struct {
	mu      sync.Mutex
	floor   *store.Store
	resolve func() (factoryTarget, error)
}

func newFactoryWorkerDial(floor *store.Store, resolve func() (factoryTarget, error)) *factoryWorkerDial {
	return &factoryWorkerDial{floor: floor, resolve: resolve}
}

// dial is the worker door for slot into the factory this machine uses now,
// nil when none answers.
func (d *factoryWorkerDial) dial(slot int) *client.WorkerClient {
	t, err := d.resolve()
	if err != nil {
		return nil
	}
	creds := d.creds(t.url)
	credential := ""
	if slot < len(creds) {
		credential = creds[slot]
	}
	return client.Open(t.url, t.token).Worker(credential)
}

// desiredSlots reads the capacity the owner saved with slot 0's own worker
// credential, never a person's token. Slot 0 always runs, so it is the one
// credential a live worker is sure to hold. Anything short of a clear answer
// (not attached yet, an older factory, no factory) is "unknown" so the worker
// keeps the capacity it has. Each queued job a person said to start now adds
// one slot until a worker takes it; the factory, not this count, keeps every
// other job inside the owner's number.
func (d *factoryWorkerDial) desiredSlots(ctx context.Context) (int, bool) {
	w := d.dial(0)
	if w == nil || w.Credential() == "" {
		return 0, false
	}
	self, err := w.Self(ctx)
	if err != nil || self.Slots < 1 {
		return 0, false
	}
	return min(self.Slots+max(self.OverLimit, 0), api.MaxRunnerSlots), true
}

// creds is the credentials cached for base, one per slot.
func (d *factoryWorkerDial) creds(base string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.readCreds()[base]
}

// keep caches slot's credential for w's factory; "" forgets it. THIS
// MACHINE'S OWN FACTORY LISTENS ON A NEW PORT AT EACH START, so saving a
// credential for a loopback factory forgets the other loopback addresses that
// nothing listens on any more: those factories are gone, and their credentials
// would otherwise pile up forever. One that still listens (a second local
// factory, or a team factory reached through a tunnel on localhost) is kept,
// and a factory elsewhere is never touched.
func (d *factoryWorkerDial) keep(slot int, w *client.WorkerClient) {
	d.mu.Lock()
	defer d.mu.Unlock()
	path := filepath.Join(d.floor.Root(), "worker.json")
	release, err := factoryFileLock(path+".lock", false)
	defer release()
	if err != nil {
		return
	}
	all := d.readCreds()
	if factoryLoopback(w.Base()) {
		for base := range all {
			if base != w.Base() && factoryLoopback(base) && !factoryListening(base) {
				delete(all, base)
			}
		}
	}
	creds := all[w.Base()]
	for len(creds) <= slot {
		creds = append(creds, "")
	}
	creds[slot] = w.Credential()
	all[w.Base()] = creds
	if body, err := json.Marshal(all); err == nil {
		_ = writeFactoryPrivate(path, append(body, '\n'))
	}
}

// readCreds follows the floor rather than a mutable process environment.
// Atomic replacement keeps readers from observing a partial credential file.
func (d *factoryWorkerDial) readCreds() map[string][]string {
	all := map[string][]string{}
	if body, err := os.ReadFile(filepath.Join(d.floor.Root(), "worker.json")); err == nil {
		_ = json.Unmarshal(body, &all)
	}
	return all
}

// waitFactoryWorker leaves other windows on the current runner until it exits.
// The injected ticks make cancellation and takeover testable without sleeps.
func waitFactoryWorker(ctx context.Context, floor *store.Store, ticks <-chan time.Time) (func(), bool) {
	for ctx.Err() == nil {
		if release, ok := floor.TryWorker(); ok {
			if ctx.Err() != nil {
				release()
				return nil, false
			}
			return release, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-ticks:
		}
	}
	return nil, false
}

// attach is how this machine introduces itself: its name, its person, and the
// unassigned work it takes. ITS OWN FACTORY'S WORK IS ALL ITS PERSON'S, so a
// machine on a loopback factory takes every job; on a factory elsewhere it
// takes unassigned work only for the repositories it has a checkout of.
func (d *factoryWorkerDial) attach(ctx context.Context, person factory.Seam) (api.WorkerAttach, error) {
	handle, err := person.PersonHandle(ctx)
	if err != nil {
		return api.WorkerAttach{}, err
	}
	machine, _ := os.Hostname()
	name := machine
	a, attached := readFactoryAttachment()
	if attached && !factoryLoopback(a.URL) && handle == "" {
		return api.WorkerAttach{}, errors.New("codeaf needs your GitHub login to work for this factory")
	}
	if attached && a.Name != "" {
		name = a.Name
	}
	takes := map[string]bool{lease.EveryRepo: true}
	if attached && !factoryLoopback(a.URL) {
		takes = map[string]bool{}
		if dirs, err := d.floor.Checkouts(); err == nil {
			for repo := range dirs {
				takes[repo] = true
			}
		}
	}
	kind := api.RunnerCI
	if runtime.GOOS == "darwin" {
		kind = api.RunnerThisMac
		if attached {
			kind = api.RunnerMac
		}
	}
	checkouts := []api.Checkout{}
	if dirs, err := d.floor.Checkouts(); err == nil {
		for repo, path := range dirs {
			checkouts = append(checkouts, api.Checkout{Repo: repo, Path: path})
		}
	}

	return api.WorkerAttach{
		Machine: machine, Name: name + " (pid " + strconv.Itoa(os.Getpid()) + ")", Handle: handle,
		OS: runtime.GOOS, CodeafVersion: buildinfo.String(), TakesUnassigned: takes, Kind: kind, Slots: factoryBenches(), Checkouts: checkouts,
	}, nil
}

// factoryListening says something still accepts connections at base's
// address. A loopback port nobody listens on refuses at once, so the wait is
// only for a listener that is slow to accept.
func factoryListening(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Port() == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", u.Host, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// factoryLoopback says base is a factory on this machine.
func factoryLoopback(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}
