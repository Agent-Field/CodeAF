package raftdirectory

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

// R5/R8/R26 require the actual native maintenance clock to elect a surviving
// anchor after leader loss. A failed authority read must not suppress ticks.
func TestR5R8R26NativeMaintenanceElectsWithoutLeader(t *testing.T) {
	ctx := context.Background()
	keys := make([]ed25519.PrivateKey, 3)
	public := make([][]byte, 3)
	for i := range keys {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(i + 1)
		keys[i] = ed25519.NewKeyFromSeed(seed)
		public[i] = keys[i].Public().(ed25519.PublicKey)
	}
	root := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	effectsSeed := make([]byte, ed25519.SeedSize)
	effectsSeed[0] = 4
	effects := ed25519.NewKeyFromSeed(effectsSeed).Public().(ed25519.PublicKey)
	now := int64(1700000000000)
	source, err := directory.OpenSQLite(filepath.Join(t.TempDir(), "source.db"), func() time.Time { return time.UnixMilli(now) })
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cfg := cs.LegacyConfig{Cluster: "maintenance-loss", Generation: 1, Identity: cs.IdentityID(root), SigningKey: root, EffectsKey: effects, Anchors: []uint64{1, 2, 3}, AnchorKeys: public, Now: now}
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
	if err = host.InitializeMembership(ctx, keys[0]); err != nil {
		t.Fatal(err)
	}
	lost := host.leader()
	if lost < 0 {
		t.Fatal("no initial leader")
	}
	if err = host.stores[lost].Close(); err != nil {
		t.Fatal(err)
	}
	host.stores[lost] = nil
	host.nodes[lost] = nil
	host.status[lost] = raftcore.Status{}
	// Recover surviving actors from their own stores with fixed timing seeds.
	// The production clock, not startup's explicit campaigns, must elect them.
	for i, store := range host.stores {
		if store == nil {
			continue
		}
		recovered, e := store.Recover(ctx)
		if e != nil {
			t.Fatal(e)
		}
		host.nodes[i], e = raftcore.New(raftcore.Config{ClusterUUID: cfg.Cluster, NodeID: uint64(i + 1), Protocol: raftcore.ProtocolVersion, Seed: uint64(i + 8), Anchors: cfg.Anchors}, recovered.Core)
		if e != nil {
			t.Fatal(e)
		}
		if e = host.drive(ctx, i, raftcore.Poll{}); e != nil {
			t.Fatal(e)
		}
	}
	if err = host.drain(ctx); err != nil {
		t.Fatal(err)
	}
	for elapsed := int64(0); elapsed <= 60000; elapsed += cs.TickEveryMillis {
		_, _ = host.Maintain(ctx, elapsed, now+elapsed, keys[0])
		if host.leader() >= 0 {
			_, _, err = host.barrier(ctx)
			if err != nil {
				t.Fatal("surviving majority cannot read", err)
			}
			t.Logf("surviving majority elected after %d virtual ms", elapsed)
			return
		}
	}
	t.Fatal("R5/R8/R26 maintenance stopped election ticks without a leader")
}
