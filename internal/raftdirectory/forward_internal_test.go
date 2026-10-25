package raftdirectory

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// R3/R6 require canceled callers waiting outside the actor to leave no new
// durable work behind. A later fresh read must still reach the healthy quorum.
func TestR3R6CanceledQueuedCallDoesNotEnterTheDurableActor(t *testing.T) {
	ctx := t.Context()
	root := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	effectSeed := make([]byte, ed25519.SeedSize)
	effectSeed[0] = 4
	effects := ed25519.NewKeyFromSeed(effectSeed).Public().(ed25519.PublicKey)
	now := time.UnixMilli(1700000000000)
	source, err := directory.OpenSQLite(filepath.Join(t.TempDir(), "source.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cfg := cs.LegacyConfig{Cluster: "cancelled-queue", Generation: 1, Identity: cs.IdentityID(root), SigningKey: root, EffectsKey: effects, Anchors: []uint64{1, 2, 3}, Now: now.UnixMilli()}
	if err = source.FreezeLegacy(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	manifest, err := source.InventoryLegacy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	host, err := OpenNative(ctx, t.TempDir(), source, manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	var entered atomic.Bool
	inspect := func(context.Context, *raftstore.SQLite, raftstore.Application) error { entered.Store(true); return nil }
	cancelled, cancel := context.WithCancel(ctx)
	host.opMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- host.Inspect(cancelled, inspect) }()
	<-started
	cancel()
	host.opMu.Unlock()
	if err = <-done; err == nil || entered.Load() {
		t.Fatal("R3/R6 canceled queued call entered the actor", err)
	}
	if err = host.Inspect(ctx, inspect); err != nil || !entered.Load() {
		t.Fatal("R3/R6 fresh quorum read failed", err)
	}
}
