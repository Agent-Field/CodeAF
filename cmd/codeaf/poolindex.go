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

// poolErrands is the lifetime of the start-up errands one profile's wiring
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
}

// poolErrandSet holds the errands of each wired profile, so [stopPoolErrands]
// can find the ones a closing process must join. It is keyed by the profile
// directory rather than carried on a process because [wirePoolIndex] is a plain
// function several doors call, and stopping takes the entry out — so a table
// answering a lookup per wiring never grows.
var (
	poolErrandsMu sync.Mutex
	poolErrandSet = map[string]*poolErrands{}
)

// poolErrandsStart seats the tracker for a profile and answers it, so the
// errands wired below register on one context and one WaitGroup. A profile
// wired twice — the host road assembles its options once per launch on the
// same profile — keeps the tracker it has: replacing it would orphan the first
// wiring's errands, which is the leak this tracker exists to close.
func poolErrandsStart(profileDir string) *poolErrands {
	poolErrandsMu.Lock()
	defer poolErrandsMu.Unlock()
	if held := poolErrandSet[profileDir]; held != nil {
		return held
	}
	ctx, cancel := context.WithCancel(context.Background())
	held := &poolErrands{ctx: ctx, cancel: cancel}
	poolErrandSet[profileDir] = held
	return held
}

// stopPoolErrands cancels the profile's start-up errands and waits for them to
// return, so that once a process closes nothing it started is still writing
// under the profile.
//
// IT IS THE HALF THE ERRANDS' OWN BUDGETS DO NOT KEEP. A budget bounds one
// fetch, not the life of the goroutine: an unstopped refresh whose relay does
// not answer is still running when the process — or, in a test, the temporary
// profile — is gone, which is a write into a directory nobody owns. It is
// idempotent: a profile with no live errands is a no-op.
func stopPoolErrands(profileDir string) {
	held := takePoolErrands(profileDir)
	if held == nil {
		return
	}
	held.cancel()
	held.wg.Wait()
}

// takePoolErrands removes and returns the profile's tracker under the lock, so
// the wait that follows happens with the lock released: a Wait under the lock
// would hold every other profile's start-up behind one profile's shutdown.
func takePoolErrands(profileDir string) *poolErrands {
	poolErrandsMu.Lock()
	defer poolErrandsMu.Unlock()
	held := poolErrandSet[profileDir]
	delete(poolErrandSet, profileDir)
	return held
}

// poolErrandGo starts one errand on the profile's tracker, so [stopPoolErrands]
// waits for it, through the same [poolRefreshGo] seam every pool goroutine
// starts through. A profile wired without a tracker — a bare
// [startPoolIndexRefresh] in a test — is the plain seam with nothing to join.
func poolErrandGo(profileDir, scope string, fn func()) {
	poolErrandGoCtx(profileDir, scope, func(context.Context) { fn() })
}

// poolErrandGoCtx is [poolErrandGo] for an errand that must be able to OBSERVE
// the tracker's cancellation rather than only be waited for: the one argument
// is the context [stopPoolErrands] cancels, so a long wait of the errand's own
// (the model warm's catalog resolve) ends at the close instead of running past
// it. An errand that ignores the context is still joined by the wait.
func poolErrandGoCtx(profileDir, scope string, fn func(context.Context)) {
	held := joinPoolErrands(profileDir)
	if held == nil {
		poolRefreshGo(scope, func() { fn(context.Background()) })
		return
	}
	poolRefreshGo(scope, func() {
		defer held.wg.Done()
		fn(held.ctx)
	})
}

// joinPoolErrands counts one more errand on the profile's tracker under the
// lock and returns the tracker, or nil when the profile was wired without one.
func joinPoolErrands(profileDir string) *poolErrands {
	poolErrandsMu.Lock()
	defer poolErrandsMu.Unlock()
	held := poolErrandSet[profileDir]
	if held != nil {
		held.wg.Add(1)
	}
	return held
}

// startPoolIndexRefresh is the loader's own tail: it starts the background
// fetch that keeps the cache fresh, and starts NOTHING when the mode forbids
// reading or the build carries no public key to check a fetched document under
// — which, on a build with no key compiled in, is every run.
func startPoolIndexRefresh(ctx context.Context, profileDir string, cfg poolcfg.Config, keys []ed25519.PublicKey) {
	if !cfg.CanRead() || len(keys) == 0 {
		return
	}
	poolErrandGo(profileDir, "pool/index", func() { refreshPoolIndex(ctx, profileDir, cfg, keys) })
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
func wirePoolIndex(profileDir string) {
	cfg := config.ModelPoolAt(profileDir)
	// The errands below run on this profile's tracker so the process that
	// seated them can join them when it closes ([stopPoolErrands]).
	held := poolErrandsStart(profileDir)
	startPoolIndexRefresh(held.ctx, profileDir, cfg, poolTrustedKeys(cfg))
	// The rows a previous run judged and could not hand over leave at once,
	// on their own goroutine behind the same guard the refresh uses: a
	// start-up errand, bounded by its own budget, and never on the run's
	// path.
	if cfg.CanSend() {
		poolErrandGo(profileDir, "pool/push", func() { poolPush(held.ctx, profileDir, cfg, poolPushBudget) })
	}
}
