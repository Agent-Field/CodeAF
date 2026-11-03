package raftabi

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
	"strconv"
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
	var birth []byte
	if len(d.b) > 0 {
		birth = d.blob()
	}
	if e := d.end(); e != nil {
		return nil, e
	}
	if clusterstate.Decode(raw, &m) != nil || m.Validate() != nil {
		return nil, clusterstate.ErrMigration
	}
	e := encoder{}
	e.message(&pb.Entry{Index: proto.Uint64(1), Term: proto.Uint64(1)})
	e.message(&pb.HardState{Term: proto.Uint64(1), Commit: proto.Uint64(1)})
	voters := m.Config.Anchors
	if len(birth) > 0 {
		if !validSingleton(m, birth) {
			return nil, clusterstate.ErrMigration
		}
		voters = voters[:1]
	}
	e.message(&pb.ConfState{Voters: voters})
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

func validSingleton(m clusterstate.LegacyManifest, raw []byte) bool {
	var birth struct {
		Cluster string   `json:"cluster"`
		Digest  string   `json:"digest"`
		Voters  []string `json:"voters"`
	}
	if clusterstate.Decode(raw, &birth) != nil || m.Validate() != nil || m.Version != 0 || len(m.Config.Anchors) != 3 || birth.Cluster != m.Config.Cluster || birth.Digest != m.Digest || len(birth.Voters) != 1 || birth.Voters[0] != strconv.FormatUint(m.Config.Anchors[0], 10) {
		return false
	}
	rows, err := clusterstate.LegacyAuthorities(m.Config)
	return err == nil && m.Rows == uint64(len(rows)+1)
}
