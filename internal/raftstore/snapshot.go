package raftstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"hash"
	"io"

	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// Snapshot.Data is the canonical manifest. Chunks travel on a separate bounded
// host channel and must be complete before Persist accepts the Raft snapshot.
func snapshotPointer(m raftsnapshot.Manifest, data []byte) (*pb.Snapshot, error) {
	c, e := m.Configuration()
	if e != nil {
		return nil, ErrSnapshot
	}
	return &pb.Snapshot{Data: bytes.Clone(data), Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(m.Index), Term: proto.Uint64(m.Term), ConfState: c}}, nil
}
func getSnapshot(ctx context.Context, tx *sql.Tx) (*pb.Snapshot, error) {
	p := &pb.Snapshot{}
	e := getProto(ctx, tx, "snapshot", p)
	return p, e
}
func decodeSnapshot(p *pb.Snapshot, identity Identity) (raftsnapshot.Manifest, error) {
	m, e := raftsnapshot.Decode(p.GetData())
	if e != nil || m.Cluster != identity.ClusterUUID || m.Protocol != uint64(identity.Protocol) || m.Index != p.GetMetadata().GetIndex() || m.Term != p.GetMetadata().GetTerm() {
		return m, ErrSnapshot
	}
	c, e := m.Configuration()
	if e != nil || !proto.Equal(c, p.GetMetadata().GetConfState()) || len(p.ProtoReflect().GetUnknown()) != 0 || len(p.GetMetadata().ProtoReflect().GetUnknown()) != 0 {
		return m, ErrSnapshot
	}
	return m, nil
}

func stage(ctx context.Context, tx *sql.Tx, data []byte) (string, error) {
	id := raftsnapshot.ID(data)
	var existing []byte
	e := tx.QueryRowContext(ctx, `SELECT manifest FROM snapshots WHERE id=?`, id).Scan(&existing)
	if e == nil {
		if !bytes.Equal(data, existing) {
			return "", ErrCorrupt
		}
		return id, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	// Only one incoming generation may stage at once. Abandoning incomplete
	// consensus bytes never removes a complete snapshot or application record.
	if _, e = tx.ExecContext(ctx, `DELETE FROM snapshot_chunks WHERE generation IN (SELECT id FROM snapshots WHERE complete=0)`); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM snapshots WHERE complete=0`); e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO snapshots VALUES (?,?,?,0)`, id, data, checksum([]byte("snapshots"), []byte(id), data))
	return id, e
}

// BeginSnapshot admits a canonical manifest without exposing it to recovery.
// Retrying the same generation preserves already durable chunks.
func (s *SQLite) BeginSnapshot(ctx context.Context, data []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, e := raftsnapshot.Decode(data)
	if e != nil || m.Cluster != s.identity.ClusterUUID || m.Protocol != uint64(s.identity.Protocol) {
		return "", ErrSnapshot
	}
	var id string
	e = s.transaction(ctx, func(tx *sql.Tx) error {
		active, err := getSnapshot(ctx, tx)
		if err != nil {
			return err
		}
		if m.Index <= active.GetMetadata().GetIndex() && !bytes.Equal(active.Data, data) {
			return ErrSnapshot
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,manifest FROM snapshots WHERE complete=1`)
		if err != nil {
			return err
		}
		blocked := false
		for rows.Next() {
			var id string
			var b []byte
			if err = rows.Scan(&id, &b); err != nil {
				rows.Close()
				return err
			}
			existing, err := raftsnapshot.Decode(b)
			if err != nil {
				rows.Close()
				return ErrCorrupt
			}
			if existing.Index > active.GetMetadata().GetIndex() && id != raftsnapshot.ID(data) {
				blocked = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if blocked {
			return ErrSnapshot
		}

		var e error
		id, e = stage(ctx, tx, data)
		s.boundary("after-snapshot-stage")
		return e
	})
	return id, e
}
func snapshotManifest(ctx context.Context, tx *sql.Tx, id string) (raftsnapshot.Manifest, []byte, bool, error) {
	var data, sum []byte
	var complete int
	e := tx.QueryRowContext(ctx, `SELECT manifest,checksum,complete FROM snapshots WHERE id=?`, id).Scan(&data, &sum, &complete)
	if errors.Is(e, sql.ErrNoRows) {
		return raftsnapshot.Manifest{}, nil, false, ErrSnapshot
	}
	if e != nil {
		return raftsnapshot.Manifest{}, nil, false, e
	}
	m, e := raftsnapshot.Decode(data)
	if e != nil || raftsnapshot.ID(data) != id || !bytes.Equal(sum, checksum([]byte("snapshots"), []byte(id), data)) || (complete != 0 && complete != 1) {
		return m, nil, false, ErrCorrupt
	}
	return m, data, complete == 1, nil
}
func chunkLength(m raftsnapshot.Manifest, offset uint64) uint64 {
	if offset >= m.Chunks {
		return 0
	}
	if offset == m.Chunks-1 {
		return m.Bytes - offset*raftsnapshot.ChunkBytes
	}
	return raftsnapshot.ChunkBytes
}
func putChunk(ctx context.Context, tx *sql.Tx, id string, offset uint64, data []byte) error {
	key := indexKey(offset)
	_, e := tx.ExecContext(ctx, `INSERT INTO snapshot_chunks VALUES (?,?,?,?) ON CONFLICT(generation,offset) DO UPDATE SET data=excluded.data,checksum=excluded.checksum`, id, key, data, checksum([]byte("snapshot_chunks"), []byte(id), key, data))
	return e
}

// PutSnapshotChunk checks size and the transport's SHA-256 before any write.
// A conflicting retry cannot overwrite a previously accepted chunk.
func (s *SQLite) PutSnapshotChunk(ctx context.Context, id string, offset uint64, data []byte, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(data) > raftsnapshot.ChunkBytes || raftsnapshot.ID(data) != digest {
		return ErrSnapshot
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		m, _, complete, e := snapshotManifest(ctx, tx, id)
		if e != nil {
			return e
		}
		if uint64(len(data)) != chunkLength(m, offset) || len(data) == 0 {
			return ErrSnapshot
		}
		var old []byte
		e = tx.QueryRowContext(ctx, `SELECT data FROM snapshot_chunks WHERE generation=? AND offset=?`, id, indexKey(offset)).Scan(&old)
		if e == nil {
			if !bytes.Equal(old, data) {
				return ErrSnapshot
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if complete {
			return ErrSnapshot
		}
		if e = putChunk(ctx, tx, id, offset, data); e != nil {
			return e
		}
		s.boundary("after-snapshot-chunk")
		return nil
	})
}

type chunkReader struct {
	ctx    context.Context
	tx     *sql.Tx
	id     string
	m      raftsnapshot.Manifest
	offset uint64
	buf    []byte
	digest hash.Hash
	read   uint64
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buf) == 0 {
		if r.offset == r.m.Chunks {
			return 0, io.EOF
		}
		var sum []byte
		key := indexKey(r.offset)
		if e := r.tx.QueryRowContext(r.ctx, `SELECT data,checksum FROM snapshot_chunks WHERE generation=? AND offset=?`, r.id, key).Scan(&r.buf, &sum); e != nil {
			return 0, ErrSnapshot
		}
		if uint64(len(r.buf)) != chunkLength(r.m, r.offset) || !bytes.Equal(sum, checksum([]byte("snapshot_chunks"), []byte(r.id), key, r.buf)) {
			return 0, ErrCorrupt
		}
		r.digest.Write(r.buf)
		r.offset++
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	r.read += uint64(n)
	return n, nil
}

// walkSnapshot never loads the whole identity. At most one record and one
// chunk are resident; canonical row order detects duplicate or reordered keys.
func walkSnapshot(ctx context.Context, tx *sql.Tx, id string, m raftsnapshot.Manifest, visit func(raftsnapshot.Row) error) error {
	r := &chunkReader{ctx: ctx, tx: tx, id: id, m: m, digest: sha256.New()}
	var count uint64
	var rowsHash [32]byte
	var previousNamespace, previousKey string
	for {
		row, e := raftsnapshot.ReadRow(r)
		if e == io.EOF {
			break
		}
		if e != nil {
			return ErrSnapshot
		}
		if count > 0 && (row.Namespace < previousNamespace || (row.Namespace == previousNamespace && row.Key <= previousKey)) {
			return ErrSnapshot
		}
		previousNamespace, previousKey = row.Namespace, row.Key
		count++
		xorHash(&rowsHash, checksum([]byte("records"), []byte(row.Namespace), []byte(row.Key), row.Value))
		if visit != nil {
			if e = visit(row); e != nil {
				return e
			}
		}
	}
	var chunks uint64
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM snapshot_chunks WHERE generation=?`, id).Scan(&chunks); e != nil {
		return e
	}
	if count != m.RecordCount || hex.EncodeToString(rowsHash[:]) != m.RecordsHash || r.read != m.Bytes || chunks != m.Chunks || hex.EncodeToString(r.digest.Sum(nil)) != m.Digest {
		return ErrSnapshot
	}
	return nil
}

// CompleteSnapshot publishes only the immutable verified generation, not the
// active application pointer. Persist performs that second atomic transition.
func (s *SQLite) CompleteSnapshot(ctx context.Context, id string) (*pb.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p *pb.Snapshot
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		m, data, _, e := snapshotManifest(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = walkSnapshot(ctx, tx, id, m, nil); e != nil {
			return e
		}
		active, e := getSnapshot(ctx, tx)
		if e != nil {
			return e
		}
		if m.Index <= active.GetMetadata().GetIndex() && !bytes.Equal(active.Data, data) {
			return ErrSnapshot
		}
		if _, e = tx.ExecContext(ctx, `UPDATE snapshots SET complete=1 WHERE id=?`, id); e != nil {
			return e
		}
		if e = retainSnapshots(ctx, tx, id); e != nil {
			return e
		}
		s.boundary("after-snapshot-complete")
		p, e = snapshotPointer(m, data)
		return e
	})
	return p, e
}

// ReadSnapshotChunk returns a bounded immutable transport buffer. A successful
// upload is not a Raft snapshot delivery acknowledgment; installation is.
func (s *SQLite) ReadSnapshotChunk(ctx context.Context, id string, offset uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []byte
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		m, _, complete, e := snapshotManifest(ctx, tx, id)
		if e != nil {
			return e
		}
		if !complete || offset >= m.Chunks {
			return ErrSnapshot
		}
		var sum []byte
		key := indexKey(offset)
		if e = tx.QueryRowContext(ctx, `SELECT data,checksum FROM snapshot_chunks WHERE generation=? AND offset=?`, id, key).Scan(&out, &sum); e != nil {
			return e
		}
		if uint64(len(out)) != chunkLength(m, offset) || !bytes.Equal(sum, checksum([]byte("snapshot_chunks"), []byte(id), key, out)) {
			return ErrCorrupt
		}
		return nil
	})
	return out, e
}

func retainSnapshots(ctx context.Context, tx *sql.Tx, active string) error {
	rows, e := tx.QueryContext(ctx, `SELECT id,manifest FROM snapshots WHERE complete=1`)
	if e != nil {
		return e
	}
	type generation struct {
		id    string
		index uint64
	}
	var keep generation
	var remove []string
	for rows.Next() {
		var id string
		var data []byte
		if e = rows.Scan(&id, &data); e != nil {
			rows.Close()
			return e
		}
		m, e := raftsnapshot.Decode(data)
		if e != nil {
			rows.Close()
			return ErrCorrupt
		}
		if id != active {
			if keep.id == "" || m.Index > keep.index {
				if keep.id != "" {
					remove = append(remove, keep.id)
				}
				keep = generation{id, m.Index}
			} else {
				remove = append(remove, id)
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range remove {
		if _, e = tx.ExecContext(ctx, `DELETE FROM snapshot_chunks WHERE generation=?`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM snapshots WHERE id=?`, id); e != nil {
			return e
		}
	}
	return nil
}

func (s *SQLite) installSnapshot(ctx context.Context, tx *sql.Tx, p *pb.Snapshot) error {
	m, e := decodeSnapshot(p, s.identity)
	if e != nil {
		return e
	}
	id := raftsnapshot.ID(p.Data)
	_, data, complete, e := snapshotManifest(ctx, tx, id)
	if e != nil {
		return e
	}
	if !complete || !bytes.Equal(data, p.Data) {
		return ErrSnapshot
	}
	a, e := getApplication(ctx, tx)
	if e != nil {
		return e
	}
	if m.Index <= a.Applied || m.LogicalTime < a.LogicalTime || m.VisibleVersion < a.VisibleVersion {
		return ErrSnapshot
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM records`); e != nil {
		return e
	}
	s.boundary("after-snapshot-clear")
	e = walkSnapshot(ctx, tx, id, m, func(r raftsnapshot.Row) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO records VALUES (?,?,?,?)`, r.Namespace, r.Key, r.Value, checksum([]byte("records"), []byte(r.Namespace), []byte(r.Key), r.Value))
		s.boundary("after-snapshot-row")
		return e
	})
	if e != nil {
		return e
	}
	var recordsHash [32]byte
	h, _ := hex.DecodeString(m.RecordsHash)
	copy(recordsHash[:], h)
	c, _ := m.Configuration()
	if e = putApplication(ctx, tx, Application{Applied: m.Index, VisibleVersion: m.VisibleVersion, LogicalTime: m.LogicalTime, RecordCount: m.RecordCount, RecordsHash: recordsHash, ConfState: c}); e != nil {
		return e
	}
	s.boundary("after-snapshot-application")
	if e = putProto(ctx, tx, "snapshot", p); e != nil {
		return e
	}
	s.boundary("after-snapshot-pointer")
	if _, e = tx.ExecContext(ctx, `DELETE FROM raft_log`); e != nil {
		return e
	}
	if e = putMeta(ctx, tx, "log_tip", indexKey(m.Index)); e != nil {
		return e
	}
	s.boundary("after-snapshot-log")
	return retainSnapshots(ctx, tx, id)
}
