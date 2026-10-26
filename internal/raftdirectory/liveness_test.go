package raftdirectory_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// R22/R26 use the real three-native-store application adapter. Renew receipts
// escape only after durable commit; markers and exact expiry survive snapshots.
func TestR22R26NativeCommittedLivenessAndSleep(t *testing.T) {
	now := int64(1_000_000)
	extra := raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Enrollment, Key: cs.AuthorityKey("anchor").Key}, Value: encoded(t, cs.Authority{Cluster: cluster, Key: key("anchor").Public().(ed25519.PublicKey), Anchor: true})}
	g := newGroup(t, func() time.Time { return time.UnixMilli(now) }, extra)
	r, e := g.Change(ctx, "a", cs.Mutation{Create: &cs.CreateCell{Cell: "chat", V1: true, Init: cs.CellInit{Head: "sealed"}}})
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	effects := raftdirectory.Effects{Host: &effectHost{g: g}, Cluster: cluster, Actor: "effects", Key: key("effects")}
	now += 30_000
	receipts, e := effects.Renew(ctx, now, cs.RenewHolds{Device: "a", SeenAt: now, Items: []cs.HoldFence{{Cell: "chat", Fence: 1}}})
	must(t, e)
	if len(receipts) != 1 || receipts[0].Index == 0 || receipts[0].Until != now+90_000 {
		t.Fatal(receipts)
	}
	g.blocked = true
	if _, err := effects.Renew(ctx, now+30_000, cs.RenewHolds{Device: "a", SeenAt: now + 30_000, Items: []cs.HoldFence{{Cell: "chat", Fence: 1}}}); err == nil {
		t.Fatal("R22 disconnected leader returned renewal")
	}
	g.blocked = false
	g.drive(g.leader(), raftcore.Tick{})
	g.drain()
	before := g.status[g.leader()].Applied
	receipts, e = effects.Renew(ctx, now, cs.RenewHolds{Device: "a", SeenAt: now, Items: []cs.HoldFence{{Cell: "chat", Fence: 1}}})
	must(t, e)
	if len(receipts) != 0 || g.status[g.leader()].Applied != before {
		t.Fatal("R22 covered sample wrote another entry")
	}
	r, e = g.Change(ctx, "a", cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 1}})
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	receipts, e = effects.Renew(ctx, now+1, cs.RenewHolds{Device: "a", SeenAt: now, Items: []cs.HoldFence{{Cell: "chat", Fence: 1}}})
	must(t, e)
	if len(receipts) != 0 {
		t.Fatal("R22 delayed release renewal", receipts)
	}
	i, app, e := g.barrier("a")
	must(t, e)
	for node := range g.nodes {
		_, e = raftdirectory.Schedule(ctx, g.stores[node], g.nodes[node], 0, now, false, nil)
		must(t, e)
	}
	r, e = g.Change(ctx, "anchor", cs.Mutation{PrepareQuiescence: &cs.PrepareQuiescence{Barrier: app.Applied, Term: g.status[i].Term, Voters: []uint64{1, 2, 3}}})
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	g.drive(g.leader(), raftcore.Tick{})
	g.drain()
	i = g.leader()
	index := g.status[i].Applied
	term := g.status[i].Term
	acks := []raftcore.SleepAck{{Node: 1, Index: index, Term: term}, {Node: 2, Index: index, Term: term}, {Node: 3, Index: index, Term: term}}
	for node := range g.nodes {
		p, e := raftdirectory.Schedule(ctx, g.stores[node], g.nodes[node], 60_000, now, false, acks)
		must(t, e)
		if !p.Sleeping {
			t.Fatal("R26 durable native marker did not sleep", node, p)
		}
	}
	// Actual SQLite snapshot/reopen must never restore serving leader/sleep state.
	restartEffects(t, g)
	for node := range g.nodes {
		p, e := raftdirectory.Schedule(ctx, g.stores[node], g.nodes[node], 0, now, false, acks)
		must(t, e)
		if p.Sleeping {
			t.Fatal("R26 recovered sleep generation")
		}
	}
	v, e := g.ReadCell(ctx, "a", "chat")
	must(t, e)
	if v.Cell.Lease.Expires != 0 || v.Cell.Lease.Fence != 1 {
		t.Fatal("R22 released state changed", v)
	}
}

// R26/R27 a committed undelivered pairing result is scheduled work. Even a
// stale host's final sleep certificate cannot suspend its delivery timer.
func TestR26R27NativePendingOutboxKeepsTimerActive(t *testing.T) {
	decision := cs.JoinDecision{Code: "fixture", Deny: true}
	extra := []raftstore.Write{
		{Key: raftstore.Key{Namespace: raftstore.Enrollment, Key: cs.AuthorityKey("anchor").Key}, Value: encoded(t, cs.Authority{Cluster: cluster, Key: key("anchor").Public().(ed25519.PublicKey), Anchor: true})},
		{Key: raftstore.Key{Namespace: raftstore.Outbox, Key: cs.JoinKey("fixture").Key}, Value: encoded(t, cs.JoinRequest{Device: "a", Decision: &decision})},
	}
	g := newGroup(t, func() time.Time { return time.UnixMilli(1000) }, extra...)
	_, e := g.Change(ctx, "a", cs.Mutation{AdvanceTime: &cs.AdvanceTime{}})
	must(t, e)
	i := g.leader()
	barrier := g.status[i].Applied
	term := g.status[i].Term
	r, e := g.Change(ctx, "anchor", cs.Mutation{PrepareQuiescence: &cs.PrepareQuiescence{Barrier: barrier, Term: term, Voters: []uint64{1, 2, 3}}})
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	g.drive(i, raftcore.Tick{})
	g.drain()
	index := g.status[i].Applied
	acks := []raftcore.SleepAck{{Node: 1, Index: index, Term: term}, {Node: 2, Index: index, Term: term}, {Node: 3, Index: index, Term: term}}
	for node := range g.nodes {
		p, e := raftdirectory.Schedule(ctx, g.stores[node], g.nodes[node], 60_000, 1000, false, acks)
		must(t, e)
		if p.Sleeping || p.Prepare {
			t.Fatal("R26 pending committed delivery slept", node, p)
		}
	}
}
