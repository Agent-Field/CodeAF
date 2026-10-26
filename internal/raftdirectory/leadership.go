package raftdirectory

import (
	"context"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// LeadershipState is called under the native actor lock. A public status read
// first passes its ordinary authorization and current-term quorum barrier;
// this helper never opens content storage or supplies a local-success route.
func LeadershipState(ctx context.Context, store *raftstore.SQLite) (cs.Membership, error) {
	app, _, err := store.ApplicationView(ctx)
	if err != nil {
		return cs.Membership{}, err
	}
	k := raftstore.Key{Namespace: raftstore.Membership, Key: cs.MembershipKey().Key}
	rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{k})
	if err != nil {
		return cs.Membership{}, err
	}
	var h cs.Membership
	if cs.Decode(rows[k], &h) != nil || h.Validate() != nil || h.Index > app.Applied {
		return h, cs.ErrDependency
	}
	return h, nil
}

// DurableLeadershipEvidence is an authenticated target response. The caller
// binds it to that member's transport generation and the requested revision;
// the leader separately checks actual Raft acknowledgment progress. A diskless
// host cannot call this with a store and therefore cannot qualify as a voter.
func DurableLeadershipEvidence(ctx context.Context, store *raftstore.SQLite, node, revision, barrier uint64) (raftcore.LeadershipEvidence, error) {
	if store.MemberIdentity().NodeID != node {
		return raftcore.LeadershipEvidence{}, raftcore.ErrTransfer
	}
	app, _, err := store.ApplicationView(ctx)
	if err != nil {
		return raftcore.LeadershipEvidence{}, err
	}
	h, err := LeadershipState(ctx, store)
	if err != nil {
		return raftcore.LeadershipEvidence{}, err
	}
	l := h.Leadership
	if l == nil || h.Revision != revision || app.Applied < barrier || l.Barrier != barrier {
		return raftcore.LeadershipEvidence{}, raftcore.ErrTransfer
	}
	k := raftstore.Key{Namespace: raftstore.Enrollment, Key: cs.MemberKey(node).Key}
	rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{k})
	if err != nil {
		return raftcore.LeadershipEvidence{}, err
	}
	var m cs.MemberRecord
	if cs.Decode(rows[k], &m) != nil || m.Node != node || m.Retired || m.Protocol != raftcore.ProtocolVersion {
		return raftcore.LeadershipEvidence{}, raftcore.ErrTransfer
	}
	if m.Device != "" {
		k := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(m.Device).Key}
		rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{k})
		if err != nil {
			return raftcore.LeadershipEvidence{}, err
		}
		if rows[k] != nil {
			var d cs.Device
			if cs.Decode(rows[k], &d) != nil || d.Revoked {
				return raftcore.LeadershipEvidence{}, raftcore.ErrTransfer
			}
		}
	}
	return raftcore.LeadershipEvidence{Node: node, Revision: revision, Applied: app.Applied, Protocol: m.Protocol, Durable: true}, nil
}

// PlanLeadership rechecks the leader's committed authorization, independently
// of a previously authenticated target reply. Revocation may commit after that
// reply was produced; a lagging target cannot vouch its old authority back in.
func PlanLeadership(ctx context.Context, store *raftstore.SQLite, core *raftcore.Core, evidence raftcore.LeadershipEvidence) (cs.Membership, raftcore.LeadershipPlan, error) {
	h, err := LeadershipState(ctx, store)
	if err != nil {
		return h, raftcore.LeadershipPlan{}, err
	}
	if evidence.Node != 0 {
		app, _, err := store.ApplicationView(ctx)
		if err != nil {
			return h, raftcore.LeadershipPlan{}, err
		}
		k := raftstore.Key{Namespace: raftstore.Enrollment, Key: cs.MemberKey(evidence.Node).Key}
		rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{k})
		if err != nil {
			return h, raftcore.LeadershipPlan{}, err
		}
		var member cs.MemberRecord
		if cs.Decode(rows[k], &member) != nil || member.Node != evidence.Node {
			return h, raftcore.LeadershipPlan{}, raftcore.ErrTransfer
		}
		var device []byte
		if member.Device != "" {
			k = raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(member.Device).Key}
			rows, _, err = store.Read(ctx, app.Applied, []raftstore.Key{k})
			if err != nil {
				return h, raftcore.LeadershipPlan{}, err
			}
			device = rows[k]
		}
		peer, err := cs.PeerFromRecord(member, device, raftcore.Configuration(app.ConfState))
		if err != nil || peer.Revoked {
			return h, raftcore.LeadershipPlan{}, raftcore.ErrTransfer
		}
	}
	plan, err := core.PlanLeadership(h, evidence)
	return h, plan, err
}
