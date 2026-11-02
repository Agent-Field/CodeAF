package raftdirectory

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"slices"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// InitializeMembership records the provisioned manifest's anchors through the
// ordinary log. It never edits a voter list or invents missing authoritative
// stores. Existing committed registry rows are left intact on every activation.
func (g *Native) InitializeMembership(ctx context.Context, key ed25519.PrivateKey) error {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	i, app, err := g.barrier(ctx)
	if err != nil {
		return err
	}
	k := raftstore.Key{Namespace: raftstore.Membership, Key: cs.MembershipKey().Key}
	rows, _, err := g.stores[i].Read(ctx, app.Applied, []raftstore.Key{k})
	if err != nil {
		return err
	}
	if rows[k] != nil {
		return nil
	}
	if len(g.manifest.Config.AnchorKeys) != len(g.manifest.Config.Anchors) {
		return cs.ErrMigration
	}
	anchors := make([]cs.MemberRecord, len(g.manifest.Config.Anchors))
	for j, id := range g.manifest.Config.Anchors {
		anchors[j] = cs.MemberRecord{Node: id, Key: g.manifest.Config.AnchorKeys[j], Incarnation: 1, Protocol: raftcore.ProtocolVersion, Anchor: true}
	}
	c, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: g.manifest.Config.Cluster, Actor: cs.AnchorActor(g.manifest.Config.Anchors[0]), Session: "initialize-membership", Sequence: 1, Now: app.LogicalTime, Change: cs.Mutation{InitMembership: &cs.InitMembership{Anchors: anchors, Limit: cs.MaxMemberRecords}}}, key)
	if err != nil {
		return err
	}
	r, err := g.submit(ctx, c)
	if err != nil {
		return err
	}
	return directory.ResultError(r.Code)
}

// Maintain connects the production host to the shared scheduler. Every final
// sleep acknowledgment is a local durable marker, and an incomplete delivery
// keeps ordinary ticks. Reads still need their own fresh quorum on wake.
func (g *Native) Maintain(ctx context.Context, elapsed, now int64, key ed25519.PrivateKey) (int64, error) {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	after := cs.TickEveryMillis
	// Election ticks must run before requiring an authority read. A missing
	// leader is exactly when the surviving followers need this clock.
	plans := make([]raftcore.SchedulePlan, len(g.nodes))
	for i, node := range g.nodes {
		if node == nil {
			continue
		}
		p, err := Schedule(ctx, g.stores[i], node, elapsed, now, false, nil)
		if err != nil {
			return after, err
		}
		plans[i] = p
		if p.Tick {
			if err = g.drive(ctx, i, raftcore.Tick{}); err != nil {
				return after, err
			}
		}
	}
	if err := g.drain(ctx); err != nil {
		return after, err
	}
	leader := g.leader()
	remote := uint64(0)
	var remotePlan raftcore.SchedulePlan
	var remoteStatus clusterctl.ForwardStatus
	var remoteAck *raftcore.SleepAck
	scheduleRemote := func(acks []raftcore.SleepAck) (raftcore.SchedulePlan, error) {
		reply, err := g.forward(ctx, remote, clusterctl.Forward{Action: "schedule", Now: now, Schedule: &raftcore.ScheduleInput{Now: elapsed, Acks: acks}})
		if err != nil || reply.Plan == nil {
			return raftcore.SchedulePlan{}, directory.ErrUnreachable
		}
		remoteStatus = reply.Status
		remoteAck = reply.SleepAck
		return *reply.Plan, nil
	}
	if leader < 0 {
		remote = g.leaderID()
		leader = g.replica()
		if remote == 0 || leader < 0 || g.forwarders[remote] == nil {
			return after, directory.ErrUnreachable
		}
		var err error
		remotePlan, err = scheduleRemote(nil)
		if err != nil {
			return after, err
		}
	}

	if g.peers != nil {
		g.peers.now = max(g.peers.now, now)
		if err := g.reconcileMembership(ctx); err != nil {
			return after, err
		}
		if _, err := g.reconcileLeadership(ctx); err != nil {
			return after, err
		}
	}
	plan := plans[leader]
	if remote != 0 {
		plan = remotePlan
	}
	if plan.Barrier {
		id := fmt.Sprintf("idle-%d", g.serial)
		g.serial++
		if remote != 0 {
			reply, err := g.forward(ctx, remote, clusterctl.Forward{Action: "idle-read", ID: id})
			if err != nil || reply.Barrier == 0 {
				return after, directory.ErrUnreachable
			}
			if _, _, err = g.appliedReplica(ctx, reply.Barrier); err != nil {
				return after, err
			}
			plan, err = scheduleRemote(nil)
			if err != nil {
				return after, err
			}
		} else {
			if err := g.drive(ctx, leader, raftcore.IdleRead{RequestID: id}); err != nil {
				return after, err
			}
			if err := g.drain(ctx); err != nil {
				return after, err
			}
			ok := g.reads[id]
			delete(g.reads, id)
			if !ok {
				return after, directory.ErrUnreachable
			}
			var err error
			plan, err = Schedule(ctx, g.stores[leader], g.nodes[leader], elapsed, now, false, nil)
			if err != nil {
				return after, err
			}
		}
	}
	if plan.Prepare {
		app, _, err := g.stores[leader].ApplicationView(ctx)
		if err != nil {
			return after, err
		}
		c, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: g.manifest.Config.Cluster, Actor: cs.AnchorActor(g.manifest.Config.Anchors[0]), Session: "sleep", Sequence: 1, Now: now, Change: cs.Mutation{PrepareQuiescence: &cs.PrepareQuiescence{Barrier: app.Applied, Term: func() uint64 {
			if remote != 0 {
				return remoteStatus.Term
			}
			return g.status[leader].Term
		}(), Voters: raftcore.Configuration(app.ConfState).Voters}}}, key)
		if err != nil {
			return after, err
		}
		sk := cs.SessionKey(c.Actor, c.Session)
		keys := []raftstore.Key{{Namespace: raftstore.Namespace(sk.Namespace), Key: sk.Key}}
		records, _, err := g.stores[leader].Read(ctx, app.Applied, keys)
		if err != nil {
			return after, err
		}
		c.Sequence, c.Ack, err = cs.NextSession(records[keys[0]])
		if err != nil {
			return after, err
		}
		c, err = cs.Sign(c, key)
		if err != nil {
			return after, err
		}
		r, err := g.submit(ctx, c)
		if err != nil {
			return after, err
		}
		if err = directory.ResultError(r.Code); err != nil {
			return after, err
		}
		// Deliver the committed marker to every reachable follower before collecting
		// durable acknowledgments. Missing voters remain active and prevent sleep.
		if remote == 0 {
			if err = g.drive(ctx, leader, raftcore.Tick{}); err != nil {
				return after, err
			}
		}
		if err = g.drain(ctx); err != nil {
			return after, err
		}
	}
	acks := []raftcore.SleepAck{}
	app, _, err := g.stores[leader].ApplicationView(ctx)
	if err != nil {
		return after, err
	}
	voters := raftcore.Configuration(app.ConfState).Voters
	for i, store := range g.stores {
		if store == nil || !slices.Contains(voters, g.manifest.Config.Anchors[i]) {
			continue
		}
		app, _, err := store.ApplicationView(ctx)
		if err != nil {
			return after, err
		}
		k := raftstore.Key{Namespace: raftstore.Membership, Key: cs.QuiescenceKey().Key}
		rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{k})
		if err != nil {
			return after, err
		}
		var m cs.Quiescence
		if rows[k] != nil && cs.Decode(rows[k], &m) == nil && app.Applied == m.Index && g.status[i].Term == m.Term {
			acks = append(acks, raftcore.SleepAck{Node: g.manifest.Config.Anchors[i], Index: m.Index, Term: m.Term})
		}
	}
	if remote != 0 {
		if _, err := scheduleRemote(nil); err != nil {
			return after, err
		}
		if remoteAck != nil {
			acks = append(acks, *remoteAck)
		}
	}
	allSleep := true
	if remote != 0 {
		p, err := scheduleRemote(acks)
		if err != nil {
			return after, err
		}
		allSleep = p.Sleeping
	}
	for i, node := range g.nodes {
		if node == nil {
			allSleep = false
			continue
		}
		p, err := Schedule(ctx, g.stores[i], node, elapsed, now, false, acks)
		if err != nil {
			return after, err
		}
		allSleep = allSleep && p.Sleeping
	}
	if allSleep {
		if g.peers != nil {
			if err := g.quiescePeers(ctx); err != nil {
				return after, err
			}
		}
		return 0, nil
	}
	return after, nil
}

// PendingEffects is only a timer hint. Actual delivery still uses Effects.Replay
// and its fresh authorization barrier; an idle scan grants no capability.
func (g *Native) PendingEffects(ctx context.Context) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.replica()
	if i < 0 {
		return false, directory.ErrUnreachable
	}
	app, _, err := g.stores[i].ApplicationView(ctx)
	if err != nil {
		return false, err
	}
	after := ""
	for {
		rows, _, err := g.stores[i].ScanPage(ctx, app.Applied, raftstore.Outbox, after)
		if err != nil {
			return false, err
		}
		if len(rows) == 0 {
			return false, nil
		}
		for _, row := range rows {
			active, err := cs.RecordActive(cs.RecordKey{Namespace: string(row.Namespace), Key: row.Key.Key}, row.Value, app.LogicalTime)
			if err != nil {
				return false, err
			}
			if active {
				return true, nil
			}
			after = row.Key.Key
		}
	}
}
