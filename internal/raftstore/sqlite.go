// Package raftstore owns native per-member durability. It is a host adapter;
// neither SQLite nor a filesystem callback enters the deterministic core.
package raftstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite" // This is the repository's existing native SQLite driver.
)

const (
	FormatVersion  = 1
	MaxRecordBytes = 1 << 20
)

var (
	ErrCorrupt  = errors.New("raftstore: invalid authoritative store; retain it and recover a replacement from the group")
	ErrIdentity = errors.New("raftstore: member identity or format mismatch")
	ErrBatch    = errors.New("raftstore: invalid persistence or application batch")
	ErrConflict = errors.New("raftstore: application read index changed")
	ErrStopped  = errors.New("raftstore: storage failed; close and recover before participating")
	ErrSnapshot = errors.New("raftstore: snapshot installation requires the snapshot lifecycle adapter")
)

// Identity permanently binds a file to one member, not just to one identity's
// directory. A different member must have its own path and freshly enrolled ID.
type Identity struct {
	ClusterUUID string `json:"cluster"`
	NodeID      uint64 `json:"node"`
	Protocol    uint16 `json:"protocol"`
}

type manifest struct {
	Format int `json:"format"`
	Identity
}

// Application is committed with every application write-set. ConfState is the
// complete DURABLY APPLIED configuration, including joint and learner fields.
// VisibleVersion deliberately does not move for ordinary consensus entries.
type Application struct {
	Applied        uint64        `json:"applied"`
	VisibleVersion uint64        `json:"version"`
	LogicalTime    int64         `json:"now"`
	ConfState      *pb.ConfState `json:"-"`
	RecordCount    uint64        `json:"record_count"`
	RecordsHash    [32]byte      `json:"records_hash"`
}

// Namespace keeps application dependencies and durable effect results in the
// same transaction without teaching this storage layer directory policy.
type Namespace string

const (
	State      Namespace = "state"
	Sessions   Namespace = "sessions"
	Membership Namespace = "membership"
	Enrollment Namespace = "enrollment"
	Tombstones Namespace = "tombstones"
	Outbox     Namespace = "outbox"
)

type Key struct {
	Namespace Namespace
	Key       string
}

// Write is a host-evaluated write-set, not permission to mutate a directory.
// Delete is reserved for explicitly authorized application changes. Consensus
// compaction never deletes these records or any sealed chat data.
type Write struct {
	Key
	Value  []byte
	Delete bool
}

type Update struct {
	ExpectedApplied uint64
	VisibleVersion  uint64
	LogicalTime     int64
	Writes          []Write
}

// SQLite serializes all use of its sole connection. Each transaction takes
// SQLite's writer lock immediately; a second opener cannot race a read-set CAS.
// The host must also serialize its core and discard callbacks from old handles.
type SQLite struct {
	mu       sync.Mutex
	db       *sql.DB
	identity Identity
	stopped  bool
	// cut lets package tests kill the PROCESS at actual transaction boundaries.
	// Production never installs a callback or receives a weaker durability mode.
	cut func(string)
}

// Indexes are fixed-width big-endian BLOBs, so SQLite sorts the full uint64
// range without signed INTEGER overflow or decimal-string collation mistakes.
const schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, data BLOB NOT NULL, checksum BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE raft_log (idx BLOB PRIMARY KEY CHECK(length(idx)=8), data BLOB NOT NULL, checksum BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE records (namespace TEXT NOT NULL, key TEXT NOT NULL, data BLOB NOT NULL, checksum BLOB NOT NULL, PRIMARY KEY(namespace,key)) WITHOUT ROWID;
`

var wire = proto.MarshalOptions{Deterministic: true}

// Open creates an absent file and otherwise validates the existing store before
// returning it. It NEVER creates missing tables in an existing authoritative
// file, repairs corrupt rows, truncates it or falls back to an empty database.
func Open(path string, identity Identity) (*SQLite, error) {
	if path == "" || path == ":memory:" || identity.ClusterUUID == "" || identity.NodeID == 0 || raft.IsLocalMsgTarget(identity.NodeID) || identity.Protocol != raftcore.ProtocolVersion {
		return nil, ErrIdentity
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	created := false
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		created = true
		err = f.Close()
	} else if errors.Is(err, os.ErrExist) {
		var info os.FileInfo
		info, err = os.Lstat(abs)
		if err == nil && (!info.Mode().IsRegular() || info.Size() == 0) {
			err = ErrCorrupt
		}
	}
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{"_txlock": {"immediate"}, "_pragma": {"busy_timeout(10000)", "journal_mode(WAL)", "synchronous(FULL)"}, "mode": {"rw"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &SQLite{db: db, identity: identity}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	ctx := context.Background()
	var mode string
	var synchronous int
	if err = db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		return nil, ErrCorrupt
	}
	if err = db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&synchronous); err != nil || mode != "wal" || synchronous != 2 {
		return nil, ErrCorrupt
	}
	if created {
		err = s.transaction(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, schema); err != nil {
				return err
			}
			m, _ := json.Marshal(manifest{FormatVersion, identity})
			if err := putMeta(ctx, tx, "manifest", m); err != nil {
				return err
			}
			if err := putProto(ctx, tx, "hardstate", &pb.HardState{}); err != nil {
				return err
			}
			if err := putMeta(ctx, tx, "log_tip", indexKey(0)); err != nil {
				return err
			}
			return putApplication(ctx, tx, Application{ConfState: &pb.ConfState{}})
		})
		if err != nil {
			return nil, err
		}
		// SQLite syncs the WAL before COMMIT returns. Sync the containing
		// directory too so first creation survives losing power immediately.
		dir, err := os.Open(filepath.Dir(abs))
		if err != nil {
			return nil, err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if _, err = s.Recover(ctx); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *SQLite) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	return s.db.Close()
}

func indexKey(index uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, index)
	return b
}

// Checksums bind each payload to its table and key, detecting both bit damage
// and misplaced rows. They detect corruption, not a malicious storage operator.
func checksum(parts ...[]byte) []byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(indexKey(uint64(len(p))))
		h.Write(p)
	}
	return h.Sum(nil)
}

func putMeta(ctx context.Context, tx *sql.Tx, key string, data []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET data=excluded.data,checksum=excluded.checksum`, key, data, checksum([]byte("meta"), []byte(key), data))
	return err
}

func getMeta(ctx context.Context, tx *sql.Tx, key string) ([]byte, error) {
	var data, hash []byte
	if err := tx.QueryRowContext(ctx, `SELECT data,checksum FROM meta WHERE key=?`, key).Scan(&data, &hash); err != nil {
		return nil, ErrCorrupt
	}
	if !bytes.Equal(hash, checksum([]byte("meta"), []byte(key), data)) {
		return nil, ErrCorrupt
	}
	return data, nil
}

func putProto(ctx context.Context, tx *sql.Tx, key string, v proto.Message) error {
	data, err := wire.Marshal(v)
	if err != nil {
		return ErrBatch
	}
	return putMeta(ctx, tx, key, data)
}

func getProto(ctx context.Context, tx *sql.Tx, key string, v proto.Message) error {
	data, err := getMeta(ctx, tx, key)
	if err != nil {
		return err
	}
	if proto.Unmarshal(data, v) != nil || len(v.ProtoReflect().GetUnknown()) != 0 {
		return ErrCorrupt
	}
	return nil
}

func putApplication(ctx context.Context, tx *sql.Tx, a Application) error {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if err := putMeta(ctx, tx, "application", data); err != nil {
		return err
	}
	return putProto(ctx, tx, "confstate", a.ConfState)
}

func getApplication(ctx context.Context, tx *sql.Tx) (Application, error) {
	var a Application
	data, err := getMeta(ctx, tx, "application")
	if err != nil {
		return a, err
	}
	if json.Unmarshal(data, &a) != nil {
		return a, ErrCorrupt
	}
	a.ConfState = &pb.ConfState{}
	err = getProto(ctx, tx, "confstate", a.ConfState)
	return a, err
}

func (s *SQLite) boundary(name string) {
	if s.cut != nil {
		s.cut(name)
	}
}

// A failed storage operation poisons the handle. Even a COMMIT error can have
// an unknown durable outcome; only recovery may decide which bytes survived.
func (s *SQLite) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.stopped {
		return ErrStopped
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.stopped = true
		return err
	}
	defer tx.Rollback()
	s.boundary("after-begin")
	if err = fn(tx); err != nil {
		if !errors.Is(err, ErrBatch) && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrSnapshot) {
			s.stopped = true
		}
		return err
	}
	s.boundary("before-commit")
	if err = tx.Commit(); err != nil {
		s.stopped = true
		return err
	}
	s.boundary("after-commit")
	return nil
}

// Persist returns only after FULL-synchronous COMMIT. Submit Persisted to the
// core only on success; a queued write or transport receipt is insufficient.
// Snapshot input is explicitly refused until P05 supplies atomic installation.
func (s *SQLite) Persist(ctx context.Context, batch raftcore.PersistBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return ErrStopped
	}
	if !raft.IsEmptySnap(batch.Snapshot) {
		return ErrSnapshot
	}
	if len(batch.Entries) == 0 && raft.IsEmptyHardState(batch.HardState) {
		return nil
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		old := &pb.HardState{}
		if err := getProto(ctx, tx, "hardstate", old); err != nil {
			return err
		}
		h := batch.HardState
		if raft.IsEmptyHardState(h) {
			h = old
		}
		if len(h.ProtoReflect().GetUnknown()) != 0 || h.GetTerm() < old.GetTerm() || h.GetCommit() < old.GetCommit() || (h.GetTerm() == old.GetTerm() && old.GetVote() != 0 && h.GetVote() != old.GetVote()) {
			return ErrBatch
		}
		last, err := lastEntry(ctx, tx)
		if err != nil {
			return err
		}
		tip, err := getMeta(ctx, tx, "log_tip")
		if err != nil || !bytes.Equal(tip, indexKey(last.GetIndex())) {
			return ErrCorrupt
		}
		if len(batch.Entries) != 0 {
			first := batch.Entries[0].GetIndex()
			if first == 0 || (last.GetIndex() != ^uint64(0) && first > last.GetIndex()+1) {
				return ErrBatch
			}
			previous := &pb.Entry{}
			if first > 1 {
				previous, err = getEntry(ctx, tx, first-1)
				if err != nil {
					return err
				}
			}
			for i, entry := range batch.Entries {
				if entry == nil || previous.GetIndex() == ^uint64(0) || entry.GetIndex() != previous.GetIndex()+1 || entry.GetTerm() == 0 || entry.GetTerm() < previous.GetTerm() || !validEntry(entry) {
					return ErrBatch
				}
				if entry.GetIndex() <= old.GetCommit() {
					stored, err := getEntry(ctx, tx, entry.GetIndex())
					if err != nil {
						return err
					}
					if !proto.Equal(stored, entry) {
						return ErrBatch
					}
				}
				previous = entry
				if i == len(batch.Entries)-1 && entry.GetIndex() < old.GetCommit() {
					return ErrBatch
				}
			}
			last = previous
			if h.GetCommit() > last.GetIndex() || h.GetTerm() < last.GetTerm() {
				return ErrBatch
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM raft_log WHERE idx>=?`, indexKey(first)); err != nil {
				return err
			}
			s.boundary("after-truncate")
			for _, entry := range batch.Entries {
				data, err := wire.Marshal(entry)
				if err != nil {
					return ErrBatch
				}
				key := indexKey(entry.GetIndex())
				if _, err := tx.ExecContext(ctx, `INSERT INTO raft_log VALUES (?,?,?)`, key, data, checksum([]byte("raft_log"), key, data)); err != nil {
					return err
				}
				s.boundary("after-entry")
			}
		}
		if h.GetCommit() > last.GetIndex() || h.GetTerm() < last.GetTerm() || (h.GetTerm() == 0 && h.GetVote() != 0) {
			return ErrBatch
		}
		if !raft.IsEmptyHardState(batch.HardState) {
			if err := putProto(ctx, tx, "hardstate", batch.HardState); err != nil {
				return err
			}
			s.boundary("after-hardstate")
		}
		if err := putMeta(ctx, tx, "log_tip", indexKey(last.GetIndex())); err != nil {
			return err
		}
		s.boundary("after-log-tip")
		return nil
	})
}

func validEntry(e *pb.Entry) bool {
	if e == nil || len(e.ProtoReflect().GetUnknown()) != 0 {
		return false
	}
	switch e.GetType() {
	case pb.EntryNormal, pb.EntryConfChange, pb.EntryConfChangeV2:
		return true
	default:
		return false
	}
}

func validKey(k Key) bool {
	if k.Key == "" {
		return false
	}
	switch k.Namespace {
	case State, Sessions, Membership, Enrollment, Tombstones, Outbox:
		return true
	default:
		return false
	}
}

// Apply compares the read-set's applied index, checks every input against the
// durable committed log, and commits rows, dedup results and metadata together.
// The caller may acknowledge clients and submit Applied ONLY after success.
func (s *SQLite) Apply(ctx context.Context, batch raftcore.ApplyBatch, update Update) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return ErrStopped
	}
	if !raft.IsEmptySnap(batch.Snapshot) {
		return ErrSnapshot
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		a, err := getApplication(ctx, tx)
		if err != nil {
			return err
		}
		if update.ExpectedApplied != a.Applied {
			return ErrConflict
		}
		h := &pb.HardState{}
		if err := getProto(ctx, tx, "hardstate", h); err != nil {
			return err
		}
		if len(batch.Entries) == 0 || batch.Index <= a.Applied || batch.Index > h.GetCommit() || batch.ConfState == nil || len(batch.ConfState.ProtoReflect().GetUnknown()) != 0 || update.LogicalTime < a.LogicalTime || update.VisibleVersion < a.VisibleVersion {
			return ErrBatch
		}
		previous := a.Applied
		for _, entry := range batch.Entries {
			if entry == nil || previous == ^uint64(0) || entry.GetIndex() != previous+1 {
				return ErrBatch
			}
			stored, err := getEntry(ctx, tx, entry.GetIndex())
			if err != nil {
				return err
			}
			if !proto.Equal(stored, entry) {
				return ErrBatch
			}
			previous = entry.GetIndex()
		}
		if previous != batch.Index {
			return ErrBatch
		}
		seen := make(map[Key]bool, len(update.Writes))
		for _, w := range update.Writes {
			if !validKey(w.Key) || seen[w.Key] || len(w.Value) > MaxRecordBytes || (w.Delete && len(w.Value) != 0) {
				return ErrBatch
			}
			seen[w.Key] = true
			var oldData, oldHash []byte
			err = tx.QueryRowContext(ctx, `SELECT data,checksum FROM records WHERE namespace=? AND key=?`, w.Namespace, w.Key.Key).Scan(&oldData, &oldHash)
			if err == nil {
				if a.RecordCount == 0 || !bytes.Equal(oldHash, checksum([]byte("records"), []byte(w.Namespace), []byte(w.Key.Key), oldData)) {
					return ErrCorrupt
				}
				a.RecordCount--
				xorHash(&a.RecordsHash, oldHash)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if w.Delete {
				_, err = tx.ExecContext(ctx, `DELETE FROM records WHERE namespace=? AND key=?`, w.Namespace, w.Key.Key)
			} else {
				data := w.Value
				if data == nil {
					data = []byte{}
				}
				hash := checksum([]byte("records"), []byte(w.Namespace), []byte(w.Key.Key), data)
				_, err = tx.ExecContext(ctx, `INSERT INTO records VALUES (?,?,?,?) ON CONFLICT(namespace,key) DO UPDATE SET data=excluded.data,checksum=excluded.checksum`, w.Namespace, w.Key.Key, data, hash)
				if a.RecordCount == ^uint64(0) {
					return ErrCorrupt
				}
				a.RecordCount++
				xorHash(&a.RecordsHash, hash)
			}
			if err != nil {
				return err
			}
			s.boundary("after-row")
		}
		a.Applied, a.VisibleVersion, a.LogicalTime, a.ConfState = batch.Index, update.VisibleVersion, update.LogicalTime, batch.ConfState
		data, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if err := putMeta(ctx, tx, "application", data); err != nil {
			return err
		}
		s.boundary("after-application-header")
		if err := putProto(ctx, tx, "confstate", a.ConfState); err != nil {
			return err
		}
		s.boundary("after-configuration")
		return nil
	})
}

func xorHash(target *[32]byte, hash []byte) {
	for i := range target {
		target[i] ^= hash[i]
	}
}

// Read supplies immutable copies at exactly expectedApplied. A missing key is
// absent from the map; it is not confused with an existing empty opaque value.
// This is a LOCAL storage read, never a linearizable directory door by itself.
func (s *SQLite) Read(ctx context.Context, expectedApplied uint64, keys []Key) (map[Key][]byte, Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[Key][]byte, len(keys))
	var a Application
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		a, err = getApplication(ctx, tx)
		if err != nil {
			return err
		}
		if a.Applied != expectedApplied {
			return ErrConflict
		}
		readBytes := 0
		for _, k := range keys {
			if !validKey(k) {
				return ErrBatch
			}
			var data, hash []byte
			err := tx.QueryRowContext(ctx, `SELECT data,checksum FROM records WHERE namespace=? AND key=?`, k.Namespace, k.Key).Scan(&data, &hash)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if !bytes.Equal(hash, checksum([]byte("records"), []byte(k.Namespace), []byte(k.Key), data)) {
				return ErrCorrupt
			}
			readBytes += len(data)
			if readBytes > MaxRecordBytes {
				return ErrBatch
			}
			out[k] = data
		}
		return nil
	})
	if err != nil {
		return nil, Application{}, err
	}
	return out, a, nil
}
