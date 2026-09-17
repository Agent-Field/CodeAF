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
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pool/index"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/pull"
	"github.com/Agent-Field/codeaf/internal/pool/record"
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

// startPoolIndexRefresh is the loader's own tail: it starts the background
// fetch that keeps the cache fresh, and starts NOTHING when the mode forbids
// reading or the build carries no public key to check a fetched document under
// — which, on a build with no key compiled in, is every run.
func startPoolIndexRefresh(ctx context.Context, profileDir string, cfg poolcfg.Config, keys []ed25519.PublicKey) {
	if !cfg.CanRead() || len(keys) == 0 {
		return
	}
	poolRefreshGo("pool/index", func() { refreshPoolIndex(ctx, profileDir, cfg, keys) })
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
func refreshPoolIndex(ctx context.Context, profileDir string, cfg poolcfg.Config, keys []ed25519.PublicKey) {
	puller := &pull.Puller{
		URL:      cfg.IndexURL,
		Keys:     keys,
		CacheDir: config.ProfilePath(profileDir, "pool"),
		TTL:      cfg.TTL,
		Budget:   pull.DefaultBudget,
	}
	if _, err := puller.Pull(ctx); err != nil && trace.Enabled() {
		log.Printf("model pool: index refresh: %v", err)
	}
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
	startPoolIndexRefresh(context.Background(), profileDir, cfg, poolPublicKeys)
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
