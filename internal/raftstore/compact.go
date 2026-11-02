package raftstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"hash"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
)

type chunkWriter struct {
	ctx           context.Context
	tx            *sql.Tx
	buf           []byte
	offset, bytes uint64
	digest        hash.Hash
}

func (w *chunkWriter) flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	if e := putChunk(w.ctx, w.tx, "creating", w.offset, w.buf); e != nil {
		return e
	}
	w.offset++
	w.buf = w.buf[:0]
	return nil
}
func (w *chunkWriter) Write(p []byte) (int, error) {
	length := len(p)
	for len(p) > 0 {
		n := min(len(p), raftsnapshot.ChunkBytes-len(w.buf))
		w.buf = append(w.buf, p[:n]...)
		w.digest.Write(p[:n])
		w.bytes += uint64(n)
		p = p[n:]
		if len(w.buf) == raftsnapshot.ChunkBytes {
			if e := w.flush(); e != nil {
				return 0, e
			}
		}
	}
	return length, nil
}

// CreateSnapshot streams a fixed applied view inside one transaction. It
// publishes a complete local checkpoint but leaves physical log deletion to
// Compact. No application data is removed, even under storage pressure.
func (s *SQLite) CreateSnapshot(ctx context.Context) (*pb.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p *pb.Snapshot
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		a, e := getApplication(ctx, tx)
		if e != nil {
			return e
		}
		if a.Applied == 0 {
			return ErrSnapshot
		}
		old, e := getSnapshot(ctx, tx)
		if e != nil {
			return e
		}
		if old.GetMetadata().GetIndex() == a.Applied {
			p = old
			return nil
		}
		entry, e := getEntry(ctx, tx, a.Applied)
		if e != nil {
			return e
		}
		conf, e := raftsnapshot.Configuration(a.ConfState)
		if e != nil {
			return e
		}
		m := raftsnapshot.Manifest{Version: raftsnapshot.Version, Cluster: s.identity.ClusterUUID, Protocol: uint64(s.identity.Protocol), Index: a.Applied, Term: entry.GetTerm(), Conf: conf, VisibleVersion: a.VisibleVersion, LogicalTime: a.LogicalTime, RecordCount: a.RecordCount, RecordsHash: hex.EncodeToString(a.RecordsHash[:])}
		w := &chunkWriter{ctx: ctx, tx: tx, buf: make([]byte, 0, raftsnapshot.ChunkBytes), digest: sha256.New()}
		rows, e := tx.QueryContext(ctx, `SELECT namespace,key,data,checksum FROM records ORDER BY namespace,key`)
		if e != nil {
			return e
		}
		var count uint64
		var rowsHash [32]byte
		for rows.Next() {
			var row raftsnapshot.Row
			var sum []byte
			if e = rows.Scan(&row.Namespace, &row.Key, &row.Value, &sum); e != nil {
				rows.Close()
				return e
			}
			if !bytes.Equal(sum, checksum([]byte("records"), []byte(row.Namespace), []byte(row.Key), row.Value)) {
				rows.Close()
				return ErrCorrupt
			}
			count++
			xorHash(&rowsHash, sum)
			if e = raftsnapshot.WriteRow(w, row); e != nil {
				rows.Close()
				if errors.Is(e, raftsnapshot.ErrFormat) {
					return ErrBatch
				}
				return e
			}
			s.boundary("after-snapshot-source-row")
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if count != a.RecordCount || rowsHash != a.RecordsHash {
			return ErrCorrupt
		}
		if e = w.flush(); e != nil {
			return e
		}
		m.Bytes, m.Chunks, m.Digest = w.bytes, w.offset, hex.EncodeToString(w.digest.Sum(nil))
		data, e := m.Encode()
		if e != nil {
			return ErrSnapshot
		}
		id := raftsnapshot.ID(data)
		existing, existingData, complete, existingErr := snapshotManifest(ctx, tx, id)
		if existingErr == nil {
			// A completed transfer may coincide with local log catch-up after
			// restart. Reuse its identity without treating a duplicate as I/O
			// failure, and never overwrite damaged complete evidence.
			if !bytes.Equal(existingData, data) {
				return ErrCorrupt
			}
			if complete {
				if e = walkSnapshot(ctx, tx, id, existing, nil); e != nil {
					return ErrCorrupt
				}
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM snapshot_chunks WHERE generation=?`, id); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE snapshots SET complete=1 WHERE id=?`, id); e != nil {
				return e
			}
		} else if errors.Is(existingErr, ErrSnapshot) {
			if _, e = tx.ExecContext(ctx, `INSERT INTO snapshots VALUES (?,?,?,1)`, id, data, checksum([]byte("snapshots"), []byte(id), data)); e != nil {
				return e
			}
		} else {
			return existingErr
		}
		if _, e = tx.ExecContext(ctx, `UPDATE snapshot_chunks SET generation=? WHERE generation='creating'`, id); e != nil {
			return e
		}
		// The temporary key never escapes the transaction. Rebind each checksum
		// without loading more than one chunk after its final content ID is known.
		for offset := uint64(0); offset < m.Chunks; offset++ {
			var chunk []byte
			if e = tx.QueryRowContext(ctx, `SELECT data FROM snapshot_chunks WHERE generation=? AND offset=?`, id, indexKey(offset)).Scan(&chunk); e != nil {
				return e
			}
			if e = putChunk(ctx, tx, id, offset, chunk); e != nil {
				return e
			}
			s.boundary("after-snapshot-created-chunk")
		}
		p, e = snapshotPointer(m, data)
		if e != nil {
			return e
		}
		if e = putProto(ctx, tx, "snapshot", p); e != nil {
			return e
		}
		s.boundary("after-snapshot-pointer")
		return retainSnapshots(ctx, tx, id)
	})
	return p, e
}

// Compact removes only the applied consensus prefix covered by the current
// complete durable checkpoint. A dead learner must install that checkpoint;
// its progress never participates in this decision.
func (s *SQLite) Compact(ctx context.Context, index uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transaction(ctx, func(tx *sql.Tx) error {
		p, e := getSnapshot(ctx, tx)
		if e != nil {
			return e
		}
		if index == 0 || index != p.GetMetadata().GetIndex() {
			return ErrSnapshot
		}
		a, e := getApplication(ctx, tx)
		if e != nil {
			return e
		}
		if index > a.Applied {
			return ErrSnapshot
		}
		m, e := decodeSnapshot(p, s.identity)
		if e != nil {
			return e
		}
		id := raftsnapshot.ID(p.Data)
		_, _, complete, e := snapshotManifest(ctx, tx, id)
		if e != nil {
			return e
		}
		if !complete {
			return ErrSnapshot
		}
		if e = walkSnapshot(ctx, tx, id, m, nil); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM raft_log WHERE idx<=?`, indexKey(index)); e != nil {
			return e
		}
		s.boundary("after-compaction")
		return nil
	})
}

// SnapshotDue measures only applied history after the last complete snapshot.
// The host services this before admitting more growth; learners cannot pin it.
func (s *SQLite) SnapshotDue(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due bool
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		a, e := getApplication(ctx, tx)
		if e != nil {
			return e
		}
		p, e := getSnapshot(ctx, tx)
		if e != nil {
			return e
		}
		var count, size uint64
		if e = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(data)),0) FROM raft_log WHERE idx>? AND idx<=?`, indexKey(p.GetMetadata().GetIndex()), indexKey(a.Applied)).Scan(&count, &size); e != nil {
			return e
		}
		due = count >= raftcore.SnapshotEntries || size >= raftcore.SnapshotLogBytes
		return nil
	})
	return due, e
}
