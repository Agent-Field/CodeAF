package raftabi

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

func (r *Runtime) leadership(d *decoder, receipt bool) ([]byte, error) {
	handle, raw := d.u32(), d.blob()
	node := r.nodes[handle]
	if node == nil {
		return nil, errFrame
	}
	var h clusterstate.Membership
	if err := clusterstate.Decode(raw, &h); err != nil {
		return nil, err
	}
	var value any
	if receipt {
		if err := d.end(); err != nil {
			return nil, err
		}
		v, err := node.LeadershipReceipt(h)
		if err != nil {
			return nil, err
		}
		value = v
	} else {
		var evidence raftcore.LeadershipEvidence
		if err := clusterstate.Decode(d.blob(), &evidence); err != nil {
			return nil, err
		}
		if err := d.end(); err != nil {
			return nil, err
		}
		v, err := node.PlanLeadership(h, evidence)
		if err != nil {
			return nil, err
		}
		value = v
	}
	b, err := clusterstate.Encode(value)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(b)
	return e.b, nil
}
