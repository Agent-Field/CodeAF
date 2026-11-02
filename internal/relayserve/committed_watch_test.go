package relayserve_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// R22 proves the production watch emits an actual durable receipt instead of
// advertising socket vouching. Release and a delayed ping cannot revive a hold.
func TestR22ProductionWatchReceipts(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	created, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	stream, err := r.dir(a).WatchHolding(ctx, []directory.Hold{{Cell: cell, Fence: created.Cell.Lease.Fence}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if stream.Vouching() {
		t.Fatal("R22 advertised socket vouching")
	}
	if _, err = stream.Next(ctx); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(30 * time.Second)
	if err = stream.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	frame, err := stream.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Holds) != 1 {
		t.Fatalf("R22 no committed receipt: %+v", frame)
	}
	receipt := frame.Holds[0]
	if receipt.Cell != cell || receipt.Fence != 1 || receipt.Index == 0 || receipt.Until != r.clock.Now().Add(directory.LeaseTTL).UnixMilli() {
		t.Fatal(receipt)
	}
	got, err := r.dir(a).Cell(bg, cell)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cell.Lease.Expires != receipt.Until {
		t.Fatal("receipt differs from committed lease", got, receipt)
	}
	if err = r.dir(a).Release(bg, cell, 1); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(10 * time.Second)
	if err = stream.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = r.dir(a).Cell(bg, cell)
	if err != nil || got.Cell.Lease.Expires != 0 {
		t.Fatal("R22 delayed ping revived released lease", got, err)
	}
}

// R23/R26/R27 exercise ten minutes of virtual idle through the REAL production
// scheduler. No client request or application write recurs after all voters
// applied the sleep marker. A first new read wakes and passes quorum again.
func TestR23R26R27ProductionTenMinuteIdle(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	if err := r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	var p struct{ Cluster string }
	raw, err := os.ReadFile(filepath.Join(r.store, "raft", id.ID(), "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &p) != nil {
		t.Fatal("provision")
	}
	inspect := func() (raftstore.Application, cs.Quiescence) {
		t.Helper()
		s, err := raftstore.Open(filepath.Join(r.store, "raft", id.ID(), "1.db"), raftstore.Identity{ClusterUUID: p.Cluster, NodeID: 1, Protocol: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		app, _, err := s.ApplicationView(bg)
		if err != nil {
			t.Fatal(err)
		}
		k := raftstore.Key{Namespace: raftstore.Membership, Key: cs.QuiescenceKey().Key}
		rows, _, err := s.Read(bg, app.Applied, []raftstore.Key{k})
		if err != nil {
			t.Fatal(err)
		}
		var marker cs.Quiescence
		if len(rows[k]) > 0 && cs.Decode(rows[k], &marker) != nil {
			t.Fatal("sleep marker")
		}
		return app, marker
	}
	for n := 0; n < 20; n++ {
		r.clock.Advance(5 * time.Second)
	}
	before, marker := inspect()
	if marker.Index == 0 || marker.Index != before.Applied {
		t.Fatalf("R26 production did not quiesce: %+v %+v", before, marker)
	}
	r.mu.Lock()
	requests := len(r.logs)
	r.mu.Unlock()
	for n := 0; n < 120; n++ {
		r.clock.Advance(5 * time.Second)
	}
	after, _ := inspect()
	r.mu.Lock()
	idleRequests := len(r.logs) - requests
	r.mu.Unlock()
	t.Logf("R23/R27 simulated idle=10m requests=%d applied-before=%d applied-after=%d", idleRequests, before.Applied, after.Applied)
	if idleRequests != 0 || after.Applied != before.Applied {
		t.Fatal("idle caused recurring requests or application writes")
	}
	if _, err = r.dir(a).List(bg); err != nil {
		t.Fatal(err)
	}
	awake, _ := inspect()
	if awake.Applied <= after.Applied {
		t.Fatal("R26 first read did not commit current ingress time")
	}
	// An idle Home socket pings without making directory HTTP requests. Its
	// presence hints are separate from the sleeping consensus group.
}
