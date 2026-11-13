package raftcore

import (
	"errors"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"testing"
)

func leadershipGroup(t *testing.T) (*testGroup, cs.Membership) {
	g := newGroup(t)
	g.elect()
	n, e := New(Config{ClusterUUID: "test-cluster", NodeID: 4, Seed: 104, Protocol: 1}, RecoveredState{})
	if e != nil {
		t.Fatal(e)
	}
	g.nodes = append(g.nodes, n)
	g.applied = append(g.applied, nil)
	g.durable = append(g.durable, RecoveredState{})
	leader := g.leader()
	h := cs.Membership{Revision: 2, Index: g.nodes[leader-1].applied, NextNode: 5, Anchors: []uint64{1, 2, 3}, Nodes: []uint64{1, 2, 3, 4}, Live: 1, Limit: 4096, Pending: &cs.MembershipIntent{Node: 4, Revision: 2}}
	p, e := g.nodes[leader-1].PlanMembership(h)
	if e != nil {
		t.Fatal(e)
	}
	g.call(leader, Configure{p.Change})
	g.pump()
	h.Pending = nil
	h.Revision++
	h.Index = g.nodes[g.leader()-1].applied
	h.Leadership = &cs.LeadershipIntent{Node: 4, Standby: 3, Revision: h.Revision, Barrier: h.Index, Term: g.nodes[g.leader()-1].status().Term}
	return g, h
}
func leadEvidence(g *testGroup, h cs.Membership, node uint64) LeadershipEvidence {
	return LeadershipEvidence{Node: node, Revision: h.Revision, Applied: g.nodes[node-1].applied, Protocol: 1, Durable: true}
}
func reconcileLeadCore(t *testing.T, g *testGroup, h cs.Membership) {
	t.Helper()
	for i := 0; i < 8; i++ {
		leader := g.leader()
		if leader == 0 {
			leader = g.elect()
		}
		target := h.Leadership.Node
		if h.Leadership.Restore {
			target = h.Leadership.Standby
		}
		p, e := g.nodes[leader-1].PlanLeadership(h, leadEvidence(g, h, target))
		if errors.Is(e, ErrTransfer) {
			g.call(leader, Tick{})
			g.pump()
			continue
		}
		if e != nil {
			t.Fatal(p, e)
		}
		if !p.Configure {
			if !p.Settle {
				t.Fatal("configuration not settled", p)
			}
			return
		}
		g.call(leader, Configure{p.Change})
		g.pump()
	}
	t.Fatal("swap failed to settle")
}

// R6/R7/R8/R12 gate the real joint swap, target durable barrier and actual
// election. No fabricated read index, cached status or previous-term read can
// be reported as a successful leadership transfer.
func TestR6R7R8R12LeadershipBarrierAndJointPolicy(t *testing.T) {
	g, h := leadershipGroup(t)
	leader := g.leader()
	evidence := leadEvidence(g, h, 4)
	for _, bad := range []LeadershipEvidence{{}, {Node: 4, Revision: h.Revision, Applied: evidence.Applied, Protocol: 1}, {Node: 4, Revision: h.Revision, Applied: h.Index - 1, Protocol: 1, Durable: true}, {Node: 4, Revision: h.Revision - 1, Applied: evidence.Applied, Protocol: 1, Durable: true}} {
		if _, err := g.nodes[leader-1].PlanLeadership(h, bad); !errors.Is(err, ErrTransfer) {
			t.Fatal("unqualified target admitted", err)
		}
	}
	reconcileLeadCore(t, g, h)
	h.Leadership.Settled = true
	h.Index = g.nodes[g.leader()-1].applied
	leader = g.leader()
	p, err := g.nodes[leader-1].PlanLeadership(h, leadEvidence(g, h, 4))
	if err != nil {
		t.Fatal(err)
	}
	if p.Transfer != 4 {
		t.Fatal(p)
	}
	g.call(leader, Transfer{p.Transfer})
	g.pump()
	if g.leader() != 4 {
		t.Fatal("requested node did not win")
	}
	if _, err := g.nodes[3].LeadershipReceipt(h); !errors.Is(err, ErrTransfer) {
		t.Fatal("intent/election mistaken for success", err)
	}
	g.call(4, Read{RequestID: "leadership"})
	g.pump()
	r, err := g.nodes[3].LeadershipReceipt(h)
	if err != nil || r.Node != 4 || r.Barrier < h.Index || len(r.Voters) != 3 {
		t.Fatal(r, err)
	}
	if _, err = g.nodes[3].LeadershipReceipt(h); !errors.Is(err, ErrTransfer) {
		t.Fatal("consumed quorum proof was reusable", err)
	}
	// A sleeping P and a lost anchor leave no majority. No false success can
	// escape the remaining anchor, including from its earlier term's barrier.
	g.drop = func(from, to uint64) bool { return from == 4 || to == 4 || from == 2 || to == 2 }
	g.call(4, Read{RequestID: "isolated-leadership"})
	g.pump()
	if _, err = g.nodes[3].LeadershipReceipt(h); !errors.Is(err, ErrTransfer) {
		t.Fatal("minority reused earlier read", err)
	}
	for n := 0; n < 13; n++ {
		for _, id := range []uint64{1, 3} {
			g.call(id, Tick{})
		}
		g.pump()
	}
	if g.nodes[0].leader() {
		t.Fatal("minority elected a leader")
	}
	// Restore the surviving A+B path, then perform the reverse joint swap
	// without the absent P. C remains an enrolled warm learner until promotion.
	g.drop = func(from, to uint64) bool { return from == 4 || to == 4 }
	// The isolated old leader must relinquish before helper leader inspection.
	for n := 0; n < 7; n++ {
		g.call(4, Tick{})
		g.pump()
	}
	g.elect()
	h.Revision++
	h.Index = g.nodes[g.leader()-1].applied
	h.Leadership = &cs.LeadershipIntent{Node: 4, Standby: 3, Revision: h.Revision, Barrier: h.Index, Restore: true}
	reconcileLeadCore(t, g, h)
	conf := g.nodes[g.leader()-1].store.conf
	if !equalIDs(conf.GetVoters(), []uint64{1, 2, 3}) || !equalIDs(conf.GetLearners(), []uint64{4}) || len(conf.GetVotersOutgoing()) != 0 {
		t.Fatal(conf)
	}
	// Returning P is a learner even after restart with its old durable bytes.
	g.drop = nil
	g.tick(2)
	if contains(g.nodes[3].store.conf.GetVoters(), 4) || !contains(g.nodes[3].store.conf.GetLearners(), 4) {
		t.Fatal("P regained vote")
	}
	if _, err := g.nodes[3].LeadershipReceipt(h); err == nil {
		t.Fatal("old receipt retained authority")
	}
}
