package teams

// db.go is where the teams are kept: one SQLite database, teams.db, in the
// profile, opened the way the plan, workspace and automation stores open theirs
// (modernc's SQLite, WAL, BEGIN IMMEDIATE writes behind a busy timeout).
//
// IT REPLACED teams.json, and the reason is the size of what a team carries. A
// factory stage keeps the whole brief it was started on, and those briefs grew
// the file past what one read should hold; every load, save and stat-and-parse
// of a 3 MB file paid for all of them. Here:
//
//   - A team is one row and each membership is one row, in the list's order. A
//     write compares what fn left with the rows it read and touches only the
//     rows that differ, so a change to one team writes that team.
//   - A member's briefs (the factory's opening brief and the brief's
//     revisions) are rows of their own, never part of a load. [WithBriefs]
//     reads one member's when they are shown.
//   - The store's revision is a counter bumped by every write that changed a
//     row. [Stamp] is the store's id and that counter, so a reader asks one
//     indexed row whether anything moved, and nothing caps the store's size.
//   - The decoded teams are held per revision, and each team's row says the
//     revision it last changed at (its own row or any of its memberships). A
//     load of a store nobody wrote since is one query and a copy; a load after
//     a write decodes only the teams that write changed.
//
// THERE IS NO MIGRATION. teams.json and spaces.json, when a profile still has
// them, are never read, written, renamed or removed here: a store that is not
// there is no teams.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// applicationID marks the file as this store's, so a database belonging to
// anything else is refused rather than read as empty. "TEAM".
const applicationID = 0x5445414d

// schemaVersion is the shape below. A newer file is refused, never rewritten.
const schemaVersion = 1

// busyTimeout bounds how long a statement waits behind another process's
// write. Writers already queue on the teams lock, so this is a stuck peer.
const busyTimeout = 10 * time.Second

// handlesKept is how many profiles' databases one process keeps open.
const handlesKept = 16

// ErrUnreadable is a teams.db this build cannot read: not a database, some
// other program's, or this store's with rows it cannot make sense of. Nothing
// here writes to it. [SetAside] moves it out of the way when the caller
// decides to start again, but only when the file is not this store's at all
// ([notOursError]): a damaged store of its own stays for a person to look at.
var ErrUnreadable = errors.New("teams: the store is unreadable")

// ErrNewer is a teams.db a newer build wrote. This build neither reads nor
// writes it, and [SetAside] never moves it: the newer build still reads it.
// It is not [ErrUnreadable].
var ErrNewer = errors.New("teams: the store was written by a newer codeaf")

// notOursError is an [ErrUnreadable] that is the file itself: bytes that are
// not a database, or a database some other program made. It is the one store
// [SetAside] moves.
type notOursError struct{ msg string }

func (e *notOursError) Error() string { return e.msg }
func (e *notOursError) Unwrap() error { return ErrUnreadable }

// notOurs is the error for path, which is not this store's for why.
func notOurs(path, why string) error {
	return &notOursError{fmt.Sprintf("%v: %s %s", ErrUnreadable, filepath.Base(path), why)}
}

const schema = `
CREATE TABLE meta (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 store TEXT NOT NULL,
 rev INTEGER NOT NULL,
 indexed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE teams (
 id TEXT PRIMARY KEY,
 pos INTEGER NOT NULL,
 rev INTEGER NOT NULL,
 parent TEXT NOT NULL,
 made_ns INTEGER NOT NULL,
 closed INTEGER NOT NULL,
 root INTEGER NOT NULL,
 factory INTEGER NOT NULL,
 founder TEXT NOT NULL DEFAULT '',
 doc TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX teams_order ON teams(pos);
CREATE INDEX teams_children ON teams(parent, closed, root, factory, made_ns DESC, id);
CREATE TABLE members (
 team_id TEXT NOT NULL,
 former INTEGER NOT NULL,
 pos INTEGER NOT NULL,
 key TEXT NOT NULL,
 conv TEXT NOT NULL DEFAULT '',
 doc TEXT NOT NULL,
 PRIMARY KEY (team_id, former, pos)
) WITHOUT ROWID;
CREATE TABLE briefs (
 team_id TEXT NOT NULL,
 key TEXT NOT NULL,
 seq INTEGER NOT NULL,
 doc TEXT NOT NULL,
 PRIMARY KEY (team_id, key, seq)
) WITHOUT ROWID;
`

// indexes are the lookups a narrow read leans on: the root, the open teams in
// the list's order, and a member's teams. They are made with the store and,
// on a store an earlier build of this schema made, the first time it opens.
const indexes = `
CREATE INDEX IF NOT EXISTS teams_root ON teams(root) WHERE root = 1;
CREATE INDEX IF NOT EXISTS teams_open ON teams(closed, pos);
CREATE INDEX IF NOT EXISTS members_conv ON members(conv, former);
CREATE INDEX IF NOT EXISTS teams_founder ON teams(founder);
`

// indexNames are [indexes]' names, to see whether a store has them all.
var indexNames = []string{"teams_root", "teams_open", "members_conv", "teams_founder"}

// addedColumns are the columns an earlier build of this schema made without,
// each with what adds it. Every one has a default, so that build still writes.
var addedColumns = []struct{ table, column, add string }{
	{"meta", "indexed", "ALTER TABLE meta ADD COLUMN indexed INTEGER NOT NULL DEFAULT 0"},
	{"teams", "founder", "ALTER TABLE teams ADD COLUMN founder TEXT NOT NULL DEFAULT ''"},
	{"members", "conv", "ALTER TABLE members ADD COLUMN conv TEXT NOT NULL DEFAULT ''"},
}

// ── HANDLES ─────────────────────────────────────────────────────────────────

// handle is one open database. It is shared by every caller in the process
// that names the same file, and closed once it is evicted or the file it
// opened is no longer the one at its path and nobody is using it.
type handle struct {
	db   *sql.DB
	path string
	file os.FileInfo
	refs int
	used uint64
	gone bool
}

var handles struct {
	sync.Mutex
	open map[string]*handle
	tick uint64
}

// acquire is the database at path, opened or shared. A file that is not there
// is (nil, nil) unless create, which makes it private and empty first. The
// caller releases what it got.
func acquire(path string, create bool) (*handle, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		forget(path)
		if !create {
			return nil, nil
		}
		// A WAL left beside a database that is gone (deleted by hand) belongs
		// to that database, and SQLite would replay it into the new one. The
		// writer that makes the store holds the teams lock, so no live store
		// owns these names.
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
		if err := makeFile(path); err != nil {
			return nil, err
		}
		info, err = os.Stat(path)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, notOurs(path, "is not a file")
	}
	if h := shared(path, info); h != nil {
		return h, nil
	}
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	handles.Lock()
	defer handles.Unlock()
	if h := handles.open[path]; h != nil && !h.gone && os.SameFile(h.file, info) {
		_ = db.Close()
		handles.tick++
		h.refs++
		h.used = handles.tick
		return h, nil
	}
	if handles.open == nil {
		handles.open = map[string]*handle{}
	}
	if old := handles.open[path]; old != nil {
		retire(old)
	}
	handles.tick++
	h := &handle{db: db, path: path, file: info, refs: 1, used: handles.tick}
	handles.open[path] = h
	evict()
	return h, nil
}

// shared is the handle already open on the file info names, nil for none. A
// handle on a file that was replaced or set aside is retired.
func shared(path string, info os.FileInfo) *handle {
	handles.Lock()
	defer handles.Unlock()
	h := handles.open[path]
	if h == nil {
		return nil
	}
	if !os.SameFile(h.file, info) {
		retire(h)
		return nil
	}
	handles.tick++
	h.refs++
	h.used = handles.tick
	return h
}

// forget retires the handle on path, whose file is gone.
func forget(path string) {
	handles.Lock()
	defer handles.Unlock()
	if h := handles.open[path]; h != nil {
		retire(h)
	}
}

// retire takes h out of the pool and closes it now or on its last release.
// The pool's lock is held.
func retire(h *handle) {
	if handles.open[h.path] == h {
		delete(handles.open, h.path)
	}
	h.gone = true
	if h.refs == 0 {
		_ = h.db.Close()
	}
}

// evict closes the least recently used idle handles past [handlesKept]. The
// pool's lock is held.
func evict() {
	for len(handles.open) > handlesKept {
		var oldest *handle
		for _, h := range handles.open {
			if h.refs == 0 && (oldest == nil || h.used < oldest.used) {
				oldest = h
			}
		}
		if oldest == nil {
			return
		}
		retire(oldest)
	}
}

func (h *handle) release() {
	handles.Lock()
	defer handles.Unlock()
	h.refs--
	if h.refs == 0 && h.gone {
		_ = h.db.Close()
	}
}

// Close closes every teams store this process has open, so SQLite folds the
// WAL back into teams.db and removes it as the last connection leaves; a
// handle still in use closes when it is released. The process calls it on
// its way out. A store used after it is opened again.
//
// Without it the WAL simply stays beside the database: that is safe, since the
// next open replays it, but a store a quit left behind looks unfinished.
func Close() {
	handles.Lock()
	defer handles.Unlock()
	for _, h := range handles.open {
		retire(h)
	}
}

// opener opens every database on one goroutine of the package's own, started
// with it. database/sql keeps a goroutine per open database until it is
// closed, and that goroutine belongs to whoever opened it: opened by a caller
// inside a testing/synctest bubble, it would outlive the bubble and fail it,
// since the pool keeps databases open across callers. Both channels are made
// here too, so a caller in a bubble only ever waits on the package's.
var opener struct {
	sync.Mutex
	ask    chan string
	answer chan openResult
}

func init() {
	opener.ask, opener.answer = make(chan string), make(chan openResult)
	go func() {
		for path := range opener.ask {
			db, err := openDB(path)
			opener.answer <- openResult{db, err}
		}
	}()
}

type openResult struct {
	db  *sql.DB
	err error
}

// open is [openDB] on the package's goroutine.
func open(path string) (*sql.DB, error) {
	opener.Lock()
	defer opener.Unlock()
	opener.ask <- path
	r := <-opener.answer
	return r.db, r.err
}

// makeFile creates the database file private and empty, so SQLite never
// creates it with its own default permissions.
func makeFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

// sqliteMagic opens every SQLite database file.
const sqliteMagic = "SQLite format 3\x00"

// foreign reports whether the file at path holds bytes that are not a SQLite
// database. An empty file is this store's own, made by [makeFile] a moment
// before SQLite wrote to it; a short or foreign one is refused before SQLite
// sees it, because SQLite reads a file shorter than its header as an empty
// database and would build the schema over it.
func foreign(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(f, head)
	switch {
	case n == 0 && errors.Is(err, io.EOF):
		return nil
	case string(head[:n]) != sqliteMagic:
		return notOurs(path, "is not a database")
	}
	return nil
}

func openDB(path string) (*sql.DB, error) {
	if err := foreign(path); err != nil {
		return nil, err
	}
	if err := lookAt(path); err != nil {
		return nil, err
	}
	q := make(url.Values)
	q.Set("mode", "rw")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	// WAL lets every window read while one writes; FULL keeps a team that was
	// written written across a power cut.
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", sqlitefile.URI(path, q.Encode()))
	if err != nil {
		return nil, err
	}
	// A few connections, not one: WAL lets readers share the file while one
	// writes, and a page's callback ([PageAt]) that reads the store again
	// must not wait on the connection its own page holds. Writers still go
	// one at a time, behind the teams lock and BEGIN IMMEDIATE.
	db.SetMaxOpenConns(4)
	if err := ensureSchema(db, path); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// lookAt is [inspectSchema] over a read-only connection that turns nothing
// on. A FILE IS LOOKED AT BEFORE ANYTHING IS TURNED ON IN IT: WAL rewrites the
// header and makes the -wal and -shm files, so [openDB] turns it on only once
// this look found a blank store or this store's own. A database some other
// program made, or a newer build's, keeps every byte.
func lookAt(path string) error {
	q := make(url.Values)
	q.Set("mode", "ro")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	db, err := sql.Open("sqlite", sqlitefile.URI(path, q.Encode()))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = inspectSchema(context.Background(), db, &sql.TxOptions{ReadOnly: true}, path)
	return err
}

// ensureSchema checks the file is this store's at this version, and builds a
// blank one. The check is a read-only transaction, which the driver begins
// plainly, so opening never waits on a peer's write; only a blank file takes
// the writer, and looks again under it.
func ensureSchema(db *sql.DB, path string) error {
	ctx := context.Background()
	state, err := inspectSchema(ctx, db, &sql.TxOptions{ReadOnly: true}, path)
	if err != nil {
		return err
	}
	if state != schemaBlank {
		if err := ensureColumns(ctx, db, path); err != nil {
			return err
		}
		return ensureIndexes(ctx, db, path)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	if state, err = inspectIn(ctx, tx, path); err != nil || state != schemaBlank {
		return err
	}
	if _, err := tx.ExecContext(ctx, schema+indexes); err != nil {
		return storeError(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO meta (id, store, rev) VALUES (1, ?, 0)", newStoreID()); err != nil {
		return storeError(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
		return storeError(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

// ensureColumns adds whichever of [addedColumns] the store lacks. Its rows
// then say nothing in them, and [writeTeams] fills them in ([reindexIn]).
func ensureColumns(ctx context.Context, db *sql.DB, path string) error {
	for _, c := range addedColumns {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info(?) WHERE name = ?", c.table, c.column).Scan(&n); err != nil {
			return unreadable(err, path)
		}
		if n > 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, c.add); err != nil {
			var again int
			// A peer that opened at the same moment may have added it first.
			if db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info(?) WHERE name = ?", c.table, c.column).Scan(&again) != nil || again == 0 {
				return storeError(err)
			}
		}
	}
	return nil
}

// ensureIndexes makes whichever of [indexes] the store lacks. Looking is a
// read; only a store without them all takes the writer.
func ensureIndexes(ctx context.Context, db *sql.DB, path string) error {
	marks, args := placeholders(indexNames)
	var have int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ("+marks+")", args...).Scan(&have); err != nil {
		return unreadable(err, path)
	}
	if have == len(indexNames) {
		return nil
	}
	_, err := db.ExecContext(ctx, indexes)
	return storeError(err)
}

type schemaState int

const (
	schemaReady schemaState = iota
	schemaBlank
)

func inspectSchema(ctx context.Context, db *sql.DB, opts *sql.TxOptions, path string) (schemaState, error) {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return schemaBlank, unreadable(err, path)
	}
	defer tx.Rollback()
	return inspectIn(ctx, tx, path)
}

func inspectIn(ctx context.Context, tx *sql.Tx, path string) (schemaState, error) {
	var app, version, tables int
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return schemaBlank, unreadable(err, path)
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return schemaBlank, unreadable(err, path)
	}
	switch {
	case app == applicationID && version == schemaVersion:
		var store string
		if err := tx.QueryRowContext(ctx, "SELECT store FROM meta WHERE id = 1").Scan(&store); err != nil {
			return schemaReady, unreadable(err, path)
		}
		for _, probe := range []string{
			"SELECT id, pos, rev, parent, made_ns, closed, root, factory, doc FROM teams LIMIT 0",
			"SELECT team_id, former, pos, key, doc FROM members LIMIT 0",
			"SELECT team_id, key, seq, doc FROM briefs LIMIT 0",
		} {
			if _, err := tx.ExecContext(ctx, probe); err != nil {
				return schemaReady, unreadable(err, path)
			}
		}
		return schemaReady, nil
	case app == applicationID && version > schemaVersion:
		return schemaReady, fmt.Errorf("%w: %s", ErrNewer, filepath.Base(path))
	case app != 0 || version != 0:
		return schemaReady, notOurs(path, "belongs to something else")
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return schemaBlank, unreadable(err, path)
	}
	if tables != 0 {
		return schemaReady, notOurs(path, "belongs to something else")
	}
	return schemaBlank, nil
}

// unreadable is err as this package says it: a peer holding the file too long
// is [ErrBusy]; a file SQLite says is not a database is not this store's
// ([notOurs]); a failure to reach the file (it cannot be opened, an I/O
// error, no memory or no room) is the error it is, since trying again may
// read it; and anything else SQLite refuses to read is [ErrUnreadable].
func unreadable(err error, path string) error {
	if err = storeError(err); err == nil || errors.Is(err, ErrBusy) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var e *sqlite.Error
	if errors.As(err, &e) {
		switch e.Code() & 0xff {
		case sqlite3.SQLITE_NOTADB:
			return notOurs(path, "is not a database")
		case sqlite3.SQLITE_IOERR, sqlite3.SQLITE_CANTOPEN, sqlite3.SQLITE_PERM, sqlite3.SQLITE_FULL,
			sqlite3.SQLITE_NOMEM, sqlite3.SQLITE_READONLY, sqlite3.SQLITE_INTERRUPT, sqlite3.SQLITE_NOLFS,
			sqlite3.SQLITE_PROTOCOL, sqlite3.SQLITE_AUTH:
			return fmt.Errorf("teams: %s: %w", filepath.Base(path), err)
		}
	}
	return fmt.Errorf("%w: %s: %w", ErrUnreadable, filepath.Base(path), err)
}

// storeError says SQLite's lock refusal as [ErrBusy]. Extended result codes
// keep their primary code in the low byte.
func storeError(err error) error {
	var e *sqlite.Error
	if errors.As(err, &e) {
		if code := e.Code() & 0xff; code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED {
			return fmt.Errorf("%w (waited %s)", ErrBusy, busyTimeout)
		}
	}
	return err
}

func newStoreID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// ── READING ─────────────────────────────────────────────────────────────────

// snapshot is the store at one revision, decoded, with each row as it was
// stored so a write can tell which rows it changed. It is never changed once
// made; readers get copies of its teams.
type snapshot struct {
	store string
	rev   int64
	teams []Team
	rows  map[string]*teamRows
	// open is a snapshot of the open teams only ([LoadOpen]); a write never
	// diffs against one.
	open bool
	// elsewhere, in an open snapshot, is every closed team an open team sits
	// under: left out of the teams, but there ([tidyWithin]).
	elsewhere map[string]bool
}

// teamRows is one team's rows as stored: rev is the store revision the team or
// any of its memberships last changed at, and at is its place in the
// snapshot's teams.
type teamRows struct {
	pos     int64
	rev     int64
	at      int
	doc     string
	members []memberRow
	former  []memberRow
}

// memberRow is one membership row: its key, the conversation it names
// ([ConversationName]) and its JSON.
type memberRow struct{ key, conv, doc string }

func (s *snapshot) stamp() string { return s.store + "." + strconv.FormatInt(s.rev, 10) }

// held is the newest snapshot read or written per database path.
var held struct {
	sync.Mutex
	by map[string]*snapshot
}

// storeReads counts the teams decoded from their rows, for a test to see that
// a team nobody wrote is not decoded again.
var storeReads atomic.Int64

func heldAt(path string) *snapshot {
	held.Lock()
	defer held.Unlock()
	return held.by[path]
}

func hold(path string, s *snapshot) {
	held.Lock()
	defer held.Unlock()
	if held.by == nil {
		held.by = map[string]*snapshot{}
	}
	if old := held.by[path]; old != nil && old.store == s.store && old.rev > s.rev {
		return
	}
	held.by[path] = s
	for p := range held.by {
		if len(held.by) <= handlesKept {
			break
		}
		if p != path {
			delete(held.by, p)
		}
	}
}

// querier is a transaction or anything else that answers queries.
type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// readPath is the store at path as it stands, nil when there is no store.
func readPath(ctx context.Context, path string) (*snapshot, error) {
	h, err := acquire(path, false)
	if err != nil || h == nil {
		return nil, err
	}
	defer h.release()
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, unreadable(err, path)
	}
	defer tx.Rollback()
	return readIn(ctx, tx, path, false)
}

// readOpenPath is [readPath] of the open teams only.
func readOpenPath(ctx context.Context, path string) (*snapshot, error) {
	h, err := acquire(path, false)
	if err != nil || h == nil {
		return nil, err
	}
	defer h.release()
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, unreadable(err, path)
	}
	defer tx.Rollback()
	return readIn(ctx, tx, path, true)
}

// heldKey is where a snapshot of path is held: one of every team, one of the
// open teams only.
func heldKey(path string, open bool) string {
	if open {
		return path + "\x00open"
	}
	return path
}

// readIn reads the store through q, answering the held snapshot when the
// revision has not moved, and otherwise decoding only the teams whose rows
// changed since it: the rest are the held snapshot's, which nobody changes.
func readIn(ctx context.Context, q querier, path string, open bool) (*snapshot, error) {
	var store string
	var rev int64
	if err := q.QueryRowContext(ctx, "SELECT store, rev FROM meta WHERE id = 1").Scan(&store, &rev); err != nil {
		return nil, unreadable(err, path)
	}
	held, other := heldAt(heldKey(path, open)), heldAt(heldKey(path, !open))
	if held != nil && held.store != store {
		held = nil
	}
	if other != nil && other.store != store {
		other = nil
	}
	if held != nil && held.rev == rev {
		return held, nil
	}
	s := &snapshot{store: store, rev: rev, rows: map[string]*teamRows{}, open: open}
	query := "SELECT id, pos, rev FROM teams ORDER BY pos"
	if open {
		query = "SELECT id, pos, rev FROM teams WHERE closed = 0 ORDER BY pos"
	}
	list, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, unreadable(err, path)
	}
	var stale []string
	for list.Next() {
		var id string
		var pos, at int64
		if err := list.Scan(&id, &pos, &at); err != nil {
			list.Close()
			return nil, unreadable(err, path)
		}
		r := &teamRows{pos: pos, rev: at, at: len(s.teams)}
		if from, old := reusable(id, at, held, other); old != nil {
			r.doc, r.members, r.former = old.doc, old.members, old.former
			s.teams = append(s.teams, from.teams[old.at])
		} else {
			stale = append(stale, id)
			s.teams = append(s.teams, Team{ID: id})
		}
		s.rows[id] = r
	}
	if err := list.Close(); err != nil {
		return nil, unreadable(err, path)
	}
	if open {
		if s.elsewhere, err = closedParents(ctx, q, path); err != nil {
			return nil, err
		}
	}
	storeReads.Add(int64(len(stale)))
	for len(stale) > 0 {
		n := min(len(stale), 400)
		if err := decodeTeams(ctx, q, path, s, stale[:n]); err != nil {
			return nil, err
		}
		stale = stale[n:]
	}
	hold(heldKey(path, open), s)
	return s, nil
}

// closedParents is every closed team an open team sits under, nil for none:
// one look per open team with a parent, by the closed team's id.
func closedParents(ctx context.Context, q querier, path string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, "SELECT DISTINCT p.id FROM teams o JOIN teams p ON p.id = o.parent WHERE o.closed = 0 AND p.closed = 1")
	if err != nil {
		return nil, unreadable(err, path)
	}
	defer rows.Close()
	var out map[string]bool
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, unreadable(err, path)
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[id] = true
	}
	return out, unreadable(rows.Err(), path)
}

// reusable is the first held snapshot that has team id's rows at revision
// at, and those rows.
func reusable(id string, at int64, held ...*snapshot) (*snapshot, *teamRows) {
	for _, s := range held {
		if s == nil {
			continue
		}
		if r := s.rows[id]; r != nil && r.rev == at {
			return s, r
		}
	}
	return nil, nil
}

// decodeTeams reads and decodes the rows of the teams ids name into s, whose
// rows already say where each sits.
func decodeTeams(ctx context.Context, q querier, path string, s *snapshot, ids []string) error {
	marks, args := placeholders(ids)
	list, err := q.QueryContext(ctx, "SELECT id, doc FROM teams WHERE id IN ("+marks+")", args...)
	if err != nil {
		return unreadable(err, path)
	}
	for list.Next() {
		var id, doc string
		if err := list.Scan(&id, &doc); err != nil {
			list.Close()
			return unreadable(err, path)
		}
		var t Team
		if err := json.Unmarshal([]byte(doc), &t); err != nil {
			list.Close()
			return fmt.Errorf("%w: team %s: %w", ErrUnreadable, id, err)
		}
		r := s.rows[id]
		t.ID, t.Members, t.FormerMembers = id, nil, nil
		s.teams[r.at], r.doc = t, doc
	}
	if err := list.Close(); err != nil {
		return unreadable(err, path)
	}
	return readMembers(ctx, q, path, s, "SELECT team_id, former, key, conv, doc FROM members WHERE team_id IN ("+marks+") ORDER BY team_id, former, pos", args...)
}

func readMembers(ctx context.Context, q querier, path string, s *snapshot, query string, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return unreadable(err, path)
	}
	defer rows.Close()
	for rows.Next() {
		var team, key, conv, doc string
		var former bool
		if err := rows.Scan(&team, &former, &key, &conv, &doc); err != nil {
			return unreadable(err, path)
		}
		r := s.rows[team]
		if r == nil {
			continue
		}
		i := r.at
		var m Member
		if err := json.Unmarshal([]byte(doc), &m); err != nil {
			return fmt.Errorf("%w: a member of team %s: %w", ErrUnreadable, team, err)
		}
		if former {
			s.teams[i].FormerMembers = append(s.teams[i].FormerMembers, m)
			r.former = append(r.former, memberRow{key, conv, doc})
		} else {
			s.teams[i].Members = append(s.teams[i].Members, m)
			r.members = append(r.members, memberRow{key, conv, doc})
		}
	}
	return unreadable(rows.Err(), path)
}

// file is a copy of the snapshot's teams the caller may change.
func (s *snapshot) file() *File {
	teams := make([]Team, len(s.teams))
	for i, t := range s.teams {
		teams[i] = t.Clone()
	}
	return &File{Version: Version, Teams: teams, localIdentities: true}
}

// ── WRITING ─────────────────────────────────────────────────────────────────

// encodeTeam is the team's own row and its memberships' rows. Briefs are
// never in them: they are rows of their own ([rowWriter.briefs]).
func encodeTeam(t Team) (string, []memberRow, []memberRow, error) {
	bare := t
	bare.Members, bare.FormerMembers = nil, nil
	doc, err := json.Marshal(bare)
	if err != nil {
		return "", nil, nil, err
	}
	members, err := encodeMembers(t.Members)
	if err != nil {
		return "", nil, nil, err
	}
	former, err := encodeMembers(t.FormerMembers)
	if err != nil {
		return "", nil, nil, err
	}
	return string(doc), members, former, nil
}

func encodeMembers(list []Member) ([]memberRow, error) {
	out := make([]memberRow, len(list))
	for i, m := range list {
		m.Briefs, m.FactoryBrief = nil, ""
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		out[i] = memberRow{m.Key, memberConv(m), string(raw)}
	}
	return out, nil
}

// writeTeams makes the store hold teams, in order, changing only the rows that
// differ from before (the snapshot the caller read, nil for none). The caller
// holds the teams lock. It answers the snapshot written.
func writeTeams(profileDir string, before *snapshot, teams []Team) (*snapshot, error) {
	return writeScoped(profileDir, before, teams, false)
}

// writeScoped is [writeTeams] over every team, or with open over the open
// teams only: then a closed team is neither compared nor written, and only an
// open team the list leaves out is deleted.
func writeScoped(profileDir string, before *snapshot, teams []Team, open bool) (*snapshot, error) {
	path := Path(profileDir)
	h, err := acquire(path, true)
	if err != nil {
		return nil, err
	}
	defer h.release()
	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeError(err)
	}
	defer tx.Rollback()
	// The rows are compared with what the store holds now. Under the lock that
	// is what the caller read; a writer that could not take the lock may have
	// moved it, and its rows are then read again inside this transaction.
	var store string
	var rev, indexed int64
	if err := tx.QueryRowContext(ctx, "SELECT store, rev, indexed FROM meta WHERE id = 1").Scan(&store, &rev, &indexed); err != nil {
		return nil, unreadable(err, path)
	}
	// A store an earlier build of this schema wrote last has lookup columns
	// that may not say what its rows hold; they are said again first.
	reindexed := indexed != rev
	if reindexed {
		if err := reindexIn(ctx, tx); err != nil {
			return nil, storeError(err)
		}
	}
	if before == nil || before.open != open || before.store != store || before.rev != rev {
		if before, err = readIn(ctx, tx, path, open); err != nil {
			return nil, err
		}
	}
	next := &snapshot{store: store, rev: rev, rows: make(map[string]*teamRows, len(teams)), open: open, elsewhere: before.elsewhere}
	// A team that needs a place goes after every team in the store, so its
	// place is never one a team outside this write already holds.
	var free int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(pos), -1) + 1 FROM teams").Scan(&free); err != nil {
		return nil, unreadable(err, path)
	}
	changed := false
	w := rowWriter{ctx: ctx, tx: tx}
	keep := make(map[string]bool, len(teams))
	for _, t := range teams {
		keep[t.ID] = true
	}
	for id := range before.rows {
		if keep[id] {
			continue
		}
		w.exec("DELETE FROM teams WHERE id = ?", id)
		w.exec("DELETE FROM members WHERE team_id = ?", id)
		changed = true
	}
	// A member who leaves one team and joins another in the same write takes
	// its briefs along (a move); one who only leaves takes them nowhere.
	joined := map[string]string{}
	left := map[string][]string{}
	last := int64(-1)
	for _, t := range teams {
		doc, members, former, err := encodeTeam(t)
		if err != nil {
			return nil, err
		}
		old := before.rows[t.ID]
		var pos int64
		if old != nil && old.pos > last {
			pos = old.pos
		} else {
			pos, free = free, free+1
		}
		last = pos
		r := &teamRows{pos: pos, at: len(next.teams), doc: doc, members: members, former: former}
		next.rows[t.ID] = r
		next.teams = append(next.teams, t)
		var oldMembers, oldFormer []memberRow
		if old != nil {
			oldMembers, oldFormer, r.rev = old.members, old.former, old.rev
		}
		// A membership written moves the team's own revision too, so a reader
		// decodes this team again and no other.
		moved := w.members(t.ID, false, oldMembers, members)
		moved = w.members(t.ID, true, oldFormer, former) || moved
		if old == nil || old.doc != doc || old.pos != pos {
			closed, root, factory := t.Closed(), t.Root, t.FactoryManaged
			w.exec(`INSERT INTO teams (id, pos, rev, parent, made_ns, closed, root, factory, founder, doc) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET pos = excluded.pos, rev = excluded.rev, parent = excluded.parent, made_ns = excluded.made_ns,
 closed = excluded.closed, root = excluded.root, factory = excluded.factory, founder = excluded.founder, doc = excluded.doc`,
				t.ID, pos, rev+1, t.Parent, madeNanos(t.Made), closed, root, factory, founderConv(t), doc)
			r.rev, changed = rev+1, true
		} else if moved {
			w.exec("UPDATE teams SET rev = ? WHERE id = ?", rev+1, t.ID)
			r.rev = rev + 1
		}
		changed = changed || moved
		was := keysOf(oldMembers)
		now := keysOf(members)
		for k := range was {
			if !now[k] {
				left[k] = append(left[k], t.ID)
			}
		}
		for k := range now {
			if !was[k] {
				if _, ok := joined[k]; !ok {
					joined[k] = t.ID
				}
			}
		}
		if w.briefs(t) {
			changed = true
		}
	}
	for id := range before.rows {
		if !keep[id] {
			for _, m := range before.rows[id].members {
				left[m.key] = append(left[m.key], id)
			}
		}
	}
	for key, teams := range left {
		sort.Strings(teams)
		for _, from := range teams {
			if to, ok := joined[key]; ok && keep[to] && to != from {
				w.exec("UPDATE OR IGNORE briefs SET team_id = ? WHERE team_id = ? AND key = ?", to, from, key)
				delete(joined, key)
			}
			w.exec("DELETE FROM briefs WHERE team_id = ? AND key = ?", from, key)
			changed = true
		}
	}
	for id := range before.rows {
		if !keep[id] {
			w.exec("DELETE FROM briefs WHERE team_id = ?", id)
		}
	}
	if w.err != nil {
		return nil, storeError(w.err)
	}
	next.teams = stripped(next.teams)
	if !changed {
		if reindexed {
			if _, err := tx.ExecContext(ctx, "UPDATE meta SET indexed = rev WHERE id = 1"); err != nil {
				return nil, storeError(err)
			}
			if err := tx.Commit(); err != nil {
				return nil, storeError(err)
			}
		}
		hold(heldKey(path, open), next)
		return next, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE meta SET rev = rev + 1, indexed = rev + 1 WHERE id = 1"); err != nil {
		return nil, storeError(err)
	}
	before0 := modTime(path)
	if err := tx.Commit(); err != nil {
		return nil, storeError(err)
	}
	next.rev = rev + 1
	// An open-teams write that closed a team left it in its list; the open
	// teams at this revision are read again, reusing every unchanged row.
	if !open || !holdsClosed(next.teams) {
		hold(heldKey(path, open), next)
	}
	// A watcher that stats the database, or waits on its directory, sees the
	// write: the commit landed in the WAL, so the file's own time is moved.
	touch(path, before0)
	return next, nil
}

// holdsClosed reports whether any of teams is closed.
func holdsClosed(teams []Team) bool {
	for _, t := range teams {
		if t.Closed() {
			return true
		}
	}
	return false
}

// stripped is a copy of teams with no briefs, as a load answers them.
func stripped(teams []Team) []Team {
	out := make([]Team, len(teams))
	for i, t := range teams {
		t = t.Clone()
		for j := range t.Members {
			t.Members[j].Briefs, t.Members[j].FactoryBrief = nil, ""
		}
		for j := range t.FormerMembers {
			t.FormerMembers[j].Briefs, t.FormerMembers[j].FactoryBrief = nil, ""
		}
		out[i] = t
	}
	return out
}

func keysOf(rows []memberRow) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.key != "" {
			out[r.key] = true
		}
	}
	return out
}

func madeNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// touch moves path's modification time past before.
func touch(path string, before time.Time) {
	now := time.Now()
	if !now.After(before) {
		now = before.Add(time.Microsecond)
	}
	_ = os.Chtimes(path, now, now)
}

// rowWriter runs statements until the first error, which it keeps.
type rowWriter struct {
	ctx context.Context
	tx  *sql.Tx
	err error
}

func (w *rowWriter) exec(query string, args ...any) {
	if w.err != nil {
		return
	}
	_, w.err = w.tx.ExecContext(w.ctx, query, args...)
}

// members makes one team's member (or former member) rows say rows, given
// they said old, and reports whether it wrote. An unchanged list writes
// nothing, a changed row is rewritten alone, an appended member is one insert,
// and anything else (a removal, a reorder) rewrites this one list.
func (w *rowWriter) members(team string, former bool, old, rows []memberRow) bool {
	same := min(len(old), len(rows))
	prefix := 0
	for prefix < same && old[prefix] == rows[prefix] {
		prefix++
	}
	switch {
	case len(old) == len(rows) && prefix == len(rows):
		return false
	case len(old) == len(rows):
		for i := prefix; i < len(rows); i++ {
			if old[i] != rows[i] {
				w.exec("UPDATE members SET key = ?, conv = ?, doc = ? WHERE team_id = ? AND former = ? AND pos = ?", rows[i].key, rows[i].conv, rows[i].doc, team, former, i)
			}
		}
	case len(rows) > len(old) && prefix == len(old):
		for i := prefix; i < len(rows); i++ {
			w.exec("INSERT INTO members (team_id, former, pos, key, conv, doc) VALUES (?, ?, ?, ?, ?, ?)", team, former, i, rows[i].key, rows[i].conv, rows[i].doc)
		}
	default:
		w.exec("DELETE FROM members WHERE team_id = ? AND former = ? AND pos >= ?", team, former, prefix)
		for i := prefix; i < len(rows); i++ {
			w.exec("INSERT INTO members (team_id, former, pos, key, conv, doc) VALUES (?, ?, ?, ?, ?, ?)", team, former, i, rows[i].key, rows[i].conv, rows[i].doc)
		}
	}
	return true
}

// briefs writes the briefs a member of t carries. A load never fills them, so
// one that is there was given by the write: a factory brief replaces the
// stored one, and revisions replace the stored revisions. A member that
// carries none leaves its stored briefs as they are.
func (w *rowWriter) briefs(t Team) bool {
	wrote := false
	for _, m := range t.Members {
		if m.Key == "" {
			continue
		}
		if m.FactoryBrief != "" {
			w.exec(`INSERT INTO briefs (team_id, key, seq, doc) VALUES (?, ?, 0, ?)
ON CONFLICT (team_id, key, seq) DO UPDATE SET doc = excluded.doc WHERE doc <> excluded.doc`, t.ID, m.Key, m.FactoryBrief)
			wrote = true
		}
		if len(m.Briefs) == 0 {
			continue
		}
		w.exec("DELETE FROM briefs WHERE team_id = ? AND key = ? AND seq > 0", t.ID, m.Key)
		for i, rev := range m.Briefs {
			raw, err := json.Marshal(rev)
			if err != nil {
				w.err = err
				return wrote
			}
			w.exec("INSERT INTO briefs (team_id, key, seq, doc) VALUES (?, ?, ?, ?)", t.ID, m.Key, i+1, string(raw))
		}
		wrote = true
	}
	return wrote
}

// ── BRIEFS, READ WHEN SHOWN ─────────────────────────────────────────────────

// WithBriefs is m, a member of team teamID as a load answered it, with its
// briefs read from the store: the factory's opening brief and the brief's
// revisions. A load leaves both out, because they are most of what the store
// holds and almost nothing that reads a team shows them. A brief m already
// carries is kept when the store has none.
func WithBriefs(profileDir, teamID string, m Member) (Member, error) {
	if m.Key == "" {
		return m, nil
	}
	path := Path(profileDir)
	h, err := acquire(path, false)
	if err != nil || h == nil {
		return m, err
	}
	defer h.release()
	ctx := context.Background()
	rows, err := h.db.QueryContext(ctx, "SELECT seq, doc FROM briefs WHERE team_id = ? AND key = ? ORDER BY seq", teamID, m.Key)
	if err != nil {
		return m, unreadable(err, path)
	}
	defer rows.Close()
	var revisions []BriefRevision
	for rows.Next() {
		var seq int
		var doc string
		if err := rows.Scan(&seq, &doc); err != nil {
			return m, unreadable(err, path)
		}
		if seq == 0 {
			m.FactoryBrief = doc
			continue
		}
		var rev BriefRevision
		if err := json.Unmarshal([]byte(doc), &rev); err != nil {
			return m, fmt.Errorf("%w: a brief of team %s: %w", ErrUnreadable, teamID, err)
		}
		revisions = append(revisions, rev)
	}
	if err := rows.Err(); err != nil {
		return m, unreadable(err, path)
	}
	if len(revisions) > 0 {
		m.Briefs = revisions
	}
	return m, nil
}
