package raftcore

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"go.etcd.io/raft/v3/confchange"
	pb "go.etcd.io/raft/v3/raftpb"
	"go.etcd.io/raft/v3/tracker"
	"google.golang.org/protobuf/proto"
)

// Configuration copies complete configuration state across the pure policy
// boundary without introducing a protobuf dependency into clusterstate.
func Configuration(c *pb.ConfState) clusterstate.Configuration {
	return clusterstate.Configuration{Voters: append([]uint64(nil), c.GetVoters()...), Learners: append([]uint64(nil), c.GetLearners()...), Outgoing: append([]uint64(nil), c.GetVotersOutgoing()...), LearnersNext: append([]uint64(nil), c.GetLearnersNext()...), AutoLeave: c.GetAutoLeave()}
}

type MembershipPlan struct {
	Change     []byte
	Checkpoint bool
	Settle     bool
}

// PlanMembership reconciles a DURABLY COMMITTED intent after any restart or
// leader change. Registry admission and Raft membership are separate stages;
// neither an uncommitted configuration nor a lagging learner pins compaction.
func (c *Core) PlanMembership(h clusterstate.Membership) (MembershipPlan, error) {
	var p MembershipPlan
	if c.stopped {
		return p, ErrStopped
	}
	if !c.leader() {
		return p, ErrLeader
	}
	if c.pending != nil {
		return p, ErrBusy
	}
	if h.Index > c.applied || h.Revision == 0 {
		return p, ErrConfig
	}
	intent := h.Pending
	if intent == nil {
		return p, nil
	}
	if intent.Revision != h.Revision || intent.Node == 0 {
		return p, ErrConfig
	}
	conf := c.store.conf
	if len(conf.GetVotersOutgoing()) != 0 || len(conf.GetLearnersNext()) != 0 || conf.GetAutoLeave() {
		return p, ErrBusy
	}
	for _, id := range conf.GetVoters() {
		if id == intent.Node {
			return p, ErrConfig
		}
	}
	// A previously proposed configuration may be durable but uncommitted. Wait
	// for Raft to decide it instead of queuing another membership mutation.
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
	present := false
	for _, id := range conf.GetLearners() {
		present = present || id == intent.Node
	}
	if present == !intent.Remove {
		p.Settle = true
		if present {
			snap, err := c.store.Snapshot()
			if err != nil {
				return p, err
			}
			included := false
			for _, id := range snap.GetMetadata().GetConfState().GetLearners() {
				included = included || id == intent.Node
			}
			// etcd refuses a snapshot that excludes its recipient. A newly admitted
			// learner catching up across compaction therefore needs a fresh checkpoint.
			p.Checkpoint = !included
		}
		return p, nil
	}
	kind := pb.ConfChangeAddLearnerNode
	if intent.Remove {
		kind = pb.ConfChangeRemoveNode
	}
	context, err := clusterstate.Encode(*intent)
	if err != nil {
		return p, err
	}
	cc := &pb.ConfChangeV2{Changes: []*pb.ConfChangeSingle{{Type: kind.Enum(), NodeId: proto.Uint64(intent.Node)}}, Context: context}
	p.Change, err = wire.Marshal(cc)
	return p, err
}

// AdvanceConfiguration reconstructs the configuration at each committed log
// position for paged application evaluation. Followers may receive a config
// entry and its settlement command in ONE Ready; using either the batch's old
// or final ConfState for every entry would make policy depend on batching.
// This invokes the same pinned library changer used by RawNode, not host rules.
func AdvanceConfiguration(before *pb.ConfState, entry *pb.Entry) (after *pb.ConfState, err error) {
	defer func() {
		if recover() != nil {
			after = nil
			err = ErrConfig
		}
	}()
	cc := new(pb.ConfChangeV2)
	switch entry.GetType() {
	case pb.EntryNormal:
		return clone(before), nil
	case pb.EntryConfChange:
		v := new(pb.ConfChange)
		if proto.Unmarshal(entry.Data, v) != nil {
			return nil, ErrCommand
		}
		cc = v.AsV2()
	case pb.EntryConfChangeV2:
		if proto.Unmarshal(entry.Data, cc) != nil {
			return nil, ErrCommand
		}
	default:
		return nil, ErrCommand
	}
	tracker := tracker.MakeProgressTracker(MaxInflightBatches, MaxMessageBytes)
	changer := confchange.Changer{Tracker: tracker, LastIndex: entry.GetIndex()}
	cfg, progress, err := confchange.Restore(changer, before)
	if err != nil {
		return nil, err
	}
	changer.Tracker.Config = cfg
	changer.Tracker.Progress = progress
	if cc.LeaveJoint() {
		cfg, progress, err = changer.LeaveJoint()
	} else if auto, joint := cc.EnterJoint(); joint {
		cfg, progress, err = changer.EnterJoint(auto, cc.Changes...)
	} else {
		cfg, progress, err = changer.Simple(cc.Changes...)
	}
	if err != nil {
		return nil, err
	}
	changer.Tracker.Config = cfg
	changer.Tracker.Progress = progress
	return changer.Tracker.ConfState(), nil
}
