// Package raftabi defines the bounded binary boundary shared by the Worker
// reactor and its native conformance peer. It owns no storage or transport.
package raftabi

import (
	"encoding/binary"
	"errors"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

const (
	Version    uint32 = 1
	MaxBuffer         = 2 << 20
	MaxHandles        = 96
)

var errFrame = errors.New("raftabi: invalid or oversized frame")
var wire = proto.MarshalOptions{Deterministic: true}

type decoder struct {
	b   []byte
	err error
}

func (d *decoder) take(n uint32) []byte {
	if d.err != nil || uint64(n) > uint64(len(d.b)) {
		d.err = errFrame
		return nil
	}
	b := d.b[:n]
	d.b = d.b[n:]
	return b
}
func (d *decoder) u32() uint32 {
	b := d.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}
func (d *decoder) u64() uint64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}
func (d *decoder) blob() []byte { return d.take(d.u32()) }
func (d *decoder) str() string  { return string(d.blob()) }
func (d *decoder) message(m proto.Message) bool {
	b := d.blob()
	if len(b) == 0 {
		return false
	}
	if proto.Unmarshal(b, m) != nil {
		d.err = errFrame
	}
	return true
}
func (d *decoder) entries() []*pb.Entry {
	n := d.u32()
	if n > MaxBuffer/4 {
		d.err = errFrame
		return nil
	}
	out := make([]*pb.Entry, 0)
	for i := uint32(0); i < n && d.err == nil; i++ {
		e := new(pb.Entry)
		if !d.message(e) {
			d.err = errFrame
		}
		out = append(out, e)
	}
	return out
}
func (d *decoder) end() error {
	if d.err != nil || len(d.b) != 0 {
		return errFrame
	}
	return nil
}

type encoder struct{ b []byte }

func (e *encoder) u32(v uint32)  { e.b = binary.LittleEndian.AppendUint32(e.b, v) }
func (e *encoder) u64(v uint64)  { e.b = binary.LittleEndian.AppendUint64(e.b, v) }
func (e *encoder) blob(b []byte) { e.u32(uint32(len(b))); e.b = append(e.b, b...) }
func (e *encoder) str(s string)  { e.blob([]byte(s)) }
func (e *encoder) message(m proto.Message) {
	if !m.ProtoReflect().IsValid() {
		e.blob(nil)
		return
	}
	b, err := wire.Marshal(m)
	if err != nil {
		panic(errFrame)
	}
	e.blob(b)
}
func (e *encoder) entries(v []*pb.Entry) {
	e.u32(uint32(len(v)))
	for _, m := range v {
		e.message(m)
	}
}
func (e *encoder) flag(v bool) {
	if v {
		e.u32(1)
	} else {
		e.u32(0)
	}
}

// Runtime shares one Go runtime across opaque handles. IDs are never reused,
// so a delayed completion cannot accidentally address a replacement member.
// Hosts still serialize each member across asynchronous durability operations.
type Runtime struct {
	nodes  map[uint32]*raftcore.Core
	serial uint32
}

func New() *Runtime { return &Runtime{nodes: make(map[uint32]*raftcore.Core)} }

// Call consumes one complete frame and returns owned bytes. The header is a
// version and operation, followed by length-prefixed binary/protobuf fields.
// A malformed frame is fully decoded before a core can change state.
func (r *Runtime) Call(b []byte) (out []byte) {
	defer func() {
		if recover() != nil {
			e := encoder{}
			e.u32(Version)
			e.str("raftabi: stopped after invalid call")
			out = e.b
		}
	}()
	e := encoder{}
	e.u32(Version)
	if len(b) > MaxBuffer {
		e.str(errFrame.Error())
		return e.b
	}
	d := decoder{b: b}
	if d.u32() != Version {
		e.str("raftabi: unsupported version")
		return e.b
	}
	payload, err := r.call(d.u32(), &d)
	if err != nil {
		e.str(err.Error())
		return e.b
	}
	e.str("")
	e.b = append(e.b, payload...)
	if len(e.b) > MaxBuffer {
		e = encoder{}
		e.u32(Version)
		e.str("raftabi: effect exceeds buffer; stop and recover")
	}
	return e.b
}
func (r *Runtime) call(op uint32, d *decoder) ([]byte, error) {
	e := encoder{}
	switch op {
	case 39:
		return hostedForward(d)
	case 36:
		return hostedPersonal(d)
	case 37:
		return hostedPeer(d)
	case 38:
		return hostedPeerWire(d)
	case 35:
		return configurationView(d)
	case 34:
		return hostedAnchor(d)
	case 33:
		return clusterIngress(d)
	case 30:
		return hostedEffect(d)
	case 31:
		return hostedProjection(d)
	case 32:
		return hostedAuthority(d)
	case 29:
		return v1Command(d)
	case 28:
		return assignmentRead(d)
	case 27:
		return workerRead(d)
	case 23:
		return r.schedule(d)
	case 24:
		return activeRecord(d)
	case 25:
		return coordinationPolicy(d)
	case 26:
		return renewalCovered(d)
	case 21, 22:
		return r.leadership(d, op == 22)
	case 20:
		return configurationAtEntry(d)
	case 19:
		return anchorRegistry(d)
	case 17:
		return r.membershipPlan(d)
	case 18:
		return learnerReady(d)
	case 16:
		return registryCodec(d)
	case 14:
		return legacyAuthorities(d)
	case 13:
		return legacyGenesis(d)
	case 12:
		return legacyCodec(d)
	case 10, 11, 15:
		return applicationCodec(op, d)
	case 1:
		cfg := raftcore.Config{ClusterUUID: d.str(), NodeID: d.u64(), Seed: d.u64()}
		protocol := d.u32()
		if protocol != uint32(raftcore.ProtocolVersion) {
			return nil, raftcore.ErrProtocol
		}
		cfg.Protocol = uint16(protocol)
		n := d.u32()
		if n > MaxHandles {
			return nil, errFrame
		}
		for i := uint32(0); i < n; i++ {
			cfg.Bootstrap = append(cfg.Bootstrap, d.u64())
		}
		h, c, s := new(pb.HardState), new(pb.ConfState), new(pb.Snapshot)
		recovered := raftcore.RecoveredState{}
		if d.message(h) {
			recovered.HardState = h
		}
		if d.message(c) {
			recovered.ConfState = c
		}
		if d.message(s) {
			recovered.Snapshot = s
		}
		recovered.Entries = d.entries()
		recovered.Applied = d.u64()
		if len(d.b) != 0 {
			n := d.u32()
			if n != 3 {
				return nil, errFrame
			}
			for range n {
				recovered.Anchors = append(recovered.Anchors, d.u64())
			}
		}
		if err := d.end(); err != nil {
			return nil, err
		}
		if len(r.nodes) >= MaxHandles || r.serial == ^uint32(0) {
			return nil, errors.New("raftabi: handle capacity")
		}
		node, err := raftcore.New(cfg, recovered)
		if err != nil {
			return nil, err
		}
		r.serial++
		r.nodes[r.serial] = node
		e.u32(r.serial)
	case 2:
		id := d.u32()
		if err := d.end(); err != nil {
			return nil, err
		}
		if r.nodes[id] == nil {
			return nil, errFrame
		}
		delete(r.nodes, id)
	case 3:
		id, tag := d.u32(), d.u32()
		var in raftcore.Input
		switch tag {
		case 0:
			in = raftcore.Poll{}
		case 1:
			in = raftcore.Tick{}
		case 2:
			in = raftcore.PeerMessage{Bytes: d.blob()}
		case 3:
			in = raftcore.Propose{Command: d.blob()}
		case 4:
			in = raftcore.Read{RequestID: d.str()}
		case 5:
			in = raftcore.Persisted{BatchID: d.u64()}
		case 6:
			in = raftcore.Applied{BatchID: d.u64()}
		case 7:
			in = raftcore.Transfer{Target: d.u64()}
		case 8:
			in = raftcore.Configure{Change: d.blob()}
		case 9:
			in = raftcore.StorageFailed{BatchID: d.u64()}
		case 10:
			in = raftcore.CancelRead{RequestID: d.str()}
		case 14:
			return legacyAuthorities(d)
		case 13:
			return legacyGenesis(d)
		case 16:
			in = raftcore.Campaign{}
		case 15:
			in = raftcore.IdleRead{RequestID: d.str()}
		case 12:
			in = raftcore.TransportResult{Peer: d.u64(), Snapshot: d.u32() != 0, Success: d.u32() != 0}
		case 11:
			p := &pb.Snapshot{}
			d.message(p)
			in = raftcore.CompactSnapshot{Snapshot: p}
		default:
			return nil, errFrame
		}
		if err := d.end(); err != nil {
			return nil, err
		}
		node := r.nodes[id]
		if node == nil {
			return nil, errFrame
		}
		effects, err := node.Step(in)
		if err != nil {
			return nil, err
		}
		encodeEffects(&e, effects)
	case 4:
		command, row := d.blob(), d.blob()
		if err := d.end(); err != nil {
			return nil, err
		}
		next, err := ProbeApply(command, row)
		if err != nil {
			return nil, err
		}
		e.blob(next)
	case 5:
		row := d.blob()
		if err := d.end(); err != nil {
			return nil, err
		}
		next, err := RoundTripRow(row)
		if err != nil {
			return nil, err
		}
		e.blob(next)
	case 6:
		id := d.u32()
		if err := d.end(); err != nil {
			return nil, err
		}
		node := r.nodes[id]
		if node == nil {
			return nil, errFrame
		}
		point := node.SnapshotNeeded()
		e.flag(point != nil)
		if point != nil {
			e.u64(point.Index)
			e.u64(point.Term)
		}
	case 7, 8, 9:
		return hostCodec(op, d)
	default:
		return nil, errFrame
	}
	return e.b, nil
}
func encodeEffects(e *encoder, v raftcore.Effects) {
	e.flag(v.Persist != nil)
	if p := v.Persist; p != nil {
		e.u64(p.ID)
		e.message(p.HardState)
		e.entries(p.Entries)
		e.message(p.Snapshot)
	}
	e.u32(uint32(len(v.Send)))
	for _, m := range v.Send {
		e.u64(m.To)
		e.blob(m.Bytes)
	}
	e.flag(v.Apply != nil)
	if a := v.Apply; a != nil {
		e.u64(a.ID)
		e.u64(a.Index)
		e.entries(a.Entries)
		e.message(a.Snapshot)
		e.message(a.ConfState)
	}
	e.u32(uint32(len(v.Reads)))
	for _, b := range v.Reads {
		e.str(b.RequestID)
		e.u64(b.Index)
	}
	s := v.Status
	e.u64(s.NodeID)
	e.u64(s.Leader)
	e.u64(s.Term)
	e.u64(s.Commit)
	e.u64(s.Applied)
	e.u32(uint32(s.Role))
	e.u64(s.RNGState)
	e.u64(s.RNGDraws)
	e.flag(s.Stopped)
	e.flag(v.NeedNextTick)
}
