package raftstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/raftcore"
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
		return &pb.Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeEntry(key, data, hash)
}

// Recover checks physical integrity, each record's checksum, member identity
// and cross-record invariants in one consistent transaction. Corrupt evidence
// stays in place; a missing log entry is never synthesized from an index.
// P05 will replace the un-compacted suffix read with snapshot-backed recovery.
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
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM meta`).Scan(&count); err != nil || count != 5 {
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
		rows, err := tx.QueryContext(ctx, `SELECT idx,data,checksum FROM raft_log ORDER BY idx`)
		if err != nil {
			return ErrCorrupt
		}
		defer rows.Close()
		var entries []*pb.Entry
		var last, term uint64
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
		out.Core = raftcore.RecoveredState{HardState: h, ConfState: a.ConfState, Entries: entries, Applied: a.Applied}
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
