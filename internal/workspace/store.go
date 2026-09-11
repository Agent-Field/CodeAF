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
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Version 1 was collections and memberships. Version 2 adds shared sourced
// context beside them. Version 3 adds explicit governing placements, without
// promoting any existing reference membership into governing scope. Version 4
// adds the direction record tables (direction_schema.go) and store_meta.
const schemaVersion = 4

// AN ORDINARY OPEN DOES NOT UPGRADE A STORE TO VERSION 4. Open and OpenExisting
// bring a store up to version 3 and read a version 4 store as it stands; only
// EnsureDirection, which the direction package calls when it opens the store,
// adds the version 4 tables. Nothing a person runs today reaches that door, so
// the owner's store stays at version 3 — readable by every build before this
// one — until the first lane that writes direction records ships. That keeps
// the version 4 tables inert in the plain sense: they are not even created
// until something needs them.
const ordinaryVersion = 3

// additions[v] is what version v adds to version v-1, spelled once, so that a
// fresh store and an upgraded one are built from the same text.
var additions = map[int]string{
	2: contextSchema,
	3: placementSchema,
	4: directionSchema + storeMetaSchema,
}

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

// A STORE STAMPED NEWER THAN THIS BUILD SAYS WHO MAY STILL READ IT. A newer
// version records, in store_meta, the oldest version whose tables it left
// readable and writable exactly as that version wrote them. This build opens a
// newer store only when that version is at most its own, and then touches only
// the tables it knows. Without this, every version bump would strand every
// older binary: the version check below refused anything it did not know, which
// made an upgrade one-way. Version 4 is the first to write the table.
const (
	storeMetaTable   = "store_meta"
	minReaderVersion = "min_reader_version"
)

// VERSION 4 LEAVES EVERY VERSION 3 TABLE EXACTLY AS VERSION 3 READS AND WRITES
// IT: the direction tables are separate, reference no version 3 table, and hold
// nothing a version 3 write could make inconsistent. So a version 3 build that
// knows store_meta may keep using a version 4 store, and says so here.
const directionMinReader = 3

var storeMetaSchema = fmt.Sprintf(`
CREATE TABLE %s (
 key TEXT PRIMARY KEY,
 value TEXT NOT NULL
);
INSERT INTO %[1]s(key,value) VALUES ('%s','%d');
`, storeMetaTable, minReaderVersion, directionMinReader)

// ErrNewerStore reports a store written by a newer build that has not declared
// this build able to read it. The store is left exactly as it was found.
var ErrNewerStore = errors.New("collections database was written by a newer aforge")

// LOCK WAITS ARE BOUNDED so a peer holding the writer cannot hang a command
// indefinitely. The pragma is built from this constant rather than repeating
// the number, because a bound that appears twice is a bound that drifts.
const busyTimeout = time.Second

// Statistics are refreshed when a handle closes, over a bounded sample, so the
// planner keeps choosing indexes as the organization graph grows. The sample
// bound is SQLite's own recommendation for this pragma on short-lived handles.
const analysisLimit = 400

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
	db, err := sql.Open("sqlite", storeDSN(absolute))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.initialize(ordinaryVersion); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open collections: %w", err)
	}
	s.useWAL()
	return s, nil
}

// useWAL moves the store to write-ahead logging the first time a build that
// knows about it opens the store; the mode is kept in the file, so every later
// open finds it already set and takes no lock for it. Under the rollback
// journal a reader holding its snapshot makes a committing writer wait, and the
// one-second bound above turns that wait into "database is locked" once the
// organization is read on every turn by several processes. WAL lets readers and
// one writer proceed together.
//
// IT RUNS ONLY AFTER THE STORE WAS ACCEPTED, because switching the mode rewrites
// the file header and a store this build refused must be left byte for byte as
// it was found. A switch that cannot happen now — a peer is mid-transaction, or
// the file system cannot share WAL memory and SQLite keeps the old mode — leaves
// the store in the rollback journal it has always used, which is correct and
// only slower, and the next open tries again. That is why the result is not an
// error: refusing to open over a journal mode would stop work the old mode
// handles correctly.
func (s *Store) useWAL() {
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || strings.EqualFold(mode, "wal") {
		return
	}
	_ = s.db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode)
}

// storeDSN spells every per-connection setting this store relies on.
func storeDSN(absolute string) string {
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
	return u.String()
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

func (s *Store) initialize(target int) error {
	app, version, err := stamp(s.db)
	if err != nil {
		return err
	}
	// OPENING A STORE THAT IS ALREADY AT THIS VERSION TAKES NO WRITER. This is by
	// far the common case — every command after the first — and every one of them
	// used to begin an immediate transaction, so a peer that merely held the
	// writer made an ordinary read fail busy. Reading the stamp and preparing the
	// reads needs only a shared lock, which a reserved writer allows. Creation and
	// every upgrade still run in the immediate transaction below, and both
	// re-read the stamp inside it, so a peer that upgrades between these two steps
	// is seen rather than raced.
	if app == applicationID && version >= target && version <= schemaVersion {
		// Preparing the actual reads catches missing tables before reporting a
		// successful open. We never recreate a damaged initialized schema.
		return verifyVersion(s.db, version)
	}
	if app == applicationID && version > schemaVersion {
		return s.acceptNewer()
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
	case app == applicationID && version >= target && version <= schemaVersion:
		if err := verifyVersion(tx, version); err != nil {
			return err
		}
		return tx.Commit()
	case app == applicationID && version > schemaVersion:
		// A peer upgraded between the first reading and this one. The newer
		// store is judged exactly as it would have been had it been seen first.
		if err := verifyNewer(tx, version); err != nil {
			return err
		}
		return tx.Commit()
	case app == applicationID && version >= 1:
		// AN UPGRADE MUST FIND THE OLD STORE INTACT BEFORE IT ADDS ANYTHING.
		// Every table addition and the version stamp share this immediate
		// transaction, so a failed upgrade leaves the previous schema intact.
		if err := verifyVersion(tx, version); err != nil {
			return fmt.Errorf("refusing to upgrade a damaged version %d store: %w", version, err)
		}
		if err := addVersions(tx, version, target); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", target)); err != nil {
			return err
		}
		if err := verifyVersion(tx, target); err != nil {
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
	if _, err := tx.Exec(collectionSchema); err != nil {
		return err
	}
	if err := addVersions(tx, 1, target); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, target)); err != nil {
		return err
	}
	return tx.Commit()
}

// addVersions applies, in order, what each version after `from` adds, up to and
// including `to`. Every addition is CREATE only (L5): an upgrade adds to a
// store and never rebuilds, renames or drops what an older build reads.
func addVersions(tx *sql.Tx, from, to int) error {
	for v := from + 1; v <= to; v++ {
		if _, err := tx.Exec(additions[v]); err != nil {
			return err
		}
	}
	return nil
}

// acceptNewer opens a store stamped by a newer build without taking the
// writer. The stamp, the declaration and the tables are read in one snapshot,
// so a peer upgrading again in between cannot be half seen. Versions only ever
// rise, so the stamp read inside the snapshot is still newer than this build.
func (s *Store) acceptNewer() error {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, version, err := stamp(tx)
	if err != nil {
		return err
	}
	return verifyNewer(tx, version)
}

// verifyNewer accepts a newer store only when it declares this build a
// permitted reader, and only after every table this build uses answers. A
// newer store that says nothing about its readers is refused: silence is not
// permission, and the store is left untouched either way.
func verifyNewer(q schemaReader, version int) error {
	var declared string
	err := q.QueryRow("SELECT value FROM "+storeMetaTable+" WHERE key=?", minReaderVersion).Scan(&declared)
	if err != nil {
		return fmt.Errorf("%w: version %d does not say which older builds may read it (%v); this build reads version %d", ErrNewerStore, version, err, schemaVersion)
	}
	minimum, err := strconv.Atoi(declared)
	if err != nil || minimum < 1 {
		return fmt.Errorf("%w: version %d declares an unreadable minimum reader %q", ErrNewerStore, version, declared)
	}
	if minimum > schemaVersion {
		return fmt.Errorf("%w: version %d needs a build that reads version %d or newer; this build reads version %d", ErrNewerStore, version, minimum, schemaVersion)
	}
	return verifySchema(q)
}

// verifySchema reads every table this build depends on. A store is only
// reported as open once all of them answer; a missing half is never an empty set.
func verifySchema(q schemaReader) error { return verifyVersion(q, schemaVersion) }

// verifyVersion reads every table a store stamped `version` must have.
func verifyVersion(q schemaReader, version int) error {
	if version >= 4 {
		if err := verifyDirectionSchema(q); err != nil {
			return err
		}
	}
	if version >= 3 {
		if _, err := q.Exec("SELECT collection_id,kind,ref_id,session_id,target_collection FROM placements LIMIT 0"); err != nil {
			return err
		}
	}
	if version >= 2 {
		return verifyContextSchema(q)
	}
	return verifyCollectionSchema(q)
}

func verifyContextSchema(q schemaReader) error {
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

// Close refreshes the planner's statistics and releases the handle. The
// refresh is PRAGMA optimize, which analyzes only the tables this handle's
// queries showed would benefit, over a bounded sample; it is what keeps the
// recursive governing walk on its indexes as the graph grows (L10). IT NEVER
// WAITS FOR A PEER: the lock wait is dropped to zero first, so a close that
// finds another writer skips the refresh and the next close does it. Statistics
// guide the planner and hold no data, so a skipped refresh loses nothing and is
// not reported as a failure of the close.
func (s *Store) Close() error {
	_, _ = s.db.Exec(fmt.Sprintf("PRAGMA busy_timeout=0; PRAGMA analysis_limit=%d; PRAGMA optimize", analysisLimit))
	return s.db.Close()
}

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
