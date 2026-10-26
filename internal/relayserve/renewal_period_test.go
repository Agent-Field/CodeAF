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
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	pb "go.etcd.io/raft/v3/raftpb"
)

func renewalEntries(t *testing.T, r *rig, identity string) []cs.RenewHolds {
	t.Helper()
	path := filepath.Join(r.store, "raft", identity)
	raw, err := os.ReadFile(filepath.Join(path, "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provision struct{ Cluster string }
	if err = json.Unmarshal(raw, &provision); err != nil {
		t.Fatal(err)
	}
	store, err := raftstore.Open(filepath.Join(path, "1.db"), raftstore.Identity{ClusterUUID: provision.Cluster, NodeID: 1, Protocol: raftcore.ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovered, err := store.Recover(bg)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Core.Snapshot.GetMetadata().GetIndex() > 1 {
		t.Fatal("renewal count requires the complete retained log")
	}
	var renewals []cs.RenewHolds
	for _, entry := range recovered.Core.Entries {
		if entry.GetIndex() > recovered.Application.Applied || entry.GetType() != pb.EntryNormal || len(entry.GetData()) == 0 {
			continue
		}
		command, err := cs.DecodeCommand(entry.GetData())
		if err != nil {
			t.Fatal(err)
		}
		if command.Change.RenewHolds != nil {
			renewals = append(renewals, *command.Change.RenewHolds)
		}
	}
	return renewals
}

func pingRenewalWatch(t *testing.T, stream dirwatch.Stream) []dirwatch.HoldReceipt {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	if err := stream.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var receipts []dirwatch.HoldReceipt
	for {
		frame, err := stream.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, frame.Holds...)
		if frame.Pong {
			return receipts
		}
	}
}

func TestR7_1NativeWatchRenewalPeriod(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	created, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1})
	if err != nil {
		t.Fatal(err)
	}
	start := r.clock.Now().UnixMilli()
	stream, err := r.dir(a).WatchHolding(bg, []directory.Hold{{Cell: cell, Fence: created.Cell.Lease.Fence}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for elapsed := cs.PresenceEveryMillis; elapsed <= 6*cs.RenewEveryMillis; elapsed += cs.PresenceEveryMillis {
		r.clock.Advance(time.Duration(cs.PresenceEveryMillis) * time.Millisecond)
		receipts := pingRenewalWatch(t, stream)
		view, err := r.dir(a).Cell(bg, cell)
		if err != nil || view.Cell.Lease.Device != a.dev.ID() || view.Cell.Lease.Fence != 1 || view.Cell.Lease.Expires <= view.Now {
			t.Fatalf("r7.1 lease lapsed at +%d: %+v, %v", elapsed, view, err)
		}
		entries := renewalEntries(t, r, id.ID())
		if got, want := len(entries), int(elapsed/cs.RenewEveryMillis); got != want {
			t.Errorf("r7.1 +%d ms: committed renewals=%d, want %d", elapsed, got, want)
		}
		wantExpiry := start + elapsed/cs.RenewEveryMillis*cs.RenewEveryMillis + cs.LeaseTTLMillis
		if view.Cell.Lease.Expires != wantExpiry {
			t.Errorf("r7.1 +%d ms: expiry=%d, want %d", elapsed, view.Cell.Lease.Expires, wantExpiry)
		}
		if elapsed%cs.RenewEveryMillis == 0 && (len(receipts) != 1 || receipts[0].Index == 0 || receipts[0].Until != view.Cell.Lease.Expires) {
			t.Errorf("r7.1 missing exact committed receipt: %+v", receipts)
		}
	}
	t.Logf("r7.1 six renewal periods, 18 presence pings, %d committed renewals", len(renewalEntries(t, r, id.ID())))
}

func TestR7_2NativeWatchNeedsFreshFenceEvidence(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	stream, err := r.dir(a).WatchHolding(bg, []directory.Hold{{Cell: cell, Fence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = r.dir(a).Release(bg, cell, 1); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(time.Duration(cs.PresenceEveryMillis) * time.Millisecond)
	if got := pingRenewalWatch(t, stream); len(got) != 0 {
		t.Fatal("r7.2 released hold received a renewal", got)
	}
	view, err := r.dir(a).Cell(bg, cell)
	if err != nil || view.Cell.Lease.Expires != 0 {
		t.Fatal("r7.2 released hold revived", view, err)
	}
	view, err = r.dir(a).Acquire(bg, cell, directory.AcquireOpts{})
	if err != nil || view.Cell.Lease.Fence != 2 {
		t.Fatal("r7.2 new fence", view, err)
	}
	expires := view.Cell.Lease.Expires
	for range 4 {
		r.clock.Advance(time.Duration(cs.PresenceEveryMillis) * time.Millisecond)
		if got := pingRenewalWatch(t, stream); len(got) != 0 {
			t.Fatal("r7.2 old socket renewed a new fence", got)
		}
	}
	view, err = r.dir(a).Cell(bg, cell)
	if err != nil || view.Cell.Lease.Expires != expires {
		t.Fatal("r7.2 old evidence extended new fence", view, err)
	}
}

func TestR7_3NativeSocketsShareRenewalPeriod(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	var streams []dirwatch.Stream
	var cells []string
	for i := range 3 {
		name := cell + string(rune('a'+i))
		if _, err := r.dir(a).Create(bg, name, directory.CellInit{Head: head1}); err != nil {
			t.Fatal(err)
		}
		stream, err := r.dir(a).WatchHolding(bg, []directory.Hold{{Cell: name, Fence: 1}})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		streams = append(streams, stream)
		cells = append(cells, name)
	}
	for period := 1; period <= 5; period++ {
		for beat := 1; beat <= int(cs.RenewEveryMillis/cs.PresenceEveryMillis); beat++ {
			r.clock.Advance(time.Duration(cs.PresenceEveryMillis) * time.Millisecond)
			pingRenewalWatch(t, streams[0])
			if beat == int(cs.RenewEveryMillis/cs.PresenceEveryMillis) {
				if got, want := len(renewalEntries(t, r, id.ID())), period*len(streams); got != want {
					t.Errorf("r7.3 first ping at shared deadline: commits=%d, want %d", got, want)
				}
			}
			for _, stream := range streams[1:] {
				pingRenewalWatch(t, stream)
			}
		}
		for _, name := range cells {
			view, err := r.dir(a).Cell(bg, name)
			if err != nil || view.Cell.Lease.Device != a.dev.ID() || view.Cell.Lease.Expires <= view.Now {
				t.Fatal("r7.3 held socket lease lapsed", name, view, err)
			}
		}
	}
	for _, entry := range renewalEntries(t, r, id.ID()) {
		if len(entry.Items) > cs.MaxRenewHolds {
			t.Fatal("r7.3 committed batch exceeded cap", len(entry.Items))
		}
	}
}
