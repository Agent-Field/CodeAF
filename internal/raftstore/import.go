package raftstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// ImportLegacy installs one source-authorized genesis, never three independent
// singleton bootstraps. The host keeps the source frozen until quorum readback.
// A crash rolls back all rows or retains the entire genesis; a retry can only
// finish its checkpoint, never replace later committed cluster work.
func (s *SQLite) ImportLegacy(ctx context.Context, m clusterstate.LegacyManifest, stream func(func(clusterstate.RecordWrite) error) error) error {
	if m.Validate() != nil || m.Config.Cluster != s.identity.ClusterUUID {
		return ErrBatch
	}
	member := false
	for _, id := range m.Config.Anchors {
		member = member || id == s.identity.NodeID
	}
	if !member {
		return ErrIdentity
	}
	encoded, e := clusterstate.Encode(m)
	if e != nil || uint64(len(encoded))+128 > clusterstate.LegacyManifestReserve {
		return ErrBatch
	}
	s.mu.Lock()
	e = s.transaction(ctx, func(tx *sql.Tx) error {
		a, e := getApplication(ctx, tx)
		if e != nil {
			return e
		}
		k := clusterstate.LegacyImportKey()
		var old []byte
		err := tx.QueryRowContext(ctx, `SELECT data FROM records WHERE namespace=? AND key=?`, k.Namespace, k.Key).Scan(&old)
		if err == nil {
			if !bytes.Equal(old, encoded) || a.Applied == 0 {
				return ErrBatch
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tip, e := getMeta(ctx, tx, "log_tip")
		if e != nil {
			return e
		}
		h := &pb.HardState{}
		if e = getProto(ctx, tx, "hardstate", h); e != nil {
			return e
		}
		if a.Applied != 0 || a.RecordCount != 0 || !bytes.Equal(tip, indexKey(0)) || h.GetTerm() != 0 || h.GetVote() != 0 || h.GetCommit() != 0 {
			return ErrBatch
		}
		count, total := uint64(0), uint64(0)
		last := clusterstate.RecordKey{}
		digest := sha256.New()
		add := func(w clusterstate.RecordWrite) error {
			key := Key{Namespace: Namespace(w.Namespace), Key: w.Key}
			if !validKey(key) || len(w.Data) > MaxRecordBytes {
				return ErrBatch
			}
			sum := checksum([]byte("records"), []byte(w.Namespace), []byte(w.Key), w.Data)
			if _, e := tx.ExecContext(ctx, `INSERT INTO records VALUES(?,?,?,?)`, w.Namespace, w.Key, w.Data, sum); e != nil {
				return e
			}
			a.RecordCount++
			xorHash(&a.RecordsHash, sum)
			s.boundary("after-import-row")
			return nil
		}
		if e = stream(func(w clusterstate.RecordWrite) error {
			if w.RecordKey == k || count > 0 && (w.Namespace < last.Namespace || w.Namespace == last.Namespace && w.Key <= last.Key) {
				return ErrBatch
			}
			b, e := clusterstate.Encode(w)
			if e != nil {
				return ErrBatch
			}
			digest.Write(b)
			digest.Write([]byte{'\n'})
			total += clusterstate.RecordBytes(w.RecordKey, w.Data)
			count++
			if count > m.Rows || total > m.Bytes {
				return ErrBatch
			}
			last = w.RecordKey
			return add(w)
		}); e != nil {
			return e
		}
		if count != m.Rows || total != m.Bytes || hex.EncodeToString(digest.Sum(nil)) != m.Digest {
			return ErrBatch
		}
		if e = add(clusterstate.RecordWrite{RecordKey: k, Data: encoded}); e != nil {
			return e
		}
		a.Applied = 1
		a.VisibleVersion = m.Version
		a.LogicalTime = m.Config.Now
		a.ConfState = &pb.ConfState{Voters: append([]uint64(nil), m.Config.Anchors...)}
		entry := &pb.Entry{Index: proto.Uint64(1), Term: proto.Uint64(1)}
		data, _ := wire.Marshal(entry)
		idx := indexKey(1)
		if _, e = tx.ExecContext(ctx, `INSERT INTO raft_log VALUES(?,?,?)`, idx, data, checksum([]byte("raft_log"), idx, data)); e != nil {
			return e
		}
		if e = putMeta(ctx, tx, "log_tip", idx); e != nil {
			return e
		}
		if e = putProto(ctx, tx, "hardstate", &pb.HardState{Term: proto.Uint64(1), Commit: proto.Uint64(1)}); e != nil {
			return e
		}
		if e = putApplication(ctx, tx, a); e != nil {
			return e
		}
		s.boundary("after-import-genesis")
		return nil
	})
	s.mu.Unlock()
	if e != nil {
		return e
	}
	_, e = s.CreateSnapshot(ctx)
	return e
}

// ImportedLegacy returns persisted provenance, including the capacity granted
// to the original rows. Hosts consult it rather than accepting a request limit.
func (s *SQLite) ImportedLegacy(ctx context.Context) (*clusterstate.LegacyManifest, error) {
	app, _, e := s.ApplicationView(ctx)
	if e != nil {
		return nil, e
	}
	k := clusterstate.LegacyImportKey()
	key := Key{Namespace: Membership, Key: k.Key}
	rows, _, e := s.Read(ctx, app.Applied, []Key{key})
	if e != nil {
		return nil, e
	}
	if rows[key] == nil {
		return nil, nil
	}
	var m clusterstate.LegacyManifest
	if clusterstate.Decode(rows[key], &m) != nil || m.Validate() != nil || m.Config.Cluster != s.identity.ClusterUUID {
		return nil, ErrCorrupt
	}
	return &m, nil
}
