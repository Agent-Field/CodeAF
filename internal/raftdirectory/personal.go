package raftdirectory

import (
	"context"
	"crypto/ed25519"
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
	h, _, err := clusterstate.MembershipView(read, raftcore.Configuration(app.ConfState), app.Applied)
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
	_, app, h, read, err := g.membership(ctx)
	if err != nil {
		return out, err
	}
	var identity clusterstate.IdentityRec
	raw, err := read(clusterstate.IdentityKey())
	if err != nil || clusterstate.Decode(raw, &identity) != nil {
		return out, clusterstate.ErrDependency
	}
	at, _ := strconv.ParseInt(proof.At, 10, 64)
	now := max(at, app.LogicalTime)
	request, granted, actor, err := clusterstate.VerifyPersonalAdmission(proof, identity, now)
	if err != nil {
		return out, directory.ErrUnauthorized
	}
	var device clusterstate.Device
	raw, err = read(clusterstate.DeviceKey(actor))
	if err != nil {
		return out, err
	}
	if raw != nil && (clusterstate.Decode(raw, &device) != nil || device.Revoked) {
		return out, directory.ErrUnauthorized
	}
	var authority clusterstate.Authority
	raw, err = read(clusterstate.AuthorityKey(actor))
	if err != nil {
		return out, err
	}
	if raw != nil && (clusterstate.Decode(raw, &authority) != nil || !authority.ValidRole() || !authority.Personal || authority.Cluster != g.manifest.Config.Cluster || !ed25519.PublicKey(authority.Key).Equal(ed25519.PublicKey(granted.Key))) {
		return out, directory.ErrUnauthorized
	}
	if request.Protocol != raftcore.ProtocolVersion {
		return out, EnrollmentRefusal{clusterstate.UpdateRequired}
	}
	var mapping clusterstate.DeviceMember
	raw, err = read(clusterstate.MemberStoreKey(actor, request.Store))
	if err != nil {
		return out, err
	}
	if raw != nil {
		if clusterstate.Decode(raw, &mapping) != nil {
			return out, clusterstate.ErrDependency
		}
	} else {
		if len(h.Nodes) >= int(h.Limit) {
			return out, EnrollmentRefusal{clusterstate.Capacity}
		}
		raw, err = read(clusterstate.MemberDeviceKey(actor))
		if err != nil {
			return out, err
		}
		if raw != nil && clusterstate.Decode(raw, &mapping) != nil {
			return out, clusterstate.ErrDependency
		}
		if mapping.Node != 0 {
			if _, err = g.controlChange(ctx, clusterstate.Mutation{RemoveMember: &clusterstate.RemoveMember{Node: mapping.Node, Device: actor, Revision: h.Revision, Admission: &proof}}, now); err != nil {
				return out, err
			}
			if err = g.reconcileMembership(ctx); err != nil {
				return out, err
			}
			_, app, h, read, err = g.membership(ctx)
			if err != nil {
				return out, err
			}
		}
		r, err := g.controlChange(ctx, clusterstate.Mutation{EnrollMember: &clusterstate.EnrollMember{Node: h.NextNode, Revision: h.Revision, Device: actor, Protocol: request.Protocol, Store: request.Store, Admission: &proof}}, now)
		if err != nil {
			return out, err
		}
		mapping.Node = r.Membership.Node
	}
	// Configuration is a separate maintenance stage. The enrollment reply can
	// survive a crash with Barrier=0; activation repairs that committed intent.
	_, app, _, read, err = g.membership(ctx)
	if err != nil {
		return out, err
	}
	raw, err = read(clusterstate.MemberKey(mapping.Node))
	if err != nil {
		return out, err
	}
	var member clusterstate.MemberRecord
	if clusterstate.Decode(raw, &member) != nil || member.Device != actor || member.Store != request.Store || member.Retired || member.Worker || member.Anchor {
		return out, directory.ErrUnauthorized
	}
	out = PersonalEnrollment{Scope: g.peers.view.Scope, Generation: g.peers.view.Generation, Node: member.Node, Incarnation: member.Incarnation, Barrier: member.ReadyIndex, Members: g.peers.view.Members}
	return out, nil
}
