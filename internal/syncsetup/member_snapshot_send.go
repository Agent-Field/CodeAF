package syncsetup

import (
	"context"
	"slices"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type memberSnapshot struct {
	manifest   raftsnapshot.Manifest
	generation string
	offset     uint64
	started    int64
}

func (m *personalMember) rememberSnapshot(to uint64, raw []byte) error {
	message := new(pb.Message)
	if proto.Unmarshal(raw, message) != nil {
		return raftcore.ErrPeer
	}
	if message.GetType() != pb.MsgSnap {
		return nil
	}
	manifest, err := raftsnapshot.Decode(message.GetSnapshot().GetData())
	if err != nil {
		return err
	}
	if m.outSnapshots == nil {
		m.outSnapshots = map[uint64]*memberSnapshot{}
	}
	if m.chunkWaiting == nil {
		m.chunkWaiting = map[uint64]uint64{}
	}
	delete(m.chunkWaiting, to)
	m.outSnapshots[to] = &memberSnapshot{manifest: manifest, generation: raftsnapshot.ID(message.GetSnapshot().GetData()), started: m.owner.now().UnixMilli()}
	return nil
}

// nextSnapshot queues at most ONE bounded chunk after normal messages. The
// next signed exchange response drives another chunk, without an idle poll.
func (m *personalMember) nextSnapshot(ctx context.Context) error {
	if len(m.pending) > 0 {
		return nil
	}
	ids := make([]uint64, 0, len(m.outSnapshots))
	for id := range m.outSnapshots {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		transfer := m.outSnapshots[id]
		if m.owner.now().UnixMilli()-transfer.started >= rafttransport.RetentionMillis {
			delete(m.outSnapshots, id)
			delete(m.chunkWaiting, id)
			if err := m.step(ctx, raftcore.TransportResult{Peer: id, Snapshot: true}); err != nil {
				return err
			}
			continue
		}
		if transfer.offset >= transfer.manifest.Chunks || m.chunkWaiting[id] != 0 {
			continue
		}
		chunk, err := m.store.ReadSnapshotChunk(ctx, transfer.generation, transfer.offset)
		if err != nil {
			delete(m.outSnapshots, id)
			delete(m.chunkWaiting, id)
			return m.step(ctx, raftcore.TransportResult{Peer: id, Snapshot: true})
		}
		destination := uint64(0)
		for _, peer := range m.record.Grant.Members {
			if peer.Node == id && !peer.Revoked {
				destination = peer.Incarnation
			}
		}
		if destination == 0 {
			delete(m.outSnapshots, id)
			delete(m.chunkWaiting, id)
			continue
		}
		if m.sequence == m.record.SequenceCeiling {
			return rafttransport.ErrGeneration
		}
		m.sequence++
		e := rafttransport.Envelope{Scope: m.record.Grant.Scope, From: m.record.Grant.Node, To: id, Incarnation: m.record.Grant.Incarnation, Destination: destination, Sequence: m.sequence, Payload: chunk, Chunk: &rafttransport.Chunk{Generation: transfer.generation, Offset: transfer.offset, Hash: raftsnapshot.ID(chunk)}}
		if err = e.SignWith(m.owner.Device.Sign); err != nil {
			return err
		}
		m.pending = append(m.pending, e)
		m.chunkWaiting[id] = e.Sequence
		transfer.offset++
		return nil
	}
	return nil
}

func (m *personalMember) hasSnapshotWork() bool {
	for id, transfer := range m.outSnapshots {
		if transfer.offset < transfer.manifest.Chunks && m.chunkWaiting[id] == 0 {
			return true
		}
	}
	return false
}
