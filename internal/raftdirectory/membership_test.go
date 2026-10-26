package raftdirectory_test

import (
	"crypto/ed25519"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

func membershipChange(t *testing.T, g *group, m cs.Mutation) cs.Result {
	t.Helper()
	r, e := g.Change(ctx, "a", m)
	must(t, e)
	if r.Code != cs.OK {
		t.Fatalf("membership %T refused: %+v", m, r)
	}
	return r
}
func registry(t *testing.T, g *group, i int) cs.Membership {
	t.Helper()
	app, _, e := g.stores[i].ApplicationView(ctx)
	must(t, e)
	rows, _, e := g.stores[i].Read(ctx, app.Applied, []raftstore.Key{{Namespace: raftstore.Membership, Key: cs.MembershipKey().Key}})
	must(t, e)
	var h cs.Membership
	must(t, cs.Decode(rows[raftstore.Key{Namespace: raftstore.Membership, Key: cs.MembershipKey().Key}], &h))
	return h
}
func recoverMembership(t *testing.T, g *group) {
	t.Helper()
	g.queue = nil
	for i := range g.nodes {
		must(t, g.stores[i].Close())
		s, e := raftstore.Open(g.paths[i], raftstore.Identity{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1})
		must(t, e)
		g.stores[i] = s
		r, e := s.Recover(ctx)
		must(t, e)
		g.nodes[i], e = raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1, Seed: uint64(i + 123)}, r.Core)
		must(t, e)
		g.drive(i, raftcore.Poll{})
	}
	g.drain()
	for n := 0; n < 40 && g.leader() < 0; n++ {
		for i := range g.nodes {
			if !g.absent[uint64(i+1)] {
				g.drive(i, raftcore.Tick{})
			}
		}
		g.drain()
	}
	if g.leader() < 0 {
		t.Fatal("restart failed election")
	}
}
func reconcileMembership(t *testing.T, g *group) {
	t.Helper()
	i := g.leader()
	h := registry(t, g, i)
	p, e := g.nodes[i].PlanMembership(h)
	must(t, e)
	if len(p.Change) > 0 {
		g.drive(i, raftcore.Configure{Change: p.Change})
		g.drain()
		p, e = g.nodes[i].PlanMembership(h)
		must(t, e)
	}
	if !p.Settle {
		t.Fatal("configuration did not reach intent")
	}
	if p.Checkpoint {
		snapshot, e := g.stores[i].CreateSnapshot(ctx)
		must(t, e)
		must(t, g.stores[i].Compact(ctx, snapshot.GetMetadata().GetIndex()))
		g.drive(i, raftcore.CompactSnapshot{Snapshot: snapshot})
	}
	membershipChange(t, g, cs.Mutation{SettleMembership: &cs.SettleMembership{Revision: h.Revision, Node: h.Pending.Node}})
}

// R9/R10/R11/R12/R13/R16/R17 cross actual native FULL transactions, restart at
// each enrollment stage, compact with a sleeping learner, and rebuild routing
// from durable revocations/tombstones rather than trusting a socket or cache.
func TestR9R10R11R12R13R16R17NativeEnrollmentRestartAndSnapshotCatchUp(t *testing.T) {
	g := newGroup(t, func() time.Time { return time.UnixMilli(1000) })
	anchors := []cs.MemberRecord{}
	for id := uint64(1); id <= 3; id++ {
		anchors = append(anchors, cs.MemberRecord{Node: id, Key: key(fmt.Sprint("anchor-", id)).Public().(ed25519.PublicKey), Anchor: true, Incarnation: 1, Protocol: 1})
	}
	membershipChange(t, g, cs.Mutation{InitMembership: &cs.InitMembership{Anchors: anchors, Limit: cs.MaxMemberRecords}})
	recoverMembership(t, g)
	h := registry(t, g, g.leader())
	seq := uint64(1)
	enrolled, e := cs.Sign(cs.Command{Schema: 1, Cluster: cluster, Actor: "a", Session: "enrollment", Sequence: seq, Ack: seq - 1, Now: 1000, Change: cs.Mutation{EnrollMember: &cs.EnrollMember{Node: 4, Revision: h.Revision, Device: "b", Protocol: 1}}}, key("a"))
	must(t, e)
	r, e := g.submit(enrolled)
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	if r.Membership.Barrier != 0 {
		t.Fatal("enrollment claimed ready")
	}
	recoverMembership(t, g)
	// Learner transport is provisioned without a listening port or a singleton
	// bootstrap. It stays asleep while admission and writes compact past it.
	retry, e := g.submit(enrolled)
	must(t, e)
	if !reflect.DeepEqual(retry, r) {
		t.Fatal("R17 restart changed enrollment result")
	}
	path := filepath.Join(t.TempDir(), "learner.db")
	s, e := raftstore.Open(path, raftstore.Identity{ClusterUUID: cluster, NodeID: 4, Protocol: 1})
	must(t, e)
	n, e := raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: 4, Seed: 4, Protocol: 1}, raftcore.RecoveredState{})
	must(t, e)
	g.paths = append(g.paths, path)
	g.stores = append(g.stores, s)
	g.nodes = append(g.nodes, n)
	g.status = append(g.status, raftcore.Status{})
	g.results = append(g.results, nil)
	g.absent[4] = true
	g.drive(3, raftcore.Poll{})
	lagged := (g.leader() + 1) % 3
	g.absent[uint64(lagged+1)] = true
	leader := g.leader()
	pending := registry(t, g, leader)
	plan, e := g.nodes[leader].PlanMembership(pending)
	must(t, e)
	g.drive(leader, raftcore.Configure{Change: plan.Change})
	g.drain()
	// Keep the uncompacted suffix for this deliberately lagged anchor. The
	// fresh checkpoint is made before the new learner's snapshot catch-up.
	anchorNode := uint64(leader + 1)
	settlement, e := cs.Sign(cs.Command{Schema: 1, Cluster: cluster, Actor: cs.AnchorActor(anchorNode), Session: "completion", Sequence: 1, Now: 1000, Change: cs.Mutation{SettleMembership: &cs.SettleMembership{Revision: pending.Revision, Node: pending.Pending.Node}}}, key(fmt.Sprint("anchor-", anchorNode)))
	must(t, e)
	completed, e := g.submit(settlement)
	must(t, e)
	if completed.Code != cs.OK {
		t.Fatal("anchor could not resume authorized admission", completed)
	}
	g.absent[uint64(lagged+1)] = false
	for step := 0; step < 3; step++ {
		g.drive(g.leader(), raftcore.Tick{})
		g.drain()
	}
	for replica := 0; replica < 3; replica++ {
		if !reflect.DeepEqual(registry(t, g, replica), registry(t, g, g.leader())) {
			t.Fatal("R12 mixed configuration/settlement batch diverged")
		}
	}
	recoverMembership(t, g)
	for step := 0; step < 3; step++ {
		_, e := g.Change(ctx, "a", cs.Mutation{AdvanceTime: &cs.AdvanceTime{}})
		must(t, e)
	}
	i := g.leader()
	snapshot, e := g.stores[i].CreateSnapshot(ctx)
	must(t, e)
	must(t, g.stores[i].Compact(ctx, snapshot.GetMetadata().GetIndex()))
	g.drive(i, raftcore.CompactSnapshot{Snapshot: snapshot})
	// Stage streamed chunks in the actual recipient store before RawNode sees
	// MsgSnap. Upload completion is not reported as an append acknowledgment.
	installNativeSnapshot(t, g, i, 3, snapshot)
	retry, e = g.submit(enrolled)
	must(t, e)
	if !reflect.DeepEqual(retry, r) {
		t.Fatal("R17 compaction changed enrollment result")
	}
	g.absent[4] = false
	for step := 0; step < 12 && g.status[3].Applied < g.status[i].Applied; step++ {
		g.drive(i, raftcore.Tick{})
		g.drain()
	}
	app, _, e := g.stores[3].ApplicationView(ctx)
	must(t, e)
	if app.Applied < snapshot.GetMetadata().GetIndex() {
		t.Fatal("learner did not apply advertised barrier")
	}
	h = registry(t, g, i)
	g.absent[4] = true
	membershipChange(t, g, cs.Mutation{RemoveMember: &cs.RemoveMember{Revision: h.Revision, Node: 4, Device: "b"}})
	reconcileMembership(t, g)
	recoverMembership(t, g)
	app, _, e = g.stores[g.leader()].ApplicationView(ctx)
	must(t, e)
	read := func(k cs.RecordKey) ([]byte, error) {
		rows, _, e := g.stores[g.leader()].Read(ctx, app.Applied, []raftstore.Key{{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}})
		return rows[raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}], e
	}
	view, e := rafttransport.CommittedView(rafttransport.Scope{Identity: "test", Cluster: cluster, Epoch: 1, Protocol: 1}, 1, app.Applied, raftcore.Configuration(app.ConfState), read)
	must(t, e)
	if len(view.Members) != 4 || !view.Members[3].Revoked {
		t.Fatal("retired node admitted after restart")
	}
	router, e := rafttransport.NewRouter(rafttransport.DefaultLimits())
	must(t, e)
	must(t, router.Install(view))
	view.Revision++
	view.Members[3].Revoked = false
	if router.Install(view) == nil {
		t.Fatal("routing accepted retired authority")
	}
}

func installNativeSnapshot(t *testing.T, g *group, from, to int, snapshot *pb.Snapshot) {
	t.Helper()
	id, e := g.stores[to].BeginSnapshot(ctx, snapshot.Data)
	must(t, e)
	manifest, e := raftsnapshot.Decode(snapshot.Data)
	must(t, e)
	for offset := uint64(0); offset < manifest.Chunks; offset++ {
		b, e := g.stores[from].ReadSnapshotChunk(ctx, id, offset)
		must(t, e)
		must(t, g.stores[to].PutSnapshotChunk(ctx, id, offset, b, raftsnapshot.ID(b)))
	}
	_, e = g.stores[to].CompleteSnapshot(ctx, id)
	must(t, e)
	raw, e := proto.Marshal(&pb.Message{Type: pb.MsgSnap.Enum(), From: proto.Uint64(uint64(from + 1)), To: proto.Uint64(uint64(to + 1)), Term: proto.Uint64(g.status[from].Term), Snapshot: snapshot})
	must(t, e)
	g.drive(to, raftcore.PeerMessage{Bytes: raw})
	g.drain()
}
