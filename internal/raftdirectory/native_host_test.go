package raftdirectory_test

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// R3/R4 require a disconnected caller to leave healthy durable actors usable.
// Client cancellation is an unknown response, never a disk-failure completion.
func TestR3R4ClientCancellationDoesNotStopNativeActors(t *testing.T) {
	f := frozenV1(t)
	clock := func() time.Time { return time.UnixMilli(f.Now) }
	source, e := directory.OpenSQLite(filepath.Join(t.TempDir(), "source.db"), clock)
	must(t, e)
	defer source.Close()
	root := key("root").Public().(ed25519.PublicKey)
	cfg := cs.LegacyConfig{Cluster: cluster, Generation: 1, Identity: cs.IdentityID(root), SigningKey: root, EffectsKey: key("effects").Public().(ed25519.PublicKey), Anchors: []uint64{1, 2, 3}, Now: f.Now}
	must(t, source.FreezeLegacy(ctx, cfg))
	manifest, e := source.InventoryLegacy(ctx)
	must(t, e)
	host, e := raftdirectory.OpenNative(ctx, t.TempDir(), source, manifest)
	must(t, e)
	defer host.Close()
	var command cs.Command
	for _, one := range f.Cases {
		if one.Name == "list" {
			proof, e := reqsign.Capture(frozenRequest(t, one, "http://localhost"), []byte(one.Body))
			must(t, e)
			command, e = cs.V1Command(cluster, f.Now, proof)
			must(t, e)
			break
		}
	}
	if command.Change.AdvanceTime == nil {
		t.Fatal("no original signed read fixture")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	// The first call may lose its response. The next real quorum read MUST work.
	_ = host.ReadV1(cancelled, command, false, func(_ context.Context, _ *raftstore.SQLite, _ raftstore.Application) error { return nil })
	must(t, host.ReadV1(ctx, command, false, func(owned context.Context, s *raftstore.SQLite, app raftstore.Application) error {
		if owned.Err() != nil {
			t.Fatal("actor inherited a disconnected client's cancellation")
		}
		_, _, e := s.ApplicationView(owned)
		return e
	}))
}
