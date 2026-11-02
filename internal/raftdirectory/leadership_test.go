package raftdirectory_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func personalGroup(t *testing.T) *group {
	extra := []raftstore.Write{}
	for _, name := range []string{"paired-a", "paired-b"} {
		pub := key(name).Public().(ed25519.PublicKey)
		id := cs.DeviceIDOfKey(pub)
		extra = append(extra, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(id).Key}, Value: encoded(t, cs.Device{V: 1})}, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Enrollment, Key: cs.AuthorityKey(id).Key}, Value: encoded(t, cs.Authority{Cluster: cluster, Key: pub, Personal: true})})
	}
	g := newGroup(t, func() time.Time { return time.UnixMilli(1000) }, extra...)
	anchors := []cs.MemberRecord{}
	g.keys = map[string]ed25519.PrivateKey{}
	for _, name := range []string{"paired-a", "paired-b"} {
		g.keys[cs.DeviceIDOfKey(key(name).Public().(ed25519.PublicKey))] = key(name)
	}
	for id := uint64(1); id <= 3; id++ {
		k := key(fmt.Sprint("anchor-", id))
		g.keys[cs.AnchorActor(id)] = k
		anchors = append(anchors, cs.MemberRecord{Node: id, Key: k.Public().(ed25519.PublicKey), Incarnation: 1, Protocol: 1, Anchor: true})
	}
	membershipChange(t, g, cs.Mutation{InitMembership: &cs.InitMembership{Anchors: anchors, Limit: cs.MaxMemberRecords}})
	path := filepath.Join(t.TempDir(), "personal.db")
	s, e := raftstore.Open(path, raftstore.Identity{ClusterUUID: cluster, NodeID: 4, Protocol: 1})
	must(t, e)
	n, e := raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: 4, Seed: 4, Protocol: 1}, raftcore.RecoveredState{})
	must(t, e)
	g.paths = append(g.paths, path)
	g.stores = append(g.stores, s)
	g.nodes = append(g.nodes, n)
	g.status = append(g.status, raftcore.Status{})
	g.results = append(g.results, nil)
	g.absent[4] = true
	h := registry(t, g, g.leader())
	membershipChange(t, g, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: 4, Device: cs.DeviceIDOfKey(key("paired-b").Public().(ed25519.PublicKey)), Protocol: 1, Revision: h.Revision}})
	reconcileMembership(t, g)
	leader := g.leader()
	snapshot, e := g.stores[leader].CreateSnapshot(ctx)
	must(t, e)
	installNativeSnapshot(t, g, leader, 3, snapshot)
	g.absent[4] = false
	// The second certified home also joins without a vote. Both can sleep
	// together without creating a two-voter default or changing quorum.
	path = filepath.Join(t.TempDir(), "second-personal.db")
	s, e = raftstore.Open(path, raftstore.Identity{ClusterUUID: cluster, NodeID: 5, Protocol: 1})
	must(t, e)
	n, e = raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: 5, Seed: 5, Protocol: 1}, raftcore.RecoveredState{})
	must(t, e)
	g.paths = append(g.paths, path)
	g.stores = append(g.stores, s)
	g.nodes = append(g.nodes, n)
	g.status = append(g.status, raftcore.Status{})
	g.results = append(g.results, nil)
	g.absent[5] = true
	h = registry(t, g, g.leader())
	membershipChange(t, g, cs.Mutation{EnrollMember: &cs.EnrollMember{Node: 5, Device: cs.DeviceIDOfKey(key("paired-a").Public().(ed25519.PublicKey)), Protocol: 1, Revision: h.Revision}})
	reconcileMembership(t, g)
	leader = g.leader()
	snapshot, e = g.stores[leader].CreateSnapshot(ctx)
	must(t, e)
	installNativeSnapshot(t, g, leader, 4, snapshot)
	g.absent[5] = false
	g.drive(g.leader(), raftcore.Tick{})
	g.drain()
	return g
}

type leaderBackend struct{ g *group }

func (b leaderBackend) Status(ctx context.Context, actor string) (clusterctl.Status, error) {
	i, app, e := b.g.barrier(actor)
	if e != nil {
		return clusterctl.Status{}, e
	}
	h, e := raftdirectory.LeadershipState(ctx, b.g.stores[i])
	if e != nil {
		return clusterctl.Status{}, e
	}
	k := raftstore.Key{Namespace: raftstore.Membership, Key: cs.MemberDeviceKey(actor).Key}
	rows, _, e := b.g.stores[i].Read(ctx, app.Applied, []raftstore.Key{k})
	if e != nil {
		return clusterctl.Status{}, e
	}
	var mapping cs.DeviceMember
	if rows[k] != nil {
		if e = cs.Decode(rows[k], &mapping); e != nil {
			return clusterctl.Status{}, e
		}
	}
	self := mapping.Node
	return clusterctl.Status{Cluster: cluster, Self: self, Leader: uint64(i + 1), Membership: h, Configuration: raftcore.Configuration(app.ConfState)}, nil
}
func (b leaderBackend) Lead(ctx context.Context, c cs.Command) (cs.LeadershipReceipt, error) {
	r, e := b.g.submit(c)
	if e != nil {
		return cs.LeadershipReceipt{}, e
	}
	if r.Code != cs.OK {
		return cs.LeadershipReceipt{}, clusterctl.CommittedRefusal{Code: r.Code, Sequence: c.Sequence}
	}
	for n := 0; n < 30; n++ {
		g := b.g
		i := g.leader()
		if i < 0 {
			for j := range g.nodes {
				g.drive(j, raftcore.Tick{})
			}
			g.drain()
			continue
		}
		h, e := raftdirectory.LeadershipState(ctx, g.stores[i])
		if e != nil {
			return cs.LeadershipReceipt{}, e
		}
		l := h.Leadership
		if l == nil {
			return cs.LeadershipReceipt{}, clusterctl.ErrUnavailable
		}
		if i == int(l.Node)-1 && l.Settled {
			g.drive(i, raftcore.Read{RequestID: fmt.Sprint("lead-proof-", n)})
			g.drain()
			confirmed, e := g.nodes[i].LeadershipReceipt(h)
			if e != nil {
				return cs.LeadershipReceipt{}, e
			}
			if l.Completed {
				return confirmed, nil
			}
			if !l.Completed {
				r, e = g.Change(ctx, cs.AnchorActor(1), cs.Mutation{CompleteLeadership: &cs.CompleteLeadership{Revision: h.Revision}})
				if e != nil || r.Code != cs.OK {
					return cs.LeadershipReceipt{}, errors.New("completion did not commit")
				}
				h, e = raftdirectory.LeadershipState(ctx, g.stores[i])
				if e != nil {
					return cs.LeadershipReceipt{}, e
				}
				g.drive(i, raftcore.Read{RequestID: "completed-leadership"})
				g.drain()
			}
			return g.nodes[i].LeadershipReceipt(h)
		}
		target := l.Node
		if l.Restore {
			target = l.Standby
		}
		evidence, e := raftdirectory.DurableLeadershipEvidence(ctx, g.stores[target-1], target, h.Revision, l.Barrier)
		if e != nil {
			g.drive(i, raftcore.Tick{})
			g.drain()
			continue
		}
		_, p, e := raftdirectory.PlanLeadership(ctx, g.stores[i], g.nodes[i], evidence)
		if errors.Is(e, raftcore.ErrTransfer) || errors.Is(e, raftcore.ErrBusy) {
			g.drive(i, raftcore.Tick{})
			g.drain()
			continue
		}
		if e != nil {
			return cs.LeadershipReceipt{}, e
		}
		if p.Configure {
			g.drive(i, raftcore.Configure{Change: p.Change})
			g.drain()
		}
		if p.Settle {
			r, e = g.Change(ctx, cs.AnchorActor(1), cs.Mutation{SettleLeadership: &cs.SettleLeadership{Revision: h.Revision}})
			if e != nil || r.Code != cs.OK {
				return cs.LeadershipReceipt{}, errors.New("settlement did not commit")
			}
		}
		if p.Transfer != 0 {
			g.drive(i, raftcore.Transfer{Target: p.Transfer})
			g.drain()
		}
	}
	return cs.LeadershipReceipt{}, clusterctl.ErrUnavailable
}

// R6/R9 exercise the authenticated status/lead door from two disposable saved
// homes, across actual FULL SQLite stores and the production planner. The thin
// leader receives only coordination metadata; no blob route is even mounted.
func TestR6R7R9NativeLeadershipControlDoorAndRestart(t *testing.T) {
	g := personalGroup(t)
	homes := []migrationHome{savedMigrationHome(t, "paired-a"), savedMigrationHome(t, "paired-b")}
	ids := []string{cs.DeviceIDOfKey(key("paired-a").Public().(ed25519.PublicKey)), cs.DeviceIDOfKey(key("paired-b").Public().(ed25519.PublicKey))}
	backend := leaderBackend{g}
	calls := 0
	server := httptest.NewServer(clusterctl.Handler(func(r *http.Request, raw []byte) (string, string, error) {
		calls++
		if r.URL.Path != clusterctl.Path {
			t.Fatal("thin leader fetched content", r.URL.Path)
		}
		id, dev, e := reqsign.Verify(r, raw, g.clock())
		if e != nil {
			return id, dev, e
		}
		if dev != ids[0] && dev != ids[1] {
			return "", "", directory.ErrUnauthorized
		}
		return id, dev, nil
	}, func(id string) (clusterctl.Backend, error) {
		if id != homes[0].id.ID() {
			return nil, directory.ErrUnauthorized
		}
		return backend, nil
	}))
	defer server.Close()
	clients := []clusterctl.Client{{Base: server.URL, Sign: reqsign.SignFor(homes[0], g.clock), HTTP: server.Client()}, {Base: server.URL, Sign: reqsign.SignFor(homes[1], g.clock), HTTP: server.Client()}}
	s, e := clients[1].Status(ctx)
	must(t, e)
	if s.Self != 4 || len(s.Configuration.Voters) != 3 {
		t.Fatal(s)
	}
	c, e := cs.Sign(cs.Command{Schema: 1, Cluster: cluster, Actor: ids[1], Session: "lead", Sequence: 1, Now: 1000, Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: s.Membership.Revision, Node: 4, Device: ids[1]}}}, key("paired-b"))
	must(t, e)
	receipt, e := clients[1].Lead(ctx, c)
	must(t, e)
	if receipt.Node != 4 || g.leader() != 3 || receipt.Barrier == 0 {
		t.Fatal(receipt)
	}
	h := registry(t, g, 3)
	if !h.Leadership.Completed {
		t.Fatal("successful transfer not durably completed")
	}
	if _, e := raftdirectory.DurableLeadershipEvidence(ctx, g.stores[0], 4, h.Revision, h.Leadership.Barrier); e == nil {
		t.Fatal("a different member attested P's store")
	}
	// An exact retry of the completed request returns another fresh read receipt
	// and cannot replay the old voter swap or increment configuration revision.
	again, e := clients[1].Lead(ctx, c)
	must(t, e)
	if again.Node != 4 || again.Revision != receipt.Revision {
		t.Fatal(again)
	}

	for i, store := range g.stores {
		recovered, err := store.Recover(ctx)
		must(t, err)
		if !slices.Equal(recovered.Core.Anchors, []uint64{1, 2, 3}) {
			t.Fatal("R7/R14 anchor ranks lost during C swap", i, recovered.Core.Anchors)
		}
	}
	recoverMembership(t, g)
	status, e := clients[0].Status(ctx)
	must(t, e)
	if status.Membership.Revision != h.Revision || len(status.Configuration.Voters) != 3 {
		t.Fatal(status)
	}
	if calls < 4 {
		t.Fatal("control did not cross the real HTTP door")
	}
	// The new anchor may restore C; it must not automatically transfer to a
	// returning P merely because P remains a caught-up voter before restoration.
	if g.leader() != 3 {
		p, e := g.nodes[g.leader()].PlanLeadership(registry(t, g, g.leader()), raftcore.LeadershipEvidence{})
		if e != nil || !p.Fallback || p.Transfer != 0 {
			t.Fatal(p, e)
		}
	}
	g.absent[4], g.absent[5] = true, true
	for n := 0; n < 30 && g.leader() < 0; n++ {
		for id := 0; id < 3; id++ {
			g.drive(id, raftcore.Tick{})
		}
		g.drain()
	}
	if g.leader() < 0 {
		t.Fatal("R7 A+B did not elect with both homes asleep")
	}
	h = registry(t, g, g.leader())
	r, e := g.Change(ctx, cs.AnchorActor(1), cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: h.Revision}})
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	for n := 0; n < 30; n++ {
		i := g.leader()
		h = registry(t, g, i)
		if h.Leadership == nil {
			break
		}
		evidence, err := raftdirectory.DurableLeadershipEvidence(ctx, g.stores[2], 3, h.Revision, h.Leadership.Barrier)
		if err != nil {
			g.drive(i, raftcore.Tick{})
			g.drain()
			continue
		}
		_, plan, err := raftdirectory.PlanLeadership(ctx, g.stores[i], g.nodes[i], evidence)
		if errors.Is(err, raftcore.ErrTransfer) {
			g.drive(i, raftcore.Tick{})
			g.drain()
			continue
		}
		must(t, err)
		if plan.Configure {
			g.drive(i, raftcore.Configure{Change: plan.Change})
			g.drain()
		}
		if plan.Settle {
			r, e = g.Change(ctx, cs.AnchorActor(1), cs.Mutation{SettleLeadership: &cs.SettleLeadership{Revision: h.Revision}})
			must(t, e)
			if r.Code != cs.OK {
				t.Fatal(r)
			}
		}
	}
	h = registry(t, g, g.leader())
	if h.Leadership != nil {
		t.Fatal("R7 anchor restoration did not finish")
	}
	app, _, e := g.stores[g.leader()].ApplicationView(ctx)
	must(t, e)
	if len(app.ConfState.GetVoters()) != 3 || len(app.ConfState.GetLearners()) != 2 {
		t.Fatal("R9 two sleeping homes changed the default", app.ConfState)
	}
	for i, store := range g.stores[:3] {
		_, err := store.CreateSnapshot(ctx)
		must(t, err)
		recovered, err := store.Recover(ctx)
		must(t, err)
		if !slices.Equal(recovered.Core.Anchors, []uint64{1, 2, 3}) {
			t.Fatal("R7/R14 snapshot/restoration lost anchor ranks", i)
		}
	}
	g.absent[4], g.absent[5] = false, false
	g.drive(g.leader(), raftcore.Tick{})
	g.drain()
	if g.leader() == 3 || g.leader() == 4 {
		t.Fatal("R7 returning home automatically regained leadership")
	}

}

// R6/R16 refuse a target whose reply predates committed revocation. A target's
// local ready barrier cannot override the leader's newer authorization state.
func TestR6R16PromotionRechecksCommittedRevocation(t *testing.T) {
	g := personalGroup(t)
	h := registry(t, g, g.leader())
	device := cs.DeviceIDOfKey(key("paired-b").Public().(ed25519.PublicKey))
	membershipChange(t, g, cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: h.Revision, Node: 4, Device: device}})
	h = registry(t, g, g.leader())
	evidence, e := raftdirectory.DurableLeadershipEvidence(ctx, g.stores[3], 4, h.Revision, h.Leadership.Barrier)
	must(t, e)
	g.absent[4] = true
	r, e := g.Change(ctx, "a", cs.Mutation{Revoke: &cs.DeviceID{Device: device}})
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	_, plan, e := raftdirectory.PlanLeadership(ctx, g.stores[g.leader()], g.nodes[g.leader()], evidence)
	if !errors.Is(e, raftcore.ErrTransfer) || plan.Configure {
		t.Fatal("stale readiness admitted revoked target", plan, e)
	}
}
