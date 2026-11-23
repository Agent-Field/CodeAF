// The Model Pool's index, kept fresh at start-up.
//
// The index is a measurement document the pool publishes: how each model did
// in each seat across the installs that share their judged runs. A machine
// that fetches one keeps it under the profile's pool directory, where
// `codeaf pool` reads it. The one fetch this file makes runs in a goroutine
// started where the pool is wired, and it never blocks a run: it writes the
// puller's own cache and the change is read at the NEXT start.
//
// Nothing here seats a crew. The crew is routed per task from an evidence
// table the build carries (internal/crewroute); reading the pool's cells into
// that router as measured evidence is follow-up work, not something this file
// half-does.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/pull"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// poolRefreshGo is [guard.Go] behind a variable so a test can prove that no
// goroutine is started when the build carries no key.
var poolRefreshGo = func(scope string, fn func()) { guard.Go(scope, fn) }

// poolErrands is the lifetime of the start-up errands one owner's wiring
// started: the context they are cancelled by and the WaitGroup that answers
// when the last of them has returned.
//
// IT EXISTS BECAUSE A FIRE-AND-FORGET ERRAND OUTLIVES THE PROCESS THAT STARTED
// IT. Both errands write under the profile's pool directory — the refresh its
// doc.json and its signature, the push the outbox and the install nonce — and
// [guard.Go] joins nothing at shutdown, so a process that closed left them
// running against a profile nobody was waiting for. In a test whose profile is
// a temporary directory, that is the directory removed out from under a live
// writer; on a door that reopens on another profile it is a write into a
// directory the process no longer owns. Neither errand is on a run's path, so
// joining them at close costs the person nothing.
type poolErrands struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// poolErrandSet holds each owner's tracker until that owner closes. The handle
// is the key so two owners of one profile never share cancellation or release
// each other's hold. This is the only table, and close removes its own entry.
var (
	poolErrandsMu sync.Mutex
	poolErrandSet = map[*poolErrands]string{}
)

// poolErrandsStart gives each owner a lifetime it keeps even after close, so a
// late callback is refused by that same tracker rather than starting untracked.
func poolErrandsStart(profileDir string) *poolErrands {
	poolErrandsMu.Lock()
	defer poolErrandsMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	held := &poolErrands{ctx: ctx, cancel: cancel}
	poolErrandSet[held] = profileDir
	return held
}

// stopPoolErrands cancels and joins only the errands this owner started, up to
// poolCloseBudget. A nil handle and an already closed owner are no-ops.
// Cancelled model answers are discarded before they can write, even when a
// provider ignores cancellation beyond that bound.
func stopPoolErrands(held *poolErrands) {
	if held != nil {
		held.stop()
	}
}

// forgetPoolErrands releases only this owner's table entry. Waiting happens
// outside the table lock so one stuck owner cannot delay another owner's close.
func forgetPoolErrands(held *poolErrands) {
	poolErrandsMu.Lock()
	defer poolErrandsMu.Unlock()
	delete(poolErrandSet, held)
}

// poolErrandGoCtx registers work on the handle its owner retained, so another
// owner's close cannot send it through the bare seam. Only a standalone
// startPoolIndexRefresh without an owner uses that seam, for loader tests.
func poolErrandGoCtx(held *poolErrands, scope string, fn func(context.Context)) {
	if held == nil {
		poolRefreshGo(scope, func() { fn(context.Background()) })
		return
	}
	held.goCtx(scope, fn)
}

// goCtx refuses work after shutdown, including a late landing from an agent
// whose reporting goroutine had not reached the hook when the owner closed.
func (held *poolErrands) goCtx(scope string, fn func(context.Context)) {
	held.mu.Lock()
	defer held.mu.Unlock()
	if held.closed {
		return
	}
	held.wg.Add(1)
	poolRefreshGo(scope, func() {
		defer held.wg.Done()
		fn(held.ctx)
	})
}

// poolCloseBudget keeps quitting prompt even if a provider ignores cancellation.
// Such a response is discarded before any score or sweep record is written.
const poolCloseBudget = time.Second

func (held *poolErrands) stop() {
	done := held.beginStop()
	if done == nil {
		return
	}
	forgetPoolErrands(held)
	timer := time.NewTimer(poolCloseBudget)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// beginStop closes the tracker once, cancels its errands and starts the one
// joiner, and answers the channel that joiner closes. The locked section is its
// own function so the unlock is a defer (internal/guard's lock-defer law).
func (held *poolErrands) beginStop() chan struct{} {
	held.mu.Lock()
	defer held.mu.Unlock()
	if held.closed {
		return nil
	}
	held.closed = true
	held.cancel()
	held.done = make(chan struct{})
	guard.Go("pool/close", func() {
		held.wg.Wait()
		close(held.done)
	})
	return held.done
}

// startPoolIndexRefresh is the loader's own tail: it starts the background
// fetch that keeps the cache fresh, and starts NOTHING when the mode forbids
// reading or the build carries no public key to check a fetched document under
// — which, on a build with no key compiled in, is every run.
func startPoolIndexRefresh(held *poolErrands, profileDir string, cfg poolcfg.Config, keys []ed25519.PublicKey) {
	if !cfg.CanRead() || len(keys) == 0 {
		return
	}
	poolErrandGoCtx(held, "pool/index", func(ctx context.Context) { refreshPoolIndex(ctx, profileDir, cfg, keys) })
}

// refreshPoolIndex fetches a fresh index and lets the puller's own cache keep
// it for the NEXT start. A changed document is NOT swapped into the running
// process: a run's picks must not move under it, so a pick here always reads
// the index this process was seated with, and the fresh one is read at the
// next start.
//
// It is quiet by design. A dead host, a timeout or a signature that does not
// check is an ordinary state for a fetch nothing waited for, so the error is
// said only under the debug record's switch and never on the surface.
//
// THE MIRROR IS ASKED WHEN THE RELAY DOES NOT ANSWER. Any failure of the
// primary address other than a signature failure falls through to the mirror,
// which is the relay's own document copied elsewhere and shares the cache
// directory, so a document the mirror serves is kept only when its version is
// not lower than the one already cached. A signature failure is its own
// statement about the primary's bytes, and a copy of the same document cannot
// vouch for them, so it is not asked; an empty mirror address turns the
// fallback off.
func refreshPoolIndex(ctx context.Context, profileDir string, cfg poolcfg.Config, keys []ed25519.PublicKey) {
	poolDir := config.ProfilePath(profileDir, "pool")
	if err := poolIndexPull(ctx, cfg.IndexURL, poolDir, cfg.TTL, keys); err != nil {
		if errors.Is(err, pull.ErrBadSignature) || cfg.MirrorURL == "" {
			if trace.Enabled() {
				log.Printf("model pool: index refresh: %v", err)
			}
			return
		}
		if err := poolIndexPull(ctx, cfg.MirrorURL, poolDir, cfg.TTL, keys); err != nil && trace.Enabled() {
			log.Printf("model pool: index refresh: %v", err)
		}
	}
}

// poolIndexPull fetches url through the puller's own cache under the config's
// TTL, and returns the fetch's error — nil when a document was read or a young
// cache answered.
func poolIndexPull(ctx context.Context, url, poolDir string, ttl time.Duration, keys []ed25519.PublicKey) error {
	puller := &pull.Puller{
		URL:      url,
		Keys:     keys,
		CacheDir: poolDir,
		TTL:      ttl,
		Budget:   pull.DefaultBudget,
	}
	_, err := puller.Pull(ctx)
	return err
}

// wirePoolIndex starts this process's pool errands: the index refresh, and
// the push of rows a previous run judged and could not hand over. It is
// called once at start-up, from the places the catalog is seated.
func wirePoolIndex(profileDir string) *poolErrands {
	held := poolErrandsStart(profileDir)
	held.wireIndex(profileDir)
	return held
}

// wireIndex seats startup work on an existing owner so the shared headless
// catalog can preserve its one-time wiring without creating a second lifetime.
func (held *poolErrands) wireIndex(profileDir string) {
	cfg := config.ModelPoolAt(profileDir)
	// Resolve the default profile before starting workers so their paths cannot
	// move to a later caller's home while a response is in flight.
	profileDir = config.ProfilePath(profileDir, "")
	keys := poolTrustedKeys(cfg)
	startPoolIndexRefresh(held, profileDir, cfg, keys)
	// The rows a previous run judged and could not hand over leave at once,
	// on their own goroutine behind the same guard the refresh uses: a
	// start-up errand, bounded by its own budget, and never on the run's
	// path.
	if cfg.CanSend() {
		held.goCtx("pool/push", func(ctx context.Context) { poolPush(ctx, profileDir, cfg, poolPushBudget) })
	}
}
