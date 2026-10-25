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

// R4/R26/R27 require fresh quorum reads without repeating an already durable
// logical time. New ingress time still becomes durable before inspection.
func TestR4R26R27ReadCommitsOnlyChangedTime(t *testing.T) {
	f := frozenV1(t)
	source, err := directory.OpenSQLite(filepath.Join(t.TempDir(), "source.db"), func() time.Time { return time.UnixMilli(f.Now) })
	must(t, err)
	defer source.Close()
	root := key("root").Public().(ed25519.PublicKey)
	cfg := cs.LegacyConfig{Cluster: cluster, Generation: 1, Identity: cs.IdentityID(root), SigningKey: root, EffectsKey: key("effects").Public().(ed25519.PublicKey), Anchors: []uint64{1, 2, 3}, Now: f.Now}
	must(t, source.FreezeLegacy(ctx, cfg))
	manifest, err := source.InventoryLegacy(ctx)
	must(t, err)
	host, err := raftdirectory.OpenNative(ctx, t.TempDir(), source, manifest)
	must(t, err)
	defer host.Close()
	var command cs.Command
	for _, one := range f.Cases {
		if one.Name == "list" {
			proof, err := reqsign.Capture(frozenRequest(t, one, "http://localhost"), []byte(one.Body))
			must(t, err)
			command, err = cs.V1Command(cluster, f.Now, proof)
			must(t, err)
			break
		}
	}
	if command.V1 == nil || command.Change.AdvanceTime == nil {
		t.Fatal("no original signed read fixture")
	}
	var before raftstore.Application
	must(t, host.Inspect(ctx, func(_ context.Context, _ *raftstore.SQLite, app raftstore.Application) error {
		before = app
		return nil
	}))
	read := func(c cs.Command) raftstore.Application {
		t.Helper()
		var got raftstore.Application
		must(t, host.ReadV1(ctx, c, false, func(_ context.Context, _ *raftstore.SQLite, app raftstore.Application) error {
			got = app
			return nil
		}))
		return got
	}
	unchanged := read(command)
	if unchanged.Applied != before.Applied || unchanged.LogicalTime != before.LogicalTime {
		t.Fatalf("unchanged time appended: before=%+v after=%+v", before, unchanged)
	}
	command.Now += 1000
	advanced := read(command)
	if advanced.Applied != before.Applied+1 || advanced.LogicalTime != command.Now {
		t.Fatalf("new ingress time was not committed before inspection: before=%+v after=%+v", before, advanced)
	}
	if repeated := read(command); repeated.Applied != advanced.Applied || repeated.LogicalTime != advanced.LogicalTime {
		t.Fatalf("repeated time appended: before=%+v after=%+v", advanced, repeated)
	}
	invalid := command
	invalid.Cluster = "another-cluster"
	called := false
	if err := host.ReadV1(ctx, invalid, false, func(context.Context, *raftstore.SQLite, raftstore.Application) error {
		called = true
		return nil
	}); err == nil || called {
		t.Fatalf("invalid proof reached inspection: error=%v called=%v", err, called)
	}
}
