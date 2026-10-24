package raftabi

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
)

func applicationCodec(op uint32, d *decoder) ([]byte, error) {
	raw := d.blob()
	c, err := clusterstate.DecodeCommand(raw)
	if err != nil {
		return nil, err
	}
	keys := clusterstate.Dependencies(c)
	e := encoder{}
	if op == 11 {
		if err := d.end(); err != nil {
			return nil, err
		}
		e.u32(uint32(len(keys)))
		for _, k := range keys {
			e.str(k.Namespace)
			e.str(k.Key)
		}
		return e.b, nil
	}
	meta := clusterstate.ApplicationMeta{Cluster: d.str(), Now: int64(d.u64()), Version: d.u64(), Bytes: d.u64(), Ceiling: d.u64()}
	count := d.u32()
	if int(count) != len(keys) {
		return nil, clusterstate.ErrDependency
	}
	rows := map[clusterstate.RecordKey][]byte{}
	size := 0
	for i := uint32(0); i < count; i++ {
		k := clusterstate.RecordKey{Namespace: d.str(), Key: d.str()}
		present := d.u32()
		b := d.blob()
		if _, ok := rows[k]; ok || present > 1 || present == 0 && len(b) > 0 {
			return nil, clusterstate.ErrDependency
		}
		if present == 0 {
			b = nil
		}
		rows[k] = b
		size += len(b) + len(k.Key) + len(k.Namespace)
	}
	if op == 15 {
		meta.Index = d.u64()
		conf := new(pb.ConfState)
		d.message(conf)
		meta.Configuration = raftcore.Configuration(conf)
	}
	if err := d.end(); err != nil {
		return nil, err
	}
	if size > clusterstate.MaxEvaluationBytes {
		return nil, clusterstate.ErrDependency
	}
	v, err := clusterstate.Evaluate(raw, meta, func(k clusterstate.RecordKey) ([]byte, error) {
		b, ok := rows[k]
		if !ok {
			return nil, clusterstate.ErrDependency
		}
		return b, nil
	})
	if err != nil {
		return nil, err
	}
	e.u64(uint64(v.Now))
	e.u64(v.Version)
	result, err := clusterstate.EncodeResult(v.Result)
	if err != nil {
		return nil, err
	}
	e.blob(result)
	e.u32(uint32(len(v.Writes)))
	for _, w := range v.Writes {
		e.str(w.Namespace)
		e.str(w.Key)
		e.blob(w.Data)
	}
	return e.b, nil
}

// registryCodec pages one peer at a time so large retained device metadata
// cannot require materializing an identity or its full registry in the reactor.
func registryCodec(d *decoder) ([]byte, error) {
	node, applied := d.u64(), d.u64()
	conf := new(pb.ConfState)
	d.message(conf)
	header, member, device := d.blob(), d.blob(), d.blob()
	if err := d.end(); err != nil {
		return nil, err
	}
	var h clusterstate.Membership
	var m clusterstate.MemberRecord
	if clusterstate.Decode(header, &h) != nil || clusterstate.Decode(member, &m) != nil || h.Index > applied || m.Node != node {
		return nil, clusterstate.ErrDependency
	}
	found := false
	for _, id := range h.Nodes {
		found = found || id == node
	}
	if !found {
		return nil, clusterstate.ErrDependency
	}
	if h.Validate() != nil {
		return nil, clusterstate.ErrDependency
	}
	peer, err := clusterstate.PeerFromRecord(m, device, raftcore.Configuration(conf))
	if err != nil {
		return nil, err
	}
	b, err := clusterstate.Encode(peer)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(b)
	return e.b, nil
}
