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
	schemaRefused
)

// Store owns collection metadata only. Transcripts, task checkpoints, standing
// items and artifacts retain their existing owners and persistence formats.
//
// THE SCHEMA IS BUILT BY THE FIRST WRITE THAT NEEDS IT, never by Open. Opening
// is also how `list`, `show` and `find` reach the file, and a read that built a
// schema would write to a database nobody asked to change and take the writer
// lock to do it — which is what put a plain read behind whichever peer happened
// to be adding a membership. `ready` is that question already answered, and
// `schemaMu` keeps two goroutines on one handle from both answering it.
type Store struct {
	db       *sql.DB
	schemaMu sync.Mutex
	ready    bool
	version  int
	clock    func() time.Time
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
	s := &Store{db: db, clock: time.Now}
	state, version, err := s.snapshotSchema(context.Background())
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
	s.ready = state == schemaReady
	s.version = version
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
func (s *Store) snapshotSchema(ctx context.Context) (schemaState, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return schemaBlank, 0, err
	}
	defer tx.Rollback()
	return inspectSchema(ctx, tx)
}

// inspectSchema decides whether the database on the other end of q is ours and
// built, ours and still blank, or somebody else's. It only ever reads; the
// caller decides what view it reads through.
func inspectSchema(ctx context.Context, q schemaQuerier) (schemaState, int, error) {
	if err := ctx.Err(); err != nil {
		return schemaBlank, 0, err
	}
	app, version, err := readPragmas(ctx, q)
	if err != nil {
		return schemaBlank, 0, err
	}
	if app == applicationID && version >= minSchemaVersion && version <= schemaVersion {
		// Preparing the actual reads catches missing tables before reporting a
		// successful open. We never recreate a damaged initialized schema.
		if err := verifyVersionTables(ctx, q, version); err != nil {
			return schemaReady, version, err
		}
		return schemaReady, version, nil
	}
	if app == applicationID && version > schemaVersion {
		return schemaRefused, version, fmt.Errorf("unsupported collections database (application %d, version %d)", app, version)
	}
	if app != 0 || version != 0 {
		return schemaRefused, version, fmt.Errorf("unsupported collections database (application %d, version %d)", app, version)
	}
	var tables int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return schemaBlank, 0, err
	}
	if tables != 0 {
		return schemaRefused, 0, errors.New("this database belongs to another feature; choose a separate collections database")
	}
	return schemaBlank, 0, nil
}

// ensureSchema serializes initialization on one handle and then takes SQLite's
// immediate transaction so separate processes also agree which one builds it.
func (s *Store) ensureSchema(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return storeError(err)
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.ready && s.version == schemaVersion {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	state, version, err := inspectSchema(ctx, tx)
	if err != nil {
		return storeError(err)
	}
	now := s.now().UTC().Format(time.RFC3339)
	switch {
	case state == schemaReady && version == schemaVersion:
	case state == schemaReady && version < schemaVersion:
		if err := migrateToCurrent(ctx, tx, version, now); err != nil {
			return storeError(err)
		}
		version = schemaVersion
	case state == schemaBlank:
		if err := createCurrent(ctx, tx, now); err != nil {
			return storeError(err)
		}
		version = schemaVersion
	default:
		return storeError(fmt.Errorf("unsupported collections database"))
	}
	if err := tx.Commit(); err != nil {
		return storeError(err)
	}
	s.ready = true
	s.version = version
	return nil
}

// readyForRead refreshes a handle that first saw a blank file, because another
// process may have initialized it since. The refresh is still only plain reads.
func (s *Store) readyForRead(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, storeError(err)
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.ready {
		return true, nil
	}
	state, version, err := s.snapshotSchema(ctx)
	if err != nil {
		return false, storeError(err)
	}
	s.ready = state == schemaReady
	if s.ready {
		s.version = version
	}
	return s.ready, nil
}

func (s *Store) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, name string) (Collection, error) {
	if err := ValidateName(name); err != nil {
		return Collection{}, storeError(err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Collection{}, storeError(err)
	}
	if err := s.ensureSchema(ctx); err != nil {
		return Collection{}, storeError(err)
	}
	now := s.now().UTC().Format(time.RFC3339)
	c := Collection{
		ID:        hex.EncodeToString(random[:]),
		Name:      name,
		Lifecycle: LifecycleActive,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Collection{}, storeError(err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO collections(id,name,purpose,lifecycle,revision,created_at,updated_at)
 VALUES (?,?,?,?,?,?,?)`, c.ID, c.Name, c.Purpose, c.Lifecycle, c.Revision, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return Collection{}, storeError(err)
	}
	if err := bumpTouched(ctx, tx, now); err != nil {
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
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	now := s.now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE collections SET name=?,revision=revision+1,updated_at=? WHERE id=?", name, now, id)
	if err != nil {
		return storeError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError(err)
	}
	if n == 0 {
		return storeError(ErrNotFound)
	}
	if err := bumpTouched(ctx, tx, now); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if !ready {
		return make([]Collection, 0), nil
	}
	meta := s.version >= 2
	cols := "id,name"
	if meta {
		cols = collectionColumns("")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+cols+" FROM collections ORDER BY seq")
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result, err := readCollections(rows, meta)
	return result, storeError(err)
}

func collectionColumns(alias string) string {
	if alias != "" {
		alias += "."
	}
	return alias + "id," + alias + "name," + alias + "purpose," + alias + "lifecycle," + alias + "revision," + alias + "created_at," + alias + "updated_at"
}

func readCollections(rows *sql.Rows, meta bool) ([]Collection, error) {
	result := make([]Collection, 0)
	for rows.Next() {
		var c Collection
		var err error
		if meta {
			err = rows.Scan(&c.ID, &c.Name, &c.Purpose, &c.Lifecycle, &c.Revision, &c.CreatedAt, &c.UpdatedAt)
		} else {
			err = rows.Scan(&c.ID, &c.Name)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// writeReady admits a mutation against an existing collections file and
// migrates it to the current schema. A blank file is not initialized here:
// Create is the only door that may build a database from nothing, because
// Add/Remove/Rename on a path nobody has used yet must leave every byte alone.
func (s *Store) writeReady(ctx context.Context) error {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return storeError(ErrNotFound)
	}
	return s.ensureSchema(ctx)
}

// SchemaVersion reports the collections schema this handle last observed.
// Listing never migrates, so a v1 file stays at 1 until a write runs.
func (s *Store) SchemaVersion() int {
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	return s.version
}

// RootState reports the virtual root's revision without writing. Listing and
// this snapshot must not migrate a v1 file; the v2 row is created only when a
// write runs ensureSchema.
func (s *Store) RootState(ctx context.Context) (revision int, purpose, updatedAt string, err error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return 0, "", "", err
	}
	if !ready || s.version < 2 {
		return 0, "", "", storeError(ErrNotFound)
	}
	err = s.db.QueryRowContext(ctx, "SELECT revision, purpose, updated_at FROM root_state WHERE id=1").Scan(&revision, &purpose, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", storeError(ErrNotFound)
	}
	return revision, purpose, updatedAt, storeError(err)
}

func requireCollection(ctx context.Context, tx *sql.Tx, id string) error {
	var found int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id=?", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) Members(ctx context.Context, id string) ([]Ref, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if !ready {
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
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if !ready {
		return make([]Collection, 0), nil
	}
	meta := s.version >= 2
	cols := "c.id,c.name"
	if meta {
		cols = collectionColumns("c")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM collections c JOIN memberships m ON m.collection_id=c.id
 WHERE m.kind=? AND m.ref_id=? AND m.session_id=? ORDER BY c.seq`, ref.Kind, ref.ID, ref.SessionID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result, err := readCollections(rows, meta)
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
