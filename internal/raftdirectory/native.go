package raftdirectory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type appliedCommand struct {
	result clusterstate.Result
	index  uint64
}

// Native owns three independently durable anchors and bridges external members.
// opMu orders public operations; mu serializes persistence, application and
// authenticated delivery. Waiting callers release only mu so peers can finish
// their quorum. An authority callback retains mu from its fresh barrier through
// its application read, preventing an intervening state change.
type Native struct {
	mu               sync.Mutex
	opMu             sync.Mutex
	changed          chan struct{}
	forwarders       map[uint64]*forwardLink
	pendingCommand   []byte
	pendingAdmission uint64
	outcome          *appliedCommand
	peers            *nativePeers
	manifest         clusterstate.LegacyManifest
	stores           [3]*raftstore.SQLite
	nodes            [3]*raftcore.Core
	status           [3]raftcore.Status
	queue            []raftcore.Outbound
	reads            map[string]bool
	results          [3][]clusterstate.Result
	serial           uint64
	failed           error
	closed           bool
	// Changed projects durable rows into advisory notifications under the queue.
	Changed func(*raftstore.SQLite, raftstore.Application) error
}

// OpenNative resumes the SAME imported genesis on every anchor. A partial or
// corrupt import refuses service and retains its source; it never bootstraps a
// singleton or recovers by deleting a member database.
func OpenNative(ctx context.Context, path string, source *directory.SQLite, m clusterstate.LegacyManifest) (_ *Native, err error) {
	if m.Validate() != nil {
		return nil, clusterstate.ErrMigration
	}
	g := &Native{manifest: m, reads: map[string]bool{}}
	defer func() {
		if err != nil {
			_ = g.Close()
		}
	}()
	gate, e := source.LegacyGate(ctx)
	if e != nil {
		return nil, e
	}
	owned := gate != nil && gate.Stage == "raft-owned"
	live := 0
	for i, id := range m.Config.Anchors {
		memberPath := filepath.Join(path, fmt.Sprintf("%d.db", id))
		// A published member with lost storage is unavailable, never a fresh copy
		// under its retired durable identity. Only actual surviving stores vote.
		if owned {
			if _, e := os.Stat(memberPath); errors.Is(e, os.ErrNotExist) {
				continue
			} else if e != nil {
				return nil, e
			}
		}
		g.stores[i], err = raftstore.Open(memberPath, raftstore.Identity{ClusterUUID: m.Config.Cluster, NodeID: id, Protocol: raftcore.ProtocolVersion})
		if err != nil {
			if owned && ctx.Err() == nil {
				g.stores[i] = nil
				continue
			}
			return nil, err
		}
		// Once ownership is published, even a valid empty file is not a returning
		// voter. Its own durable import receipt must predate this activation.
		if owned {
			imported, e := g.stores[i].ImportedLegacy(ctx)
			if e != nil && ctx.Err() != nil {
				return nil, e
			}
			if e != nil || imported == nil {
				_ = g.stores[i].Close()
				g.stores[i] = nil
				continue
			}
		}
		if err = g.stores[i].ImportLegacy(ctx, m, func(visit func(clusterstate.RecordWrite) error) error {
			_, e := source.ExportLegacy(ctx, visit)
			return e
		}); err != nil {
			if owned && ctx.Err() == nil {
				_ = g.stores[i].Close()
				g.stores[i] = nil
				continue
			}
			return nil, err
		}
		recovered, e := g.stores[i].Recover(ctx)
		if e != nil {
			if owned && ctx.Err() == nil {
				_ = g.stores[i].Close()
				g.stores[i] = nil
				continue
			}
			return nil, e
		}
		var seed [8]byte
		if _, err = rand.Read(seed[:]); err != nil {
			return nil, err
		}
		g.nodes[i], err = raftcore.New(raftcore.Config{ClusterUUID: m.Config.Cluster, NodeID: id, Protocol: raftcore.ProtocolVersion, Seed: binary.LittleEndian.Uint64(seed[:]), Anchors: m.Config.Anchors}, recovered.Core)
		if err != nil {
			return nil, err
		}
		live++
	}
	if live < 2 {
		return nil, directory.ErrUnreachable
	}
	for i := range g.nodes {
		if g.nodes[i] == nil {
			continue
		}
		if err = g.drive(ctx, i, raftcore.Poll{}); err != nil {
			return nil, err
		}
	}
	if err = g.drain(ctx); err != nil {
		return nil, err
	}
	// A surviving anchor may have an older suffix. Request a real campaign
	// from each available anchor until a current majority elects one of them.
	for i, node := range g.nodes {
		if node == nil {
			continue
		}
		if err = g.drive(ctx, i, raftcore.Campaign{}); err != nil {
			return nil, err
		}
		if err = g.drain(ctx); err != nil {
			return nil, err
		}
		if g.leader() >= 0 {
			break
		}
	}
	if _, _, err = g.barrier(ctx); err != nil {
		return nil, err
	}
	receipts := []directory.LegacyReceipt{}
	for i, s := range g.stores {
		if s == nil {
			continue
		}
		got, e := s.ImportedLegacy(ctx)
		if e != nil {
			return nil, e
		}
		if got == nil {
			return nil, clusterstate.ErrMigration
		}
		app, _, e := s.ApplicationView(ctx)
		if e != nil {
			return nil, e
		}
		receipts = append(receipts, directory.LegacyReceipt{Node: m.Config.Anchors[i], Manifest: *got, Applied: app.Applied})
	}
	if err = source.OwnLegacy(ctx, receipts); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Native) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	g.signal()
	for _, link := range g.forwarders {
		link.conn.CloseNow()
	}
	var err error
	for _, s := range g.stores {
		if s != nil {
			err = errors.Join(err, s.Close())
		}
	}
	return err
}
func (g *Native) leader() int {
	if g.closed || g.failed != nil {
		return -1
	}
	for i, s := range g.status {
		if s.Role == raft.StateLeader && !s.Stopped {
			return i
		}
	}
	return -1
}
func (g *Native) drive(ctx context.Context, i int, input raftcore.Input) error {
	// An admitted Ready finishes atomically even if the waiting HTTP deadline
	// expires. Only an actual storage error may stop this durable actor.
	ctx = context.WithoutCancel(ctx)
	defer g.signal()
	for n := 0; n < 10000; n++ {
		e, err := g.nodes[i].Step(input)
		if err != nil {
			return err
		}
		g.status[i] = e.Status
		if proposal, ok := input.(raftcore.Propose); ok && bytes.Equal(proposal.Command, g.pendingCommand) && e.Persist != nil {
			for _, entry := range e.Persist.Entries {
				if bytes.Equal(entry.GetData(), g.pendingCommand) {
					g.pendingAdmission = entry.GetIndex()
				}
			}
		}
		g.queue = append(g.queue, e.Send...)
		for _, r := range e.Reads {
			app, _, err := g.stores[i].ApplicationView(ctx)
			if err != nil {
				return err
			}
			if app.Applied < r.Index {
				return directory.ErrUnreachable
			}
			g.reads[r.RequestID] = true
		}
		if e.Persist != nil {
			if err = g.stores[i].Persist(ctx, *e.Persist); err != nil {
				_, _ = g.nodes[i].Step(raftcore.StorageFailed{BatchID: e.Persist.ID})
				return fmt.Errorf("raftdirectory: persist member %d snapshot %d: %w", g.manifest.Config.Anchors[i], e.Persist.Snapshot.GetMetadata().GetIndex(), err)
			}
			input = raftcore.Persisted{BatchID: e.Persist.ID}
		} else if e.Apply != nil {
			results, err := Apply(ctx, g.stores[i], *e.Apply, g.manifest.Config.Cluster, g.manifest.Ceiling)
			if err != nil {
				_, _ = g.nodes[i].Step(raftcore.StorageFailed{BatchID: e.Apply.ID})
				return fmt.Errorf("raftdirectory: apply member %d index %d: %w", g.manifest.Config.Anchors[i], e.Apply.Index, err)
			}
			g.results[i] = results
			resultIndex := 0
			for _, entry := range e.Apply.Entries {
				if entry.GetType() != pb.EntryNormal || len(entry.GetData()) == 0 {
					continue
				}
				if len(g.pendingCommand) > 0 && bytes.Equal(entry.GetData(), g.pendingCommand) && entry.GetIndex() == g.pendingAdmission && g.pendingAdmission != 0 && g.outcome == nil {
					g.outcome = &appliedCommand{result: results[resultIndex], index: entry.GetIndex()}
				}
				resultIndex++
			}
			input = raftcore.Applied{BatchID: e.Apply.ID}
		} else if e.SnapshotNeeded != nil {
			snapshot, err := g.stores[i].CreateSnapshot(ctx)
			if err != nil {
				return err
			}
			if err = g.stores[i].Compact(ctx, snapshot.GetMetadata().GetIndex()); err != nil {
				return err
			}
			input = raftcore.CompactSnapshot{Snapshot: snapshot}
		} else {
			return nil
		}
	}
	return directory.ErrUnreachable
}
func (g *Native) drain(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	for n := 0; len(g.queue) > 0; n++ {
		if n >= 10000 {
			return directory.ErrUnreachable
		}
		m := g.queue[0]
		g.queue = g.queue[1:]
		found := false
		for i, id := range g.manifest.Config.Anchors {
			if m.To == id {
				found = true
				if g.nodes[i] == nil {
					break
				}
				message := &pb.Message{}
				if proto.Unmarshal(m.Bytes, message) != nil {
					return raftcore.ErrPeer
				}
				if message.GetType() == pb.MsgSnap {
					if err := g.stageSnapshot(ctx, i, message); err != nil {
						return err
					}
				}
				if err := g.drive(ctx, i, raftcore.PeerMessage{Bytes: m.Bytes}); err != nil {
					return err
				}
				break
			}
		}
		if !found {
			if g.peers == nil {
				// Activation can recover voters before their committed router
				// opens. An unreachable learner cannot gate that recovery.
				continue
			}
			if err := g.sendExternal(ctx, m); err != nil {
				return err
			}
		}
	}
	return nil
}

// stageSnapshot streams the SAME portable generation before RawNode may
// persist its pointer. Neither a copied chunk nor a completed upload is an
// append acknowledgment; the target's subsequent Ready installation is.
func (g *Native) stageSnapshot(ctx context.Context, target int, message *pb.Message) error {
	pointer := message.GetSnapshot()
	app, _, err := g.stores[target].ApplicationView(ctx)
	if err != nil {
		return err
	}
	if pointer.GetMetadata().GetIndex() <= app.Applied {
		return nil
	}
	manifest, err := raftsnapshot.Decode(pointer.GetData())
	if err != nil {
		return err
	}
	var source *raftstore.SQLite
	for i, id := range g.manifest.Config.Anchors {
		if id == message.GetFrom() {
			source = g.stores[i]
			break
		}
	}
	if source == nil {
		return directory.ErrUnreachable
	}
	id, err := g.stores[target].BeginSnapshot(ctx, pointer.GetData())
	if err != nil {
		return fmt.Errorf("raftdirectory: begin snapshot %d on member %d: %w", manifest.Index, g.manifest.Config.Anchors[target], err)
	}
	for offset := uint64(0); offset < manifest.Chunks; offset++ {
		chunk, err := source.ReadSnapshotChunk(ctx, id, offset)
		if err != nil {
			return err
		}
		if err = g.stores[target].PutSnapshotChunk(ctx, id, offset, chunk, raftsnapshot.ID(chunk)); err != nil {
			return err
		}
	}
	complete, err := g.stores[target].CompleteSnapshot(ctx, id)
	if err != nil {
		return fmt.Errorf("raftdirectory: complete snapshot %d on member %d: %w", manifest.Index, g.manifest.Config.Anchors[target], err)
	}
	if !proto.Equal(complete, pointer) {
		return fmt.Errorf("raftdirectory: snapshot pointer mismatch on member %d: %w", g.manifest.Config.Anchors[target], raftstore.ErrSnapshot)
	}
	return nil
}

func (g *Native) submit(ctx context.Context, c clusterstate.Command) (clusterstate.Result, error) {
	if ctx.Err() != nil {
		return clusterstate.Result{}, directory.ErrUnreachable
	}
	if g.peers != nil {
		g.peers.sleeping = false
		g.peers.now = max(g.peers.now, c.Now)
	}
	ctx, cancel := context.WithTimeout(ctx, clusterctl.ForwardTimeout)
	defer cancel()
	raw, err := clusterstate.EncodeCommand(c)
	if err != nil {
		return clusterstate.Result{}, err
	}
	i := g.leader()
	if i < 0 {
		node := g.leaderID()
		reply, e := g.forward(ctx, node, clusterctl.Forward{Action: "submit", Command: raw})
		if e != nil {
			if err := g.refreshHint(ctx); err != nil {
				return clusterstate.Result{}, err
			}
			if g.leader() >= 0 {
				return g.submit(ctx, c)
			}
			// A stale hint retries the SAME signed request and forwarding ID.
			newer := g.leaderID()
			if newer != 0 && newer != node {
				reply, e = g.forward(ctx, newer, clusterctl.Forward{ID: reply.ID, Action: "submit", Command: raw})
			}
		}
		if e != nil || reply.Result == nil {
			return clusterstate.Result{}, directory.ErrUnreachable
		}
		if _, _, e = g.appliedReplica(ctx, reply.Applied); e != nil {
			return clusterstate.Result{}, e
		}
		if g.peers != nil {
			if e = g.installPeers(ctx); e != nil {
				return clusterstate.Result{}, e
			}
		}
		if g.Changed != nil {
			j, app, e := g.appliedReplica(ctx, reply.Applied)
			if e != nil {
				return clusterstate.Result{}, e
			}
			if e = g.Changed(g.stores[j], app); e != nil {
				return clusterstate.Result{}, e
			}
		}
		return *reply.Result, nil
	}
	g.pendingCommand = raw
	g.pendingAdmission = 0
	g.outcome = nil
	defer func() { g.pendingCommand = nil; g.pendingAdmission = 0; g.outcome = nil }()
	for j := range g.results {
		g.results[j] = nil
	}
	if err = g.drive(ctx, i, raftcore.Propose{Command: raw}); err == nil {
		err = g.drain(ctx)
	}
	if err != nil {
		g.failed = err
		return clusterstate.Result{}, err
	}
	for g.outcome == nil {
		if g.leader() != i {
			return g.submit(ctx, c)
		}
		if g.peers == nil || len(g.forwarders) == 0 {
			return clusterstate.Result{}, directory.ErrUnreachable
		}
		if err = g.waitProgress(ctx); err != nil {
			return clusterstate.Result{}, err
		}
	}
	r := g.outcome.result
	if _, _, err := g.appliedReplica(ctx, g.outcome.index); err != nil {
		return clusterstate.Result{}, err
	}
	ctx = context.WithoutCancel(ctx)
	// Compaction follows complete local delivery so every local voter has the
	// checkpoint's prefix. It removes only redundant consensus history.
	for j, s := range g.stores {
		if s == nil {
			continue
		}
		due, e := s.SnapshotDue(ctx)
		if e != nil {
			g.failed = e
			return r, e
		}
		if due {
			p, e := s.CreateSnapshot(ctx)
			if e != nil {
				g.failed = e
				return r, e
			}
			if e = s.Compact(ctx, p.GetMetadata().GetIndex()); e != nil {
				g.failed = e
				return r, e
			}
			if e = g.drive(ctx, j, raftcore.CompactSnapshot{Snapshot: p}); e != nil {
				g.failed = e
				return r, e
			}
		}
	}
	if err := g.checkpointExternal(ctx); err != nil {
		g.failed = err
		return r, err
	}
	if g.Changed != nil {
		app, _, e := g.stores[i].ApplicationView(ctx)
		if e != nil {
			return r, e
		}
		if e = g.Changed(g.stores[i], app); e != nil {
			return r, e
		}
	}
	if g.peers != nil {
		if err := g.installPeers(ctx); err != nil {
			return r, err
		}
	}
	return r, nil
}
func (g *Native) barrier(ctx context.Context) (int, raftstore.Application, error) {
	if ctx.Err() != nil {
		return -1, raftstore.Application{}, directory.ErrUnreachable
	}
	if g.peers != nil {
		g.peers.sleeping = false
	}
	ctx, cancel := context.WithTimeout(ctx, clusterctl.ForwardTimeout)
	defer cancel()
	i := g.leader()
	if i < 0 {
		node := g.leaderID()
		g.serial++
		id := fmt.Sprintf("read-%d", g.serial)
		reply, e := g.forward(ctx, node, clusterctl.Forward{ID: id, Action: "read"})
		if e != nil {
			if err := g.refreshHint(ctx); err != nil {
				return i, raftstore.Application{}, err
			}
			if local := g.leader(); local >= 0 {
				if err := g.drive(ctx, local, raftcore.Read{RequestID: id}); err != nil {
					return local, raftstore.Application{}, err
				}
				if err := g.drain(ctx); err != nil {
					return local, raftstore.Application{}, err
				}
				defer delete(g.reads, id)
				for !g.reads[id] {
					if err := g.waitProgress(ctx); err != nil {
						return local, raftstore.Application{}, err
					}
				}
				app, _, err := g.stores[local].ApplicationView(ctx)
				return local, app, err
			}
			newer := g.leaderID()
			if newer != 0 && newer != node {
				reply, e = g.forward(ctx, newer, clusterctl.Forward{ID: id, Action: "read"})
			}
		}
		if e != nil || reply.Barrier == 0 {
			return i, raftstore.Application{}, directory.ErrUnreachable
		}
		return g.appliedReplica(ctx, reply.Barrier)
	}
	g.serial++
	id := fmt.Sprintf("native-%d", g.serial)
	if err := g.drive(ctx, i, raftcore.Read{RequestID: id}); err != nil {
		return i, raftstore.Application{}, err
	}
	if err := g.drain(ctx); err != nil {
		g.failed = err
		return i, raftstore.Application{}, err
	}
	defer delete(g.reads, id)
	for !g.reads[id] {
		if node := g.leaderID(); node != 0 && node != g.manifest.Config.Anchors[i] {
			reply, err := g.forward(ctx, node, clusterctl.Forward{ID: id, Action: "read"})
			if err != nil || reply.Barrier == 0 {
				return i, raftstore.Application{}, directory.ErrUnreachable
			}
			return g.appliedReplica(ctx, reply.Barrier)
		}
		if g.peers == nil || len(g.forwarders) == 0 {
			return i, raftstore.Application{}, directory.ErrUnreachable
		}
		if err := g.waitProgress(ctx); err != nil {
			return i, raftstore.Application{}, err
		}
	}
	app, _, err := g.stores[i].ApplicationView(ctx)
	return i, app, err
}
func (g *Native) CommitV1(ctx context.Context, c clusterstate.Command) (clusterstate.Result, error) {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.submit(ctx, c)
}
func (g *Native) ReadV1(ctx context.Context, c clusterstate.Command, committed bool, inspect func(context.Context, *raftstore.SQLite, raftstore.Application) error) error {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !committed {
		if c.Change.AdvanceTime == nil {
			return directory.ErrUnauthorized
		}
		r, e := g.submit(ctx, c)
		if e != nil {
			return e
		}
		if e = directory.ResultError(r.Code); e != nil {
			return e
		}
	}
	i, app, err := g.barrier(ctx)
	if err != nil {
		return err
	}
	return inspect(ctx, g.stores[i], app)
}
func (g *Native) CommitEffect(ctx context.Context, c clusterstate.Command) (clusterstate.Result, error) {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.submit(ctx, c)
}

// Inspect is an internal service read. It always passes a new current-term
// quorum barrier and gives the callback its actor-owned context. A disconnected
// HTTP request cannot turn a healthy durable read into a storage failure.
func (g *Native) Inspect(ctx context.Context, inspect func(context.Context, *raftstore.SQLite, raftstore.Application) error) error {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	i, app, err := g.barrier(ctx)
	if err != nil {
		return err
	}
	return inspect(ctx, g.stores[i], app)
}
func (g *Native) ReadEffect(ctx context.Context, k clusterstate.RecordKey) (raw []byte, err error) {
	err = g.Inspect(ctx, func(ctx context.Context, s *raftstore.SQLite, a raftstore.Application) error {
		if _, e := AuthorizedEffectRead(ctx, s, g.manifest.Config.Cluster, clusterstate.DeviceIDOfKey(g.manifest.Config.EffectsKey)); e != nil {
			return e
		}
		key := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
		rows, _, e := s.Read(ctx, a.Applied, []raftstore.Key{key})
		raw = rows[key]
		return e
	})
	return
}
func (g *Native) ScanEffects(ctx context.Context, after string, limit int) (rows []clusterstate.RecordWrite, err error) {
	err = g.Inspect(ctx, func(ctx context.Context, s *raftstore.SQLite, a raftstore.Application) error {
		if _, e := AuthorizedEffectRead(ctx, s, g.manifest.Config.Cluster, clusterstate.DeviceIDOfKey(g.manifest.Config.EffectsKey)); e != nil {
			return e
		}
		page, _, e := s.ScanPage(ctx, a.Applied, raftstore.Outbox, after)
		for _, r := range page[:min(len(page), limit)] {
			rows = append(rows, clusterstate.RecordWrite{RecordKey: clusterstate.RecordKey{Namespace: string(r.Namespace), Key: r.Key.Key}, Data: r.Value})
		}
		return e
	})
	return
}
