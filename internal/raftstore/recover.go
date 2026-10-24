package raftstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// Recovery has no leadership, timers or batch IDs to restore. Fresh means the
// store has never persisted Raft work; only then may the host supply Bootstrap.
// Entries after Applied, including committed ones, are replayed by the core.
type Recovery struct {
	Core        raftcore.RecoveredState
	Application Application
	Fresh       bool
}

func decodeEntry(key, data, hash []byte) (*pb.Entry, error) {
	if len(key) != 8 || !bytes.Equal(hash, checksum([]byte("raft_log"), key, data)) {
		return nil, ErrCorrupt
	}
	e := &pb.Entry{}
	if proto.Unmarshal(data, e) != nil || !validEntry(e) || e.GetIndex() == 0 || e.GetIndex() != binary.BigEndian.Uint64(key) || e.GetTerm() == 0 {
		return nil, ErrCorrupt
	}
	return e, nil
}

func getEntry(ctx context.Context, tx *sql.Tx, index uint64) (*pb.Entry, error) {
	p, err := getSnapshot(ctx, tx)
	if err != nil {
		return nil, err
	}
	if index > 0 && p.GetMetadata().GetIndex() == index {
		return &pb.Entry{Index: proto.Uint64(index), Term: proto.Uint64(p.GetMetadata().GetTerm())}, nil
	}
	var data, hash []byte
	key := indexKey(index)
	if err := tx.QueryRowContext(ctx, `SELECT data,checksum FROM raft_log WHERE idx=?`, key).Scan(&data, &hash); err != nil {
		return nil, ErrCorrupt
	}
	return decodeEntry(key, data, hash)
}

func lastEntry(ctx context.Context, tx *sql.Tx) (*pb.Entry, error) {
	var key, data, hash []byte
	err := tx.QueryRowContext(ctx, `SELECT idx,data,checksum FROM raft_log ORDER BY idx DESC LIMIT 1`).Scan(&key, &data, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		p, e := getSnapshot(ctx, tx)
		if e != nil {
			return nil, e
		}
		return &pb.Entry{Index: proto.Uint64(p.GetMetadata().GetIndex()), Term: proto.Uint64(p.GetMetadata().GetTerm())}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeEntry(key, data, hash)
}

// Recover checks physical integrity, each record's checksum, member identity
// and cross-record invariants in one consistent transaction. Corrupt evidence
// stays in place; a missing log entry is never synthesized from an index.
// Only the suffix after the complete active checkpoint enters the core cache.
func (s *SQLite) Recover(ctx context.Context) (Recovery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Recovery
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var result string
		if err := tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil || result != "ok" {
			return ErrCorrupt
		}
		data, err := getMeta(ctx, tx, "manifest")
		if err != nil {
			return err
		}
		var m manifest
		if json.Unmarshal(data, &m) != nil {
			return ErrCorrupt
		}
		if m.Format != FormatVersion || m.Identity != s.identity {
			return ErrIdentity
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM meta`).Scan(&count); err != nil || count != 6 {
			return ErrCorrupt
		}
		h := &pb.HardState{}
		if err := getProto(ctx, tx, "hardstate", h); err != nil {
			return err
		}
		a, err := getApplication(ctx, tx)
		if err != nil {
			return err
		}
		snapshot, err := getSnapshot(ctx, tx)
		if err != nil {
			return err
		}
		snapIndex := snapshot.GetMetadata().GetIndex()
		if snapIndex > 0 {
			if _, err = decodeSnapshot(snapshot, s.identity); err != nil {
				return ErrCorrupt
			}
			id := raftsnapshot.ID(snapshot.Data)
			_, data, complete, err := snapshotManifest(ctx, tx, id)
			if err != nil || !complete || !bytes.Equal(data, snapshot.Data) {
				return ErrCorrupt
			}
		} else if !proto.Equal(snapshot, &pb.Snapshot{}) {
			return ErrCorrupt
		}
		// Incomplete staged transfers are ignored. Every retained complete
		// generation is checked so damaged authoritative evidence is refused.
		generations, err := tx.QueryContext(ctx, `SELECT id FROM snapshots WHERE complete=1`)
		if err != nil {
			return ErrCorrupt
		}
		var ids []string
		for generations.Next() {
			var id string
			if err = generations.Scan(&id); err != nil {
				generations.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = generations.Err()
		generations.Close()
		if err != nil {
			return err
		}
		if len(ids) > raftsnapshot.RetainedGenerations {
			return ErrCorrupt
		}
		for _, id := range ids {
			m, _, _, err := snapshotManifest(ctx, tx, id)
			if err != nil {
				return ErrCorrupt
			}
			if err = walkSnapshot(ctx, tx, id, m, nil); err != nil {
				return ErrCorrupt
			}
		}
		if a.Applied < snapIndex {
			return ErrCorrupt
		}
		rows, err := tx.QueryContext(ctx, `SELECT idx,data,checksum FROM raft_log WHERE idx>? ORDER BY idx`, indexKey(snapIndex))
		if err != nil {
			return ErrCorrupt
		}
		defer rows.Close()
		var entries []*pb.Entry
		last, term := snapIndex, snapshot.GetMetadata().GetTerm()
		for rows.Next() {
			var key, data, hash []byte
			if err := rows.Scan(&key, &data, &hash); err != nil {
				return ErrCorrupt
			}
			e, err := decodeEntry(key, data, hash)
			if err != nil {
				return err
			}
			if last == ^uint64(0) || e.GetIndex() != last+1 || e.GetTerm() < term {
				return ErrCorrupt
			}
			last, term = e.GetIndex(), e.GetTerm()
			entries = append(entries, e)
		}
		if err := rows.Err(); err != nil {
			return ErrCorrupt
		}
		if err := rows.Close(); err != nil {
			return err
		}
		tip, err := getMeta(ctx, tx, "log_tip")
		if err != nil || !bytes.Equal(tip, indexKey(last)) {
			return ErrCorrupt
		}
		if a.Applied > h.GetCommit() || h.GetCommit() > last || term > h.GetTerm() || (h.GetTerm() == 0 && h.GetVote() != 0) || (a.Applied == 0 && !proto.Equal(a.ConfState, &pb.ConfState{})) {
			return ErrCorrupt
		}
		// Stream application rows through checksum validation. Recovery does not
		// materialize an identity or silently discard unknown namespaces.
		r, err := tx.QueryContext(ctx, `SELECT namespace,key,data,checksum FROM records ORDER BY namespace,key`)
		if err != nil {
			return ErrCorrupt
		}
		defer r.Close()
		var appRows uint64
		var recordsHash [32]byte
		for r.Next() {
			var namespace Namespace
			var key string
			var data, hash []byte
			if err := r.Scan(&namespace, &key, &data, &hash); err != nil {
				return ErrCorrupt
			}
			if !validKey(Key{namespace, key}) || len(data) > MaxRecordBytes || !bytes.Equal(hash, checksum([]byte("records"), []byte(namespace), []byte(key), data)) {
				return ErrCorrupt
			}
			appRows++
			xorHash(&recordsHash, hash)
		}
		if err := r.Err(); err != nil {
			return ErrCorrupt
		}
		if err := r.Close(); err != nil {
			return err
		}
		if a.RecordCount != appRows || a.RecordsHash != recordsHash || (a.Applied == 0 && appRows != 0) {
			return ErrCorrupt
		}
		var anchors []uint64
		var registry []byte
		err = tx.QueryRowContext(ctx, `SELECT data FROM records WHERE namespace='membership' AND key='registry'`).Scan(&registry)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if registry != nil {
			var membership clusterstate.Membership
			if clusterstate.Decode(registry, &membership) != nil || membership.Validate() != nil || membership.Index > a.Applied {
				return ErrCorrupt
			}
			anchors = membership.Anchors
		}
		if registry == nil {
			var imported []byte
			err = tx.QueryRowContext(ctx, `SELECT data FROM records WHERE namespace='membership' AND key='legacy-import'`).Scan(&imported)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if imported != nil {
				var manifest clusterstate.LegacyManifest
				if clusterstate.Decode(imported, &manifest) != nil || manifest.Validate() != nil || manifest.Config.Cluster != s.identity.ClusterUUID {
					return ErrCorrupt
				}
				anchors = manifest.Config.Anchors
			}
		}
		out.Core = raftcore.RecoveredState{Anchors: anchors, HardState: h, ConfState: a.ConfState, Snapshot: snapshot, Entries: entries, Applied: a.Applied}
		if snapIndex == 0 {
			out.Core.Snapshot = nil
		}
		out.Application = a
		out.Fresh = last == 0 && raft.IsEmptyHardState(h) && a.Applied == 0
		if out.Fresh {
			// A nonnil recovered ConfState forbids core bootstrap. Empty
			// initialized storage has no configuration to recover yet.
			out.Core.ConfState = nil
		}
		cfg := raftcore.Config{ClusterUUID: s.identity.ClusterUUID, NodeID: s.identity.NodeID, Protocol: s.identity.Protocol}
		if _, err := raftcore.New(cfg, out.Core); err != nil {
			return ErrCorrupt
		}
		return nil
	})
	if err != nil {
		return Recovery{}, err
	}
	return out, nil
}
