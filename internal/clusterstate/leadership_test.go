package clusterstate_test

import (
	"fmt"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"math/rand"
	"testing"
)

func leadReady(t *testing.T) *application {
	a := membershipApp(t, cs.MaxMemberRecords)
	a.meta.Term = 1
	if r := enroll(a, "b"); r.Code != cs.OK {
		t.Fatal(r)
	}
	if r := settle(a); r.Code != cs.OK {
		t.Fatal(r)
	}
	return a
}
func leadRequest(a *application) cs.Result {
	h := memberHeader(a)
	return memberRun(a, cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: h.Revision, Node: 4, Device: "b"}})
}
func anchorChange(a *application, m cs.Mutation) cs.Result {
	a.meta.Index++
	c := a.prepareBare(cs.AnchorActor(1), 1000, m)
	c, e := cs.Sign(c, actorKey("anchor-1"))
	if e != nil {
		a.t.Fatal(e)
	}
	return a.evaluate(c).Result
}
func forwardConfiguration(a *application) {
	a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 4}, Learners: []uint64{3}}
}
func settleLead(a *application) cs.Result {
	return anchorChange(a, cs.Mutation{SettleLeadership: &cs.SettleLeadership{Revision: memberHeader(a).Revision}})
}

// R6/R7/R8/R9/R12 require a durable intent rather than an editable voter list.
// Joint state cannot settle; fallback needs provisioned anchor authority, keeps
// the third anchor, and leaves the returning personal member without a vote.
func TestR6R7R8R9R12LeadershipPolicy(t *testing.T) {
	a := leadReady(t)
	if r := leadRequest(a); r.Code != cs.OK {
		t.Fatal(r)
	}
	h := memberHeader(a)
	if h.Leadership.Settled || len(a.meta.Configuration.Voters) != 3 {
		t.Fatal("intent claimed an election")
	}
	if r := settleLead(a); r.Code != cs.Conflict {
		t.Fatal("settled before swap", r)
	}
	a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 4}, Outgoing: []uint64{1, 2, 3}, LearnersNext: []uint64{3}}
	if r := settleLead(a); r.Code != cs.Conflict {
		t.Fatal("settled joint state", r)
	}
	forwardConfiguration(a)
	if r := settleLead(a); r.Code != cs.OK {
		t.Fatal(r)
	}
	h = memberHeader(a)
	if r := memberRun(a, cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: h.Revision}}); r.Code != cs.Unauthorized {
		t.Fatal("personal caller forced fallback", r)
	}
	if r := anchorChange(a, cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: h.Revision}}); r.Code != cs.OK {
		t.Fatal(r)
	}
	if r := anchorChange(a, cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: h.Revision}}); r.Code != cs.Conflict {
		t.Fatal("simultaneous fallback did not serialize", r)
	}
	if r := settleLead(a); r.Code != cs.Conflict {
		t.Fatal("fallback before reverse swap", r)
	}
	a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 3}, Learners: []uint64{4}}
	if r := settleLead(a); r.Code != cs.OK {
		t.Fatal(r)
	}
	if memberHeader(a).Leadership != nil || !memberHeader(a).StableTopology(a.meta.Configuration) {
		t.Fatal("P automatically regained a vote")
	}
	if cs.LeadershipUnavailable != "this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here" {
		t.Fatal("R8 unavailable sentence changed")
	}
}

// R6/R12/R16 refuse retired, revoked, unready and mismatched targets, including
// requests signed by a different still-authorized personal owner.
func TestR6R12R16LeadershipAdmission(t *testing.T) {
	for _, kind := range []string{"revoked", "unready", "wrong-device", "anchor", "stale"} {
		t.Run(kind, func(t *testing.T) {
			a := leadReady(t)
			h := memberHeader(a)
			v := cs.RequestLeadership{Revision: h.Revision, Node: 4, Device: "b"}
			switch kind {
			case "revoked":
				memberRun(a, cs.Mutation{Revoke: &cs.DeviceID{Device: "b"}})
			case "unready":
				var m cs.MemberRecord
				_ = cs.Decode(a.rows[cs.MemberKey(4)], &m)
				m.ReadyIndex = 0
				a.put(cs.MemberKey(4), m)
			case "wrong-device":
				v.Device = "a"
			case "anchor":
				v.Node = 3
			case "stale":
				v.Revision--
			}
			if r := memberRun(a, cs.Mutation{RequestLeadership: &v}); r.Code == cs.OK {
				t.Fatal("ineligible target admitted", kind)
			}
		})
	}
}

// R6/R7/R12 randomize durable stage delivery against the independently stated
// expected topology. Duplicate and stale requests cannot settle an earlier
// stage, nor can returning P create a new promotion without an owner request.
func TestR6R7R12SeededLeadershipIntentOrder(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) {
			a := leadReady(t)
			rng := rand.New(rand.NewSource(int64(seed)))
			if r := leadRequest(a); r.Code != cs.OK {
				t.Fatal(r)
			}
			for i := 0; i < rng.Intn(4)+1; i++ {
				if r := settleLead(a); r.Code != cs.Conflict {
					t.Fatal(r)
				}
			}
			// Loss may occur before, during, or after the forward configuration.
			switch rng.Intn(3) {
			case 1:
				a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 4}, Outgoing: []uint64{1, 2, 3}, LearnersNext: []uint64{3}}
			case 2:
				forwardConfiguration(a)
				if r := settleLead(a); r.Code != cs.OK {
					t.Fatal(r)
				}
			}
			rev := memberHeader(a).Revision
			if r := anchorChange(a, cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: rev}}); r.Code != cs.OK {
				t.Fatal(r)
			}
			if r := anchorChange(a, cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: rev}}); r.Code != cs.Conflict {
				t.Fatal(r)
			}
			a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 3}, Learners: []uint64{4}}
			if r := settleLead(a); r.Code != cs.OK {
				t.Fatal(r)
			}
			h := memberHeader(a)
			if h.Leadership != nil || len(h.LeadershipVoters()) != 3 || h.Validate() != nil {
				t.Fatal(h)
			}
		})
	}
}
