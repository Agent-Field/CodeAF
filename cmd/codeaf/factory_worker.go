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

func factoryWorkerPath() string { return filepath.Join(v3FactoryRoot(), "worker.json") }

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

// creds is the credentials cached for base, one per slot.
func (d *factoryWorkerDial) creds(base string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return readFactoryWorkerCreds()[base]
}

// keep caches slot's credential for w's factory; "" forgets it.
func (d *factoryWorkerDial) keep(slot int, w *client.WorkerClient) {
	d.mu.Lock()
	defer d.mu.Unlock()
	all := readFactoryWorkerCreds()
	creds := all[w.Base()]
	for len(creds) <= slot {
		creds = append(creds, "")
	}
	creds[slot] = w.Credential()
	all[w.Base()] = creds
	if body, err := json.Marshal(all); err == nil {
		_ = writeFactoryPrivate(factoryWorkerPath(), append(body, '\n'))
	}
}

func readFactoryWorkerCreds() map[string][]string {
	all := map[string][]string{}
	if body, err := os.ReadFile(factoryWorkerPath()); err == nil {
		_ = json.Unmarshal(body, &all)
	}
	return all
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
	return api.WorkerAttach{
		Machine: machine, Name: name + " (pid " + strconv.Itoa(os.Getpid()) + ")", Handle: handle,
		OS: runtime.GOOS, CodeafVersion: buildinfo.String(), TakesUnassigned: takes,
	}, nil
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
