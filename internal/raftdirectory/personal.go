package raftdirectory

import (
	"context"
	"errors"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
)

// PersonalEnrollment advertises a committed barrier, never a readiness claim.
// A returning store receives the same node, incarnation and protocol authority.
type PersonalEnrollment struct {
	Scope       rafttransport.Scope    `json:"scope"`
	Generation  uint64                 `json:"generation,string"`
	Node        uint64                 `json:"node,string"`
	Incarnation uint64                 `json:"incarnation,string"`
	Barrier     uint64                 `json:"barrier,string"`
	Members     []rafttransport.Member `json:"members"`
}

// EnrollmentRefusal carries the shared manual sentence without inventing a
// different capacity or protocol limit at this HTTP boundary.
type EnrollmentRefusal struct{ Code clusterstate.Code }

func (e EnrollmentRefusal) Error() string { return clusterstate.EnrollmentMessage(e.Code) }

func recordReader(ctx context.Context, s *raftstore.SQLite, app raftstore.Application) clusterstate.ReadRecord {
	return func(k clusterstate.RecordKey) ([]byte, error) {
		key := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
		rows, _, err := s.Read(ctx, app.Applied, []raftstore.Key{key})
		return rows[key], err
	}
}
func (g *Native) membership(ctx context.Context) (int, raftstore.Application, clusterstate.Membership, clusterstate.ReadRecord, error) {
	i := g.replica()
	if i < 0 {
		return i, raftstore.Application{}, clusterstate.Membership{}, nil, directory.ErrUnreachable
	}
	app, _, err := g.stores[i].ApplicationView(ctx)
	if err != nil {
		return i, app, clusterstate.Membership{}, nil, err
	}
	read := recordReader(ctx, g.stores[i], app)
	// Membership planning needs the registry. Routing separately validates
	// each peer before installing its authority at this applied index.
	var h clusterstate.Membership
	raw, err := read(clusterstate.MembershipKey())
	if err == nil && raw != nil && (clusterstate.Decode(raw, &h) != nil || h.Validate() != nil || h.Index > app.Applied) {
		err = clusterstate.ErrDependency
	}
	return i, app, h, read, err
}
func (g *Native) controlChange(ctx context.Context, change clusterstate.Mutation, now int64) (clusterstate.Result, error) {
	_, app, _, read, err := g.membership(ctx)
	if err != nil {
		return clusterstate.Result{}, err
	}
	actor := clusterstate.AnchorActor(g.manifest.Config.Anchors[0])
	raw, err := read(clusterstate.SessionKey(actor, "member-maintenance"))
	if err != nil {
		return clusterstate.Result{}, err
	}
	seq, ack, err := clusterstate.NextSession(raw)
	if err != nil {
		return clusterstate.Result{}, err
	}
	c, err := clusterstate.Sign(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: g.manifest.Config.Cluster, Actor: actor, Session: "member-maintenance", Sequence: seq, Ack: ack, Now: max(now, app.LogicalTime), Change: change}, g.peers.keys[0])
	if err != nil {
		return clusterstate.Result{}, err
	}
	r, err := g.submit(ctx, c)
	if err == nil && r.Code != clusterstate.OK {
		err = EnrollmentRefusal{r.Code}
		if err.Error() == "" {
			err = directory.ResultError(r.Code)
		}
	}
	return r, err
}

// reconcileMembership runs at activation and maintenance, independently of a
// retrying client. Registry intent alone is enough to finish either stage.
func (g *Native) reconcileMembership(ctx context.Context) error {
	for stage := 0; stage < 3; stage++ {
		i, app, h, _, err := g.membership(ctx)
		if err != nil {
			return err
		}
		if h.Pending == nil {
			return g.installPeers(ctx)
		}
		var plan raftcore.MembershipPlan
		if g.leader() < 0 {
			reply, e := g.forward(ctx, g.leaderID(), clusterctl.Forward{Action: "membership"})
			if reply.Membership == nil {
				return directory.ErrUnreachable
			}
			if e != nil {
				return nil
			}
			plan = *reply.Membership
			if len(plan.Change) > 0 {
				return nil
			}
		} else {
			plan, err = g.nodes[i].PlanMembership(h)
		}
		if errors.Is(err, raftcore.ErrBusy) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(plan.Change) > 0 {
			if err = g.drive(ctx, i, raftcore.Configure{Change: plan.Change}); err != nil {
				return err
			}
			if err = g.drain(ctx); err != nil {
				return err
			}
			continue
		}
		if !plan.Settle {
			return nil
		}
		if _, err = g.controlChange(ctx, clusterstate.Mutation{SettleMembership: &clusterstate.SettleMembership{Revision: h.Revision, Node: h.Pending.Node}}, app.LogicalTime); err != nil {
			return err
		}
		if plan.Checkpoint && g.leader() >= 0 {
			p, err := g.stores[i].CreateSnapshot(ctx)
			if err != nil {
				return err
			}
			if err = g.stores[i].Compact(ctx, p.GetMetadata().GetIndex()); err != nil {
				return err
			}
			if err = g.drive(ctx, i, raftcore.CompactSnapshot{Snapshot: p}); err != nil {
				return err
			}
		}
	}
	return nil
}

// EnrollPersonal serializes allocation, replacement and the original signed
// proof with all other member input. No public call waits on the learner.
func (g *Native) EnrollPersonal(ctx context.Context, proof clusterstate.V1Request) (PersonalEnrollment, error) {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	ctx, cancel := boundedCall(ctx)
	defer cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	var out PersonalEnrollment
	if g.peers == nil {
		return out, directory.ErrUnreachable
	}
	if _, _, err := g.barrier(ctx); err != nil {
		return out, err
	}
	if err := g.reconcileMembership(ctx); err != nil {
		return out, err
	}
	at, _ := strconv.ParseInt(proof.At, 10, 64)
	var node uint64
	for stage := 0; stage < 3; stage++ {
		_, app, _, read, err := g.membership(ctx)
		if err != nil {
			return out, err
		}
		plan, err := clusterstate.PlanPersonalAdmission(g.manifest.Config.Cluster, proof, max(at, app.LogicalTime), read)
		if err != nil {
			return out, err
		}
		if plan.Code != clusterstate.OK {
			if plan.Code == clusterstate.Capacity || plan.Code == clusterstate.UpdateRequired {
				return out, EnrollmentRefusal{plan.Code}
			}
			return out, directory.ErrUnauthorized
		}
		node = plan.Node
		if plan.Change == nil {
			break
		}
		result, err := g.controlChange(ctx, *plan.Change, max(at, app.LogicalTime))
		if err != nil {
			return out, err
		}
		node = result.Membership.Node
		if plan.Change.EnrollMember != nil {
			break
		}
		if err = g.reconcileMembership(ctx); err != nil {
			return out, err
		}
	}
	// Configuration is a separate maintenance stage. The enrollment reply can
	// survive a crash with Barrier=0; activation repairs that committed intent.
	_, _, _, read, err := g.membership(ctx)
	if err != nil {
		return out, err
	}
	raw, err := read(clusterstate.MemberKey(node))
	if err != nil {
		return out, err
	}
	var member clusterstate.MemberRecord
	if clusterstate.Decode(raw, &member) != nil || member.Retired || member.Worker || member.Anchor {
		return out, directory.ErrUnauthorized
	}
	out = PersonalEnrollment{Scope: g.peers.view.Scope, Generation: g.peers.view.Generation, Node: member.Node, Incarnation: member.Incarnation, Barrier: member.ReadyIndex, Members: g.peers.view.Members}
	return out, nil
}
