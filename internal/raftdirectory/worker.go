package raftdirectory

import (
	"context"
	"errors"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// AuthorizedWorkerRead runs under the actor lock after CURRENT committed time
// and a fresh quorum barrier, just like AuthorizedRead. It exposes one selected
// chat and its sealed grant, never a directory listing or the identity vault.
func AuthorizedWorkerRead(ctx context.Context, store *raftstore.SQLite, cluster, worker, cell string, incarnation uint64) (cs.CellView, []byte, error) {
	app, _, err := store.ApplicationView(ctx)
	if err != nil {
		return cs.CellView{}, nil, err
	}
	source := cs.WorkerReadKeys(worker, cell)
	keys := make([]raftstore.Key, len(source))
	for i, k := range source {
		keys[i] = raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
	}
	rows, _, err := store.Read(ctx, app.Applied, keys)
	if err != nil {
		return cs.CellView{}, nil, err
	}
	result, err := cs.ReadWorkerCell(cluster, worker, cell, incarnation, app.LogicalTime, func(k cs.RecordKey) ([]byte, error) {
		return rows[raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}], nil
	})
	if err != nil {
		return cs.CellView{}, nil, err
	}
	if result.Code == cs.NotFound {
		return cs.CellView{}, nil, cs.ErrNotFound
	}
	if result.Code != cs.OK {
		return cs.CellView{}, nil, errors.New("raftdirectory: unauthorized worker")
	}
	return *result.View, result.Grant, nil
}
