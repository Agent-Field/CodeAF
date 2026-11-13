// Package docs keeps an organization's live documents: the pages and canvas
// text frames two people type into at once. Each document is a Yjs document
// on the desks that edit it; HERE IT IS ONLY BYTES. The server never parses a
// Yjs update, never merges two, and never makes a snapshot of its own. It
// stamps every update it accepts with a sequence number, the person who sent
// it and the time, keeps them in order, and hands them to everyone else who
// has the document open.
//
// Per document the store keeps an append-only log of the updates since the
// latest snapshot, and that snapshot: a client-made Y.encodeStateAsUpdate
// with the sequence number it covers. A snapshot replaces the log it covers,
// which is the whole of compaction: the server drops the covered prefix, and
// a late reader gets the snapshot plus what came after it.
//
// THE COMPACTION RULE. A log past [SoftUpdates] updates or [SoftBytes] bytes
// keeps accepting updates and answers every one with compact=true, which asks
// the writer's desk to post a snapshot. A log past [HardUpdates] or
// [HardBytes] refuses new updates with [ErrSnapshotNeeded] until a snapshot
// arrives. Typing never stops at the soft line, and the hard line bounds both
// the disk and the read a newcomer must make; a desk that compacts at the
// soft line never meets the hard one.
package docs

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	_ "modernc.org/sqlite" // the store's driver
)

// File is the store's database under the factory root.
const File = "factory-docs.db"

// The limits, in one place so the wire, the manual and the tests read the
// same figures.
const (
	// UpdateMost is the largest one update may be, decoded. A paste of a
	// long document is one update; this is far past any paste a person makes.
	UpdateMost = 256 << 10
	// SnapshotMost is the largest one snapshot may be, decoded. Its base64
	// fits under the 4 MiB request line a desk may send its engine, and a
	// read of the whole document (this plus a log at [HardBytes]) under the
	// 8 MiB answer the engine reads from a server.
	SnapshotMost = 2 << 20
	// SoftUpdates and SoftBytes are where every accepted update starts
	// answering compact=true.
	SoftUpdates = 500
	SoftBytes   = 512 << 10
	// HardUpdates and HardBytes are where new updates are refused until a
	// snapshot arrives.
	HardUpdates = 5000
	HardBytes   = 2 << 20
)

var (
	// ErrSnapshotNeeded refuses an update to a log at the hard line.
	ErrSnapshotNeeded = errors.New("this document has too many changes since its last snapshot; post a snapshot, then send again")
	// ErrSeeded refuses a seed to a document that already has content.
	ErrSeeded = errors.New("this document already has content; read it, then edit it")
	// ErrAhead refuses a snapshot or a base that names a change the server
	// never stamped: the client's copy is from a document that was reset.
	ErrAhead = errors.New("this document was reset since you read it; read it again")
)

// Update is one stamped update.
type Update struct {
	Seq  int64     `json:"seq"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Data []byte    `json:"update"`
}

// Snapshot is the latest client-made state and the sequence it covers.
type Snapshot struct {
	Seq   int64     `json:"seq"`
	By    string    `json:"by"`
	At    time.Time `json:"at"`
	State []byte    `json:"state"`
}

// State is a read of one document after a sequence number: the snapshot when
// the reader is behind it, then every update after.
type State struct {
	Seq      int64     // the newest stamped sequence number, 0 for none
	Snapshot *Snapshot // nil when the reader needs none
	Updates  []Update
	// Pending and PendingBytes are the log since the snapshot.
	Pending      int
	PendingBytes int64
}

// Compact is whether the log has crossed the soft line.
func (s State) Compact() bool { return compactAt(s.Pending, s.PendingBytes) }

func compactAt(n int, bytes int64) bool { return n >= SoftUpdates || bytes >= SoftBytes }

// Store is the documents' database. Its methods are safe for concurrent use;
// writes are serialized by the store's one connection.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens or makes the store at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("docs store directory: %w", err)
	}
	db, err := sql.Open("sqlite", sqlitefile.URI(path, "_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"))
	if err != nil {
		return nil, err
	}
	// ONE CONNECTION, so the pragmas land on the connection every write
	// uses, and the store's own writes never race each other for the lock.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("docs store schema: %w", err)
	}
	return &Store{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS docs (
	doc       TEXT PRIMARY KEY,
	head      INTEGER NOT NULL DEFAULT 0,
	snap_seq  INTEGER NOT NULL DEFAULT 0,
	snap_by   TEXT NOT NULL DEFAULT '',
	snap_at   TEXT NOT NULL DEFAULT '',
	snap      BLOB,
	log_count INTEGER NOT NULL DEFAULT 0,
	log_bytes INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS updates (
	doc  TEXT NOT NULL,
	seq  INTEGER NOT NULL,
	by   TEXT NOT NULL,
	at   TEXT NOT NULL,
	data BLOB NOT NULL,
	PRIMARY KEY (doc, seq)
) WITHOUT ROWID;
`

// Close closes the database.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return s.db.Close()
}

type head struct {
	seq, snapSeq int64
	count        int
	bytes        int64
}

func readHead(tx *sql.Tx, doc string) (head, error) {
	var h head
	err := tx.QueryRow(`SELECT head, snap_seq, log_count, log_bytes FROM docs WHERE doc = ?`, doc).Scan(&h.seq, &h.snapSeq, &h.count, &h.bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return head{}, nil
	}
	return h, err
}

// Append stamps data as the document's next update, by at. base is the
// newest sequence number the sender had seen; one past the head is a client
// of a document that was reset ([ErrAhead]). seed accepts the update only
// into a document with nothing in it ([ErrSeeded]), so two desks opening a
// page for the first time cannot both put its text in. compact is whether
// the log is past the soft line after this update. after runs, still under
// the store's lock, once the update is committed, so whatever it publishes
// leaves in sequence order.
func (s *Store) Append(doc, by string, at time.Time, data []byte, base int64, seed bool, after func(Update)) (Update, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Update{}, false, err
	}
	defer tx.Rollback()
	h, err := readHead(tx, doc)
	if err != nil {
		return Update{}, false, err
	}
	switch {
	case base > h.seq:
		return Update{}, false, ErrAhead
	case seed && (h.seq != 0 || h.snapSeq != 0):
		return Update{}, false, ErrSeeded
	case h.count >= HardUpdates || h.bytes+int64(len(data)) > HardBytes:
		return Update{}, false, ErrSnapshotNeeded
	}
	u := Update{Seq: h.seq + 1, By: by, At: at.UTC(), Data: data}
	if _, err := tx.Exec(`INSERT INTO updates (doc, seq, by, at, data) VALUES (?, ?, ?, ?, ?)`, doc, u.Seq, by, stamp(u.At), data); err != nil {
		return Update{}, false, err
	}
	if _, err := tx.Exec(`INSERT INTO docs (doc, head, log_count, log_bytes) VALUES (?, ?, 1, ?)
		ON CONFLICT(doc) DO UPDATE SET head = excluded.head, log_count = log_count + 1, log_bytes = log_bytes + excluded.log_bytes`,
		doc, u.Seq, len(data)); err != nil {
		return Update{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Update{}, false, err
	}
	if after != nil {
		after(u)
	}
	return u, compactAt(h.count+1, h.bytes+int64(len(data))), nil
}

// Since reads doc for a reader who has everything up to since. A reader
// behind the snapshot, or one past the head (its copy is of a document that
// was reset), gets the snapshot and every update after it; 0 is a reader
// with nothing.
func (s *Store) Since(doc string, since int64) (State, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	var (
		st     State
		snapBy string
		snapAt string
		snap   []byte
	)
	var snapSeq int64
	err = tx.QueryRow(`SELECT head, snap_seq, snap_by, snap_at, snap, log_count, log_bytes FROM docs WHERE doc = ?`, doc).
		Scan(&st.Seq, &snapSeq, &snapBy, &snapAt, &snap, &st.Pending, &st.PendingBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	from := since
	if since > st.Seq || since < snapSeq {
		from = 0
		if snapSeq > 0 {
			st.Snapshot = &Snapshot{Seq: snapSeq, By: snapBy, At: parse(snapAt), State: snap}
			from = snapSeq
		}
	}
	rows, err := tx.Query(`SELECT seq, by, at, data FROM updates WHERE doc = ? AND seq > ? ORDER BY seq`, doc, from)
	if err != nil {
		return State{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var u Update
		var at string
		if err := rows.Scan(&u.Seq, &u.By, &at, &u.Data); err != nil {
			return State{}, err
		}
		u.At = parse(at)
		st.Updates = append(st.Updates, u)
	}
	return st, rows.Err()
}

// Head is doc's last seq: 0 for a document nothing was ever written into.
func (s *Store) Head(doc string) (int64, error) {
	var head int64
	err := s.db.QueryRow(`SELECT head FROM docs WHERE doc = ?`, doc).Scan(&head)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return head, err
}

// PutSnapshot keeps state as the document's snapshot covering seq, and drops
// the updates it covers. A snapshot at or behind the one kept is not an
// error, because two desks may compact at once: kept is false and nothing
// changes. One past the head is [ErrAhead].
func (s *Store) PutSnapshot(doc, by string, at time.Time, seq int64, state []byte) (kept bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	h, err := readHead(tx, doc)
	if err != nil {
		return false, err
	}
	if seq > h.seq {
		return false, ErrAhead
	}
	if seq <= h.snapSeq {
		return false, nil
	}
	if _, err := tx.Exec(`DELETE FROM updates WHERE doc = ? AND seq <= ?`, doc, seq); err != nil {
		return false, err
	}
	var n int
	var bytes int64
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(SUM(LENGTH(data)), 0) FROM updates WHERE doc = ?`, doc).Scan(&n, &bytes); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE docs SET snap_seq = ?, snap_by = ?, snap_at = ?, snap = ?, log_count = ?, log_bytes = ? WHERE doc = ?`,
		seq, by, stamp(at), state, n, bytes, doc); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Drop forgets doc entirely: its log and its snapshot. The next writer
// starts it again at sequence 1.
func (s *Store) Drop(doc string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM updates WHERE doc = ?`, doc); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM docs WHERE doc = ?`, doc); err != nil {
		return err
	}
	return tx.Commit()
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parse(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
