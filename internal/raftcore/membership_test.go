package raftcore

import (
	"errors"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// R11/R12 serialize actual configuration proposals, reconcile them after
// restart and refresh a compacted snapshot to include the admitted learner.
func TestR11R12MembershipIntentReconciliationAndStaleResponse(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	c := g.nodes[leader-1]
	h := cs.Membership{Revision: 2, Index: c.applied, Pending: &cs.MembershipIntent{Node: 4, Revision: 2}}
	p, e := c.PlanMembership(h)
	if e != nil || len(p.Change) == 0 || p.Settle {
		t.Fatal(p, e)
	}
	// Withhold replication responses. A durable-but-uncommitted configuration
	// is not permission to issue another learner change or mark the first ready.
	g.drop = func(from, to uint64) bool { return to == leader || to == 4 }
	g.call(leader, Configure{Change: p.Change})
	g.pump()
	if _, e = c.PlanMembership(h); !errors.Is(e, ErrBusy) {
		t.Fatal("uncommitted configuration accepted another intent", e)
	}
	g.drop = func(from, to uint64) bool { return to == 4 }
	g.call(leader, Tick{})
	g.pump()
	p, e = c.PlanMembership(h)
	if e != nil || !p.Settle || !p.Checkpoint {
		t.Fatal("admission did not request fresh snapshot", p, e)
	}
	// Restart from the actual flushed configuration, not the caller's intent.
	cfg := c.cfg
	cfg.Bootstrap = nil
	cfg.Seed++
	restored, e := New(cfg, g.durable[leader-1])
	if e != nil {
		t.Fatal(e)
	}
	if _, e = restored.PlanMembership(h); !errors.Is(e, ErrLeader) {
		t.Fatal("restart restored leader authority", e)
	}
	confBytes, e := raftsnapshot.Configuration(c.store.conf)
	if e != nil {
		t.Fatal(e)
	}
	term, e := c.store.Term(c.applied)
	if e != nil {
		t.Fatal(e)
	}
	manifest := raftsnapshot.Manifest{Version: 1, Cluster: c.cfg.ClusterUUID, Protocol: 1, Index: c.applied, Term: term, Conf: confBytes, RecordsHash: strings.Repeat("0", 64), Digest: raftsnapshot.ID(nil)}
	data, e := manifest.Encode()
	if e != nil {
		t.Fatal(e)
	}
	snapshot := &pb.Snapshot{Data: data, Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(c.applied), Term: proto.Uint64(term), ConfState: clone(c.store.conf)}}
	g.call(leader, CompactSnapshot{Snapshot: snapshot})
	g.pump()
	p, e = c.PlanMembership(h)
	if e != nil || !p.Settle || p.Checkpoint {
		t.Fatal("already matching snapshot not recognized", p, e)
	}
	h.Revision++
	h.Index = c.applied
	h.Pending = &cs.MembershipIntent{Node: 4, Revision: h.Revision, Remove: true}
	p, e = c.PlanMembership(h)
	if e != nil {
		t.Fatal(e)
	}
	g.call(leader, Configure{Change: p.Change})
	g.pump()
	p, e = c.PlanMembership(h)
	if e != nil || !p.Settle || p.Checkpoint {
		t.Fatal("remove did not reconcile", p, e)
	}
	before := c.status()
	raw, _ := wire.Marshal(&pb.Message{Type: pb.MsgAppResp.Enum(), From: proto.Uint64(4), To: proto.Uint64(leader), Term: proto.Uint64(before.Term), Index: proto.Uint64(before.Commit)})
	g.call(leader, PeerMessage{Bytes: raw})
	g.pump()
	if c.status().Term != before.Term || len(c.store.conf.GetVoters()) != 3 || len(c.store.conf.GetLearners()) != 0 {
		t.Fatal("retired response changed authority")
	}
}
