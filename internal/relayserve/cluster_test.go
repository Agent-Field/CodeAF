package relayserve

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftabi"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// clusterFixture drives actual native stores and cores. The only listener is
// the relay; members can only make outbound exchange requests. Application is
// the narrow real-rule probe, never the authorized directory writer.
type clusterFixture struct {
	t         *testing.T
	ctx       context.Context
	router    *rafttransport.Router
	scope     rafttransport.Scope
	client    rafttransport.Client
	cores     [3]*raftcore.Core
	stores    [3]*raftstore.SQLite
	paths     [3]string
	apps      [3]raftstore.Application
	status    [3]raftcore.Status
	row       [3][]byte
	pending   [3][]rafttransport.Envelope
	ack       [3][3]uint64
	seq       [3]uint64
	alive     [3]bool
	events    int
	exchanges int
	reads     []raftcore.ReadBarrier
}

func clusterMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func clusterKey(id byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{id}, 32))
}
func newClusterFixture(t *testing.T) *clusterFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	router, err := rafttransport.NewRouter(rafttransport.DefaultLimits())
	clusterMust(t, err)
	scope := rafttransport.Scope{Identity: "id_native", Cluster: "native-outbound", Epoch: 1, Protocol: rafttransport.Protocol}
	view := rafttransport.View{Scope: scope, Revision: 1, Generation: 1}
	for id := uint64(1); id <= 3; id++ {
		view.Members = append(view.Members, rafttransport.Member{Node: id, Incarnation: 1, Key: clusterKey(byte(id)).Public().(ed25519.PublicKey), Voter: true})
	}
	clusterMust(t, router.Install(view))
	service := New(Config{Cluster: rafttransport.Handler(router, func() time.Time { return time.UnixMilli(1000) }), Now: func() time.Time { return time.UnixMilli(1000) }})
	t.Cleanup(func() { clusterMust(t, service.Close()) })
	server := httptest.NewServer(service.Handler)
	t.Cleanup(server.Close)
	f := &clusterFixture{t: t, ctx: ctx, router: router, scope: scope, client: rafttransport.Client{URL: server.URL, HTTP: server.Client()}}
	for i := range f.cores {
		f.paths[i] = filepath.Join(t.TempDir(), "member.db")
		f.start(i)
	}
	return f
}
func (f *clusterFixture) start(i int) {
	f.t.Helper()
	id := raftstore.Identity{ClusterUUID: f.scope.Cluster, NodeID: uint64(i + 1), Protocol: raftcore.ProtocolVersion}
	s, err := raftstore.Open(f.paths[i], id)
	clusterMust(f.t, err)
	f.t.Cleanup(func() { s.Close() })
	recovered, err := s.Recover(f.ctx)
	clusterMust(f.t, err)
	cfg := raftcore.Config{ClusterUUID: id.ClusterUUID, NodeID: id.NodeID, Seed: uint64(100 + i + f.events), Protocol: id.Protocol}
	if recovered.Fresh {
		cfg.Bootstrap = []uint64{1, 2, 3}
	}
	f.cores[i], err = raftcore.New(cfg, recovered.Core)
	clusterMust(f.t, err)
	f.stores[i], f.apps[i], f.alive[i] = s, recovered.Application, true
	records, _, err := s.Read(f.ctx, recovered.Application.Applied, []raftstore.Key{{Namespace: raftstore.State, Key: "cell"}})
	clusterMust(f.t, err)
	f.row[i] = records[raftstore.Key{Namespace: raftstore.State, Key: "cell"}]
	f.call(i, raftcore.Poll{})
}
func (f *clusterFixture) call(i int, input raftcore.Input) {
	f.t.Helper()
	if !f.alive[i] {
		return
	}
	for depth := 0; depth < 100; depth++ {
		f.events++
		if f.events > 2000 {
			f.t.Fatal("R10: outbound fixture exceeded 2,000 events")
		}
		effects, err := f.cores[i].Step(input)
		clusterMust(f.t, err)
		f.status[i] = effects.Status
		for _, out := range effects.Send {
			f.seq[i]++
			e := rafttransport.Envelope{Scope: f.scope, From: uint64(i + 1), To: out.To, Incarnation: 1, Destination: 1, Sequence: f.seq[i], Payload: out.Bytes}
			clusterMust(f.t, e.Sign(clusterKey(byte(i+1))))
			f.pending[i] = append(f.pending[i], e)
		}
		for _, read := range effects.Reads {
			if f.apps[i].Applied < read.Index {
				f.t.Fatal("R4: transport escaped durable application")
			}
			f.reads = append(f.reads, read)
		}
		if effects.Persist != nil {
			if len(effects.Send) != 0 {
				f.t.Fatal("R3: transport saw a pre-flush append response")
			}
			clusterMust(f.t, f.stores[i].Persist(f.ctx, *effects.Persist))
			input = raftcore.Persisted{BatchID: effects.Persist.ID}
			continue
		}
		if effects.Apply != nil {
			a := f.apps[i]
			u := raftstore.Update{ExpectedApplied: a.Applied, VisibleVersion: a.VisibleVersion, LogicalTime: a.LogicalTime}
			for _, entry := range effects.Apply.Entries {
				if entry.GetType() != pb.EntryNormal || len(entry.Data) == 0 {
					continue
				}
				f.row[i], err = raftabi.ProbeApply(entry.Data, f.row[i])
				clusterMust(f.t, err)
				command, err := clusterstate.DecodeCommand(entry.Data)
				clusterMust(f.t, err)
				u.LogicalTime = max(u.LogicalTime, command.Now)
				u.VisibleVersion++
			}
			if !bytes.Equal(f.row[i], nil) {
				u.Writes = []raftstore.Write{{Key: raftstore.Key{Namespace: raftstore.State, Key: "cell"}, Value: f.row[i]}}
			}
			clusterMust(f.t, f.stores[i].Apply(f.ctx, *effects.Apply, u))
			recovered, err := f.stores[i].Recover(f.ctx)
			clusterMust(f.t, err)
			f.apps[i] = recovered.Application
			input = raftcore.Applied{BatchID: effects.Apply.ID}
			continue
		}
		return
	}
	f.t.Fatal("R10: unbounded completion chain")
}
func (f *clusterFixture) pump() {
	f.t.Helper()
	for round := 0; round < 40; round++ {
		activity := false
		for i := range f.cores {
			if !f.alive[i] {
				continue
			}
			// Deliberately reverse and duplicate batches before relay admission. No
			// member can rely on an inbound socket, FIFO delivery or unique arrival.
			outgoing := slices.Clone(f.pending[i])
			slices.Reverse(outgoing)
			if len(outgoing) > 0 {
				outgoing = []rafttransport.Envelope{outgoing[0], outgoing[0]}
			}
			req := rafttransport.Request{Scope: f.scope, Node: uint64(i + 1), Incarnation: 1, Generation: 1, Ack: f.ack[i], Messages: outgoing}
			clusterMust(f.t, req.Sign(clusterKey(byte(i+1))))
			resp, err := f.client.Exchange(f.ctx, req)
			if len(outgoing) > 0 {
				f.pending[i] = f.pending[i][:len(f.pending[i])-1]
			}
			if err == rafttransport.ErrCapacity {
				// Failed delivery is reported to the core, which owns retry.
				// A blocked destination cannot hold healthy peers behind it.
				f.call(i, raftcore.TransportResult{Peer: outgoing[0].To, Success: false})
				req.Messages = nil
				clusterMust(f.t, req.Sign(clusterKey(byte(i+1))))
				resp, err = f.client.Exchange(f.ctx, req)
			}
			clusterMust(f.t, err)
			f.exchanges++
			if len(outgoing) != 0 || len(resp.Messages) != 0 {
				activity = true
			}
			for _, delivery := range resp.Messages {
				e := delivery.Envelope
				clusterMust(f.t, e.Verify(clusterKey(byte(e.From)).Public().(ed25519.PublicKey)))
				if e.Scope != f.scope || e.To != uint64(i+1) || e.Incarnation != 1 || e.Destination != 1 {
					f.t.Fatal("R16: wrong dispatch authority")
				}
				f.call(i, raftcore.PeerMessage{Bytes: e.Payload})
				f.ack[i][delivery.Lane] = max(f.ack[i][delivery.Lane], delivery.Cursor)
			}
		}
		if !activity {
			return
		}
	}
	f.t.Fatal("R10: relay did not drain within bounded exchanges")
}
func (f *clusterFixture) tick() {
	for i := range f.cores {
		f.call(i, raftcore.Tick{})
	}
	f.pump()
}
func (f *clusterFixture) leader() int {
	f.t.Helper()
	for range 40 {
		f.tick()
		for i, s := range f.status {
			if f.alive[i] && s.Role == raft.StateLeader {
				return i
			}
		}
	}
	f.t.Fatal("R10: outbound majority could not elect")
	return -1
}
func (f *clusterFixture) propose(i int, seq uint64, change clusterstate.Mutation) {
	f.t.Helper()
	command, err := clusterstate.EncodeCommand(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: f.scope.Cluster, Actor: "device", Session: "probe", Sequence: seq, RequestDigest: fmt.Sprint(seq), Now: int64(1000 + seq), Change: change})
	clusterMust(f.t, err)
	f.call(i, raftcore.Propose{Command: command})
	f.pump()
}

// R3/R4/R10 prove three outbound-only members elect, commit a real chat rule,
// pass ReadIndex and recover through the relay after loss of the old leader.
func TestR3R4R10OutboundOnlyNativeRelayProgressAndRecovery(t *testing.T) {
	f := newClusterFixture(t)
	leader := f.leader()
	f.propose(leader, 1, clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: "cell", Init: clusterstate.CellInit{Head: "sealed-head"}, Receipt: "uploaded"}})
	f.call(leader, raftcore.Read{RequestID: "outbound-read"})
	f.pump()
	if len(f.reads) != 1 {
		t.Fatal("R4/R10: outbound quorum read did not finish")
	}
	before := bytes.Clone(f.row[leader])
	for i := range f.cores {
		if !bytes.Equal(before, f.row[i]) {
			t.Fatal("R3: connected member missed durable create")
		}
	}
	f.alive[leader] = false
	clusterMust(t, f.stores[leader].Close())
	f.cores[leader] = nil
	next := f.leader()
	f.call(next, raftcore.Read{RequestID: "fallback-read"})
	f.pump()
	if len(f.reads) != 2 || !bytes.Equal(before, f.row[next]) {
		t.Fatal("R3/R4/R10: outbound fallback lost committed row or read authority")
	}
	f.start(leader)
	for range 2 {
		f.tick()
	}
	if !bytes.Equal(before, f.row[leader]) || f.status[leader].Role == raft.StateLeader {
		t.Fatal("R3/R10: restarted peer did not recover as follower")
	}
	// Demote one node through the real joint mechanism, then lose that learner.
	leader = next
	change, err := protoChange(3)
	clusterMust(t, err)
	f.call(leader, raftcore.Configure{Change: change})
	f.pump()
	empty, err := protoChange(0)
	clusterMust(t, err)
	f.call(leader, raftcore.Configure{Change: empty})
	f.pump()
	f.alive[2] = false
	leader = f.leader()
	f.propose(leader, 2, clusterstate.Mutation{Release: &clusterstate.ReleaseCell{Cell: "cell", Fence: 1}})
	row, err := clusterstate.DecodeRow(f.row[leader])
	clusterMust(t, err)
	if row.Cell.Lease.Expires != 0 || row.Cell.Lease.ReleasedAt != 1002 {
		t.Fatal("R11: departed learner blocked a committed release")
	}
	t.Logf("R3/R4/R10/R11: %d core inputs, %d outbound HTTP exchanges; only relay owns a listener", f.events, f.exchanges)
}
func protoChange(target uint64) ([]byte, error) {
	change := &pb.ConfChangeV2{}
	if target != 0 {
		change.Transition = pb.ConfChangeTransitionJointExplicit.Enum()
		change.Changes = []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: &target}}
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(change)
}

// R10/R30 keep unassembled peer capability absent from a blind relay and route
// signed peer exchange separately from legacy pairing and directory requests.
func TestR10R30ClusterRouteAbsentUntilHostAssembly(t *testing.T) {
	service := New(Config{})
	defer service.Close()
	for _, path := range []string{rafttransport.ExchangePath, rafttransport.SocketPath} {
		got := httptest.NewRecorder()
		service.Handler.ServeHTTP(got, httptest.NewRequest(http.MethodPost, path, nil))
		if got.Code != 404 {
			t.Fatal("R30: unassembled cluster transport advertised", path, got.Code)
		}
	}
}
