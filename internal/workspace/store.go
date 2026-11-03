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

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// schemaVersion is the schema this build writes. Schemas 1, 2 and 3 remain
// readable without migration; the first successful write upgrades them to 4.
const (
	schemaVersion       = WorkstreamWakeSchemaVersion
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
	schemaTwo
	schemaThree
	schemaRefused
)

// version is the schema a state reads through: 0 for a blank file.
func (s schemaState) version() int {
	switch s {
	case schemaReady:
		return schemaVersion
	case schemaTwo:
		return 2
	case schemaThree:
		return 3
	case schemaLegacy:
		return legacySchemaVersion
	}
	return 0
}

// Store owns collection and workstream metadata. Transcripts, task checkpoints, standing
// items and artifacts retain their existing owners and persistence formats.
//
// THE SCHEMA IS BUILT BY THE FIRST WRITE THAT NEEDS IT, never by Open. Opening
// is also how `list`, `show` and `find` reach the file, and a read that built a
// schema would write to a database nobody asked to change and take the writer
// lock to do it — which is what put a plain read behind whichever peer happened
// to be adding a membership. `version` is that question already answered (0
// while the file is blank), and `schemaMu` keeps two goroutines on one handle
// from both answering it. The same law covers migration: a schema-1 or schema-2 file is
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
	q := make(url.Values)
	// The private file was created above. SQLite must not create another file
	// with its default permissions if the path is a dangling link or disappears.
	q.Set("mode", "rw")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", sqlitefile.URI(absolute, q.Encode()))
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
	if app == applicationID && (version == schemaVersion || version == 3 || version == 2 || version == legacySchemaVersion) {
		state := schemaReady
		collections := "SELECT seq,id,name,created_at FROM collections LIMIT 0"
		memberships := "SELECT seq,collection_id,kind,ref_id,session_id,target_collection,added_at FROM memberships LIMIT 0"
		if version == 2 {
			state = schemaTwo
		}
		if version == 3 {
			state = schemaThree
		}
		if version >= 3 {
			memberships = "SELECT seq,collection_id,kind,ref_id,session_id,target_collection,added_at,factory FROM memberships LIMIT 0"
		}
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
		if version >= 3 {
			for _, query := range workstreamSchemaReads {
				if _, err := q.ExecContext(ctx, query); err != nil {
					return state, err
				}
			}
		}
		if version >= WorkstreamWakeSchemaVersion {
			if _, err := q.ExecContext(ctx, "SELECT id,workstream_id,event_key,event_json,state,summary,error,cost_usd,started_at,settled_at,dismissed_at FROM workstream_wakes LIMIT 0"); err != nil {
				return state, err
			}
			if _, err := q.ExecContext(ctx, "SELECT id,wake_id,position,kind,state,why,item_json,undo_json,note,applied_at,undone_at FROM workstream_wake_items LIMIT 0"); err != nil {
				return state, err
			}
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

// schemaTables builds the collection tables under the names given, so the same
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
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact','crew','process','factory_job')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES %[1]s(id) ON DELETE CASCADE,
 added_at INTEGER NOT NULL DEFAULT 0,
 factory TEXT NOT NULL DEFAULT '',
 CHECK ((kind='factory_job' AND length(factory)>0) OR (kind!='factory_job' AND factory='')),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 UNIQUE(collection_id,kind,ref_id,session_id,factory)
);
`, collections, memberships)
}

const schemaIndexes = `
CREATE INDEX memberships_reference ON memberships(kind,ref_id,session_id,factory);
CREATE INDEX memberships_target ON memberships(target_collection);
`

// migrateLegacy rebuilds a schema-1 or schema-2 file as schema 3 inside the caller's
// immediate transaction: either every row arrives in the new tables, in the
// same order under the same sequence numbers, or the file is left exactly as
// it was. A row from schema 1 says nothing of when it was made, so its
// created_at and added_at are 0, which every reader takes as unknown.
//
// THE TABLES ARE COPIED, NOT ALTERED, because SQLite cannot change a CHECK or
// a foreign key in place. The old memberships go first so dropping the old
// collections deletes nothing anything still points at, and renaming the copy
// carries the foreign keys that name it along. Sequence high-water marks also
// survive deleted rows so migration cannot reuse a sequence named by an older
// page cursor.
func migrateLegacy(ctx context.Context, tx *sql.Tx, version int) error {
	created, added := "0", "0"
	if version >= 2 {
		created, added = "created_at", "added_at"
	}
	_, err := tx.ExecContext(ctx, schemaTables("collections_v3", "memberships_v3")+fmt.Sprintf(`
INSERT INTO collections_v3(seq,id,name,created_at) SELECT seq,id,name,%s FROM collections ORDER BY seq;
INSERT INTO memberships_v3(seq,collection_id,kind,ref_id,session_id,target_collection,added_at)
 SELECT seq,collection_id,kind,ref_id,session_id,target_collection,%s FROM memberships ORDER BY seq;
UPDATE sqlite_sequence SET seq=max(seq,coalesce((SELECT seq FROM sqlite_sequence WHERE name='collections'),0))
 WHERE name='collections_v3';
INSERT INTO sqlite_sequence(name,seq) SELECT 'collections_v3',seq FROM sqlite_sequence
 WHERE name='collections' AND NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='collections_v3');
UPDATE sqlite_sequence SET seq=max(seq,coalesce((SELECT seq FROM sqlite_sequence WHERE name='memberships'),0))
 WHERE name='memberships_v3';
INSERT INTO sqlite_sequence(name,seq) SELECT 'memberships_v3',seq FROM sqlite_sequence
 WHERE name='memberships' AND NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='memberships_v3');
DROP TABLE memberships;
DROP TABLE collections;
ALTER TABLE collections_v3 RENAME TO collections;
ALTER TABLE memberships_v3 RENAME TO memberships;
`, created, added)+schemaIndexes+workstreamSchema)
	return err
}

// beginWrite ensures schema and data changes share one immediate transaction.
// A failed first write rolls back the migration as well as the requested change.
func (s *Store) beginWrite(ctx context.Context, create bool) (*sql.Tx, error) {
	if !create {
		version, err := s.readyForRead(ctx)
		if err != nil {
			return nil, err
		}
		if version == 0 {
			return nil, ErrNotFound
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeError(err)
	}
	state, err := inspectSchema(ctx, tx)
	if err == nil {
		switch state {
		case schemaReady:
			_, err = tx.ExecContext(ctx, workstreamCollectionKeep)
		case schemaThree:
			err = migrateWorkstreamWakes(ctx, tx)
		case schemaLegacy, schemaTwo:
			err = migrateLegacy(ctx, tx, state.version())
			if err == nil {
				err = migrateWorkstreamWakes(ctx, tx)
			}
		case schemaBlank:
			if !create {
				err = ErrNotFound
			} else {
				_, err = tx.ExecContext(ctx, schemaTables("collections", "memberships")+schemaIndexes+workstreamSchema)
				if err == nil {
					err = migrateWorkstreamWakes(ctx, tx)
				}
			}
		}
	}
	if err == nil && state != schemaReady {
		_, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion))
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, storeError(err)
	}
	return tx, nil
}

// ensureSchema is used by bulk fixtures that initialize without adding rows.
func (s *Store) ensureSchema(ctx context.Context) error {
	return s.Write(ctx, func(*sql.Tx) error { return nil })
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

// columns are the expressions a read uses for created_at and added_at: the
// columns themselves at schema 2, and the unknown 0 on a schema-1 file.
func columns(version int) (created, added string) {
	if version >= 2 {
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
//
// IT IS strings.EqualFold, WHICH IS SIMPLE FOLDING, ONE RUNE FOR ONE RUNE,
// with no locale and no normalization. So "ΣΊΣΥΦΟΣ" and "σίσυφος" are one
// name (Σ, σ and the final ς fold together), and so are "K" and the Kelvin
// sign. What simple folding cannot see stays apart: "İstanbul" (a dotted
// capital I, whose lower case is two runes) and "istanbul" are two names, as
// are "Straße" and "STRASSE", and a precomposed "é" and an "e" followed by a
// combining accent. schema2_test pins every one of these.
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
	tx, err := s.beginWrite(ctx, true)
	if err != nil {
		return Collection{}, storeError(err)
	}
	defer tx.Rollback()
	c := Collection{ID: id, Name: name}
	if err := CreateIn(ctx, tx, id, name, s.millis()); err != nil {
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
	tx, err := s.beginWrite(ctx, false)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	if err := RenameIn(ctx, tx, id, name); err != nil {
		return storeError(err)
	}
	if err := touchWorkstream(ctx, tx, id, s.millis()); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

// Delete removes a collection, its memberships, and every membership naming
// it in another collection, and answers those other collections, oldest
// first, because each of them changed too. The records any of it referenced
// are untouched.
func (s *Store) Delete(ctx context.Context, id string) ([]string, error) {
	tx, err := s.beginWrite(ctx, false)
	if err != nil {
		return nil, storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return nil, storeError(err)
	}
	var isWorkstream bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workstreams WHERE id=?)", id).Scan(&isWorkstream); err != nil {
		return nil, storeError(err)
	}
	if isWorkstream {
		return nil, ErrWorkstreamDelete
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
	for _, parent := range parents {
		if err := touchWorkstream(ctx, tx, parent, s.millis()); err != nil {
			return nil, storeError(err)
		}
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
	copyID, err := newID()
	if err != nil {
		return Collection{}, storeError(err)
	}
	tx, err := s.beginWrite(ctx, false)
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection,added_at,factory)
 SELECT ?,kind,ref_id,session_id,target_collection,added_at,factory FROM memberships WHERE collection_id=? ORDER BY seq`, copyID, id); err != nil {
		return Collection{}, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return Collection{}, storeError(err)
	}
	return Collection{ID: copyID, Name: name}, nil
}

func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	out := make([]Collection, 0)
	err := s.Read(ctx, func(tx *sql.Tx, version int) error {
		if version == 0 {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT c.id,c.name,"+workstreamColumn(version)+" FROM collections c ORDER BY seq")
		if err != nil {
			return err
		}
		defer rows.Close()
		out, err = readCollections(rows)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func readCollections(rows *sql.Rows) ([]Collection, error) {
	result := make([]Collection, 0)
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.ID, &c.Name, &c.Workstream); err != nil {
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
	tx, err := s.beginWrite(ctx, false)
	if err != nil {
		return nil, nil, storeError(err)
	}
	defer tx.Rollback()
	added, already, err := AddRefsIn(ctx, tx, id, refs, s.millis())
	if err != nil {
		return nil, nil, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, storeError(err)
	}
	return added, already, nil
}

// AddRefsIn applies collection rules inside a transaction obtained from Write.
// It is shared with workstream creation so memberships and metadata commit together.
func AddRefsIn(ctx context.Context, tx *sql.Tx, id string, refs []Addition, now int64) ([]Member, []Ref, error) {
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, nil, err
		}
		if ref.AddedAt < 0 {
			return nil, nil, ErrInvalid
		}
	}
	if err := requireCollection(ctx, tx, id); err != nil {
		return nil, nil, storeError(err)
	}

	added, already := make([]Member, 0, len(refs)), make([]Ref, 0)
	for _, ref := range refs {
		var target any
		if ref.Kind == CollectionKind {
			if err := nestable(ctx, tx, id, ref.ID); err != nil {
				return nil, nil, storeError(err)
			}
			target = ref.ID
		}
		if err := checkWorkstreamJob(ctx, tx, id, ref.Ref); err != nil {
			return nil, nil, err
		}
		at := ref.AddedAt
		if at == 0 {
			at = now
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection,added_at,factory)
 VALUES (?,?,?,?,?,?,?) ON CONFLICT(collection_id,kind,ref_id,session_id,factory) DO NOTHING`, id, ref.Kind, ref.ID, ref.SessionID, target, at, ref.Factory)
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
	if len(added) > 0 {
		if err := touchWorkstream(ctx, tx, id, now); err != nil {
			return nil, nil, err
		}
	}
	return added, already, nil
}

// nestable refuses putting the collection child inside id: ErrNotFound when
// child does not exist, ErrCycle when child already holds id at any depth
// (or is id).
//
// THE CYCLE CHECK AND THE WRITE SHARE THE CALLER'S WRITER TRANSACTION. Two
// processes cannot both approve opposite edges against an old snapshot, and
// an earlier reference of the same call is already in the graph the next one
// is checked against.
func nestable(ctx context.Context, tx *sql.Tx, id, child string) error {
	if err := requireCollection(ctx, tx, child); err != nil {
		return err
	}
	var cycle bool
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
 VALUES (?) UNION
 SELECT m.ref_id FROM memberships m JOIN descendants d ON m.collection_id=d.id WHERE m.kind='collection'
) SELECT EXISTS(SELECT 1 FROM descendants WHERE id=?)`, child, id).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return ErrCycle
	}
	return nil
}

// Relink puts to where from is in collection id, in one writer transaction:
// to takes from's row, so its added_at and its place in every order are
// from's. When to is already a member, from is simply removed and already
// is true, and the member answered is to as it was. from that is not a member
// is ErrNotMember; a collection to is checked as Add checks one (ErrNotFound,
// ErrCycle). Whether a to of any other kind is really there now is its
// owner's question, asked by the caller before this; the store keeps
// references, never the things.
func (s *Store) Relink(ctx context.Context, id string, from, to Ref) (Member, bool, error) {
	if err := from.Validate(); err != nil {
		return Member{}, false, storeError(err)
	}
	if err := to.Validate(); err != nil {
		return Member{}, false, storeError(err)
	}
	tx, err := s.beginWrite(ctx, false)
	if err != nil {
		return Member{}, false, storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, id); err != nil {
		return Member{}, false, storeError(err)
	}
	membership := func(ref Ref) (seq, at int64, err error) {
		err = tx.QueryRowContext(ctx, "SELECT seq,added_at FROM memberships WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? AND factory=?",
			id, ref.Kind, ref.ID, ref.SessionID, ref.Factory).Scan(&seq, &at)
		return seq, at, err
	}
	seq, at, err := membership(from)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, false, storeError(ErrNotMember)
	}
	if err != nil {
		return Member{}, false, storeError(err)
	}
	if from.key() == to.key() {
		return Member{Ref: to, AddedAt: at}, true, nil
	}
	if _, toAt, err := membership(to); err == nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM memberships WHERE seq=?", seq); err != nil {
			return Member{}, false, storeError(err)
		}
		if err := touchWorkstream(ctx, tx, id, s.millis()); err != nil {
			return Member{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Member{}, false, storeError(err)
		}
		return Member{Ref: to, AddedAt: toAt}, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Member{}, false, storeError(err)
	}
	if err := checkWorkstreamJob(ctx, tx, id, to); err != nil {
		return Member{}, false, err
	}
	var target any
	if to.Kind == CollectionKind {
		if err := nestable(ctx, tx, id, to.ID); err != nil {
			return Member{}, false, storeError(err)
		}
		target = to.ID
	}
	if _, err := tx.ExecContext(ctx, "UPDATE memberships SET kind=?,ref_id=?,session_id=?,target_collection=?,factory=? WHERE seq=?",
		to.Kind, to.ID, to.SessionID, target, to.Factory, seq); err != nil {
		return Member{}, false, storeError(err)
	}
	if err := touchWorkstream(ctx, tx, id, s.millis()); err != nil {
		return Member{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Member{}, false, storeError(err)
	}
	return Member{Ref: to, AddedAt: at}, false, nil
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
	tx, err := s.beginWrite(ctx, false)
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
		err := tx.QueryRowContext(ctx, "DELETE FROM memberships WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? AND factory=? RETURNING added_at",
			id, ref.Kind, ref.ID, ref.SessionID, ref.Factory).Scan(&at)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, storeError(err)
		}
		removed = append(removed, Member{Ref: ref, AddedAt: at})
	}
	if len(removed) > 0 {
		if err := touchWorkstream(ctx, tx, id, s.millis()); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, storeError(err)
	}
	return removed, nil
}

func (s *Store) Members(ctx context.Context, id string) ([]Ref, error) {
	out := make([]Ref, 0)
	err := s.Read(ctx, func(tx *sql.Tx, version int) error {
		if version == 0 {
			return ErrNotFound
		}
		if err := requireCollection(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT kind,ref_id,session_id,"+factoryColumn(version)+" FROM memberships WHERE collection_id=? ORDER BY seq", id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref Ref
			if err := rows.Scan(&ref.Kind, &ref.ID, &ref.SessionID, &ref.Factory); err != nil {
				return err
			}
			out = append(out, ref)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CollectionsFor(ctx context.Context, ref Ref) ([]Collection, error) {
	if err := ref.Validate(); err != nil {
		return nil, storeError(err)
	}
	out := make([]Collection, 0)
	err := s.Read(ctx, func(tx *sql.Tx, version int) error {
		if version == 0 {
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT c.id,c.name,`+workstreamColumn(version)+` FROM collections c JOIN memberships m ON m.collection_id=c.id
 WHERE m.kind=? AND m.ref_id=? AND m.session_id=? AND `+factoryColumn(version)+`=? ORDER BY c.seq`, ref.Kind, ref.ID, ref.SessionID, ref.Factory)
		if err != nil {
			return err
		}
		defer rows.Close()
		out, err = readCollections(rows)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
