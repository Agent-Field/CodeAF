package raftabi

import (
	"crypto/sha256"
	"encoding"

	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// Host codecs keep protobuf validation and portable snapshot interpretation in
// the shared Go binary. They evaluate no application or authorization policy.
func hostCodec(op uint32, d *decoder) ([]byte, error) {
	e := encoder{}
	switch op {
	case 7:
		kind, data := d.u32(), d.blob()
		if err := d.end(); err != nil {
			return nil, err
		}
		switch kind {
		case 1:
			v := new(pb.Entry)
			if proto.Unmarshal(data, v) != nil || len(v.ProtoReflect().GetUnknown()) != 0 {
				return nil, errFrame
			}
			e.u32(uint32(v.GetType()))
			e.u64(v.GetIndex())
			e.u64(v.GetTerm())
			e.blob(v.GetData())
		case 2:
			v := new(pb.HardState)
			if proto.Unmarshal(data, v) != nil || len(v.ProtoReflect().GetUnknown()) != 0 {
				return nil, errFrame
			}
			e.u64(v.GetTerm())
			e.u64(v.GetVote())
			e.u64(v.GetCommit())
		case 3:
			v := new(pb.Snapshot)
			if proto.Unmarshal(data, v) != nil || len(v.ProtoReflect().GetUnknown()) != 0 {
				return nil, errFrame
			}
			m, err := raftsnapshot.Decode(v.GetData())
			if err != nil || m.Index != v.GetMetadata().GetIndex() || m.Term != v.GetMetadata().GetTerm() || len(v.GetMetadata().ProtoReflect().GetUnknown()) != 0 {
				return nil, errFrame
			}
			conf, err := m.Configuration()
			if err != nil || !proto.Equal(conf, v.GetMetadata().GetConfState()) {
				return nil, errFrame
			}
			e.u64(m.Index)
			e.u64(m.Term)
			e.blob(m.Conf)
			e.blob(v.Data)
		case 4:
			v := new(pb.Message)
			if proto.Unmarshal(data, v) != nil || v.Type == nil || v.GetTerm() == 0 || len(v.GetResponses()) != 0 || len(v.ProtoReflect().GetUnknown()) != 0 {
				return nil, errFrame
			}
			switch v.GetType() {
			case pb.MsgApp, pb.MsgAppResp, pb.MsgVote, pb.MsgVoteResp, pb.MsgSnap, pb.MsgHeartbeat, pb.MsgHeartbeatResp, pb.MsgPreVote, pb.MsgPreVoteResp, pb.MsgTimeoutNow:
			default:
				return nil, errFrame
			}
			e.u32(uint32(v.GetType()))
			e.u64(v.GetFrom())
			e.u64(v.GetTo())
			e.message(v.GetSnapshot())
		default:
			return nil, errFrame
		}
	case 8:
		state, block := d.blob(), d.blob()
		if err := d.end(); err != nil {
			return nil, err
		}
		if len(block) > raftsnapshot.RecordBytes+1024 {
			return nil, errFrame
		}
		h := sha256.New()
		if len(state) > 0 {
			if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
				return nil, errFrame
			}
		}
		h.Write(block)
		state, err := h.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return nil, err
		}
		e.blob(state)
		e.blob(h.Sum(nil))
	case 9:
		data := d.blob()
		if err := d.end(); err != nil {
			return nil, err
		}
		m, err := raftsnapshot.Decode(data)
		if err != nil {
			return nil, err
		}
		conf, err := m.Configuration()
		if err != nil {
			return nil, err
		}
		e.message(&pb.Snapshot{Data: data, Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(m.Index), Term: proto.Uint64(m.Term), ConfState: conf}})
	}
	return e.b, nil
}
