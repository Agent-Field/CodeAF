package raftdirectory

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type incomingSnapshot struct {
	message *pb.Message
	chunks  map[uint64]bool
}

// receivePeer stages the actual sender's portable chunks before the native
// actor may Step its pointer. A delivery acknowledgment is never installation.
func (g *Native) receivePeer(ctx context.Context, i int, e rafttransport.Envelope) error {
	if e.AppendChunk() {
		e.Chunk = nil
	}
	if e.Chunk != nil {
		transfer := g.peers.incoming[i]
		if transfer == nil || transfer.message.GetFrom() != e.From || raftsnapshot.ID(transfer.message.GetSnapshot().GetData()) != e.Chunk.Generation {
			return nil
		}
		if err := g.stores[i].PutSnapshotChunk(ctx, e.Chunk.Generation, e.Chunk.Offset, e.Payload, e.Chunk.Hash); err != nil {
			return err
		}
		transfer.chunks[e.Chunk.Offset] = true
		manifest, err := raftsnapshot.Decode(transfer.message.GetSnapshot().GetData())
		if err != nil {
			return err
		}
		if uint64(len(transfer.chunks)) != manifest.Chunks {
			return nil
		}
		pointer, err := g.stores[i].CompleteSnapshot(ctx, e.Chunk.Generation)
		if err != nil {
			return err
		}
		if !proto.Equal(pointer, transfer.message.GetSnapshot()) {
			return raftstore.ErrSnapshot
		}
		raw, err := proto.Marshal(transfer.message)
		if err != nil {
			return err
		}
		delete(g.peers.incoming, i)
		return g.drive(ctx, i, raftcore.PeerMessage{Bytes: raw})
	}
	message := new(pb.Message)
	if proto.Unmarshal(e.Payload, message) != nil {
		return raftcore.ErrPeer
	}
	if message.GetType() == pb.MsgSnap && message.GetSnapshot().GetMetadata().GetIndex() > g.status[i].Applied {
		transfer := g.peers.incoming[i]
		if transfer == nil || !proto.Equal(transfer.message.GetSnapshot(), message.GetSnapshot()) {
			if _, err := g.stores[i].BeginSnapshot(ctx, message.GetSnapshot().GetData()); err != nil {
				return err
			}
			g.peers.incoming[i] = &incomingSnapshot{message: message, chunks: map[uint64]bool{}}
		}
		return nil
	}
	return g.drive(ctx, i, raftcore.PeerMessage{Bytes: e.Payload})
}
