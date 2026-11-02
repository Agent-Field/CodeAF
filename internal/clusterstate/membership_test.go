package clusterstate_test

import (
	"crypto/ed25519"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func membershipApp(t *testing.T, limit uint32) *application {
	a := newApplication(t)
	a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 3}}
	anchors := []cs.MemberRecord{}
	for id := uint64(1); id <= 3; id++ {
		anchors = append(anchors, cs.MemberRecord{Node: id, Key: actorKey(fmt.Sprint("anchor-", id)).Public().(ed25519.PublicKey), Incarnation: 1, Protocol: 1, Anchor: true})
	}
	if r := memberRun(a, cs.Mutation{InitMembership: &cs.InitMembership{Anchors: anchors, Limit: limit}}); r.Code != cs.OK {
		t.Fatal(r)
	}
	return a
}
func memberRun(a *application, m cs.Mutation) cs.Result { a.meta.Index++; return a.run("a", 1000, m) }
func memberHeader(a *application) cs.Membership {
	var h cs.Membership
	if e := cs.Decode(a.rows[cs.MembershipKey()], &h); e != nil {
		a.t.Fatal(e)
	}
	return h
}
func enroll(a *application, device string) cs.Result {
	h := memberHeader(a)
	return memberRun(a, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: h.NextNode, Revision: h.Revision, Device: device, Protocol: 1}})
}
func settle(a *application) cs.Result {
	h := memberHeader(a)
	id := h.Pending.Node
	if h.Pending.Remove {
		ids := []uint64{}
		for _, n := range a.meta.Configuration.Learners {
			if n != id {
				ids = append(ids, n)
			}
		}
		a.meta.Configuration.Learners = ids
	} else {
		a.meta.Configuration.Learners = append(a.meta.Configuration.Learners, id)
	}
	return memberRun(a, cs.Mutation{SettleMembership: &cs.SettleMembership{Revision: h.Revision, Node: id}})
}
func addAuthority(a *application, id string) {
	a.put(cs.AuthorityKey(id), cs.Authority{Cluster: "test", Key: actorKey(id).Public().(ed25519.PublicKey), Personal: true})
	a.put(cs.DeviceKey(id), cs.Device{V: 1})
}

// R9/R11/R12/R16/R17 require bounded learner-only admission, durable allocation,
// one resumable intent, exact retry, and no revival of a retired node ID.
func TestR9R11R12R16R17LearnerAdmissionAndRetainedTombstones(t *testing.T) {
	a := membershipApp(t, cs.MaxMemberRecords)
	h := memberHeader(a)
	command := a.prepareBare("a", 1000, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: 4, Revision: h.Revision, Device: "b", Protocol: 1}})
	a.meta.Index++
	r := a.evaluate(command).Result
	if r.Code != cs.OK || r.Membership.Node != 4 || r.Membership.Barrier != 0 {
		t.Fatal(r)
	}
	if got := a.evaluate(command).Result; !reflect.DeepEqual(got, r) {
		t.Fatal("R17 retry changed allocation", got, r)
	}
	if got := memberRun(a, cs.Mutation{SettleMembership: &cs.SettleMembership{Revision: 2, Node: 4}}); got.Code != cs.Conflict {
		t.Fatal("ready before configuration", got)
	}
	if got := enroll(a, "a"); got.Code != cs.Conflict {
		t.Fatal("second concurrent intent", got)
	}
	done := settle(a)
	if done.Code != cs.OK || done.Membership.Barrier == 0 {
		t.Fatal(done)
	}
	var m cs.MemberRecord
	_ = cs.Decode(a.rows[cs.MemberKey(4)], &m)
	if cs.LearnerReady(m, a.meta.Configuration, done.Membership.Barrier-1, done.Membership.Barrier) || !cs.LearnerReady(m, a.meta.Configuration, done.Membership.Barrier, done.Membership.Barrier) {
		t.Fatal("R11 ready ignored applied barrier")
	}
	h = memberHeader(a)
	if got := memberRun(a, cs.Mutation{RemoveMember: &cs.RemoveMember{Revision: h.Revision, Node: 4, Device: "b"}}); got.Code != cs.OK {
		t.Fatal(got)
	}
	if a.rows[cs.MemberTombstoneKey(4)] == nil {
		t.Fatal("R12 missing tombstone")
	}
	_, peers, e := cs.MembershipView(func(k cs.RecordKey) ([]byte, error) { return a.rows[k], nil }, a.meta.Configuration, a.meta.Index)
	if e != nil || !peers[3].Revoked {
		t.Fatal("R16 removed transport remains admitted", e, peers)
	}
	if got := settle(a); got.Code != cs.OK {
		t.Fatal(got)
	}
	if got := enroll(a, "b"); got.Code != cs.OK || got.Membership.Node != 5 {
		t.Fatal("R12 reused removed node", got)
	}
	if got := settle(a); got.Code != cs.OK {
		t.Fatal(got)
	}
	if got := memberRun(a, cs.Mutation{Revoke: &cs.DeviceID{Device: "b"}}); got.Code != cs.OK {
		t.Fatal(got)
	}
	_, peers, e = cs.MembershipView(func(k cs.RecordKey) ([]byte, error) { return a.rows[k], nil }, a.meta.Configuration, a.meta.Index)
	if e != nil || !peers[4].Revoked {
		t.Fatal("R16 committed device revoke missed routing", e)
	}
	if got := enroll(a, "b"); got.Code != cs.Revoked {
		t.Fatal(got)
	}
	if len(a.meta.Configuration.Voters) != 3 {
		t.Fatal("R9 pairing changed quorum")
	}
}

// R11/R27 cap live learners separately from permanently retained records.
// Capacity pressure must retain every prior enrollment and node tombstone.
func TestR11R27LearnerAndRetainedRecordLimits(t *testing.T) {
	a := membershipApp(t, cs.MaxMemberRecords)
	for i := 0; i < cs.MaxLearners; i++ {
		id := fmt.Sprint("personal-", i)
		addAuthority(a, id)
		if r := enroll(a, id); r.Code != cs.OK {
			t.Fatal(i, r)
		}
		if r := settle(a); r.Code != cs.OK {
			t.Fatal(r)
		}
	}
	addAuthority(a, "overflow")
	before := append([]byte(nil), a.rows[cs.MembershipKey()]...)
	if r := enroll(a, "overflow"); r.Code != cs.Capacity {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(before, a.rows[cs.MembershipKey()]) {
		t.Fatal("capacity altered records")
	}
	b := membershipApp(t, 4)
	if r := enroll(b, "b"); r.Code != cs.OK {
		t.Fatal(r)
	}
	settle(b)
	h := memberHeader(b)
	memberRun(b, cs.Mutation{RemoveMember: &cs.RemoveMember{Revision: h.Revision, Node: 4, Device: "b"}})
	settle(b)
	if r := enroll(b, "b"); r.Code != cs.Capacity {
		t.Fatal("retained ceiling silently erased history", r)
	}
	if b.rows[cs.MemberTombstoneKey(4)] == nil {
		t.Fatal("lost tombstone")
	}
}

// R9/R12/R16 reject unsupported protocols, forged metadata-only enrollment,
// stale revisions, voter removal and configuration changes during joint state.
func TestR9R12R16MembershipRefusals(t *testing.T) {
	a := membershipApp(t, 100)
	a.put(cs.DeviceKey("metadata-only"), cs.Device{V: 1})
	if r := enroll(a, "metadata-only"); r.Code != cs.Unauthorized {
		t.Fatal(r)
	}
	h := memberHeader(a)
	if r := memberRun(a, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: h.NextNode, Revision: h.Revision, Device: "b", Protocol: 2}}); r.Code != cs.UpdateRequired {
		t.Fatal(r)
	}
	if r := memberRun(a, cs.Mutation{RemoveMember: &cs.RemoveMember{Revision: h.Revision, Node: 1}}); r.Code != cs.Conflict {
		t.Fatal("anchor removed", r)
	}
	a.meta.Configuration.Outgoing = []uint64{1, 2, 3}
	if r := enroll(a, "b"); r.Code != cs.Conflict {
		t.Fatal("joined during joint state", r)
	}
	a.meta.Configuration.Outgoing = nil
	if r := memberRun(a, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: 3, Revision: h.Revision, Device: "b", Protocol: 1}}); r.Code != cs.Conflict {
		t.Fatal("allocated reused id", r)
	}
}

// R11/R12/R16/R17 compare random request/configuration orderings with a small
// independent admission model, including stale observations and revocation.
func TestR11R12R17SeededMembershipCampaign(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			a := membershipApp(t, 100)
			rng := rand.New(rand.NewSource(int64(seed)))
			revision, next := uint64(1), uint64(4)
			active, target := uint64(0), uint64(4)
			pending := ""
			present, revoked := false, false
			retired := map[uint64]bool{}
			for step := 0; step < 16; step++ {
				var mutation cs.Mutation
				want := cs.OK
				switch rng.Intn(7) {
				case 0, 1:
					mutation.EnrollMember = &cs.EnrollMember{Node: next, Revision: revision, Device: "b", Protocol: 1}
					switch {
					case pending != "":
						want = cs.Conflict
					case revoked:
						want = cs.Revoked
					case active != 0:
						want = cs.Conflict
					default:
						active = next
						target = next
						next++
						revision++
						pending = "add"
					}
				case 2:
					mutation.RemoveMember = &cs.RemoveMember{Revision: revision, Node: target, Device: "b"}
					switch {
					case pending != "":
						want = cs.Conflict
					case target >= next:
						want = cs.NotFound
					case retired[target]:
						want = cs.Conflict
					default:
						retired[target] = true
						active = 0
						revision++
						pending = "remove"
					}
				case 3:
					mutation.SettleMembership = &cs.SettleMembership{Revision: revision, Node: target}
					if pending == "" || pending == "add" && !present || pending == "remove" && present {
						want = cs.Conflict
					} else {
						pending = ""
					}
				case 4:
					// Explicit host configuration delivery is independent of request order.
					if pending == "add" {
						present = true
						a.meta.Configuration.Learners = []uint64{target}
					} else if pending == "remove" {
						present = false
						a.meta.Configuration.Learners = nil
					}
					continue
				case 5:
					mutation.EnrollMember = &cs.EnrollMember{Node: next, Revision: revision - 1, Device: "b", Protocol: 1}
					want = cs.Conflict
				case 6:
					mutation.Revoke = &cs.DeviceID{Device: "b"}
					revoked = true
				}
				got := memberRun(a, mutation)
				if got.Code != want {
					t.Fatalf("step %d got %s want %s mutation=%+v", step, got.Code, want, mutation)
				}
				h := memberHeader(a)
				if h.NextNode != next || h.Revision != revision {
					t.Fatalf("allocation differs: %+v next=%d revision=%d", h, next, revision)
				}
				for id := range retired {
					var m cs.MemberRecord
					if cs.Decode(a.rows[cs.MemberTombstoneKey(id)], &m) != nil || !m.Retired {
						t.Fatal("retirement lost", id)
					}
				}
			}
		})
	}
}

// R9/R12/R16 require an anchor to finish an already authorized join while its
// requester sleeps, without granting that protocol key personal authority.
func TestR9R12R16AnchorCanOnlyCompleteCommittedIntent(t *testing.T) {
	a := membershipApp(t, 100)
	if r := enroll(a, "b"); r.Code != cs.OK {
		t.Fatal(r)
	}
	h := memberHeader(a)
	actor := cs.AnchorActor(1)
	run := func(sequence uint64, m cs.Mutation) cs.Result {
		a.meta.Index++
		c, e := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: actor, Session: "completion", Sequence: sequence, Ack: sequence - 1, Now: 1000, Change: m}, actorKey("anchor-1"))
		if e != nil {
			t.Fatal(e)
		}
		return a.evaluate(c).Result
	}
	if r := run(1, cs.Mutation{SetVault: &cs.VaultCAS{New: "forbidden"}}); r.Code != cs.Unauthorized {
		t.Fatal("anchor gained content authority", r)
	}
	if r := run(1, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: 5, Revision: h.Revision, Device: "a", Protocol: 1}}); r.Code != cs.Unauthorized {
		t.Fatal("anchor invented enrollment", r)
	}
	if r := run(1, cs.Mutation{SettleMembership: &cs.SettleMembership{Node: 4, Revision: h.Revision}}); r.Code != cs.Conflict {
		t.Fatal("anchor skipped applied configuration", r)
	}
	a.meta.Configuration.Learners = []uint64{4}
	if r := run(2, cs.Mutation{SettleMembership: &cs.SettleMembership{Node: 4, Revision: h.Revision}}); r.Code != cs.OK || r.Membership.Barrier == 0 {
		t.Fatal("anchor could not resume authorized enrollment", r)
	}
}
