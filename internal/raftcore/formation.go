package raftcore

import (
	"slices"

	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type FormationPlan struct {
	Change     []byte
	Configure  bool
	Checkpoint bool
	Settle     bool
}

// PlanFormation expands only an immutable singleton birth configuration. It
// cannot restore or shrink a formed cluster; the durable ConfState governs
// every retry, including recovery after the joint entry has committed.
func (c *Core) PlanFormation(anchors []uint64) (FormationPlan, error) {
	var p FormationPlan
	if c.stopped {
		return p, ErrStopped
	}
	if !c.leader() {
		return p, ErrLeader
	}
	if c.pending != nil {
		return p, ErrBusy
	}
	if len(anchors) != 3 || !slices.Equal(c.cfg.Anchors, anchors) {
		return p, ErrConfig
	}
	conf := c.store.conf
	voters, learners, outgoing := conf.GetVoters(), conf.GetLearners(), conf.GetVotersOutgoing()
	// A queued configuration must be decided before another can be planned.
	last, err := c.store.LastIndex()
	if err != nil {
		return p, err
	}
	if last > c.applied {
		entries, err := c.store.Entries(c.applied+1, last+1, ^uint64(0))
		if err != nil {
			return p, err
		}
		for _, entry := range entries {
			if entry.GetType() != pb.EntryNormal {
				return p, ErrBusy
			}
		}
	}
	change := &pb.ConfChangeV2{}
	if len(outgoing) > 0 {
		if !sameIDs(voters, anchors) || !sameIDs(outgoing, anchors[:1]) || len(learners) > 0 || len(conf.GetLearnersNext()) > 0 || conf.GetAutoLeave() {
			return p, ErrConfig
		}
		// An empty explicit change leaves joint consensus using both quorums.
	} else if sameIDs(voters, anchors) {
		if len(learners) > 0 || len(conf.GetLearnersNext()) > 0 || conf.GetAutoLeave() {
			return p, ErrConfig
		}
		p.Settle = true
		return p, nil
	} else {
		if !sameIDs(voters, anchors[:1]) || len(conf.GetLearnersNext()) > 0 || conf.GetAutoLeave() {
			return p, ErrConfig
		}
		for _, node := range learners {
			if !slices.Contains(anchors[1:], node) {
				return p, ErrConfig
			}
		}
		for _, node := range anchors[1:] {
			if !slices.Contains(learners, node) {
				change.Changes = []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(node)}}
				p.Configure = true
				p.Checkpoint = true
				p.Change, err = proto.MarshalOptions{Deterministic: true}.Marshal(change)
				return p, err
			}
		}
		snapshot, err := c.store.Snapshot()
		if err != nil {
			return p, err
		}
		for _, node := range anchors[1:] {
			if !slices.Contains(snapshot.GetMetadata().GetConfState().GetLearners(), node) {
				p.Checkpoint = true
				return p, nil
			}
		}
		progress := c.node.Status().Progress
		for _, node := range anchors[1:] {
			peer, ok := progress[node]
			if !ok || peer.Match < c.applied {
				return p, nil
			}
		}
		change.Transition = pb.ConfChangeTransitionJointExplicit.Enum()
		for _, node := range anchors[1:] {
			change.Changes = append(change.Changes, &pb.ConfChangeSingle{Type: pb.ConfChangeAddNode.Enum(), NodeId: proto.Uint64(node)})
		}
	}
	p.Configure = true
	p.Change, err = proto.MarshalOptions{Deterministic: true}.Marshal(change)
	return p, err
}

func sameIDs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for _, node := range a {
		if !slices.Contains(b, node) {
			return false
		}
	}
	return true
}
