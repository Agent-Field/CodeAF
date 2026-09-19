package wsdiscover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// AFDS distinguishes this derived index from collections.db (AFCL) so a
// misplaced path is refused rather than treated as an empty discovery store.
const applicationID = 0x41464453

const schemaVersion = 1

const busyTimeout = 10 * time.Second

type schemaState uint8

const (
	schemaBlank schemaState = iota
	schemaReady
	schemaRefused
)

// Store is one discovery.db handle. Opening a missing file creates the
// private file; the schema is written by the first ingest so a read of a
// path nobody has indexed yet does not take the writer lock.
type Store struct {
	db       *sql.DB
	schemaMu sync.Mutex
	ready    bool
}

type schemaQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Open creates a private discovery store or checks an existing one. A
// collections file, a future schema, or a stranger's SQLite is refused.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: empty database path", ErrInvalid)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(absolute); errors.Is(err, os.ErrNotExist) {
		f, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err == nil {
			if err := f.Close(); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := u.Query()
	q.Set("mode", "rw")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	state, _, err := s.snapshotSchema(context.Background())
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s.ready = state == schemaReady
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) snapshotSchema(ctx context.Context) (schemaState, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return schemaBlank, 0, err
	}
	defer tx.Rollback()
	return inspectSchema(ctx, tx)
}

func inspectSchema(ctx context.Context, q schemaQuerier) (schemaState, int, error) {
	if err := ctx.Err(); err != nil {
		return schemaBlank, 0, err
	}
	var app, version int
	if err := q.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return schemaBlank, 0, err
	}
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return schemaBlank, 0, err
	}
	if app == applicationID && version == schemaVersion {
		if err := verifyTables(ctx, q); err != nil {
			return schemaReady, version, err
		}
		return schemaReady, version, nil
	}
	if app != 0 || version != 0 {
		return schemaRefused, version, fmt.Errorf("%w: application %d version %d", ErrForeign, app, version)
	}
	var tables int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return schemaBlank, 0, err
	}
	if tables != 0 {
		return schemaRefused, 0, ErrForeign
	}
	return schemaBlank, 0, nil
}

func verifyTables(ctx context.Context, q schemaQuerier) error {
	if _, err := q.ExecContext(ctx, "SELECT session_id,generation,ordinal,content_hash FROM cursors LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT id,session_id,generation,ordinal,content_hash,source_ref,speaker,text,model,version,dimension,vector FROM passages LIMIT 0"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "SELECT session_id,state,generation FROM sources LIMIT 0"); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "SELECT session_id,speaker,text FROM passages_fts LIMIT 0")
	return err
}

func (s *Store) ensureSchema(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.ready {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, _, err := inspectSchema(ctx, tx)
	if err != nil {
		return err
	}
	switch state {
	case schemaReady:
	case schemaBlank:
		if err := createSchema(ctx, tx); err != nil {
			return err
		}
	default:
		return ErrForeign
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.ready = true
	return nil
}

func (s *Store) readyForRead(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.ready {
		return true, nil
	}
	state, _, err := s.snapshotSchema(ctx)
	if err != nil {
		return false, err
	}
	s.ready = state == schemaReady
	return s.ready, nil
}

const schemaDDL = `
CREATE TABLE sources (
 session_id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK(state IN ('present','deleted','unavailable')),
 generation INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE cursors (
 session_id TEXT NOT NULL,
 generation INTEGER NOT NULL,
 ordinal INTEGER NOT NULL,
 content_hash TEXT NOT NULL,
 PRIMARY KEY (session_id, generation, ordinal, content_hash)
);
CREATE TABLE passages (
 id INTEGER PRIMARY KEY,
 session_id TEXT NOT NULL,
 generation INTEGER NOT NULL,
 ordinal INTEGER NOT NULL,
 content_hash TEXT NOT NULL,
 source_ref TEXT NOT NULL,
 speaker TEXT NOT NULL,
 text TEXT NOT NULL,
 model TEXT NOT NULL DEFAULT '',
 version TEXT NOT NULL DEFAULT '',
 dimension INTEGER NOT NULL DEFAULT 0,
 vector BLOB,
 UNIQUE(session_id, generation, ordinal, content_hash)
);
CREATE INDEX passages_session_gen ON passages(session_id, generation);
CREATE VIRTUAL TABLE passages_fts USING fts5(
 session_id UNINDEXED,
 speaker UNINDEXED,
 text
);
`

func createSchema(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, schemaDDL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d", applicationID)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}
