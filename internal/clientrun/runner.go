// Package clientrun keeps scoped clients current and executes their signed
// queued appwire calls. The relay holds ciphertext; the desktop engine remains
// the only implementation of the methods a client reads or calls.
package clientrun

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"golang.org/x/crypto/nacl/box"
)

// Config supplies the same engine the desktop serves. Home holds local grants
// and durable execution receipts; it must belong to one running executor.
type Config struct {
	Home      string
	Relay     string
	Signer    reqsign.Signer
	Directory directory.Client
	Engine    appwire.Engine
	HTTP      *http.Client
	Now       func() time.Time
	// After supplies the refresh clock; nil uses time.After.
	After func(time.Duration) <-chan time.Time
	Log   func(Receipt) error
}

// Runner serializes rounds while keeping settings available during a call.
// No paired clients means no directory reads, engine calls, or running timers.
type Runner struct {
	lock    *os.File
	cfg     Config
	run     sync.Mutex
	mu      sync.Mutex
	state   state
	changed chan struct{}
	hashes  map[string]publication
}

type publication struct {
	hash [32]byte
	at   time.Time
}

// Open reads local pairing state without making keys or contacting the relay.
func Open(cfg Config) (*Runner, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	r := &Runner{cfg: cfg, changed: make(chan struct{}, 1), hashes: map[string]publication{}}
	if err := os.MkdirAll(filepath.Dir(StatePath(cfg.Home)), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(StatePath(cfg.Home)+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		lock.Close()
		if filelock.IsBusy(err) {
			return nil, fmt.Errorf("client runner state already open: %s", StatePath(cfg.Home))
		}
		return nil, err
	}
	// The separate lock inode survives atomic replacement of runner.json.
	r.lock = lock
	opened := false
	defer func() {
		if !opened {
			_ = r.Close()
		}
	}()
	raw, err := os.ReadFile(StatePath(cfg.Home))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &r.state); err != nil {
			return nil, err
		}
	}
	if r.state.Clients == nil {
		r.state.Clients = map[string]Client{}
	}
	if r.state.Receipts == nil {
		r.state.Receipts = map[string]Receipt{}
	}
	opened = true
	return r, nil
}

// Close waits for the current round and releases the process lock. The caller
// cancels and joins Run first so no later round can use the closed state.
func (r *Runner) Close() error {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lock == nil {
		return nil
	}
	err := r.lock.Close()
	r.lock = nil
	return err
}

// StatePath names the executor's local pairing and receipt file.
func StatePath(home string) string { return filepath.Join(home, "clients", "runner.json") }

// Changed requests a coalesced round after a publish or an engine event. It is
// safe on the publishing path and does no work when nobody is paired.
func (r *Runner) Changed() {
	if len(r.active()) == 0 {
		return
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

// Run refreshes on change and on a bounded timer, backing off failed rounds.
// With no active clients it waits only for a settings change or cancellation.
func (r *Runner) Run(ctx context.Context) {
	delay := 5 * time.Second
	for {
		if len(r.active()) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-r.changed:
			}
			continue
		}
		changes := r.changed
		if err := r.Step(ctx); err != nil {
			delay = min(delay*2, 5*time.Minute)
			// Change notifications cannot defeat network backoff during an outage.
			changes = nil
		} else {
			delay = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-changes:
		case <-r.cfg.After(delay):
		}
	}
}

// Step performs one deterministic round; tests and callers with their own
// clocks use it without starting the timer. Each client fails independently.
func (r *Runner) Step(ctx context.Context) error {
	r.run.Lock()
	defer r.run.Unlock()
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
	clients := r.active()
	if len(clients) == 0 {
		return nil
	}
	listing, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range clients {
		d, ok := listing.Devices[c.ID]
		if !ok || d.Revoked || d.Kind != "client" {
			continue
		}
		if err := r.drain(ctx, c); err != nil {
			errs = append(errs, err)
		}
		if err := r.publish(ctx, c, listing); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) active() []Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Client, 0, len(r.state.Clients))
	for _, c := range r.state.Clients {
		if !c.Paused {
			out = append(out, c)
		}
	}
	return out
}

func (r *Runner) boxKey() error {
	if len(r.state.Secret) == 32 {
		return nil
	}
	_, priv, err := box.GenerateKey(rand.Reader)
	if err == nil {
		r.state.Secret = priv[:]
	}
	return err
}
