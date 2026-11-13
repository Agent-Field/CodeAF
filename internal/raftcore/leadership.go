package raftcore

import (
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// LeadershipEvidence is supplied by the authenticated host from the target's
// durable store at the advertised barrier. It is not accepted from a public
// request. RecentActive in the actual Raft progress adds the reverse direction;
// queued transport, an uploaded snapshot or a socket alone cannot qualify.
type LeadershipEvidence struct {
	Node     uint64 `json:"node"`
	Revision uint64 `json:"revision"`
	Applied  uint64 `json:"applied"`
	Protocol uint16 `json:"protocol"`
	Durable  bool   `json:"durable"`
}
type LeadershipPlan struct {
	Configure bool   `json:"configure"`
	Change    []byte `json:"change"`
	Settle    bool   `json:"settle"`
	Transfer  uint64 `json:"transfer"`
	Fallback  bool   `json:"fallback"`
}

func (c *Core) configurationPending() (bool, error) {
	last, err := c.store.LastIndex()
	if err != nil || last <= c.applied {
		return false, err
	}
	entries, err := c.store.Entries(c.applied+1, last+1, ^uint64(0))
	for _, entry := range entries {
		if entry.GetType() != pb.EntryNormal {
			return true, err
		}
	}
	return false, err
}

// PlanLeadership resumes ONLY a durably committed policy intent. Leaving a
// joint state is always its own durable entry; reversing an interrupted swap
// first finishes that joint state, never edits its voter list in place.
func (c *Core) PlanLeadership(h clusterstate.Membership, evidence LeadershipEvidence) (LeadershipPlan, error) {
	var p LeadershipPlan
	if c.stopped {
		return p, ErrStopped
	}
	if !c.leader() {
		return p, ErrLeader
	}
	if c.pending != nil {
		return p, ErrBusy
	}
	if h.Validate() != nil || h.Index > c.applied || h.Pending != nil {
		return p, ErrConfig
	}
	pending, err := c.configurationPending()
	if err != nil {
		return p, err
	}
	if pending {
		return p, ErrBusy
	}
	l := h.Leadership
	if l == nil {
		return p, nil
	}
	conf := Configuration(c.store.conf)
	status := c.node.Status()
	// Fallback housekeeping is restricted to a provisioned anchor that has
	// actually won a new term. Absent quorum cannot elect or commit this work.
	if !l.Restore && status.Lead != l.Node && contains(h.Anchors, status.Lead) {
		progress, ok := status.Progress[l.Node]
		term, e := c.store.Term(status.GetCommit())
		p.Fallback = e == nil && term == status.GetTerm() && (l.Completed || status.GetTerm() > l.Term && (!ok || !progress.RecentActive))
	}
	if p.Fallback {
		return p, nil
	}
	if conf.Joint() {
		// Both possible joint configurations must be the exact policy swap.
		forward := h
		copyIntent := *l
		copyIntent.Restore = false
		forward.Leadership = &copyIntent
		incoming := conf.Voters
		outgoing := conf.Outgoing
		if !(equalIDs(incoming, forward.LeadershipVoters()) && equalIDs(outgoing, h.Anchors) || equalIDs(incoming, h.Anchors) && equalIDs(outgoing, forward.LeadershipVoters())) {
			return p, ErrConfig
		}
		p.Configure = true
		p.Change, err = wire.Marshal(&pb.ConfChangeV2{})
		return p, err
	}
	if h.StableTopology(conf) {
		if !l.Settled {
			p.Settle = true
			return p, nil
		}
		if !l.Restore && !l.Completed && c.cfg.NodeID != l.Node {
			if !c.qualified(l.Node, h.Revision, h.Index, evidence) {
				return p, ErrTransfer
			}
			p.Transfer = l.Node
		}
		return p, nil
	}
	forward := h
	intent := *l
	intent.Restore = false
	forward.Leadership = &intent
	old := h.Anchors
	promote, demote := l.Node, l.Standby
	if l.Restore {
		old = forward.LeadershipVoters()
		promote, demote = demote, promote
	}
	if !equalIDs(conf.Voters, old) || !contains(conf.Learners, promote) {
		return p, ErrConfig
	}
	if !c.qualified(promote, h.Revision, l.Barrier, evidence) {
		return p, ErrTransfer
	}
	cc := &pb.ConfChangeV2{Transition: pb.ConfChangeTransitionJointExplicit.Enum(), Changes: []*pb.ConfChangeSingle{
		{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(demote)},
		{Type: pb.ConfChangeAddNode.Enum(), NodeId: proto.Uint64(promote)},
	}}
	p.Configure = true
	p.Change, err = wire.Marshal(cc)
	return p, err
}
func (c *Core) qualified(node, revision, barrier uint64, e LeadershipEvidence) bool {
	s := c.node.Status()
	progress, ok := s.Progress[node]
	return ok && progress.RecentActive && progress.Match >= s.GetCommit() && progress.Match >= barrier && e.Node == node && e.Revision == revision && e.Applied >= barrier && e.Durable && e.Protocol == ProtocolVersion
}
func contains(ids []uint64, id uint64) bool {
	for _, n := range ids {
		if n == id {
			return true
		}
	}
	return false
}
func equalIDs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !contains(b, id) {
			return false
		}
	}
	return true
}

// LeadershipReceipt refuses cached leader hints and barriers from an earlier
// term. quorumIndex is recorded only when this core emits a completed read.
func (c *Core) LeadershipReceipt(h clusterstate.Membership) (clusterstate.LeadershipReceipt, error) {
	var r clusterstate.LeadershipReceipt
	if c.stopped {
		return r, ErrStopped
	}
	if !c.leader() {
		return r, ErrLeader
	}
	l := h.Leadership
	s := c.node.BasicStatus()
	if h.Validate() != nil || l == nil || l.Restore || !l.Settled || l.Node != c.cfg.NodeID || h.Index > c.applied || !h.StableTopology(Configuration(c.store.conf)) || c.quorumTerm != s.GetTerm() || c.quorumIndex < h.Index || c.applied < c.quorumIndex {
		return r, ErrTransfer
	}
	term, err := c.store.Term(c.quorumIndex)
	if err != nil || term != s.GetTerm() {
		return r, ErrTransfer
	}
	r = clusterstate.LeadershipReceipt{Node: c.cfg.NodeID, Term: s.GetTerm(), Revision: h.Revision, Barrier: c.quorumIndex, Voters: append([]uint64(nil), c.store.conf.GetVoters()...)}
	c.quorumTerm, c.quorumIndex = 0, 0
	return r, nil
}
