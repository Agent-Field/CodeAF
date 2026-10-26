package raftdirectory

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// Renew is called only with authenticated ingress evidence. The host cannot
// return from CommitEffect until quorum and application persistence complete.
// Socket pongs remain advisory while this call waits or fails.
func (e Effects) Renew(ctx context.Context, now int64, evidence clusterstate.RenewHolds) ([]dirwatch.HoldReceipt, error) {
	if len(evidence.Items) > clusterstate.MaxRenewHolds {
		return nil, errors.New("raftdirectory: hold batch capacity")
	}
	items := make([]clusterstate.HoldFence, 0, len(evidence.Items))
	for _, h := range evidence.Items {
		raw, err := e.Host.ReadEffect(ctx, clusterstate.CellKey(h.Cell))
		if err != nil {
			return nil, err
		}
		covered, err := clusterstate.RenewalCovered(raw, evidence.Device, h, evidence.SeenAt)
		if err != nil {
			return nil, err
		}
		if !covered {
			items = append(items, h)
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	evidence.Items = items
	c, err := clusterstate.Sign(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: e.Cluster, Actor: e.Actor, Session: "holds", Sequence: 1, Now: now, Change: clusterstate.Mutation{RenewHolds: &evidence}}, e.Key)
	if err != nil {
		return nil, err
	}
	r, err := e.Host.CommitEffect(ctx, c)
	if err != nil {
		return nil, err
	}
	if r.Code != clusterstate.OK || r.Index == 0 {
		return nil, errors.New("raftdirectory: hold renewal did not commit")
	}
	receipts := []dirwatch.HoldReceipt{}
	for _, item := range r.Items {
		if item.Code == clusterstate.OK && item.View != nil {
			receipts = append(receipts, dirwatch.HoldReceipt{Cell: item.Cell, Fence: item.View.Cell.Lease.Fence, Index: r.Index, At: r.Now, Until: item.View.Cell.Lease.Expires})
		}
	}
	return receipts, nil
}

// Schedule reads local durable activity only to decide timer suppression. It
// authorizes no operation. Callers hold the actor queue for this paged scan and
// include queued ingress, assignments and external work in busy.
func Schedule(ctx context.Context, store *raftstore.SQLite, core *raftcore.Core, elapsed, ingressNow int64, busy bool, acks []raftcore.SleepAck) (raftcore.SchedulePlan, error) {
	app, _, err := store.ApplicationView(ctx)
	if err != nil {
		return raftcore.SchedulePlan{}, err
	}
	for _, ns := range []raftstore.Namespace{raftstore.State, raftstore.Membership, raftstore.Outbox} {
		after := ""
		for {
			rows, _, err := store.ScanPage(ctx, app.Applied, ns, after)
			if err != nil {
				return raftcore.SchedulePlan{}, err
			}
			for _, row := range rows {
				active, err := clusterstate.RecordActive(clusterstate.RecordKey{Namespace: string(ns), Key: row.Key.Key}, row.Value, ingressNow)
				if err != nil {
					return raftcore.SchedulePlan{}, err
				}
				busy = busy || active
				after = row.Key.Key
			}
			if len(rows) == 0 {
				break
			}
		}
	}
	key := raftstore.Key{Namespace: raftstore.Membership, Key: clusterstate.QuiescenceKey().Key}
	rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{key})
	if err != nil {
		return raftcore.SchedulePlan{}, err
	}
	var marker clusterstate.Quiescence
	if raw := rows[key]; raw != nil {
		if err = clusterstate.Decode(raw, &marker); err != nil {
			return raftcore.SchedulePlan{}, err
		}
	}
	return core.Schedule(raftcore.ScheduleInput{Now: elapsed, Busy: busy, Marker: marker, Acks: acks})
}
