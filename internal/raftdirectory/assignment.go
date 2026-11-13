package raftdirectory

import (
	"context"
	"errors"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// AuthorizedWorkerAssignment runs inside the actor queue AFTER current ingress
// time commits and a fresh quorum ReadIndex passes. This helper cannot turn a
// follower's local state into permission to execute work.
func AuthorizedWorkerAssignment(ctx context.Context, store *raftstore.SQLite, cluster, worker string, incarnation uint64, ref cs.AssignmentRef) (cs.Assignment, cs.CellView, error) {
	app, _, err := store.ApplicationView(ctx)
	if err != nil {
		return cs.Assignment{}, cs.CellView{}, err
	}
	source := cs.AssignmentReadKeys(worker, ref)
	keys := make([]raftstore.Key, len(source))
	for i, k := range source {
		keys[i] = raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
	}
	rows, _, err := store.Read(ctx, app.Applied, keys)
	if err != nil {
		return cs.Assignment{}, cs.CellView{}, err
	}
	r, err := cs.ReadWorkerAssignment(cluster, worker, incarnation, ref, app.LogicalTime, func(k cs.RecordKey) ([]byte, error) {
		return rows[raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}], nil
	})
	if err != nil {
		return cs.Assignment{}, cs.CellView{}, err
	}
	if r.Code != cs.OK {
		return cs.Assignment{}, cs.CellView{}, errors.New("raftdirectory: assignment unavailable")
	}
	return *r.Assignment, *r.View, nil
}
