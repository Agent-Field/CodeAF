package raftcore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"slices"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

var wire = proto.MarshalOptions{Deterministic: true}

// storage is an explicit in-memory view of recovered and completed durable
// bytes. No host callback or filesystem read occurs while stepping consensus.
// Durable checkpoints bound this cache; this is not a durable store.
type storage struct {
	*raft.MemoryStorage
	conf *pb.ConfState
}

func (s *storage) InitialState() (*pb.HardState, *pb.ConfState, error) {
	h, _, err := s.MemoryStorage.InitialState()
	return h, s.conf, err
}

// Snapshot returns the exact portable configuration encoding. The library
// fills optional zero fields in memory; that presence must not change the
// manifest-bound bytes which the durable store and another runtime compare.
func (s *storage) Snapshot() (*pb.Snapshot, error) {
	p, err := s.MemoryStorage.Snapshot()
	if err != nil {
		return nil, err
	}
	return canonicalSnapshot(p)
}

// canonicalSnapshot accepts only equal effective configurations and restores
// the manifest's exact protobuf presence. It never changes a voter, learner,
// joint field, term or index, and rejects unknown snapshot metadata.
func canonicalSnapshot(p *pb.Snapshot) (*pb.Snapshot, error) {
	if raft.IsEmptySnap(p) {
		return p, nil
	}
	m, err := raftsnapshot.Decode(p.GetData())
	if err != nil {
		return nil, ErrSnapshot
	}
	conf, err := m.Configuration()
	if err != nil {
		return nil, ErrSnapshot
	}
	meta := p.GetMetadata()
	if meta == nil || len(p.ProtoReflect().GetUnknown()) != 0 || len(meta.ProtoReflect().GetUnknown()) != 0 || meta.GetConfState() == nil || len(meta.GetConfState().ProtoReflect().GetUnknown()) != 0 || meta.GetIndex() != m.Index || meta.GetTerm() != m.Term || !reflect.DeepEqual(Configuration(meta.GetConfState()), Configuration(conf)) {
		return nil, ErrSnapshot
	}
	p = clone(p)
	p.Metadata.ConfState = conf
	return p, nil
}

type pending struct {
	id        uint64
	ready     raft.Ready
	persisted bool
	index     uint64
}
type readRequest struct {
	id        string
	term      uint64
	submitted bool
	cancelled bool
}

// Core owns no goroutines or ambient clocks. A host serializes calls and
// retries ErrBusy inputs after completing the outstanding batch; nothing is
// queued implicitly. Storage failure or an unknown committed command stops the
// handle. Recovery always starts as a follower from supplied durable bytes.
type Core struct {
	schedule     scheduling
	cfg          Config
	node         *raft.RawNode
	store        *storage
	rng          splitMix64
	electionRank int
	applied      uint64
	serial       uint64
	quorumTerm   uint64
	quorumIndex  uint64
	readSerial   uint64
	reads        map[string]readRequest
	barriers     map[string]uint64
	pending      *pending
	stopped      bool
}

func clone[T proto.Message](v T) T {
	if !v.ProtoReflect().IsValid() {
		return v
	}
	return proto.Clone(v).(T)
}
func copyEntries(entries []*pb.Entry) []*pb.Entry {
	if entries == nil {
		return nil
	}
	out := make([]*pb.Entry, len(entries))
	for i, e := range entries {
		out[i] = clone(e)
	}
	return out
}

// New refuses inconsistent recovery rather than manufacturing missing committed
// bytes. The caller validates durable checksums and cluster/format manifests
// before supplying this state; storage formats are the host adapters' responsibility.
func New(cfg Config, recovered RecoveredState) (core *Core, err error) {
	defer func() {
		if recover() != nil {
			core, err = nil, ErrConfig
		}
	}()
	if cfg.Protocol != ProtocolVersion {
		return nil, ErrProtocol
	}
	if cfg.ClusterUUID == "" || cfg.NodeID == 0 || raft.IsLocalMsgTarget(cfg.NodeID) {
		return nil, ErrConfig
	}
	if cfg.TimingVersion == 0 {
		cfg.TimingVersion = ElectionTimingVersion
	}
	if cfg.TimingVersion != 1 && cfg.TimingVersion != ElectionTimingVersion {
		return nil, ErrConfig
	}
	anchors := cfg.Anchors
	if len(recovered.Anchors) != 0 {
		if len(anchors) != 0 && !slices.Equal(anchors, recovered.Anchors) {
			return nil, ErrConfig
		}
		anchors = recovered.Anchors
	} else if len(anchors) == 0 && len(cfg.Bootstrap) == 3 {
		anchors = slices.Clone(cfg.Bootstrap)
		slices.Sort(anchors)
	}
	rank := 3
	if len(anchors) != 0 {
		if len(anchors) != 3 {
			return nil, ErrConfig
		}
		for i, id := range anchors {
			if id == 0 || raft.IsLocalMsgTarget(id) || i > 0 && id <= anchors[i-1] {
				return nil, ErrConfig
			}
			if id == cfg.NodeID {
				rank = i
			}
		}
	}
	cfg.Anchors = slices.Clone(anchors)
	snapshot := clone(recovered.Snapshot)
	snapIndex := snapshot.GetMetadata().GetIndex()
	last := snapIndex
	lastTerm := snapshot.GetMetadata().GetTerm()
	for _, e := range recovered.Entries {
		if e == nil || last == ^uint64(0) || e.GetIndex() != last+1 || e.GetTerm() == 0 || e.GetTerm() < lastTerm {
			return nil, ErrConfig
		}
		last = e.GetIndex()
		lastTerm = e.GetTerm()
	}
	h := recovered.HardState
	if recovered.Applied < snapIndex || recovered.Applied > h.GetCommit() || h.GetCommit() > last || h.GetCommit() < snapIndex || h.GetTerm() < lastTerm {
		return nil, ErrConfig
	}
	if len(cfg.Bootstrap) != 0 && (last != 0 || recovered.Applied != 0 || !raft.IsEmptyHardState(h) || recovered.ConfState != nil) {
		return nil, ErrConfig
	}
	conf := clone(recovered.ConfState)
	if conf == nil {
		conf = clone(snapshot.GetMetadata().GetConfState())
	}
	if conf == nil {
		conf = &pb.ConfState{}
	}
	c := &Core{schedule: scheduling{cancelled: recovered.Applied}, cfg: cfg, electionRank: rank, rng: splitMix64{state: cfg.Seed}, applied: recovered.Applied, reads: make(map[string]readRequest), barriers: make(map[string]uint64)}
	c.store = &storage{MemoryStorage: raft.NewMemoryStorage(), conf: conf}
	if !raft.IsEmptySnap(snapshot) {
		if err := c.store.ApplySnapshot(snapshot); err != nil {
			return nil, ErrConfig
		}
	}
	if err := c.store.Append(copyEntries(recovered.Entries)); err != nil {
		return nil, ErrConfig
	}
	if h != nil {
		if err := c.store.SetHardState(clone(h)); err != nil {
			return nil, ErrConfig
		}
	}
	randomBound := ElectionRandomBound
	if cfg.TimingVersion == 1 {
		randomBound = ElectionTicks
	}
	c.node, err = raft.NewRawNode(&raft.Config{
		ID: cfg.NodeID, ElectionTick: ElectionTicks, HeartbeatTick: HeartbeatTicks,
		Storage: c.store, Applied: recovered.Applied, ElectionRandom: c.electionRandom, ElectionRandomBound: randomBound,
		PreVote: true, CheckQuorum: true, ReadOnlyOption: raft.ReadOnlySafe,
		DisableProposalForwarding: true, AsyncStorageWrites: false,
		MaxSizePerMsg: MaxMessageBytes, MaxCommittedSizePerReady: MaxMessageBytes,
		MaxUncommittedEntriesSize: MaxUncommittedBytes, MaxInflightMsgs: MaxInflightBatches,
		MaxInflightBytes:  MaxInflightBatches * MaxMessageBytes,
		StepDownOnRemoval: true, Logger: silentLogger{},
	})
	if err != nil {
		return nil, err
	}
	if len(cfg.Bootstrap) != 0 {
		ids := slices.Clone(cfg.Bootstrap)
		slices.Sort(ids)
		peers := make([]raft.Peer, len(ids))
		for i, id := range ids {
			if id == 0 || raft.IsLocalMsgTarget(id) || (i > 0 && ids[i-1] == id) {
				return nil, ErrConfig
			}
			peers[i] = raft.Peer{ID: id}
		}
		if err := c.node.Bootstrap(peers); err != nil {
			return nil, ErrConfig
		}
	}
	return c, nil
}

func (c *Core) status() Status {
	s := c.node.BasicStatus()
	return Status{NodeID: c.cfg.NodeID, Leader: s.Lead, Term: s.GetTerm(), Commit: s.GetCommit(), Applied: c.applied, Role: s.RaftState, RNGState: c.rng.state, RNGDraws: c.rng.draws, Stopped: c.stopped}
}
func (c *Core) leader() bool { return c.node.BasicStatus().RaftState == raft.StateLeader }

// Step is synchronous. Effects contain independent bytes, not aliases into
// RawNode. Peer authentication and membership generations are host obligations;
// this boundary additionally forbids all network-injected local control verbs.
func (c *Core) Step(input Input) (effects Effects, err error) {
	defer func() {
		if recover() != nil {
			c.stopped = true
			effects = Effects{}
			err = ErrStopped
		}
		effects.Status = c.status()
		effects.NeedNextTick = !c.stopped && !c.schedule.sleeping
		if !c.stopped && c.pending == nil {
			effects.SnapshotNeeded = c.SnapshotNeeded()
		}
	}()
	if idle, ok := input.(IdleRead); ok {
		active, sleeping := c.schedule.active, c.schedule.sleeping
		input = Read{RequestID: idle.RequestID}
		defer func() {
			if err == nil && !sleeping {
				c.schedule.active = active
			}
		}()
	}
	if c.stopped {
		return effects, ErrStopped
	}
	switch in := input.(type) {
	case StorageFailed:
		if c.pending == nil || in.BatchID != c.pending.id {
			return effects, ErrCompletion
		}
		c.stopped = true
		return effects, ErrStopped
	case Persisted:
		p := c.pending
		if p == nil || in.BatchID != p.id || p.persisted {
			return effects, ErrCompletion
		}
		rd := p.ready
		if !raft.IsEmptySnap(rd.Snapshot) {
			if err := c.store.ApplySnapshot(clone(rd.Snapshot)); err != nil {
				c.stopped = true
				return effects, ErrStopped
			}
			c.store.conf = clone(rd.Snapshot.GetMetadata().GetConfState())
		}
		if err := c.store.Append(copyEntries(rd.Entries)); err != nil {
			c.stopped = true
			return effects, ErrStopped
		}
		if !raft.IsEmptyHardState(rd.HardState) {
			if err := c.store.SetHardState(clone(rd.HardState)); err != nil {
				c.stopped = true
				return effects, ErrStopped
			}
		}
		p.persisted = true
		return c.release()
	case Applied:
		p := c.pending
		if p == nil || in.BatchID != p.id || !p.persisted || p.index == 0 {
			return effects, ErrCompletion
		}
		c.applied = p.index
		c.finish(&effects)
		return effects, c.collect(&effects)
	}
	if c.pending != nil {
		return effects, ErrBusy
	}
	switch input.(type) {
	case Read, Propose, Configure, Transfer:
		c.wakeScheduling()
	case PeerMessage:
		if c.schedule.sleeping {
			c.wakeScheduling()
		}
	}
	switch in := input.(type) {
	case CompactSnapshot:
		p := in.Snapshot
		m, e := raftsnapshot.Decode(p.GetData())
		index := p.GetMetadata().GetIndex()
		term, e2 := c.store.Term(index)
		if e != nil || e2 != nil || index != c.applied || m.Index != index || m.Term != term || m.Cluster != c.cfg.ClusterUUID || m.Protocol != uint64(c.cfg.Protocol) || p.GetMetadata().GetTerm() != term || !proto.Equal(p.GetMetadata().GetConfState(), c.store.conf) {
			return effects, ErrSnapshot
		}
		conf, e := m.Configuration()
		if e != nil || !proto.Equal(conf, c.store.conf) {
			return effects, ErrSnapshot
		}
		existing, e := c.store.Snapshot()
		if e != nil {
			return effects, ErrSnapshot
		}
		if existing.GetMetadata().GetIndex() == index {
			if !proto.Equal(existing, p) {
				return effects, ErrSnapshot
			}
		} else {
			if _, e = c.store.CreateSnapshot(index, clone(c.store.conf), bytes.Clone(p.Data)); e != nil {
				return effects, ErrSnapshot
			}
		}
		first, _ := c.store.FirstIndex()
		if first <= index {
			if e = c.store.Compact(index); e != nil {
				return effects, ErrSnapshot
			}
		}
	case Campaign:
		if err = c.node.Campaign(); err != nil {
			return effects, err
		}
	case Poll:
	case Tick:
		if !c.schedule.sleeping {
			c.node.Tick()
		}
	case PeerMessage:
		m := &pb.Message{}
		if len(in.Bytes) > MaxUncommittedBytes || proto.Unmarshal(in.Bytes, m) != nil || !c.validPeer(m) {
			return effects, ErrPeer
		}
		// A delayed response can outlive committed learner removal. RawNode
		// refuses it before changing consensus; discard that stale delivery rather
		// than treating ordinary membership churn as a failed member.
		if err = c.node.Step(m); err != nil && !errors.Is(err, raft.ErrStepPeerNotFound) {
			return effects, err
		}
	case Propose:
		if !c.leader() {
			return effects, ErrLeader
		}
		if !c.validCommand(in.Command) {
			return effects, ErrCommand
		}
		if err = c.node.Propose(bytes.Clone(in.Command)); err != nil {
			return effects, err
		}
	case Configure:
		if !c.leader() {
			return effects, ErrLeader
		}
		cc := &pb.ConfChangeV2{}
		if len(in.Change) > MaxMessageBytes || proto.Unmarshal(in.Change, cc) != nil {
			return effects, ErrCommand
		}
		if err = c.node.ProposeConfChange(cc); err != nil {
			return effects, err
		}
	case Read:
		c.quorumIndex, c.quorumTerm = 0, 0
		if !c.leader() {
			return effects, ErrLeader
		}
		if in.RequestID == "" || len(in.RequestID) > MaxRequestIDBytes || len(c.reads) >= MaxReadRequests {
			return effects, ErrRead
		}
		for _, r := range c.reads {
			if r.id == in.RequestID && !r.cancelled {
				return effects, ErrRead
			}
		}
		if c.readSerial == ^uint64(0) {
			c.stopped = true
			return effects, ErrStopped
		}
		c.readSerial++
		context := make([]byte, 16)
		term := c.node.BasicStatus().GetTerm()
		binary.BigEndian.PutUint64(context, term)
		binary.BigEndian.PutUint64(context[8:], c.readSerial)
		c.reads[string(context)] = readRequest{id: in.RequestID, term: term}
	case CancelRead:
		for key, r := range c.reads {
			if r.id == in.RequestID {
				if r.submitted {
					// RawNode still owns this context until its quorum round
					// completes. Keep its slot charged to bound cancelled work.
					r.cancelled = true
					c.reads[key] = r
				} else {
					delete(c.reads, key)
				}
			}
		}
	case Transfer:
		if !c.leader() {
			return effects, ErrLeader
		}
		s := c.node.Status()
		progress, ok := s.Progress[in.Target]
		// The host's authorized transfer barrier must already have committed. A
		// learner, outgoing-only voter or lagging node cannot qualify here.
		if !ok || in.Target == c.cfg.NodeID || progress.IsLearner || progress.Match < s.GetCommit() {
			return effects, ErrTransfer
		}
		if _, voter := s.Config.Voters[0][in.Target]; !voter {
			return effects, ErrTransfer
		}
		if len(s.Config.Voters[1]) != 0 {
			return effects, ErrTransfer
		}
		c.node.TransferLeader(in.Target)
	case TransportResult:
		if in.Peer == 0 || raft.IsLocalMsgTarget(in.Peer) {
			return effects, ErrPeer
		}
		if in.Snapshot {
			result := raft.SnapshotFailure
			if in.Success {
				result = raft.SnapshotFinish
			}
			c.node.ReportSnapshot(in.Peer, result)
		} else if !in.Success {
			c.node.ReportUnreachable(in.Peer)
		}
	default:
		return effects, ErrCommand
	}
	return effects, c.collect(&effects)
}

func (c *Core) validCommand(data []byte) bool {
	if len(data) == 0 || len(data) > MaxMessageBytes {
		return false
	}
	cmd, err := clusterstate.DecodeCommand(data)
	return err == nil && cmd.Cluster == c.cfg.ClusterUUID
}

func (c *Core) validPeer(m *pb.Message) bool {
	if m.Type == nil || m.GetTo() != c.cfg.NodeID || m.GetFrom() == 0 || m.GetFrom() == c.cfg.NodeID || raft.IsLocalMsgTarget(m.GetFrom()) || m.GetTerm() == 0 || len(m.GetResponses()) != 0 || len(m.ProtoReflect().GetUnknown()) != 0 {
		return false
	}
	switch m.GetType() {
	case pb.MsgApp, pb.MsgAppResp, pb.MsgVote, pb.MsgVoteResp, pb.MsgSnap, pb.MsgHeartbeat, pb.MsgHeartbeatResp, pb.MsgPreVote, pb.MsgPreVoteResp, pb.MsgTimeoutNow:
		return true
	default:
		return false
	}
}

// issueReads keeps pre-commit reads in our bounded map instead of RawNode's
// pending-current-term queue. Cancelled submitted contexts retain admission
// slots until completion, so repeated timeouts cannot grow internal queues.
func (c *Core) issueReads() {
	s := c.node.BasicStatus()
	keys := make([]string, 0, len(c.reads))
	for key, request := range c.reads {
		if !c.leader() || request.term != s.GetTerm() {
			delete(c.reads, key)
			delete(c.barriers, key)
		} else if !request.submitted {
			keys = append(keys, key)
		}
	}
	term, err := c.store.Term(s.GetCommit())
	if err != nil || term != s.GetTerm() {
		return
	}
	slices.Sort(keys)
	for _, key := range keys {
		r := c.reads[key]
		r.submitted = true
		c.reads[key] = r
		c.node.ReadIndex([]byte(key))
	}
}

func (c *Core) collect(e *Effects) error {
	c.issueReads()
	if !c.node.HasReady() {
		return nil
	}
	if c.serial == ^uint64(0) {
		c.stopped = true
		return ErrStopped
	}
	c.serial++
	rd := c.node.Ready()
	if !raft.IsEmptySnap(rd.Snapshot) {
		var err error
		rd.Snapshot, err = canonicalSnapshot(rd.Snapshot)
		if err != nil {
			c.stopped = true
			return err
		}
	}
	c.pending = &pending{id: c.serial, ready: rd}
	e.Persist = &PersistBatch{ID: c.serial, HardState: clone(rd.HardState), Entries: copyEntries(rd.Entries), Snapshot: clone(rd.Snapshot)}
	return nil
}

func (c *Core) release() (e Effects, err error) {
	p := c.pending
	rd := p.ready
	// Validate the whole committed batch before changing configuration. Unknown
	// schemas stop application and participation, never become successful no-ops.
	for _, entry := range rd.CommittedEntries {
		switch entry.GetType() {
		case pb.EntryNormal:
			if len(entry.Data) != 0 && !c.validCommand(entry.Data) {
				c.stopped = true
				return e, ErrCommand
			}
		case pb.EntryConfChange, pb.EntryConfChangeV2:
		default:
			c.stopped = true
			return e, ErrCommand
		}
	}
	for _, entry := range rd.CommittedEntries {
		switch entry.GetType() {
		case pb.EntryConfChange:
			cc := &pb.ConfChange{}
			if proto.Unmarshal(entry.Data, cc) != nil {
				c.stopped = true
				return e, ErrCommand
			}
			c.store.conf = clone(c.node.ApplyConfChange(cc))
		case pb.EntryConfChangeV2:
			cc := &pb.ConfChangeV2{}
			if proto.Unmarshal(entry.Data, cc) != nil {
				c.stopped = true
				return e, ErrCommand
			}
			c.store.conf = clone(c.node.ApplyConfChange(cc))
		}
	}
	for _, message := range rd.Messages {
		encoded, err := wire.Marshal(message)
		if err != nil {
			c.stopped = true
			return Effects{}, ErrStopped
		}
		e.Send = append(e.Send, Outbound{To: message.GetTo(), Bytes: encoded})
	}
	// Preserve RawNode's already stable send order, including ordering to the
	// SAME peer. Sorting messages by their bytes could invert dependent appends.
	index := rd.Snapshot.GetMetadata().GetIndex()
	if n := len(rd.CommittedEntries); n != 0 {
		index = rd.CommittedEntries[n-1].GetIndex()
	}
	if index > c.applied {
		p.index = index
		e.Apply = &ApplyBatch{ID: p.id, Entries: copyEntries(rd.CommittedEntries), Snapshot: clone(rd.Snapshot), ConfState: clone(c.store.conf), Index: index}
	} else {
		c.finish(&e)
		err = c.collect(&e)
	}
	return e, err
}

func (c *Core) finish(e *Effects) {
	p := c.pending
	s := c.node.BasicStatus()
	for _, barrier := range p.ready.ReadStates {
		key := string(barrier.RequestCtx)
		if _, ok := c.reads[key]; ok {
			c.barriers[key] = barrier.Index
		}
	}
	// ReadIndex may run ahead of a size-limited application batch. Retain the
	// barrier until its index is durable rather than losing it at Advance.
	keys := make([]string, 0, len(c.barriers))
	for key := range c.barriers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		index := c.barriers[key]
		r := c.reads[key]
		if r.term == s.GetTerm() && c.leader() && c.applied >= index {
			if !r.cancelled {
				e.Reads = append(e.Reads, ReadBarrier{RequestID: r.id, Index: index})
				c.quorumTerm, c.quorumIndex = s.GetTerm(), index
			}
			delete(c.reads, key)
			delete(c.barriers, key)
		}
	}
	c.node.Advance(p.ready)
	c.pending = nil
}

// SnapshotNeeded is a pure maintenance hint, never a client receipt. Hosts
// service it before admitting more growth. Its bounds exclude uncommitted
// entries, whose separate Raft admission budget remains unchanged.
func (c *Core) SnapshotNeeded() *SnapshotPoint {
	if c.stopped || c.pending != nil {
		return nil
	}
	first, e := c.store.FirstIndex()
	if e != nil || first > c.applied {
		return nil
	}
	entries, e := c.store.Entries(first, c.applied+1, ^uint64(0))
	if e != nil {
		return nil
	}
	var size uint64
	for _, entry := range entries {
		size += uint64(proto.Size(entry))
	}
	if len(entries) < SnapshotEntries && size < SnapshotLogBytes {
		return nil
	}
	term, e := c.store.Term(c.applied)
	if e != nil {
		return nil
	}
	return &SnapshotPoint{Index: c.applied, Term: term}
}
