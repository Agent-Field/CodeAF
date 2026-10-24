// Package raftcore is the single-threaded consensus boundary shared by native
// hosts and the WASI reactor. Hosts own durability, scheduling and transport.
package raftcore

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/clusterstate"

	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
)

const (
	ProtocolVersion       uint16 = clusterstate.ClusterProtocol
	ElectionTicks                = 6
	ElectionWindowTicks          = 2
	ElectionTimingVersion        = 2
	ElectionRandomBound          = 4 * ElectionWindowTicks
	HeartbeatTicks               = 1
	MaxMessageBytes              = 64 << 10
	MaxUncommittedBytes          = 1 << 20
	MaxInflightBatches           = 8
	MaxReadRequests              = 128
	MaxRequestIDBytes            = 128
	SnapshotEntries              = 1024
	SnapshotLogBytes             = 4 << 20
)

var (
	ErrConfig     = errors.New("raftcore: invalid configuration or recovery state")
	ErrProtocol   = errors.New("raftcore: unsupported protocol")
	ErrBusy       = errors.New("raftcore: complete the outstanding batch before retrying input")
	ErrCompletion = errors.New("raftcore: unexpected batch completion")
	ErrStopped    = errors.New("raftcore: member stopped; recover from durable storage")
	ErrPeer       = errors.New("raftcore: invalid peer message")
	ErrLeader     = errors.New("raftcore: operation requires the elected leader")
	ErrCommand    = errors.New("raftcore: invalid command or committed schema")
	ErrRead       = errors.New("raftcore: invalid, duplicate or excess read request")
	ErrSnapshot   = errors.New("raftcore: invalid durable snapshot checkpoint")
	ErrTransfer   = errors.New("raftcore: transfer target is not a caught-up voter")
)

// Config contains only explicit inputs. Seed is noncryptographic election
// entropy; hosts must supply a new seed on restart and record it for replay.
// Bootstrap is used once on empty storage, with the SAME voter set on all
// members. Enrollment and topology authorization belong to the host policy.
type Config struct {
	ClusterUUID string
	NodeID      uint64
	Seed        uint64
	Protocol    uint16
	Bootstrap   []uint64
	// Anchors is the immutable provisioned manifest in committed rank order.
	// Recovery supplies it from applied registry rows, never current voters.
	Anchors []uint64
	// TimingVersion is explicit replay compatibility, never a deployment switch.
	// Zero selects the current version; version 1 reproduces recorded old inputs.
	TimingVersion uint16
}

// RecoveredState is immutable input copied at construction. ConfState is the
// last DURABLY APPLIED configuration, including all joint and learner fields.
// Entries contains the contiguous suffix after Snapshot, not just committed
// entries. Applied belongs to the atomic application transaction.
type RecoveredState struct {
	Anchors   []uint64
	HardState *pb.HardState
	ConfState *pb.ConfState
	Snapshot  *pb.Snapshot
	Entries   []*pb.Entry
	Applied   uint64
}

// Input is closed so an untrusted peer cannot masquerade as a local completion.
type Input interface{ coreInput() }

// CompactSnapshot is supplied ONLY after the host has durably created and
// compacted this exact applied checkpoint. It carries the portable manifest,
// never application chunks. No outstanding Ready may overlap the operation.
type CompactSnapshot struct{ Snapshot *pb.Snapshot }

func (CompactSnapshot) coreInput() {}

type SnapshotPoint struct {
	Index uint64
	Term  uint64
}

type Poll struct{}
type Tick struct{}
type PeerMessage struct{ Bytes []byte }
type Propose struct{ Command []byte }

// Configure is a mechanism for later authorized membership intents, not a
// public enrollment door. The bytes encode a ConfChangeV2.
type Configure struct{ Change []byte }
type Read struct{ RequestID string }

// IdleRead is an internal quiescence barrier; it cannot authorize a client
// operation and preserves only the already elapsed idle interval.
type IdleRead struct{ RequestID string }

func (IdleRead) coreInput() {}

// CancelRead marks a host timeout without producing a successful read. A
// submitted context retains its admission slot until Raft finishes that round;
// otherwise repeated cancellation could create unbounded hidden work.
type CancelRead struct{ RequestID string }

// Transfer starts a request; observing a successful transfer still requires an
// election, a current-term commit and a read barrier on the new leader.
type Transfer struct{ Target uint64 }
type Persisted struct{ BatchID uint64 }
type Applied struct{ BatchID uint64 }

// StorageFailed poisons the member rather than weakening durability on error.
type StorageFailed struct{ BatchID uint64 }
type TransportResult struct {
	Peer     uint64
	Snapshot bool
	Success  bool
}

func (Poll) coreInput()            {}
func (Tick) coreInput()            {}
func (PeerMessage) coreInput()     {}
func (Propose) coreInput()         {}
func (Configure) coreInput()       {}
func (Read) coreInput()            {}
func (CancelRead) coreInput()      {}
func (Transfer) coreInput()        {}
func (Persisted) coreInput()       {}
func (Applied) coreInput()         {}
func (StorageFailed) coreInput()   {}
func (TransportResult) coreInput() {}

// PersistBatch must be written atomically, replacing any conflicting suffix.
// Persisted means the ENTIRE batch is durable, even when Raft's MustSync would
// permit a weaker boundary. An empty batch is a dependency barrier and needs
// no physical write. All exported protobuf values are independent copies.
type PersistBatch struct {
	ID        uint64
	HardState *pb.HardState
	Entries   []*pb.Entry
	Snapshot  *pb.Snapshot
}

// ApplyBatch is emitted only after persistence. The host atomically applies
// every entry and persists the index, rows, dedup results and ConfState. An
// installed snapshot's application state must be installed in that transaction
// before applying the suffix. Empty normal entries still advance the index.
// Client results may escape only after this transaction is durable; Propose
// itself never returns a successful coordination result.
type ApplyBatch struct {
	ID        uint64
	Entries   []*pb.Entry
	Snapshot  *pb.Snapshot
	ConfState *pb.ConfState
	Index     uint64
}

type Outbound struct {
	To    uint64
	Bytes []byte
}
type ReadBarrier struct {
	RequestID string
	Index     uint64
}

// Status is diagnostic, not evidence of authority. It deliberately avoids the
// upstream status map so serialized effects have a stable order.
type Status struct {
	NodeID   uint64
	Leader   uint64
	Term     uint64
	Commit   uint64
	Applied  uint64
	Role     raft.StateType
	RNGState uint64
	RNGDraws uint64
	Stopped  bool
}

type Effects struct {
	Persist        *PersistBatch
	Send           []Outbound
	Apply          *ApplyBatch
	Reads          []ReadBarrier
	Status         Status
	NeedNextTick   bool
	SnapshotNeeded *SnapshotPoint `json:",omitempty"`
}
