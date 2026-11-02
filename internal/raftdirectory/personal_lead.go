package raftdirectory

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

// reconcileLeadership enacts only the committed policy. Every configuration
// entry finishes durably before another plan; fallback uses A+B, never P's
// consent or a connectivity-based forced voter removal.
func (g *Native) reconcileLeadership(ctx context.Context) (bool, error) {
	i, app, h, _, err := g.membership(ctx)
	if err != nil {
		return false, err
	}
	if h.Leadership == nil || g.leader() < 0 {
		return false, nil
	}
	l := h.Leadership
	_, plan, err := PlanLeadership(ctx, g.stores[i], g.nodes[i], raftcore.LeadershipEvidence{})
	if err == nil && plan.Fallback {
		_, err = g.controlChange(ctx, cs.Mutation{RestoreAnchors: &cs.RestoreAnchors{Revision: h.Revision}}, app.LogicalTime)
		return true, err
	}
	target := l.Node
	if l.Restore {
		target = l.Standby
	}
	var evidence raftcore.LeadershipEvidence
	if !h.StableTopology(raftcore.Configuration(app.ConfState)) || l.Settled && !l.Restore && !l.Completed {
		local := -1
		for j, id := range h.Anchors {
			if id == target {
				local = j
			}
		}
		if local >= 0 && g.stores[local] != nil {
			evidence, err = DurableLeadershipEvidence(ctx, g.stores[local], target, h.Revision, l.Barrier)
		} else {
			var reply clusterctl.Forward
			reply, err = g.forward(ctx, target, clusterctl.Forward{Action: "evidence", Revision: h.Revision, Barrier: l.Barrier})
			if err == nil && reply.Evidence != nil {
				evidence = *reply.Evidence
			} else {
				err = raftcore.ErrTransfer
			}
		}
		if err != nil {
			return false, nil
		}
	}
	if g.leader() != i {
		return true, nil
	}
	_, plan, err = PlanLeadership(ctx, g.stores[i], g.nodes[i], evidence)
	if errors.Is(err, raftcore.ErrBusy) || errors.Is(err, raftcore.ErrTransfer) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if plan.Configure {
		before := g.status[i].Applied
		if err = g.drive(ctx, i, raftcore.Configure{Change: plan.Change}); err != nil {
			return false, err
		}
		if err = g.drain(ctx); err != nil {
			return false, err
		}
		for g.status[i].Applied <= before {
			if err = g.waitProgress(ctx); err != nil {
				return false, err
			}
		}
		return true, g.installPeers(ctx)
	}
	if plan.Settle {
		_, err = g.controlChange(ctx, cs.Mutation{SettleLeadership: &cs.SettleLeadership{Revision: h.Revision}}, app.LogicalTime)
		return true, err
	}
	if plan.Transfer != 0 {
		if err = g.drive(ctx, i, raftcore.Transfer{Target: plan.Transfer}); err != nil {
			return false, err
		}
		return false, g.drain(ctx)
	}
	return false, nil
}

func (g *Native) PersonalLead(ctx context.Context, c cs.Command, certificate string) (cs.LeadershipReceipt, error) {
	var empty cs.LeadershipReceipt
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	i, app, err := g.barrier(ctx)
	if err != nil {
		return empty, err
	}
	read := recordReader(ctx, g.stores[i], app)
	raw, err := read(cs.IdentityKey())
	if err != nil {
		return empty, err
	}
	var identity cs.IdentityRec
	if cs.Decode(raw, &identity) != nil || cs.PersonalCertificate(identity, c.Actor, certificate) != nil {
		return empty, directory.ErrUnauthorized
	}
	raw, err = read(cs.MemberDeviceKey(c.Actor))
	if err != nil {
		return empty, err
	}
	var mapping cs.DeviceMember
	if raw == nil || cs.Decode(raw, &mapping) != nil || mapping.Node == 0 {
		return empty, clusterctl.ErrUnsupported
	}
	r, err := g.submit(ctx, c)
	if err != nil {
		return empty, err
	}
	if r.Code != cs.OK {
		return empty, clusterctl.CommittedRefusal{Code: r.Code, Sequence: c.Sequence}
	}
	for ctx.Err() == nil {
		_, _, h, _, err := g.membership(ctx)
		if err != nil {
			return empty, err
		}
		l := h.Leadership
		if l == nil || l.Restore || l.Node != c.Change.RequestLeadership.Node {
			return empty, clusterctl.CommittedRefusal{Code: cs.Conflict, Sequence: c.Sequence}
		}
		if g.leaderID() == l.Node && g.leader() < 0 && l.Settled {
			// Completion persists before the FINAL fresh current-term receipt. A retry
			// of this exact request performs another read instead of reusing a hint.
			if !l.Completed {
				reply, e := g.forward(ctx, l.Node, clusterctl.Forward{Action: "receipt"})
				if e != nil || reply.Receipt == nil {
					return empty, directory.ErrUnreachable
				}
				if _, _, e = g.appliedReplica(ctx, reply.Barrier); e != nil {
					return empty, e
				}
				if _, e = g.controlChange(ctx, cs.Mutation{CompleteLeadership: &cs.CompleteLeadership{Revision: h.Revision}}, app.LogicalTime); e != nil {
					return empty, e
				}
			}
			reply, e := g.forward(ctx, l.Node, clusterctl.Forward{Action: "receipt"})
			if e != nil || reply.Receipt == nil || reply.Receipt.Node != l.Node || reply.Receipt.Revision != h.Revision {
				return empty, directory.ErrUnreachable
			}
			if _, _, e = g.appliedReplica(ctx, reply.Barrier); e != nil {
				return empty, e
			}
			return *reply.Receipt, nil
		}
		before := g.changed
		changed, err := g.reconcileLeadership(ctx)
		if err != nil {
			return empty, err
		}
		if !changed && before == g.changed {
			if err = g.waitProgress(ctx); err != nil {
				return empty, err
			}
		}
	}
	return empty, directory.ErrUnreachable
}
