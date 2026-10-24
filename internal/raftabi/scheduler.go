package raftabi

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

func (r *Runtime) schedule(d *decoder) ([]byte, error) {
	id, raw := d.u32(), d.blob()
	var in raftcore.ScheduleInput
	if err := clusterstate.Decode(raw, &in); err != nil {
		return nil, err
	}
	if err := d.end(); err != nil {
		return nil, err
	}
	node := r.nodes[id]
	if node == nil {
		return nil, errFrame
	}
	p, err := node.Schedule(in)
	if err != nil {
		return nil, err
	}
	b, err := clusterstate.Encode(p)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(b)
	return e.b, nil
}
func activeRecord(d *decoder) ([]byte, error) {
	key := clusterstate.RecordKey{Namespace: d.str(), Key: d.str()}
	raw, now := d.blob(), d.u64()
	if err := d.end(); err != nil || now > clusterstate.MaxSafeInteger {
		return nil, errFrame
	}
	active, err := clusterstate.RecordActive(key, raw, int64(now))
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.flag(active)
	return e.b, nil
}

func coordinationPolicy(d *decoder) ([]byte, error) {
	if err := d.end(); err != nil {
		return nil, err
	}
	b, err := clusterstate.Encode(struct {
		TickEvery       int64 `json:"tick_every"`
		RenewEvery      int64 `json:"renew_every"`
		PresenceEvery   int64 `json:"presence_every"`
		EvidenceWindow  int64 `json:"evidence_window"`
		IdleAfter       int64 `json:"idle_after"`
		MaxRenewHolds   int   `json:"max_renew_holds"`
		MaxMessageBytes int   `json:"max_message_bytes"`
	}{clusterstate.TickEveryMillis, clusterstate.RenewEveryMillis, clusterstate.PresenceEveryMillis, clusterstate.EvidenceWindowMillis, clusterstate.IdleAfterMillis, clusterstate.MaxRenewHolds, clusterstate.MaxCommandBytes})
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(b)
	return e.b, nil
}

func renewalCovered(d *decoder) ([]byte, error) {
	raw, device, cell, fence, seen := d.blob(), d.str(), d.str(), d.u64(), d.u64()
	if err := d.end(); err != nil || seen > clusterstate.MaxSafeInteger {
		return nil, errFrame
	}
	if len(raw) == 0 {
		raw = nil
	}
	covered, err := clusterstate.RenewalCovered(raw, device, clusterstate.HoldFence{Cell: cell, Fence: fence}, int64(seen))
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.flag(covered)
	return e.b, nil
}
