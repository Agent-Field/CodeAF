// Package raftdirectory assembles deterministic directory evaluation with the
// durable native member store. It is not a second consensus or policy writer.
package raftdirectory

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	pb "go.etcd.io/raft/v3/raftpb"
)

// Apply evaluates a complete committed batch under the actor's serialization.
// Dependencies see prior entries in this batch; the host persists the final
// overlay, all exact results and the applied header in one FULL transaction.
// Returned results may escape only after this function returns successfully.
func Apply(ctx context.Context, store *raftstore.SQLite, b raftcore.ApplyBatch, cluster string, ceiling uint64) ([]clusterstate.Result, error) {
	if imported, err := store.ImportedLegacy(ctx); err != nil {
		return nil, err
	} else if imported != nil {
		ceiling = imported.Ceiling
	}
	app, size, err := store.ApplicationView(ctx)
	if err != nil {
		return nil, err
	}
	update := raftstore.Update{ExpectedApplied: app.Applied, VisibleVersion: app.VisibleVersion, LogicalTime: app.LogicalTime}
	overlay := map[clusterstate.RecordKey][]byte{}
	original := map[clusterstate.RecordKey][]byte{}
	read := func(k clusterstate.RecordKey) ([]byte, error) {
		if v, ok := overlay[k]; ok {
			return v, nil
		}
		if v, ok := original[k]; ok {
			return v, nil
		}
		records, _, e := store.Read(ctx, app.Applied, []raftstore.Key{{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}})
		if e != nil {
			return nil, e
		}
		v := records[raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}]
		original[k] = v
		return v, nil
	}
	results := []clusterstate.Result{}
	conf := app.ConfState
	for _, entry := range b.Entries {
		conf, err = raftcore.AdvanceConfiguration(conf, entry)
		if err != nil {
			return nil, err
		}
		if entry.GetType() != pb.EntryNormal || len(entry.GetData()) == 0 {
			continue
		}
		e, err := clusterstate.Evaluate(entry.GetData(), clusterstate.ApplicationMeta{Term: entry.GetTerm(), Index: entry.GetIndex(), Configuration: raftcore.Configuration(conf), Cluster: cluster, Now: update.LogicalTime, Version: update.VisibleVersion, Bytes: size, Ceiling: ceiling}, read)
		if err != nil {
			return nil, err
		}
		for _, w := range e.Writes {
			old, err := read(w.RecordKey)
			if err != nil {
				return nil, err
			}
			if old != nil {
				size -= clusterstate.RecordBytes(w.RecordKey, old)
			}
			size += clusterstate.RecordBytes(w.RecordKey, w.Data)
			overlay[w.RecordKey] = w.Data
		}
		update.LogicalTime = e.Now
		update.VisibleVersion = e.Version
		results = append(results, e.Result)
	}
	for k, v := range overlay {
		update.Writes = append(update.Writes, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}, Value: v})
	}
	slices.SortFunc(update.Writes, func(a, b raftstore.Write) int {
		if v := cmp.Compare(a.Namespace, b.Namespace); v != 0 {
			return v
		}
		return cmp.Compare(a.Key.Key, b.Key.Key)
	})
	if err = store.Apply(ctx, b, update); err != nil {
		return nil, err
	}
	return results, nil
}

// AuthorizedRead runs only within the actor after its current-term quorum
// barrier. Enrollment is not inferred from a device metadata upsert.
func AuthorizedRead(ctx context.Context, store *raftstore.SQLite, cluster, actor string) (raftstore.Application, error) {
	return authorizedRead(ctx, store, cluster, actor, false)
}

// AuthorizedEffectRead uses the same current-term barrier contract for the
// restricted delivery service. Rotation does not discard a committed outbox.
func AuthorizedEffectRead(ctx context.Context, store *raftstore.SQLite, cluster, actor string) (raftstore.Application, error) {
	return authorizedRead(ctx, store, cluster, actor, true)
}
func authorizedRead(ctx context.Context, store *raftstore.SQLite, cluster, actor string, effects bool) (raftstore.Application, error) {
	app, _, err := store.ApplicationView(ctx)
	if err != nil {
		return app, err
	}
	keys := []raftstore.Key{{Namespace: raftstore.Enrollment, Key: clusterstate.AuthorityKey(actor).Key}, {Namespace: raftstore.State, Key: clusterstate.DeviceKey(actor).Key}}
	rows, _, err := store.Read(ctx, app.Applied, keys)
	if err != nil {
		return app, err
	}
	var auth clusterstate.Authority
	var device clusterstate.Device
	if clusterstate.Decode(rows[keys[0]], &auth) != nil || auth.Cluster != cluster || (auth.Personal == effects) || auth.Effects != effects || auth.Anchor || len(auth.Key) != ed25519.PublicKeySize {
		return app, errors.New("raftdirectory: unauthorized")
	}
	if raw := rows[keys[1]]; raw != nil {
		if err := clusterstate.Decode(raw, &device); err != nil {
			return app, err
		}
		if device.Revoked {
			return app, errors.New("raftdirectory: revoked")
		}
	}
	return app, nil
}
