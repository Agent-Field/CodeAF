package clusterstate_test

import (
	"crypto/ed25519"
	"fmt"
	"math/rand"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R22 pins freshness, release-before-delayed-renew, exact fences and committed
// time without importing a socket into the authorization rule.
func TestR22CommittedRenewalEvidence(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			a := newApplication(t)
			a.put(cs.AuthorityKey("ingress"), cs.Authority{Cluster: "test", Key: actorKey("ingress").Public().(ed25519.PublicKey), Effects: true})
			now := int64(1_000_000 + seed)
			a.run("a", now, cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "sealed"}}})
			renew := func(device string, seen int64, fence uint64) cs.Result {
				a.meta.Index++
				return a.run("ingress", now, cs.Mutation{RenewHolds: &cs.RenewHolds{Device: device, SeenAt: seen, Items: []cs.HoldFence{{Cell: "chat", Fence: fence}}}})
			}
			before := a.meta.Version
			now += 30_000
			r := renew("a", now, 1)
			if r.Code != cs.OK || r.Index == 0 || len(r.Items) != 1 || r.Items[0].Code != cs.OK || a.cell().Lease.Expires != now+90_000 || a.meta.Version != before {
				t.Fatal("R22 committed expiry/version", r, a.cell())
			}
			until := a.cell().Lease.Expires
			now += 10_000
			r = renew("a", now-10_000, 1)
			if r.Code != cs.OK || a.cell().Lease.Expires != until {
				t.Fatal("R22 replay extended expiry", r)
			}
			r = renew("a", now-25_001, 1)
			if r.Code != cs.BadRequest {
				t.Fatal("R22 stale evidence", r)
			}
			r = renew("a", now+1, 1)
			if r.Code != cs.BadRequest {
				t.Fatal("R22 future evidence", r)
			}
			r = renew("a", now, 2)
			if r.Items[0].Code != cs.FenceStale || a.cell().Lease.Expires != until {
				t.Fatal("R22 wrong fence", r)
			}
			for _, quiet := range []bool{false, true} {
				a.run("a", now, cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 1, Quiet: quiet}})
				r = renew("a", now-1, 1)
				if r.Items[0].Code != cs.FenceStale || a.cell().Lease.Expires != 0 {
					t.Fatal("R22 delayed renew revived release", r)
				}
			}
			r = a.run("b", now, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Expect: cs.LeaseExpectation{Device: "a", Fence: 1}}})
			if r.Code != cs.OK {
				t.Fatal(r)
			}
			r = renew("a", now, 1)
			if r.Items[0].Code != cs.FenceStale || a.cell().Lease.Fence != 2 {
				t.Fatal("R22 displaced holder renewed", r)
			}
			a.run("b", now, cs.Mutation{Revoke: &cs.DeviceID{Device: "a"}})
			r = renew("a", now, 1)
			if r.Code != cs.Revoked {
				t.Fatal("R22 revoked evidence", r)
			}
			rng := rand.New(rand.NewSource(int64(seed)))
			// The independent expiry oracle uses only the accepted evidence
			// sample, never the implementation's resulting cell to predict it.
			expected := a.cell().Lease.Expires
			for step := 0; step < 12; step++ {
				now += int64(rng.Intn(9000))
				seen := now - int64(rng.Intn(40_000))
				r = renew("b", seen, 2)
				if now-seen > 25_000 {
					if r.Code != cs.BadRequest {
						t.Fatal("R22 randomized stale sample", r)
					}
				} else {
					expected = max(expected, seen+90_000)
					if r.Code != cs.OK || r.Items[0].Code != cs.OK {
						t.Fatal("R22 randomized fresh sample", r)
					}
				}
				if a.cell().Lease.Expires != expected || a.cell().Lease.Fence != 2 {
					t.Fatal("R22 randomized evidence changed authority", seen, expected, a.cell())
				}
			}

		})
	}
}

func TestR26QuiescenceMarkerCancelsOnCommand(t *testing.T) {
	a := newApplication(t)
	a.put(cs.AuthorityKey("anchor"), cs.Authority{Cluster: "test", Key: actorKey("anchor").Public().(ed25519.PublicKey), Anchor: true})
	a.meta.Index = 11
	a.meta.Term = 2
	a.meta.Configuration = cs.Configuration{Voters: []uint64{1, 2, 3}}
	r := a.run("anchor", 1000, cs.Mutation{PrepareQuiescence: &cs.PrepareQuiescence{Barrier: 10, Term: 2, Voters: []uint64{1, 2, 3}}})
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	var marker cs.Quiescence
	if cs.Decode(a.rows[cs.QuiescenceKey()], &marker) != nil || marker.Index != 11 {
		t.Fatal(marker)
	}
	a.meta.Index++
	a.run("a", 1001, cs.Mutation{AdvanceTime: &cs.AdvanceTime{}})
	if cs.Decode(a.rows[cs.QuiescenceKey()], &marker) != nil || marker.Index != 0 {
		t.Fatal("R26 mutation retained old generation", marker)
	}
	a.meta.Index++
	r = a.run("anchor", 1002, cs.Mutation{PrepareQuiescence: &cs.PrepareQuiescence{Barrier: 10, Term: 2, Voters: []uint64{1, 2, 3}}})
	if r.Code != cs.Conflict {
		t.Fatal("R26 stale barrier accepted", r)
	}
}

// R26/R27 retain scheduled committed delivery work without holding an idle
// pairing request or a completed historical outbox row active forever.
func TestR26R27CommittedOutboxPreventsSleep(t *testing.T) {
	key := cs.JoinKey("fixture")
	for _, v := range []struct {
		request cs.JoinRequest
		active  bool
	}{
		{cs.JoinRequest{Device: "a", Expires: 5000}, false},
		{cs.JoinRequest{Device: "a", Decision: &cs.JoinDecision{Deny: true}}, true},
		{cs.JoinRequest{Device: "a", Decision: &cs.JoinDecision{Deny: true}, Outcome: "delivered"}, false},
		{cs.JoinRequest{Device: "a", Decision: &cs.JoinDecision{Deny: true}, Outcome: "gone"}, false},
	} {
		raw, err := cs.Encode(v.request)
		if err != nil {
			t.Fatal(err)
		}
		active, err := cs.RecordActive(key, raw, 1000)
		if err != nil || active != v.active {
			t.Fatal(v, active, err)
		}
	}
}
