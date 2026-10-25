package raftdirectory

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// PersonalStatus binds the original certificate to CURRENT committed identity,
// revocation and membership under the same actor barrier. Pairing alone creates
// no member: an absent device mapping remains Self=0 rather than a guessed ID.
func (g *Native) PersonalStatus(ctx context.Context, actor, certificate string) (out clusterctl.Status, err error) {
	err = g.Inspect(ctx, func(ctx context.Context, store *raftstore.SQLite, app raftstore.Application) error {
		read := func(k cs.RecordKey) ([]byte, error) {
			key := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
			rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{key})
			return rows[key], err
		}
		var identity cs.IdentityRec
		raw, err := read(cs.IdentityKey())
		if err != nil {
			return err
		}
		if cs.Decode(raw, &identity) != nil || cs.PersonalCertificate(identity, actor, certificate) != nil {
			return directory.ErrUnauthorized
		}
		raw, err = read(cs.DeviceKey(actor))
		if err != nil {
			return err
		}
		if raw != nil {
			var device cs.Device
			if cs.Decode(raw, &device) != nil || device.Revoked {
				return directory.ErrUnauthorized
			}
		}
		raw, err = read(cs.AuthorityKey(actor))
		if err != nil {
			return err
		}
		if raw != nil {
			var authority cs.Authority
			if cs.Decode(raw, &authority) != nil || !authority.ValidRole() || !authority.Personal || authority.Cluster != g.manifest.Config.Cluster {
				return directory.ErrUnauthorized
			}
		}
		conf := raftcore.Configuration(app.ConfState)
		membership, _, err := cs.MembershipView(read, conf, app.Applied)
		if err != nil || membership.Revision == 0 {
			return cs.ErrDependency
		}
		out = clusterctl.Status{Cluster: g.manifest.Config.Cluster, Leader: g.leaderID(), Membership: membership, Configuration: conf, LeadershipUnavailable: true}
		if g.peers != nil {
			for _, id := range conf.Learners {
				raw, err := read(cs.MemberKey(id))
				if err != nil {
					return err
				}
				var record cs.MemberRecord
				if cs.Decode(raw, &record) != nil {
					return cs.ErrDependency
				}
				if peer, ok := g.peer(id); ok && !peer.Revoked && cs.LearnerReady(record, conf, g.peers.applied[id], record.ReadyIndex) {
					out.Ready = append(out.Ready, id)
				}
			}
		}
		raw, err = read(cs.MemberDeviceKey(actor))
		if err != nil || raw == nil {
			return err
		}
		var mapping cs.DeviceMember
		if cs.Decode(raw, &mapping) != nil {
			return cs.ErrDependency
		}
		if mapping.Node == 0 {
			return nil
		}
		raw, err = read(cs.MemberKey(mapping.Node))
		if err != nil {
			return err
		}
		var member cs.MemberRecord
		if cs.Decode(raw, &member) != nil || member.Node != mapping.Node || member.Device != actor || member.Anchor || member.Worker {
			return cs.ErrDependency
		}
		if !member.Retired {

			out.Self = member.Node
			out.LeadershipUnavailable = false
		}
		return nil
	})
	if err != nil {
		return clusterctl.Status{}, err
	}
	return out, nil
}
