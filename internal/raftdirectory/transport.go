package raftdirectory

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type snapshotTransfer struct {
	from       int
	message    []byte
	manifest   raftsnapshot.Manifest
	generation string
	offset     uint64
	started    int64
}
type nativePeers struct {
	viewStore   *raftstore.SQLite
	viewApplied uint64
	incoming    map[int]*incomingSnapshot
	router      *rafttransport.Router
	view        rafttransport.View
	keys        [3]ed25519.PrivateKey
	sequence    [3]uint64
	ack         [3][3]uint64
	applied     map[uint64]uint64
	snapshots   map[uint64]*snapshotTransfer
	now         int64
	sleeping    bool
	checkpoint  bool
}

// AttachTransport restores authority from applied membership before exposing
// the listener. Generation was durably advanced by the host before this call.
func (g *Native) AttachTransport(ctx context.Context, generation uint64, keys [3]ed25519.PrivateKey) (*rafttransport.Router, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	router, err := rafttransport.NewRouter(rafttransport.DefaultLimits())
	if err != nil {
		return nil, err
	}
	for i, key := range keys {
		if len(key) != ed25519.PrivateKeySize || !key.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(g.manifest.Config.AnchorKeys[i])) {
			return nil, rafttransport.ErrAuth
		}
	}
	g.peers = &nativePeers{router: router, keys: keys, view: rafttransport.View{Scope: rafttransport.Scope{Identity: g.manifest.Config.Identity, Cluster: g.manifest.Config.Cluster, Epoch: g.manifest.Config.Generation, Protocol: raftcore.ProtocolVersion}, Generation: generation}, incoming: map[int]*incomingSnapshot{}, applied: map[uint64]uint64{}, snapshots: map[uint64]*snapshotTransfer{}}
	if err = g.installPeers(ctx); err != nil {
		return nil, err
	}
	if err = g.reconcileMembership(ctx); err != nil {
		return nil, err
	}
	router.Pump = g.pumpPeers
	return router, nil
}
func (g *Native) installPeers(ctx context.Context) error {
	p := g.peers
	if p == nil {
		return nil
	}
	i := g.replica()
	if i < 0 {
		return directory.ErrUnreachable
	}
	// Peer authority changes only with durable application. Acknowledgements
	// and read barriers reuse the view for that same store and applied index.
	if p.viewStore == g.stores[i] && p.viewApplied == g.status[i].Applied {
		return nil
	}
	app, _, err := g.stores[i].ApplicationView(ctx)
	if err != nil {
		return err
	}
	read := recordReader(ctx, g.stores[i], app)
	v, err := rafttransport.CommittedView(p.view.Scope, p.view.Generation, app.Applied, raftcore.Configuration(app.ConfState), read)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(v.Members, p.view.Members) {
		p.viewStore, p.viewApplied = g.stores[i], app.Applied
		return nil
	}
	if err = p.router.Install(v); err != nil {
		return err
	}
	p.view = v
	p.viewStore, p.viewApplied = g.stores[i], app.Applied
	// Installing a changed voter class discards expendable deliveries. Start a
	// real read round so append progress is retried immediately, including while
	// a public transfer owns opMu and the periodic maintenance turn is waiting.
	if i := g.leader(); i >= 0 {
		g.serial++
		id := fmt.Sprintf("routing-%d", g.serial)
		if err = g.drive(ctx, i, raftcore.IdleRead{RequestID: id}); err != nil {
			return err
		}
		if err = g.drain(ctx); err != nil {
			return err
		}
		if err = g.drive(ctx, i, raftcore.CancelRead{RequestID: id}); err != nil {
			return err
		}
		delete(g.reads, id)
	} else if target := g.leaderID(); target != 0 {
		if link := g.forwarders[target]; link != nil {
			g.serial++
			work := clusterctl.Forward{Scope: p.view.Scope, Generation: p.view.Generation, From: g.manifest.Config.Anchors[0], To: target, ID: fmt.Sprintf("routing-%d", g.serial), Action: "poke"}
			work.Sign(func(b []byte) []byte { return ed25519.Sign(p.keys[0], b) })
			// A pump must never await this application callback. The elected member
			// starts a real read round to retry deliveries discarded by the new view.
			select {
			case link.send <- work:
			default:
			}
		}
	}
	// Registry changes discard expendable inboxes. Report an unfinished snapshot
	// as failed so Raft can restart it under the new committed transport view.
	for to, transfer := range p.snapshots {
		delete(p.snapshots, to)
		if err = g.drive(ctx, transfer.from, raftcore.TransportResult{Peer: to, Snapshot: true}); err != nil {
			return err
		}
	}
	return nil
}
func (g *Native) peer(id uint64) (rafttransport.Member, bool) {
	for _, m := range g.peers.view.Members {
		if m.Node == id && !m.Revoked {
			return m, true
		}
	}
	return rafttransport.Member{}, false
}
func (g *Native) envelope(from int, to uint64, payload []byte, chunk *rafttransport.Chunk) (rafttransport.Envelope, error) {
	p := g.peers
	target, ok := g.peer(to)
	if !ok {
		return rafttransport.Envelope{}, rafttransport.ErrAuth
	}
	if p.sequence[from] == ^uint64(0) {
		return rafttransport.Envelope{}, rafttransport.ErrGeneration
	}
	p.sequence[from]++
	e := rafttransport.Envelope{Scope: p.view.Scope, From: g.manifest.Config.Anchors[from], To: to, Incarnation: 1, Destination: target.Incarnation, Sequence: p.sequence[from], Payload: payload, Chunk: chunk}
	err := e.Sign(p.keys[from])
	return e, err
}
func (g *Native) localExchange(from int, messages []rafttransport.Envelope) (rafttransport.Response, error) {
	p := g.peers
	request := rafttransport.Request{Scope: p.view.Scope, Generation: p.view.Generation, Node: g.manifest.Config.Anchors[from], Incarnation: 1, Ack: p.ack[from], Messages: messages}
	if err := request.Sign(p.keys[from]); err != nil {
		return rafttransport.Response{}, err
	}
	return p.router.LocalExchange(request, p.now)
}
func (g *Native) queueExternal(from int, to uint64, payload []byte, chunk *rafttransport.Chunk) error {
	e, err := g.envelope(from, to, payload, chunk)
	if err != nil {
		return err
	}
	_, err = g.localExchange(from, []rafttransport.Envelope{e})
	return err
}
func (g *Native) sendExternal(ctx context.Context, m raftcore.Outbound) error {
	p := g.peers
	_, ok := g.peer(m.To)
	if !ok {
		return nil
	}

	if p.sleeping {
		return nil
	}
	message := new(pb.Message)
	if proto.Unmarshal(m.Bytes, message) != nil {
		return raftcore.ErrPeer
	}
	from := -1
	for i, id := range g.manifest.Config.Anchors {
		if id == message.GetFrom() {
			from = i
		}
	}
	if from < 0 {
		return raftcore.ErrPeer
	}
	// A legal maximum command can gain protobuf framing beyond the normal
	// transport envelope. Keep BOTH caps: checkpoint its applied prefix and
	// let that learner receive chunks, rather than failing a voter write.
	if len(m.Bytes) > rafttransport.MaxBatchBytes {
		p.checkpoint = true
		return g.drive(ctx, from, raftcore.TransportResult{Peer: m.To})
	}
	if message.GetType() == pb.MsgSnap {
		manifest, err := raftsnapshot.Decode(message.GetSnapshot().GetData())
		if err != nil {
			return err
		}
		p.snapshots[m.To] = &snapshotTransfer{from: from, message: m.Bytes, manifest: manifest, generation: raftsnapshot.ID(message.GetSnapshot().GetData()), started: p.now}
		// Start with the manifest; chunks occupy their independently bounded lane.
		if err = g.queueExternal(from, m.To, m.Bytes, nil); err != nil {
			delete(p.snapshots, m.To)
			if errors.Is(err, rafttransport.ErrCapacity) {
				return g.drive(ctx, from, raftcore.TransportResult{Peer: m.To, Snapshot: true})
			}
			return err
		}
		return nil
	}
	if err := g.queueExternal(from, m.To, m.Bytes, nil); err != nil {
		if errors.Is(err, rafttransport.ErrCapacity) {
			return g.drive(ctx, from, raftcore.TransportResult{Peer: m.To})
		}
		return err
	}
	return nil
}

// pumpPeers steps authenticated external input through the same persistence
// queue as local messages. The router lock is released before acquiring mu;
// public writes never wait for a learner, its socket or a network callback.
func (g *Native) pumpPeers(request rafttransport.Request, now int64) error {
	ctx := context.Background()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.failed != nil {
		return directory.ErrUnreachable
	}
	p := g.peers
	p.now = max(p.now, now)
	if err := g.installPeers(ctx); err != nil {
		return err
	}
	if _, ok := g.peer(request.Node); !ok || request.Scope != p.view.Scope || request.Generation != p.view.Generation {
		return rafttransport.ErrAuth
	}
	if request.Applied != 0 {
		i := g.replica()
		if i < 0 {
			return directory.ErrUnreachable
		}
		app, _, err := g.stores[i].ApplicationView(ctx)
		if err != nil {
			return err
		}
		peer, _ := g.peer(request.Node)
		if !peer.Voter && request.Applied > app.Applied {
			return rafttransport.ErrWire
		}
		p.applied[request.Node] = max(p.applied[request.Node], request.Applied)
	}
	// A final learner response can arrive after all voters agreed to sleep.
	// It has no quorum weight and must not restart periodic work. New requests
	// and higher-term evidence still wake through the ordinary peer path.
	for _, envelope := range request.Messages {
		if !g.idleLearnerResponse(envelope) {
			p.sleeping = false
		}
	}
	for round := 0; round < rafttransport.MaxInflight; round++ {
		progressed := false
		for i, node := range g.nodes {
			if node == nil {
				continue
			}
			response, err := g.localExchange(i, nil)
			if err != nil {
				return err
			}
			for _, delivery := range response.Messages {
				e := delivery.Envelope
				member, ok := g.peer(e.From)
				if !ok || e.Scope != p.view.Scope || e.Destination != 1 || e.Verify(member.Key) != nil {
					return rafttransport.ErrAuth
				}
				if !g.idleLearnerResponse(e) {
					if err = g.receivePeer(ctx, i, e); err != nil {
						return err
					}
				}
				p.ack[i][delivery.Lane] = max(p.ack[i][delivery.Lane], delivery.Cursor)
				progressed = true
			}
		}
		if err := g.drain(ctx); err != nil {
			return err
		}
		if !progressed {
			break
		}
	}
	if err := g.checkpointExternal(ctx); err != nil {
		return err
	}
	if p.sleeping {
		return nil
	}
	// Read a single chunk at a time. Backpressure discards no authoritative
	// state and never pins compaction; an expired transfer is retried by Raft.
	for to, transfer := range p.snapshots {
		if p.applied[to] >= transfer.manifest.Index {
			delete(p.snapshots, to)
			if err := g.drive(ctx, transfer.from, raftcore.TransportResult{Peer: to, Snapshot: true, Success: true}); err != nil {
				return err
			}
			continue
		}
		if p.now-transfer.started >= rafttransport.RetentionMillis {
			delete(p.snapshots, to)
			if err := g.drive(ctx, transfer.from, raftcore.TransportResult{Peer: to, Snapshot: true}); err != nil {
				return err
			}
			continue
		}
		if transfer.offset >= transfer.manifest.Chunks {
			continue
		}
		chunk, err := g.stores[transfer.from].ReadSnapshotChunk(ctx, transfer.generation, transfer.offset)
		if err != nil {
			delete(p.snapshots, to)
			if err = g.drive(ctx, transfer.from, raftcore.TransportResult{Peer: to, Snapshot: true}); err != nil {
				return err
			}
			continue
		}
		err = g.queueExternal(transfer.from, to, chunk, &rafttransport.Chunk{Generation: transfer.generation, Offset: transfer.offset, Hash: raftsnapshot.ID(chunk)})
		if errors.Is(err, rafttransport.ErrCapacity) {
			continue
		}
		if err != nil {
			return err
		}
		transfer.offset++
	}
	return g.drain(ctx)
}

// quiescePeers abandons expendable partial transfers as well as inboxes. The
// next real ingress can immediately retry from the current compacted prefix.
func (g *Native) quiescePeers(ctx context.Context) error {
	p := g.peers
	p.sleeping = true
	p.router.Quiesce(p.view.Scope)
	for to, transfer := range p.snapshots {
		delete(p.snapshots, to)
		if err := g.drive(ctx, transfer.from, raftcore.TransportResult{Peer: to, Snapshot: true}); err != nil {
			return err
		}
	}
	return g.drain(ctx)
}

// checkpointExternal handles normal framing overflow with the existing streamed
// snapshot lane. Only the durably applied prefix is compacted; learner progress
// never delays publication or changes an application or transport byte limit.
func (g *Native) checkpointExternal(ctx context.Context) error {
	if g.peers == nil || !g.peers.checkpoint {
		return nil
	}
	i := g.leader()
	if i < 0 {
		return directory.ErrUnreachable
	}
	snapshot, err := g.stores[i].CreateSnapshot(ctx)
	if err != nil {
		return err
	}
	if err = g.stores[i].Compact(ctx, snapshot.GetMetadata().GetIndex()); err != nil {
		return err
	}
	if err = g.drive(ctx, i, raftcore.CompactSnapshot{Snapshot: snapshot}); err != nil {
		return err
	}
	g.peers.checkpoint = false
	return g.drain(ctx)
}

// idleLearnerResponse discards only response traffic rendered unnecessary by
// the completed all-voter sleep barrier. It cannot hide a newer term or a vote.
func (g *Native) idleLearnerResponse(e rafttransport.Envelope) bool {
	if !g.peers.sleeping {
		return false
	}
	member, ok := g.peer(e.From)
	if !ok || member.Voter {
		return false
	}
	leader := g.leader()
	if leader < 0 {
		return false
	}
	message := new(pb.Message)
	if proto.Unmarshal(e.Payload, message) != nil || message.GetTerm() > g.status[leader].Term {
		return false
	}
	return message.GetType() == pb.MsgAppResp || message.GetType() == pb.MsgHeartbeatResp
}
