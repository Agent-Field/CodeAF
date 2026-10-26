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
		progress := map[uint64]uint64{}
		if g.peers != nil {
			progress = g.peers.applied
		}
		status, code, err := cs.PersonalControlView(g.manifest.Config.Cluster, actor, certificate, raftcore.Configuration(app.ConfState), app.Applied, g.leaderID(), app.LogicalTime, progress, read)
		if err != nil {
			return err
		}
		if code != cs.OK {
			return directory.ErrUnauthorized
		}
		out = clusterctl.Status{Ready: status.Ready, LeadershipUnavailable: status.Self == 0, Cluster: status.Cluster, Self: status.Self, Leader: status.Leader, Membership: status.Membership, Configuration: status.Configuration}

		return nil
	})
	if err != nil {
		return clusterctl.Status{}, err
	}
	return out, nil
}
