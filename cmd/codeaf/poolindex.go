// The Model Pool's index, seated at start-up.
//
// The index is a measurement document the crew picker reads a seat's quality
// from (config.PoolQualityMetric, through config.AutoIndex). A machine that
// has fetched one keeps it under the profile's pool directory; a machine that
// has never fetched one still has the index the binary carries
// (internal/pool/index's Seed), so a tier row that says `auto` has numbers on
// its first run rather than none.
//
// READ ONCE, NEVER ON THE RUN'S PATH. [poolIndexFor] parses the cache beside
// the seed exactly once and hands back a function answering the value it
// already has, so a pick asks no disk and no lock. The one fetch this file
// makes runs in a goroutine started where the loader is seated, and it never
// answers a pick: it writes the puller's own cache and the change is read at
// the NEXT start. A run's picks must not move under it.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pool/index"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/pull"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/pool/shape"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// poolIndexFor builds this process's one index reader. The read is
// start-up work and nothing on a run's path: the document is parsed here,
// once, and the returned function answers the value already in hand, so a
// pick over many tiers does no disk and no decode per call.
//
// THE CACHE WINS ONLY WHEN IT IS FRESHER. A cached doc.json is handed to
// index.Fallback beside the embedded seed, which trusts whichever declares
// the newer generated day — a young cache keeps its numbers, an old or
// unparsable one leaves the seed in place. A mode that forbids reading
// (poolcfg.Off) answers no index at all, which is the same nothing the seam
// read before this file existed.
//
// THE ONE ERROR IT CANNOT RECOVER FROM is an embedded seed that does not
// parse; that answers nil rather than panicking, though a test in the index
// package parses the very bytes this build carries.
func poolIndexFor(profileDir string, cfg poolcfg.Config, now func() time.Time) func() *index.Index {
	if !cfg.CanRead() {
		return func() *index.Index { return nil }
	}
	var cached []byte
	if doc, err := os.ReadFile(filepath.Join(config.ProfilePath(profileDir, "pool"), "doc.json")); err == nil {
		cached = doc
	}
	held, err := index.Fallback(cached, index.Seed())
	if err != nil {
		seed, seedErr := index.SeedIndex()
		if seedErr != nil {
			return func() *index.Index { return nil }
		}
		held = seed
	}
	return func() *index.Index { return held }
}

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
	held := joinPoolErrands(profileDir)
	if held == nil {
		poolRefreshGo(scope, fn)
		return
	}
	poolRefreshGo(scope, func() {
		defer held.wg.Done()
		fn()
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

// wirePoolIndex seats this process's pool readings beside its catalog: it
// hands the index reader to seat resolution (config.AutoIndex), the install's
// own judged scores beside them (config.AutoOwnCells, read off the own sheet
// under the pool directory), and starts the refresh beside both. It is called
// once at start-up, from the same places config.AutoModels is set, so a tier
// row that says `auto` resolves against a prior on every door that resolves a
// seat.
func wirePoolIndex(profileDir string) {
	cfg := config.ModelPoolAt(profileDir)
	config.AutoIndex = poolIndexFor(profileDir, cfg, time.Now)
	config.AutoOwnCells = poolOwnCellsFor(profileDir, cfg)
	config.AutoShapes = poolShapesFor(session.UsageLedgerPath())
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

// poolShapesFor builds this process's one reader of what real tasks spent on
// each seat: the usage ledger at path is read once, here, and the seats'
// shapes learned from it (crewpick.ShapesFrom over the defaults) are what the
// returned function answers, so a pick prices its front on measured volumes
// and does no disk per call. THE READ IS START-UP WORK AND NOTHING ON A RUN'S
// PATH, the posture the own-sheet reader keeps. A ledger that cannot be read
// is said under the debug switch and answers nil, which the seam reads as the
// defaults: a bill priced the old way is a loss, not a fault a pick should
// stop for.
func poolShapesFor(path string) func() map[crewpick.Seat]crewpick.SeatShape {
	tasks, err := shape.Tasks(path)
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: usage ledger: %v", err)
		}
		return nil
	}
	if len(tasks) == 0 {
		return nil
	}
	shapes := crewpick.ShapesFrom(crewpick.DefaultShapes(), tasks)
	return func() map[crewpick.Seat]crewpick.SeatShape { return shapes }
}

// poolOwnCellsFor builds this process's one reader of the install's own judged
// scores. THE READ IS START-UP WORK AND NOTHING ON A RUN'S PATH, the same
// posture the index reader keeps: the own sheet under the profile's pool
// directory is read and parsed here, once, and the returned function answers
// the cells already in hand, so a pick does no disk and no decode per call.
// A mode that forbids reading answers nil — the same nothing the seam reads
// before any sheet exists — and so does a sheet that does not parse, which is
// said under the debug record's switch and read as absent rather than fatal:
// an own sheet is the install's own evidence, and a broken one is a loss, not
// a fault a pick should stop for.
func poolOwnCellsFor(profileDir string, cfg poolcfg.Config) func() []crewpick.Cell {
	if !cfg.CanRead() {
		return nil
	}
	sheet, err := record.LoadSheet(record.OwnSheetPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		if trace.Enabled() {
			log.Printf("model pool: own sheet: %v", err)
		}
		return nil
	}
	cells := record.Cells(sheet)
	return func() []crewpick.Cell { return cells }
}
