package raftabi

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// The hosted source feeds original SQL JSON to this bounded conversion call.
// It never reimplements directory policy or coerces a v1 fence through Number.
func legacyCodec(d *decoder) ([]byte, error) {
	var c clusterstate.LegacyConfig
	config := d.blob()
	kind, id := d.str(), d.str()
	raw := d.blob()
	until := int64(d.u64())
	if e := d.end(); e != nil {
		return nil, e
	}
	if clusterstate.Decode(config, &c) != nil {
		return nil, clusterstate.ErrMigration
	}
	w, e := clusterstate.LegacyRow(c, kind, id, raw, until)
	if e != nil {
		return nil, e
	}
	b, e := clusterstate.Encode(w)
	if e != nil {
		return nil, e
	}
	out := encoder{}
	out.blob(b)
	return out.b, nil
}

func legacyGenesis(d *decoder) ([]byte, error) {
	var m clusterstate.LegacyManifest
	raw := d.blob()
	if e := d.end(); e != nil {
		return nil, e
	}
	if clusterstate.Decode(raw, &m) != nil || m.Validate() != nil {
		return nil, clusterstate.ErrMigration
	}
	e := encoder{}
	e.message(&pb.Entry{Index: proto.Uint64(1), Term: proto.Uint64(1)})
	e.message(&pb.HardState{Term: proto.Uint64(1), Commit: proto.Uint64(1)})
	e.message(&pb.ConfState{Voters: m.Config.Anchors})
	return e.b, nil
}

func legacyAuthorities(d *decoder) ([]byte, error) {
	var c clusterstate.LegacyConfig
	raw := d.blob()
	if e := d.end(); e != nil {
		return nil, e
	}
	if clusterstate.Decode(raw, &c) != nil {
		return nil, clusterstate.ErrMigration
	}
	rows, e := clusterstate.LegacyAuthorities(c)
	if e != nil {
		return nil, e
	}
	out := encoder{}
	out.u32(uint32(len(rows)))
	for _, row := range rows {
		b, e := clusterstate.Encode(row)
		if e != nil {
			return nil, e
		}
		out.blob(b)
	}
	return out.b, nil
}
