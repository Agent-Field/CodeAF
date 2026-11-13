package raftdirectory

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
)

// R11/R27 bound the work of acknowledging an unchanged committed peer view.
func TestR11R27UnchangedPeerAcknowledgementWork(t *testing.T) {
	ctx := t.Context()
	var keys [3]ed25519.PrivateKey
	var public [][]byte
	for i := range keys {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(i + 1)
		keys[i] = ed25519.NewKeyFromSeed(seed)
		public = append(public, keys[i].Public().(ed25519.PublicKey))
	}
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
	cfg := cs.LegacyConfig{Cluster: "peer-work", Generation: 1, Identity: cs.IdentityID(root), SigningKey: root, EffectsKey: effects, Anchors: []uint64{1, 2, 3}, AnchorKeys: public, Now: now.UnixMilli()}
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
	router, err := host.AttachTransport(ctx, 1, keys)
	if err != nil {
		t.Fatal(err)
	}
	request := rafttransport.Request{Scope: host.peers.view.Scope, Node: 1, Incarnation: 1, Generation: 1, Applied: host.status[0].Applied}
	if err = request.Sign(keys[0]); err != nil {
		t.Fatal(err)
	}
	before := host.status[host.replica()].Applied
	allocations := testing.AllocsPerRun(5, func() {
		if _, err := router.Exchange(request, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	})
	if after := host.status[host.replica()].Applied; after != before {
		t.Fatalf("an empty acknowledgement appended application work: %d -> %d", before, after)
	}
	t.Logf("unchanged peer acknowledgement: %.0f allocations", allocations)
	const maxAllocations = 1000
	if allocations > maxAllocations {
		t.Fatalf("unchanged peer acknowledgement allocated %.0f times, limit %d", allocations, maxAllocations)
	}
}
