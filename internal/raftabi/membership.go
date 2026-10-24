package raftabi

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
)

func (r *Runtime) membershipPlan(d *decoder) ([]byte, error) {
	handle, raw := d.u32(), d.blob()
	if err := d.end(); err != nil {
		return nil, err
	}
	node := r.nodes[handle]
	if node == nil {
		return nil, errFrame
	}
	var h clusterstate.Membership
	if clusterstate.Decode(raw, &h) != nil {
		return nil, clusterstate.ErrCodec
	}
	p, err := node.PlanMembership(h)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(p.Change)
	e.flag(p.Checkpoint)
	e.flag(p.Settle)
	return e.b, nil
}
func learnerReady(d *decoder) ([]byte, error) {
	raw := d.blob()
	conf := new(pb.ConfState)
	d.message(conf)
	applied, advertised := d.u64(), d.u64()
	if err := d.end(); err != nil {
		return nil, err
	}
	var m clusterstate.MemberRecord
	if clusterstate.Decode(raw, &m) != nil {
		return nil, clusterstate.ErrCodec
	}
	e := encoder{}
	e.flag(clusterstate.LearnerReady(m, raftcore.Configuration(conf), applied, advertised))
	return e.b, nil
}

func anchorRegistry(d *decoder) ([]byte, error) {
	raw := d.blob()
	if err := d.end(); err != nil {
		return nil, err
	}
	var input struct {
		Anchors []clusterstate.MemberRecord `json:"anchors"`
		Voters  []uint64                    `json:"voters"`
	}
	if err := clusterstate.Decode(raw, &input); err != nil {
		return nil, err
	}
	peers, err := clusterstate.AnchorEnrollments(input.Anchors, input.Voters)
	if err != nil {
		return nil, err
	}
	b, err := clusterstate.Encode(peers)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(b)
	return e.b, nil
}

func configurationAtEntry(d *decoder) ([]byte, error) {
	conf := new(pb.ConfState)
	d.message(conf)
	entry := new(pb.Entry)
	d.message(entry)
	if err := d.end(); err != nil {
		return nil, err
	}
	after, err := raftcore.AdvanceConfiguration(conf, entry)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.message(after)
	return e.b, nil
}
