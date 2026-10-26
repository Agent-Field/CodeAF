package raftsim

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"google.golang.org/protobuf/proto"
)

// R3/R12/R13 compare the simulated durability protocol with the real native
// adapter. SQLite and the pure snapshot codec receive the exact production
// batches observed by the simulator, with no alternative consensus algorithm.
func TestR3R12R13SimulatedDiskMatchesNativeStoreAndSnapshot(t *testing.T) {
	s := group(t, Config{Seed: 311, RecordDisk: true})
	create(t, s, "a")
	leader := s.Leader()
	submit(t, s, leader, model.Call{Kind: "acquire", Actor: "b", Cell: "a", Expected: observed(s.Nodes[leader].Stable.App.Cells["a"]), Force: true, Session: "exact", Sequence: 1, Digest: "opaque"})
	submit(t, s, leader, model.Call{Kind: "revoke", Actor: "owner", Target: "removed"})
	submit(t, s, leader, model.Call{Kind: "set_vault", Actor: "owner", NewVault: "opaque-vault"})
	ctx := context.Background()
	stores := make([]*raftstore.SQLite, len(s.Nodes))
	applied := make([]uint64, len(s.Nodes))
	for node := range stores {
		store, err := raftstore.Open(filepath.Join(t.TempDir(), "member.sqlite"), raftstore.Identity{ClusterUUID: clusterUUID, NodeID: uint64(node + 1), Protocol: raftcore.ProtocolVersion})
		if err != nil {
			t.Fatal(err)
		}
		stores[node] = store
		t.Cleanup(func() { store.Close() })
	}
	key := raftstore.Key{Namespace: raftstore.State, Key: "probe"}
	for _, tx := range s.journal {
		store := stores[tx.Node]
		if tx.Persist != nil {
			if err := store.Persist(ctx, *tx.Persist); err != nil {
				t.Fatal(err)
			}
		}
		if tx.Apply != nil {
			update := raftstore.Update{ExpectedApplied: applied[tx.Node], LogicalTime: tx.App.Now, Writes: []raftstore.Write{{Key: key, Value: encodedApp(tx.App)}}}
			if err := store.Apply(ctx, *tx.Apply, update); err != nil {
				t.Fatal(err)
			}
			applied[tx.Node] = tx.Apply.Index
		}
	}
	for node, store := range stores {
		recovered, err := store.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		d := s.Nodes[node].Stable
		if recovered.Core.Applied != d.Applied || !proto.Equal(recovered.Core.HardState, d.Hard) || !proto.Equal(recovered.Core.ConfState, d.Conf) || len(recovered.Core.Entries) != len(d.Entries) {
			t.Fatal("simulated durable metadata differs from native store")
		}
		for i, e := range d.Entries {
			if !proto.Equal(e, recovered.Core.Entries[i]) {
				t.Fatal("simulated log differs from native store")
			}
		}
		rows, _, err := store.Read(ctx, d.Applied, []raftstore.Key{key})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rows[key], encodedApp(d.App)) {
			t.Fatal("simulated application differs from native")
		}
		simulated, err := d.checkpoint()
		if err != nil {
			t.Fatal(err)
		}
		native, err := store.CreateSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(native, simulated.Pointer) {
			t.Fatal("portable manifest differs from real native snapshot")
		}
		if err = store.Compact(ctx, d.Applied); err != nil {
			t.Fatal(err)
		}
		// The portable simulated chunks must also be installable by the production
		// store, including the exact retained result and revocation/vault state.
		target, err := raftstore.Open(filepath.Join(t.TempDir(), "restored.sqlite"), raftstore.Identity{ClusterUUID: clusterUUID, NodeID: uint64(node + 1), Protocol: raftcore.ProtocolVersion})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { target.Close() })
		id, err := target.BeginSnapshot(ctx, simulated.Pointer.Data)
		if err != nil {
			t.Fatal(err)
		}
		for i, chunk := range simulated.Chunks {
			if err = target.PutSnapshotChunk(ctx, id, uint64(i), chunk, raftsnapshot.ID(chunk)); err != nil {
				t.Fatal(err)
			}
		}
		pointer, err := target.CompleteSnapshot(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = target.Persist(ctx, raftcore.PersistBatch{ID: 1, HardState: d.Hard, Snapshot: pointer}); err != nil {
			t.Fatal(err)
		}
		restored, err := target.Recover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if restored.Core.Applied != d.Applied || !proto.Equal(restored.Core.ConfState, d.Conf) {
			t.Fatal("native snapshot lost application/configuration metadata")
		}
		rows, _, err = target.Read(ctx, d.Applied, []raftstore.Key{key})
		if err != nil || !bytes.Equal(rows[key], encodedApp(d.App)) {
			t.Fatal("native snapshot lost observable state", err)
		}
	}
	finish(t, s)
}
