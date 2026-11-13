package raftstore

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// nativeGroup is a bounded, socket-free host fixture using REAL per-member
// SQLite databases and production cores. Its one-cell rule evaluator is not
// the authorized directory writer or the independent history oracle.
type nativeGroup struct {
	t             *testing.T
	nodes         [3]*raftcore.Core
	stores        [3]*SQLite
	paths         [3]string
	apps          [3]Application
	alive         [3]bool
	status        [3]raftcore.Status
	queue         []raftcore.Outbound
	reads         []raftcore.ReadBarrier
	events        int
	cutApply      func(int, string, *raftcore.ApplyBatch) bool
	maintain      bool
	snapshotsSent int
}

func newNativeGroup(t *testing.T) *nativeGroup {
	t.Helper()
	g := &nativeGroup{t: t}
	for i := range g.nodes {
		g.paths[i] = filepath.Join(t.TempDir(), "member.sqlite")
		g.start(i)
	}
	g.pump()
	return g
}

func (g *nativeGroup) start(i int) {
	g.t.Helper()
	id := Identity{testIdentity.ClusterUUID, uint64(i + 1), raftcore.ProtocolVersion}
	s, err := Open(g.paths[i], id)
	must(g.t, err)
	g.t.Cleanup(func() { s.Close() })
	r := recovered(g.t, s)
	cfg := raftcore.Config{ClusterUUID: id.ClusterUUID, NodeID: id.NodeID, Protocol: id.Protocol, Seed: uint64(100 + i + g.events)}
	if r.Fresh {
		cfg.Bootstrap = []uint64{1, 2, 3}
	}
	c, err := raftcore.New(cfg, r.Core)
	must(g.t, err)
	g.nodes[i], g.stores[i], g.apps[i], g.alive[i] = c, s, r.Application, true
	g.call(i, raftcore.Poll{})
}

func (g *nativeGroup) call(i int, input raftcore.Input) {
	g.t.Helper()
	if !g.alive[i] {
		return
	}
	g.events++
	if g.events > 2000 {
		g.t.Fatal("native fixture exceeded its fixed event budget")
	}
	e, err := g.nodes[i].Step(input)
	must(g.t, err)
	for depth := 0; ; depth++ {
		if depth > 100 {
			g.t.Fatal("unbounded Ready completion chain")
		}
		g.status[i] = e.Status
		g.queue = append(g.queue, e.Send...)
		for _, barrier := range e.Reads {
			if g.apps[i].Applied < barrier.Index {
				g.t.Fatal("read escaped before durable application")
			}
			g.reads = append(g.reads, barrier)
		}
		if e.Persist != nil {
			if len(e.Send) != 0 || e.Apply != nil || len(e.Reads) != 0 {
				g.t.Fatal("dependent effect escaped before Ready durability")
			}
			must(g.t, g.stores[i].Persist(testContext, *e.Persist))
			if !raft.IsEmptySnap(e.Persist.Snapshot) {
				g.apps[i] = recovered(g.t, g.stores[i]).Application
			}
			e, err = g.nodes[i].Step(raftcore.Persisted{BatchID: e.Persist.ID})
		} else if e.Apply != nil {
			if g.cutApply != nil && g.cutApply(i, "before-apply", e.Apply) {
				g.alive[i] = false
				return
			}
			u := g.evaluate(i, *e.Apply)
			must(g.t, g.stores[i].Apply(testContext, *e.Apply, u))
			if g.cutApply != nil && g.cutApply(i, "after-apply", e.Apply) {
				g.alive[i] = false
				return
			}
			g.apps[i] = recovered(g.t, g.stores[i]).Application
			e, err = g.nodes[i].Step(raftcore.Applied{BatchID: e.Apply.ID})
		} else if g.maintain && e.SnapshotNeeded != nil {
			p, createErr := g.stores[i].CreateSnapshot(testContext)
			must(g.t, createErr)
			must(g.t, g.stores[i].Compact(testContext, p.GetMetadata().GetIndex()))
			e, err = g.nodes[i].Step(raftcore.CompactSnapshot{Snapshot: p})
		} else {
			return
		}
		must(g.t, err)
	}
}

func (g *nativeGroup) evaluate(i int, batch raftcore.ApplyBatch) Update {
	g.t.Helper()
	a := g.apps[i]
	u := Update{ExpectedApplied: a.Applied, VisibleVersion: a.VisibleVersion, LogicalTime: a.LogicalTime}
	working := make(map[Key][]byte)
	for _, entry := range batch.Entries {
		if entry.GetType() != pb.EntryNormal || len(entry.Data) == 0 {
			continue
		}
		cmd, err := clusterstate.DecodeCommand(entry.Data)
		must(g.t, err)
		key := Key{State, "cell"}
		data, ok := working[key]
		if !ok {
			rows, _, err := g.stores[i].Read(testContext, a.Applied, []Key{key})
			must(g.t, err)
			data = rows[key]
		}
		var cell clusterstate.Cell
		if len(data) != 0 {
			row, err := clusterstate.DecodeRow(data)
			must(g.t, err)
			cell = *row.Cell
		}
		u.LogicalTime = max(u.LogicalTime, cmd.Now)
		switch {
		case cmd.Change.Create != nil:
			if len(data) != 0 {
				g.t.Fatal("fixture create duplicated")
			}
			cell = clusterstate.Created(cmd.Change.Create.Init, cmd.Actor, u.LogicalTime)
		case cmd.Change.Publish != nil:
			cell, err = clusterstate.PublishTo(cell, cmd.Actor, cmd.Change.Publish.Publish, u.LogicalTime)
			must(g.t, err)
		default:
			g.t.Fatal("unsupported native fixture command")
		}
		u.VisibleVersion++
		working[key], err = clusterstate.EncodeRow(clusterstate.Row{Schema: clusterstate.SchemaVersion, Key: "cell", Cell: &cell})
		must(g.t, err)
		result, err := clusterstate.EncodeResult(clusterstate.Result{Schema: clusterstate.SchemaVersion, Code: clusterstate.OK, Now: u.LogicalTime, Version: u.VisibleVersion})
		must(g.t, err)
		working[Key{Sessions, cmd.Session}] = result
	}
	for key, data := range working {
		u.Writes = append(u.Writes, Write{Key: key, Value: data})
	}
	slices.SortFunc(u.Writes, func(a, b Write) int {
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return cmp.Compare(a.Key.Key, b.Key.Key)
	})
	return u
}

func (g *nativeGroup) pump() {
	g.t.Helper()
	for len(g.queue) != 0 {
		out := g.queue[0]
		g.queue = g.queue[1:]
		target := int(out.To) - 1
		message := &pb.Message{}
		must(g.t, proto.Unmarshal(out.Bytes, message))
		if g.alive[target] && message.GetType() == pb.MsgSnap {
			source := int(message.GetFrom()) - 1
			pointer := message.GetSnapshot()
			id, err := g.stores[target].BeginSnapshot(testContext, pointer.Data)
			must(g.t, err)
			m, err := raftsnapshot.Decode(pointer.Data)
			must(g.t, err)
			for offset := uint64(0); offset < m.Chunks; offset++ {
				data, err := g.stores[source].ReadSnapshotChunk(testContext, id, offset)
				must(g.t, err)
				must(g.t, g.stores[target].PutSnapshotChunk(testContext, id, offset, data, raftsnapshot.ID(data)))
			}
			_, err = g.stores[target].CompleteSnapshot(testContext, id)
			must(g.t, err)
			g.snapshotsSent++
		}
		g.call(target, raftcore.PeerMessage{Bytes: out.Bytes})
	}
}

func (g *nativeGroup) tick() {
	for i := range g.nodes {
		g.call(i, raftcore.Tick{})
	}
	g.pump()
}

func (g *nativeGroup) elect() int {
	g.t.Helper()
	for ticks := 0; ticks < 30; ticks++ {
		g.tick()
		for i, status := range g.status {
			if g.alive[i] && status.Role == raft.StateLeader {
				return i
			}
		}
	}
	g.t.Fatal("no leader within bounded native election")
	return -1
}

func (g *nativeGroup) crash(i int) {
	g.t.Helper()
	// Copy only the database and WAL while the completed store is still open.
	// No Close/checkpoint can make an unflushed application transaction durable.
	g.paths[i] = copyDurable(g.t, g.paths[i])
	must(g.t, g.stores[i].Close())
	g.nodes[i], g.alive[i] = nil, false
}

func (g *nativeGroup) propose(i int, seq uint64, mutation clusterstate.Mutation) {
	g.t.Helper()
	b, err := clusterstate.EncodeCommand(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: testIdentity.ClusterUUID, Actor: "device", Session: "session", Sequence: seq, RequestDigest: fmt.Sprintf("digest-%d", seq), Now: int64(1000 + seq), Change: mutation})
	must(g.t, err)
	g.call(i, raftcore.Propose{Command: b})
	g.pump()
}

func (g *nativeGroup) cell(i int) clusterstate.Cell {
	g.t.Helper()
	key := Key{State, "cell"}
	rows, _, err := g.stores[i].Read(testContext, g.apps[i].Applied, []Key{key})
	must(g.t, err)
	row, err := clusterstate.DecodeRow(rows[key])
	must(g.t, err)
	return *row.Cell
}

// R3's native mechanism uses three actual stores: a completed create/publish
// survives losing the leader and restoring a second member from durable bytes.
// A fresh quorum ReadIndex then observes the same head, fence and exact result.
func TestR3ThreeNativeStoresCrashRecoverAndRead(t *testing.T) {
	g := newNativeGroup(t)
	leader := g.elect()
	g.propose(leader, 1, clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: "cell", Init: clusterstate.CellInit{Head: "opaque-head-1"}, Receipt: "uploaded-1"}})
	g.propose(leader, 2, clusterstate.Mutation{Publish: &clusterstate.PublishCell{Cell: "cell", Publish: clusterstate.Publish{OldHead: "opaque-head-1", Head: "opaque-head-2", Fence: 1}, Receipt: "uploaded-2"}})
	key := Key{Sessions, "session"}
	before, _, err := g.stores[leader].Read(testContext, g.apps[leader].Applied, []Key{key})
	must(t, err)
	for i := range g.nodes {
		if c := g.cell(i); c.Head != "opaque-head-2" || c.Lease.Fence != 1 {
			t.Fatal("publish acknowledged before all connected replicas applied")
		}
	}
	other := (leader + 1) % 3
	g.crash(leader)
	g.crash(other)
	g.start(other)
	g.pump()
	fallback := g.elect()
	g.call(fallback, raftcore.Read{RequestID: "after-power-recovery"})
	g.pump()
	if len(g.reads) != 1 || g.cell(fallback).Head != "opaque-head-2" {
		t.Fatal("quorum read lost acknowledged publication after restart")
	}
	after, _, err := g.stores[fallback].Read(testContext, g.apps[fallback].Applied, []Key{key})
	must(t, err)
	if string(before[key]) != string(after[key]) {
		t.Fatal("recovery lost exact retained application result")
	}
	g.start(leader)
	g.pump()
	for range 2 {
		g.tick()
	}
	if g.cell(leader).Head != "opaque-head-2" || g.status[leader].Role == raft.StateLeader {
		t.Fatal("old leader did not recover as a caught-up follower")
	}
}

// R12 preserves the library's entire joint ConfState across process recovery;
// a demoted member cannot regain an incoming vote from an older bootstrap list.
func TestR12NativeJointConfigurationRecovery(t *testing.T) {
	g := newNativeGroup(t)
	leader := g.elect()
	if leader == 2 {
		g.call(leader, raftcore.Transfer{Target: 1})
		g.pump()
		leader = g.elect()
	}
	change, err := wire.Marshal(&pb.ConfChangeV2{Transition: pb.ConfChangeTransitionJointExplicit.Enum(), Changes: []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(3)}}})
	must(t, err)
	g.call(leader, raftcore.Configure{Change: change})
	g.pump()
	for i := range g.nodes {
		conf := recovered(t, g.stores[i]).Core.ConfState
		if len(conf.VotersOutgoing) != 3 || !slices.Contains(conf.LearnersNext, 3) {
			t.Fatal("joint/learner-next configuration was not durably applied")
		}
		g.crash(i)
	}
	for i := range g.nodes {
		g.start(i)
	}
	g.pump()
	leader = g.elect()
	change, err = wire.Marshal(&pb.ConfChangeV2{})
	must(t, err)
	g.call(leader, raftcore.Configure{Change: change})
	g.pump()
	for i := range g.nodes {
		conf := recovered(t, g.stores[i]).Core.ConfState
		if len(conf.VotersOutgoing) != 0 || len(conf.LearnersNext) != 0 || !slices.Equal(conf.Voters, []uint64{1, 2}) || !slices.Equal(conf.Learners, []uint64{3}) {
			t.Fatal("leaving joint state lost its committed learner/voter configuration")
		}
	}
	// An outgoing voter may have won the joint-state election. Leaving joint
	// consensus then correctly steps that leader down, so wait for an incoming
	// voter to win before testing the demoted member's restart.
	leader = g.elect()
	if leader == 2 {
		t.Fatal("demoted learner won an election after leaving joint state")
	}
	g.crash(2)
	g.start(2)
	for range 12 {
		g.tick()
	}
	if g.status[2].Role != raft.StateFollower || g.status[2].Leader != uint64(leader+1) {
		t.Fatalf("recovered learner regained a vote or displaced leader %d: %+v", leader+1, g.status[2])
	}
}

// R3 and R17 require replay after durable consensus but before application, and
// skipping already-applied work after application COMMIT but before completion.
// Both paths preserve one create and the same persisted result without a reply.
func TestR3R17NativeCommittedApplyRecovery(t *testing.T) {
	for _, phase := range []string{"before-apply", "after-apply"} {
		t.Run(phase, func(t *testing.T) {
			g := newNativeGroup(t)
			leader := g.elect()
			target := g.status[leader].Commit + 1
			g.cutApply = func(i int, current string, batch *raftcore.ApplyBatch) bool {
				return i == leader && phase == current && batch.Index == target
			}
			g.propose(leader, 1, clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: "cell", Init: clusterstate.CellInit{Head: "committed-without-reply"}, Receipt: "uploaded"}})
			if g.alive[leader] {
				t.Fatal("requested host completion boundary was not reached")
			}
			r := recovered(t, g.stores[leader])
			if r.Core.HardState.GetCommit() < target || (r.Core.Applied >= target) != (phase == "after-apply") {
				t.Fatal("cut did not straddle the durable application boundary")
			}
			g.crash(leader)
			g.cutApply = nil
			g.start(leader)
			g.pump()
			if g.cell(leader).Head != "committed-without-reply" || g.apps[leader].VisibleVersion != 1 || g.apps[leader].RecordCount != 2 {
				t.Fatal("recovery lost or applied the command twice")
			}
			key := Key{Sessions, "session"}
			rows, _, err := g.stores[leader].Read(testContext, g.apps[leader].Applied, []Key{key})
			must(t, err)
			result, err := clusterstate.DecodeResult(rows[key])
			must(t, err)
			if result.Code != clusterstate.OK || result.Version != 1 {
				t.Fatal("reply-drop recovery lost the exact application result")
			}
		})
	}
}
