package workspace

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// schemaVersion is the schema this build writes. Schema 2 added when a
// collection was made and when each membership was added, the crew and
// process kinds, and deletes that cascade. legacySchemaVersion is the one
// older schema this build still reads as it is and migrates on its first
// write; anything else claiming to be ours is refused, never misread.
const (
	schemaVersion       = 2
	legacySchemaVersion = 1
)
const applicationID = 0x4146434c // AFCL distinguishes this store from optional memory.

// LOCK WAITS ARE BOUNDED because every door here is a person's command and a
// peer must not freeze it forever. The sibling store's busyWait gives writes
// the same ten seconds: a slow command is preferable to a dropped membership.
// The pragma and the refusal both read this constant so the bound cannot drift.
const busyTimeout = 10 * time.Second

type schemaState uint8

const (
	schemaBlank schemaState = iota
	schemaReady
	schemaLegacy
	schemaRefused
)

// version is the schema a state reads through: 0 for a blank file.
func (s schemaState) version() int {
	switch s {
	case schemaReady:
		return schemaVersion
	case schemaLegacy:
		return legacySchemaVersion
	}
	return 0
}

// Store owns collection metadata only. Transcripts, task checkpoints, standing
// items and artifacts retain their existing owners and persistence formats.
//
// THE SCHEMA IS BUILT BY THE FIRST WRITE THAT NEEDS IT, never by Open. Opening
// is also how `list`, `show` and `find` reach the file, and a read that built a
// schema would write to a database nobody asked to change and take the writer
// lock to do it — which is what put a plain read behind whichever peer happened
// to be adding a membership. `version` is that question already answered (0
// while the file is blank), and `schemaMu` keeps two goroutines on one handle
// from both answering it. The same law covers migration: a schema-1 file is
// read as it stands and rebuilt only by the first write.
type Store struct {
	db       *sql.DB
	schemaMu sync.Mutex
	version  int
	// now is the clock created_at and added_at are read from.
	now func() time.Time
}

// Open creates a private collection store or checks an existing one. Unknown
// databases fail explicitly; they must never be interpreted as empty state.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, storeError(fmt.Errorf("%w: empty database path", ErrInvalid))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, storeError(err)
	}
	missing, err := checkDatabaseFile(absolute)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, collectionsOpenFailure(absolute, err)
	}
	if missing {
		f, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err == nil {
			if err := f.Close(); err != nil {
				return nil, collectionsOpenFailure(absolute, err)
			}
		} else if !errors.Is(err, os.ErrExist) {
			return nil, collectionsOpenFailure(absolute, err)
		}
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := u.Query()
	// The private file was created above. SQLite must not create another file
	// with its default permissions if the path is a dangling link or disappears.
	q.Set("mode", "rw")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, collectionsOpenFailure(absolute, err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: time.Now}
	state, err := s.snapshotSchema(context.Background())
	if err != nil {
		_ = db.Close()
		if translated := storeError(err); errors.Is(translated, ErrBusy) {
			return nil, translated
		}
		if state != schemaBlank {
			return nil, fmt.Errorf("open collections: %w", err)
		}
		return nil, collectionsOpenFailure(absolute, err)
	}
	s.version = state.version()
	return s, nil
}

type schemaQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// snapshotSchema reads the schema state off ONE consistent view of the file.
//
// IT MUST NOT ASK FOR THE WRITER. Every connection here carries
// `_txlock=immediate`, which turns an ordinary transaction into a write
// transaction and would put every `list`, `show` and `find` behind whichever
// peer happens to be writing — the defect this is here to prevent. A read-only
// transaction is the one shape the driver begins plainly instead, so the
// questions below cost a shared lock and nothing more.
//
// The transaction is what makes the answer trustworthy as well as cheap. Asked
// as three loose statements, a peer's initialization can commit between two of
// them, and the half-seen commit that comes back — our schema version over an
// application id that has not landed yet — is indistinguishable from a database
// belonging to somebody else, which this store refuses rather than touches.
func (s *Store) snapshotSchema(ctx context.Context) (schemaState, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return schemaBlank, err
	}
	defer tx.Rollback()
	return inspectSchema(ctx, tx)
}

// inspectSchema decides whether the database on the other end of q is ours and
// built (at this schema or the legacy one), ours and still blank, or somebody
// else's. It only ever reads; the caller decides what view it reads through.
func inspectSchema(ctx context.Context, q schemaQuerier) (schemaState, error) {
	if err := ctx.Err(); err != nil {
		return schemaBlank, err
	}
	var app, version, tables int
	if err := q.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return schemaBlank, err
	}
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return schemaBlank, err
	}
	if app == applicationID && (version == schemaVersion || version == legacySchemaVersion) {
		state := schemaReady
		collections := "SELECT seq,id,name,created_at FROM collections LIMIT 0"
		memberships := "SELECT seq,collection_id,kind,ref_id,session_id,target_collection,added_at FROM memberships LIMIT 0"
		if version == legacySchemaVersion {
			state = schemaLegacy
			collections = "SELECT seq,id,name FROM collections LIMIT 0"
			memberships = "SELECT seq,collection_id,kind,ref_id,session_id,target_collection FROM memberships LIMIT 0"
		}
		// Preparing the actual reads catches missing tables before reporting a
		// successful open. We never recreate a damaged initialized schema.
		if _, err := q.ExecContext(ctx, collections); err != nil {
			return state, err
		}
		if _, err := q.ExecContext(ctx, memberships); err != nil {
			return state, err
		}
		return state, nil
	}
	if app != 0 || version != 0 {
		return schemaRefused, fmt.Errorf("unsupported collections database (application %d, version %d)", app, version)
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return schemaBlank, err
	}
	if tables != 0 {
		return schemaRefused, errors.New("this database belongs to another feature; choose a separate collections database")
	}
	return schemaBlank, nil
}

// schemaTables is schema 2's two tables under the names given, so the same
// words build a fresh store and the copies a migration fills.
//
// EVERY DELETE CASCADES: a deleted collection takes its own memberships and
// every membership naming it in another collection, and never the records any
// of them referenced.
func schemaTables(collections, memberships string) string {
	return fmt.Sprintf(`
CREATE TABLE %[1]s (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE %[2]s (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 collection_id TEXT NOT NULL REFERENCES %[1]s(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact','crew','process')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES %[1]s(id) ON DELETE CASCADE,
 added_at INTEGER NOT NULL DEFAULT 0,
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id)
);
`, collections, memberships)
}

const schemaIndexes = `
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id);
CREATE INDEX memberships_target ON memberships(target_collection);
`

// migrateLegacy rebuilds a schema-1 file as schema 2 inside the caller's
// immediate transaction: either every row arrives in the new tables, in the
// same order under the same sequence numbers, or the file is left exactly as
// it was. A row from schema 1 says nothing of when it was made, so its
// created_at and added_at are 0, which every reader takes as unknown.
//
// THE TABLES ARE COPIED, NOT ALTERED, because SQLite cannot change a CHECK or
// a foreign key in place. The old memberships go first so dropping the old
// collections deletes nothing anything still points at, and renaming the copy
// carries the foreign keys that name it along.
func migrateLegacy(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, schemaTables("collections_v2", "memberships_v2")+`
INSERT INTO collections_v2(seq,id,name,created_at) SELECT seq,id,name,0 FROM collections ORDER BY seq;
INSERT INTO memberships_v2(seq,collection_id,kind,ref_id,session_id,target_collection,added_at)
 SELECT seq,collection_id,kind,ref_id,session_id,target_collection,0 FROM memberships ORDER BY seq;
DROP TABLE memberships;
DROP TABLE collections;
ALTER TABLE collections_v2 RENAME TO collections;
ALTER TABLE memberships_v2 RENAME TO memberships;
`+schemaIndexes)
	return err
}

// ensureSchema serializes initialization on one handle and then takes SQLite's
// immediate transaction so separate processes also agree which one builds it,
// or which one migrates a schema-1 file to this one.
func (s *Store) ensureSchema(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return storeError(err)
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.version == schemaVersion {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	state, err := inspectSchema(ctx, tx)
	if err != nil {
		return storeError(err)
	}
	switch state {
	case schemaReady:
		if err := tx.Commit(); err != nil {
			return storeError(err)
		}
		s.version = schemaVersion
		return nil
	case schemaLegacy:
		err = migrateLegacy(ctx, tx)
	default:
		_, err = tx.ExecContext(ctx, schemaTables("collections", "memberships")+schemaIndexes)
	}
	if err != nil {
		return storeError(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion)); err != nil {
		return storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return storeError(err)
	}
	s.version = schemaVersion
	return nil
}

// readyForRead answers the schema a read goes through, and 0 for a blank
// file. A handle that saw a blank or a schema-1 file looks again, because
// another process may have initialized or migrated it since. The look is
// still only plain reads.
func (s *Store) readyForRead(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, storeError(err)
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.version == schemaVersion {
		return s.version, nil
	}
	state, err := s.snapshotSchema(ctx)
	if err != nil {
		return 0, storeError(err)
	}
	s.version = state.version()
	return s.version, nil
}

// readyForWrite is readyForRead for a change to a collection that must
// already exist: false on a blank file, which such a change cannot find
// anything in and must leave blank, and a schema-1 file migrated first.
func (s *Store) readyForWrite(ctx context.Context) (bool, error) {
	version, err := s.readyForRead(ctx)
	if err != nil || version == 0 {
		return false, err
	}
	if version != schemaVersion {
		if err := s.ensureSchema(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}

// columns are the expressions a read uses for created_at and added_at: the
// columns themselves at schema 2, and the unknown 0 on a schema-1 file.
func columns(version int) (created, added string) {
	if version >= schemaVersion {
		return "c.created_at", "m.added_at"
	}
	return "0", "0"
}

func (s *Store) Close() error { return s.db.Close() }

// millis is the store's clock in unix milliseconds.
func (s *Store) millis() int64 { return s.now().UnixMilli() }

// newID is a fresh collection id.
func newID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

// nameTaken refuses a name another collection already has, compared without
// regard to case, inside the writer transaction so two writers cannot both
// claim one. except is the collection being renamed, which may take its own
// name in another case.
//
// THE COMPARISON IS UNICODE CASE FOLDING IN GO, not SQLite's NOCASE, which
// folds only ASCII: "Ärger" and "ärger" are one name to a person.
func nameTaken(ctx context.Context, tx *sql.Tx, name, except string) error {
	rows, err := tx.QueryContext(ctx, "SELECT id,name FROM collections")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, other string
		if err := rows.Scan(&id, &other); err != nil {
			return err
		}
		if id != except && strings.EqualFold(other, name) {
			return ErrNameTaken
		}
	}
	return rows.Err()
}

func (s *Store) Create(ctx context.Context, name string) (Collection, error) {
	if err := ValidateName(name); err != nil {
		return Collection{}, storeError(err)
	}
	id, err := newID()
	if err != nil {
		return Collection{}, storeError(err)
	}
	if err := s.ensureSchema(ctx); err != nil {
		return Collection{}, storeError(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Collection{}, storeError(err)
	}
	defer tx.Rollback()
	if err := nameTaken(ctx, tx, name, ""); err != nil {
		return Collection{}, storeError(err)
	}
	c := Collection{ID: id, Name: name}
	if _, err := tx.ExecContext(ctx, "INSERT INTO collections(id,name,created_at) VALUES (?,?,?)", c.ID, c.Name, s.millis()); err != nil {
		return Collection{}, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return Collection{}, storeError(err)
	}
	return c, nil
}

func (s *Store) Rename(ctx context.Context, id, name string) error {
	if err := ValidateName(name); err != nil {
		return storeError(err)
	}
	ready, err := s.readyForWrite(ctx)
	if err != nil {
		return storeError(err)
	}
	if !ready {
		return storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return storeError(err)
	}
	if err := nameTaken(ctx, tx, name, id); err != nil {
		return storeError(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE collections SET name=? WHERE id=?", name, id); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

// Delete removes a collection, its memberships, and every membership naming
// it in another collection, and answers those other collections, oldest
// first, because each of them changed too. The records any of it referenced
// are untouched.
func (s *Store) Delete(ctx context.Context, id string) ([]string, error) {
	ready, err := s.readyForWrite(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if !ready {
		return nil, storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return nil, storeError(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id FROM collections c WHERE c.id!=? AND EXISTS(
 SELECT 1 FROM memberships m WHERE m.collection_id=c.id AND m.kind='collection' AND m.ref_id=?) ORDER BY c.seq`, id, id)
	if err != nil {
		return nil, storeError(err)
	}
	parents := make([]string, 0)
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			rows.Close()
			return nil, storeError(err)
		}
		parents = append(parents, parent)
	}
	if err := rows.Close(); err != nil {
		return nil, storeError(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM collections WHERE id=?", id); err != nil {
		return nil, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, storeError(err)
	}
	return parents, nil
}

// Duplicate makes a new collection named name holding the same references as
// id, each with the same added_at and in the same order. A copy holds what
// its source holds and nothing holds the copy, so it can make no cycle.
func (s *Store) Duplicate(ctx context.Context, id, name string) (Collection, error) {
	if err := ValidateName(name); err != nil {
		return Collection{}, storeError(err)
	}
	ready, err := s.readyForWrite(ctx)
	if err != nil {
		return Collection{}, storeError(err)
	}
	if !ready {
		return Collection{}, storeError(ErrNotFound)
	}
	copyID, err := newID()
	if err != nil {
		return Collection{}, storeError(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Collection{}, storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return Collection{}, storeError(err)
	}
	if err := nameTaken(ctx, tx, name, ""); err != nil {
		return Collection{}, storeError(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO collections(id,name,created_at) VALUES (?,?,?)", copyID, name, s.millis()); err != nil {
		return Collection{}, storeError(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection,added_at)
 SELECT ?,kind,ref_id,session_id,target_collection,added_at FROM memberships WHERE collection_id=? ORDER BY seq`, copyID, id); err != nil {
		return Collection{}, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return Collection{}, storeError(err)
	}
	return Collection{ID: copyID, Name: name}, nil
}

func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	version, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if version == 0 {
		return make([]Collection, 0), nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,name FROM collections ORDER BY seq")
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result, err := readCollections(rows)
	return result, storeError(err)
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

// Addition is a reference to add and, when AddedAt is above zero, the moment
// it was first added, which puts a removed reference back where it was.
type Addition struct {
	Ref
	AddedAt int64
}

// Add files one reference. Adding one already there changes nothing.
func (s *Store) Add(ctx context.Context, id string, ref Ref) error {
	_, _, err := s.AddRefs(ctx, id, []Addition{{Ref: ref}})
	return err
}

// AddRefs files every reference in one writer transaction, all or nothing,
// and answers what it added and what was already there. A collection
// reference that would make a cycle refuses the whole call with ErrCycle; one
// naming a collection that does not exist refuses it with ErrNotFound.
func (s *Store) AddRefs(ctx context.Context, id string, refs []Addition) ([]Member, []Ref, error) {
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, nil, storeError(err)
		}
		if ref.AddedAt < 0 {
			return nil, nil, storeError(fmt.Errorf("%w: added_at cannot be negative", ErrInvalid))
		}
	}
	ready, err := s.readyForWrite(ctx)
	if err != nil {
		return nil, nil, storeError(err)
	}
	if !ready {
		return nil, nil, storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return nil, nil, storeError(err)
	}
	now := s.millis()
	added, already := make([]Member, 0, len(refs)), make([]Ref, 0)
	for _, ref := range refs {
		var target any
		if ref.Kind == CollectionKind {
			if err := requireCollection(ctx, tx, ref.ID); err != nil {
				return nil, nil, storeError(err)
			}
			// THE CYCLE CHECK AND INSERT SHARE THE WRITER TRANSACTION. Two
			// processes cannot both approve opposite edges against an old
			// snapshot, and an earlier reference of this same call is
			// already in the graph the next one is checked against.
			var cycle bool
			err := tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
 VALUES (?) UNION
 SELECT m.ref_id FROM memberships m JOIN descendants d ON m.collection_id=d.id WHERE m.kind='collection'
) SELECT EXISTS(SELECT 1 FROM descendants WHERE id=?)`, ref.ID, id).Scan(&cycle)
			if err != nil {
				return nil, nil, storeError(err)
			}
			if cycle {
				return nil, nil, storeError(ErrCycle)
			}
			target = ref.ID
		}
		at := ref.AddedAt
		if at == 0 {
			at = now
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection,added_at)
 VALUES (?,?,?,?,?,?) ON CONFLICT(collection_id,kind,ref_id,session_id) DO NOTHING`, id, ref.Kind, ref.ID, ref.SessionID, target, at)
		if err != nil {
			return nil, nil, storeError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, nil, storeError(err)
		}
		if n == 0 {
			already = append(already, ref.Ref)
		} else {
			added = append(added, Member{Ref: ref.Ref, AddedAt: at})
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, storeError(err)
	}
	return added, already, nil
}

// Remove detaches a reference; it never removes or stops the referenced work.
func (s *Store) Remove(ctx context.Context, id string, ref Ref) error {
	_, err := s.RemoveRefs(ctx, id, []Ref{ref})
	return err
}

// RemoveRefs detaches every reference in one writer transaction and answers
// what was there with when it was added, so the removal can be undone in
// place. A reference that was not there is not an error and is not answered.
func (s *Store) RemoveRefs(ctx context.Context, id string, refs []Ref) ([]Member, error) {
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, storeError(err)
		}
	}
	ready, err := s.readyForWrite(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if !ready {
		return nil, storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return nil, storeError(err)
	}
	removed := make([]Member, 0, len(refs))
	for _, ref := range refs {
		var at int64
		err := tx.QueryRowContext(ctx, "DELETE FROM memberships WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? RETURNING added_at",
			id, ref.Kind, ref.ID, ref.SessionID).Scan(&at)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, storeError(err)
		}
		removed = append(removed, Member{Ref: ref, AddedAt: at})
	}
	if err := tx.Commit(); err != nil {
		return nil, storeError(err)
	}
	return removed, nil
}

func (s *Store) Members(ctx context.Context, id string) ([]Ref, error) {
	version, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if version == 0 {
		return nil, storeError(ErrNotFound)
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM collections WHERE id=?)", id).Scan(&exists); err != nil {
		return nil, storeError(err)
	}
	if !exists {
		return nil, storeError(ErrNotFound)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT kind,ref_id,session_id FROM memberships WHERE collection_id=? ORDER BY seq", id)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]Ref, 0)
	for rows.Next() {
		var ref Ref
		if err := rows.Scan(&ref.Kind, &ref.ID, &ref.SessionID); err != nil {
			return nil, storeError(err)
		}
		result = append(result, ref)
	}
	return result, storeError(rows.Err())
}

func (s *Store) CollectionsFor(ctx context.Context, ref Ref) ([]Collection, error) {
	if err := ref.Validate(); err != nil {
		return nil, storeError(err)
	}
	version, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if version == 0 {
		return make([]Collection, 0), nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name FROM collections c JOIN memberships m ON m.collection_id=c.id
 WHERE m.kind=? AND m.ref_id=? AND m.session_id=? ORDER BY c.seq`, ref.Kind, ref.ID, ref.SessionID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result, err := readCollections(rows)
	return result, storeError(err)
}

// storeError replaces SQLite's lock account with the stable sentence exposed by
// this package. Extended result codes retain their primary code in the low byte.
func storeError(err error) error {
	if err == nil || errors.Is(err, ErrBusy) {
		return err
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code() & 0xff
		if code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED {
			return fmt.Errorf("%w (waited %s)", ErrBusy, busyTimeout)
		}
	}
	return err
}

// checkDatabaseFile refuses a path that cannot be a collections database, and
// tells Open whether the private file still has to be made.
//
// SQLITE'S OWN ACCOUNT OF A PATH IT WILL NOT OPEN IS ACTIVELY MISLEADING, which
// is the reason this runs first. A directory, a file belonging to somebody else
// and a read-only disk all arrive back as one code, and the driver renders that
// code as `unable to open database file: out of memory (14)`. Nothing has run
// out of memory; a person who reads it goes hunting a leak on a machine with
// fifty gigabytes free. So the operating system is asked instead, in its own
// words, and a dangling link is refused here rather than followed and created
// with SQLite's default permissions. `internal/store` answers the same question
// the same way; the boundary test forbids importing it, so this is local.
func checkDatabaseFile(path string) (needsCreating bool, err error) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, collectionsOpenFailure(path, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false, fmt.Errorf("open collections: %s is not a regular database file", path)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return false, fmt.Errorf("open collections: %s: %w", path, pathReason(err))
	}
	// A zero-byte file is the one blank store. Every non-empty SQLite file starts
	// with this header; checking it matters because SQLite accepts one junk byte
	// as another blank database instead of returning its not-a-database code.
	const sqliteHeader = "SQLite format 3\x00"
	header := make([]byte, len(sqliteHeader))
	if info.Size() != 0 {
		n, readErr := file.Read(header)
		if readErr != nil || n != len(header) || string(header) != sqliteHeader {
			_ = file.Close()
			return false, fmt.Errorf("open collections: %s is not a collections database", path)
		}
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("open collections: %s: %w", path, pathReason(err))
	}
	return false, nil
}

// collectionsOpenFailure asks the operating system for an actionable reason.
// SQLITE'S OWN OPEN WORDS CAN CLAIM MEMORY RAN OUT when the path is merely
// unwritable; if the operating system is content, the bytes are not our store.
func collectionsOpenFailure(path string, sqliteErr error) error {
	if busy := storeError(sqliteErr); errors.Is(busy, ErrBusy) {
		return busy
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err == nil {
		_ = file.Close()
		return fmt.Errorf("open collections: %s is not a collections database", path)
	}
	return fmt.Errorf("open collections: %s: %w", path, pathReason(err))
}

func pathReason(err error) error {
	var failure *fs.PathError
	if errors.As(err, &failure) && failure.Err != nil {
		return failure.Err
	}
	return err
}
