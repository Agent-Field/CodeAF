package workspace

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Version 1 was collections and memberships. Version 2 adds shared sourced
// context beside them, without touching a single version 1 row.
const schemaVersion = 2
const applicationID = 0x4146434c // AFCL distinguishes this store from optional memory.

// The version 1 tables, spelled exactly as version 1 created them. A store that
// was written by version 1 is upgraded by adding to it, never by rebuilding it.
const collectionSchema = `
CREATE TABLE collections (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL
);
CREATE TABLE memberships (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
`

// The version 2 addition. A context has one identity and a pointer to its
// current revision; revisions and their target sets are written once and never
// edited, which is what makes a revision safe to read while a peer writes one.
const contextSchema = `
CREATE TABLE contexts (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 revision INTEGER NOT NULL CHECK(revision > 0)
);
CREATE TABLE context_revisions (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 context_id TEXT NOT NULL REFERENCES contexts(id),
 revision INTEGER NOT NULL CHECK(revision > 0),
 title TEXT NOT NULL,
 text TEXT NOT NULL,
 source_kind TEXT NOT NULL CHECK(source_kind IN ('conversation','task','artifact')),
 source_id TEXT NOT NULL,
 source_session TEXT NOT NULL,
 withdrawn INTEGER NOT NULL CHECK(withdrawn IN (0,1)),
 UNIQUE(context_id,revision)
);
CREATE TABLE context_targets (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 context_id TEXT NOT NULL,
 revision INTEGER NOT NULL,
 position INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 FOREIGN KEY(context_id,revision) REFERENCES context_revisions(context_id,revision),
 UNIQUE(context_id,revision,kind,ref_id,session_id)
);
CREATE INDEX context_targets_reference ON context_targets(kind,ref_id,session_id);
`

// LOCK WAITS ARE BOUNDED so a peer holding the writer cannot hang a command
// indefinitely. The pragma is built from this constant rather than repeating
// the number, because a bound that appears twice is a bound that drifts.
const busyTimeout = time.Second

// Store owns collection metadata and sourced context revisions. Transcripts, task checkpoints, standing
// items and artifacts retain their existing owners and persistence formats.
type Store struct{ db *sql.DB }

// Open creates a private collection store or checks an existing one. Unknown
// databases fail explicitly; they must never be interpreted as empty state.
func Open(path string) (*Store, error) {
	absolute, err := storePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return openStore(absolute)
}

// OpenExisting opens a store that must already be there. It creates neither the
// directory nor the file, so A READ PATH CANNOT BRING A REMOVED DATABASE BACK as
// an empty one: a caller who deletes the file and then reads is told the store is
// gone rather than handed a store with nothing in it. The absence is reported as
// the operating system's own error, so `errors.Is(err, os.ErrNotExist)` answers
// it. A version 1 store opened this way is still upgraded in place, because an
// upgrade adds to a database that exists rather than creating one.
func OpenExisting(path string) (*Store, error) {
	absolute, err := storePath(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(absolute); err != nil {
		return nil, err
	}
	return openStore(absolute)
}

func storePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: empty database path", ErrInvalid)
	}
	return filepath.Abs(path)
}

// openStore is the one place this package spells how a database file is opened,
// so the creating door and the read-only door cannot drift apart in their
// pragmas or their locking.
func openStore(absolute string) (*Store, error) {
	u := url.URL{Scheme: "file", Path: absolute}
	q := u.Query()
	// The caller either created the private file or required it to exist. SQLite
	// must not create another one with its default permissions if the path is a
	// dangling link or disappears underneath us.
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
	if err := s.initialize(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open collections: %w", err)
	}
	return s, nil
}

// schemaReader is the surface initialization needs, satisfied by both the
// database handle and a transaction, so the same verification runs inside the
// upgrade's writer and outside it.
type schemaReader interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func stamp(q schemaReader) (app, version int, err error) {
	if err := q.QueryRow("PRAGMA application_id").Scan(&app); err != nil {
		return 0, 0, err
	}
	if err := q.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, 0, err
	}
	return app, version, nil
}

func (s *Store) initialize() error {
	app, version, err := stamp(s.db)
	if err != nil {
		return err
	}
	// OPENING A STORE THAT IS ALREADY AT THIS VERSION TAKES NO WRITER. This is by
	// far the common case — every command after the first — and every one of them
	// used to begin an immediate transaction, so a peer that merely held the
	// writer made an ordinary read fail busy. Reading the stamp and preparing the
	// reads needs only a shared lock, which a reserved writer allows. Creation and
	// the version 1 upgrade still run in the immediate transaction below, and both
	// re-read the stamp inside it, so a peer that upgrades between these two steps
	// is seen rather than raced.
	if app == applicationID && version == schemaVersion {
		// Preparing the actual reads catches missing tables before reporting a
		// successful open. We never recreate a damaged initialized schema.
		return verifySchema(s.db)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tables int
	if app, version, err = stamp(tx); err != nil {
		return err
	}
	switch {
	case app == applicationID && version == schemaVersion:
		if err := verifySchema(tx); err != nil {
			return err
		}
		return tx.Commit()
	case app == applicationID && version == 1:
		// AN UPGRADE MUST FIND THE OLD STORE INTACT BEFORE IT ADDS ANYTHING, so a
		// database that claims version 1 with a damaged half of that schema is
		// refused rather than quietly completed at the new version. The whole
		// upgrade — the new tables and the version stamp — is this one immediate
		// transaction, so an interrupted process leaves a working version 1 store.
		if err := verifyCollectionSchema(tx); err != nil {
			return fmt.Errorf("refusing to upgrade a damaged version 1 store: %w", err)
		}
		if _, err := tx.Exec(contextSchema); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); err != nil {
			return err
		}
		if err := verifySchema(tx); err != nil {
			return err
		}
		return tx.Commit()
	case app != 0 || version != 0:
		return fmt.Errorf("unsupported collections database (application %d, version %d)", app, version)
	}
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return err
	}
	if tables != 0 {
		return errors.New("this database belongs to another feature; choose a separate collections database")
	}
	if _, err := tx.Exec(collectionSchema + contextSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// verifySchema reads every table this version depends on. A store is only
// reported as open once all of them answer; a missing half is never an empty set.
func verifySchema(q schemaReader) error {
	if err := verifyCollectionSchema(q); err != nil {
		return err
	}
	for _, read := range []string{
		"SELECT seq,id,revision FROM contexts LIMIT 0",
		"SELECT seq,context_id,revision,title,text,source_kind,source_id,source_session,withdrawn FROM context_revisions LIMIT 0",
		"SELECT seq,context_id,revision,position,kind,ref_id,session_id FROM context_targets LIMIT 0",
	} {
		if _, err := q.Exec(read); err != nil {
			return err
		}
	}
	return nil
}

func verifyCollectionSchema(q schemaReader) error {
	for _, read := range []string{
		"SELECT seq,id,name FROM collections LIMIT 0",
		"SELECT seq,collection_id,kind,ref_id,session_id,target_collection FROM memberships LIMIT 0",
	} {
		if _, err := q.Exec(read); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, name string) (Collection, error) {
	if err := ValidateName(name); err != nil {
		return Collection{}, err
	}
	id, err := newID()
	if err != nil {
		return Collection{}, err
	}
	c := Collection{ID: id, Name: name}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO collections(id,name) VALUES (?,?)", c.ID, c.Name); err != nil {
		return Collection{}, err
	}
	return c, nil
}

// newID mints the stable identity a record keeps for its whole life. Names,
// wording, membership and revisions all change around it; this does not.
func newID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func (s *Store) Rename(ctx context.Context, id, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE collections SET name=? WHERE id=?", name, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name FROM collections ORDER BY seq")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readCollections(rows)
}

func readCollections(rows *sql.Rows) ([]Collection, error) {
	result := make([]Collection, 0)
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func requireCollection(ctx context.Context, tx *sql.Tx, id string) error {
	var found int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id=?", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) Add(ctx context.Context, id string, ref Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return err
	}
	var target any
	if ref.Kind == CollectionKind {
		if err := requireCollection(ctx, tx, ref.ID); err != nil {
			return err
		}
		// THE CYCLE CHECK AND INSERT SHARE THE WRITER TRANSACTION. Two
		// processes cannot both approve opposite edges against an old snapshot.
		var cycle bool
		err := tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
 VALUES (?) UNION
 SELECT m.ref_id FROM memberships m JOIN descendants d ON m.collection_id=d.id WHERE m.kind='collection'
) SELECT EXISTS(SELECT 1 FROM descendants WHERE id=?)`, ref.ID, id).Scan(&cycle)
		if err != nil {
			return err
		}
		if cycle {
			return ErrCycle
		}
		target = ref.ID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES (?,?,?,?,?) ON CONFLICT(collection_id,kind,ref_id,session_id) DO NOTHING`, id, ref.Kind, ref.ID, ref.SessionID, target)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Remove detaches a reference; it never removes or stops the referenced work.
func (s *Store) Remove(ctx context.Context, id string, ref Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM memberships WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=?", id, ref.Kind, ref.ID, ref.SessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Members(ctx context.Context, id string) ([]Ref, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM collections WHERE id=?)", id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, "SELECT kind,ref_id,session_id FROM memberships WHERE collection_id=? ORDER BY seq", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Ref, 0)
	for rows.Next() {
		var ref Ref
		if err := rows.Scan(&ref.Kind, &ref.ID, &ref.SessionID); err != nil {
			return nil, err
		}
		result = append(result, ref)
	}
	return result, rows.Err()
}

func (s *Store) CollectionsFor(ctx context.Context, ref Ref) ([]Collection, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name FROM collections c JOIN memberships m ON m.collection_id=c.id
 WHERE m.kind=? AND m.ref_id=? AND m.session_id=? ORDER BY c.seq`, ref.Kind, ref.ID, ref.SessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readCollections(rows)
}
