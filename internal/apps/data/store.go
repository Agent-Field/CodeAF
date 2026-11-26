package data

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
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	_ "modernc.org/sqlite"
)

// The store's bounds and clocks.
const (
	// busyWait is SQLite's own patience. The writer is the only writer, so a
	// wait is only ever another process holding the file (a backup, a second
	// codeaf on the same home), and it should not be long.
	busyWait = 5 * time.Second
	// readers is the most connections reading at once.
	readers = 8
	// writeQueue is how many writes may wait for the writer.
	writeQueue = 256
	// dispatchQueue is how many committed changes may wait to be told to
	// subscribers and hooks before a write waits for them.
	dispatchQueue = 4096
	// refreshEvery is how often computed fields that read the clock are
	// computed again. days_since() changes once a day; an hour keeps it right
	// to the hour without walking the table every minute.
	refreshEvery = time.Hour
)

// migrations are the schema's versions, in order; PRAGMA user_version is how
// many have run. A version once shipped is never edited: a change is a new
// entry.
//
// EVERYTHING IS NAMED BY A STABLE ID, NOT BY ITS NAME. A type, a field and the
// person each have a ULID made once; records point to their type by id and
// keep their values under field ids, so renaming a field or a type is one row
// and every value, history row and index stays where it was. A name a type or
// field was once known by stays readable as an alias.
var migrations = []string{
	// 1: the first store.
	`
	CREATE TABLE space(
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	CREATE TABLE apps(
		app TEXT PRIMARY KEY,
		def_json TEXT NOT NULL,
		registered_at TEXT NOT NULL
	);
	CREATE TABLE types(
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		created_by_app TEXT NOT NULL,
		def_app TEXT NOT NULL,
		def_json TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE type_aliases(
		alias TEXT PRIMARY KEY,
		type_id TEXT NOT NULL
	);
	CREATE TABLE app_types(
		app TEXT NOT NULL,
		type_id TEXT NOT NULL,
		uses INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(app, type_id)
	);
	CREATE TABLE fields(
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL,
		name TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(type_id, name)
	);
	CREATE TABLE field_aliases(
		type_id TEXT NOT NULL,
		alias TEXT NOT NULL,
		field_id TEXT NOT NULL,
		PRIMARY KEY(type_id, alias)
	);
	CREATE TABLE type_fields(
		field_id TEXT NOT NULL,
		app TEXT NOT NULL,
		def_json TEXT NOT NULL,
		pos INTEGER NOT NULL,
		hidden INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(field_id, app)
	);
	CREATE TABLE records(
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL,
		rev INTEGER NOT NULL DEFAULT 1,
		data_json TEXT NOT NULL,
		meta_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		deleted_at TEXT
	);
	CREATE INDEX records_type_created ON records(type_id, created_at, id) WHERE deleted_at IS NULL;
	CREATE INDEX records_type_updated ON records(type_id, updated_at, id) WHERE deleted_at IS NULL;
	CREATE TABLE batches(
		batch TEXT PRIMARY KEY,
		app TEXT NOT NULL,
		actor TEXT NOT NULL,
		by_kind TEXT NOT NULL,
		cause TEXT NOT NULL DEFAULT '',
		op TEXT NOT NULL,
		type_id TEXT NOT NULL,
		at TEXT NOT NULL,
		undoes TEXT,
		undone_by TEXT
	);
	CREATE TABLE history(
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		batch TEXT NOT NULL,
		type_id TEXT NOT NULL,
		record TEXT NOT NULL,
		app TEXT NOT NULL,
		actor TEXT NOT NULL,
		by_kind TEXT NOT NULL,
		cause TEXT NOT NULL DEFAULT '',
		op TEXT NOT NULL,
		note TEXT NOT NULL DEFAULT '',
		before_json TEXT,
		after_json TEXT,
		at TEXT NOT NULL
	);
	CREATE INDEX history_record ON history(record, seq);
	CREATE INDEX history_batch ON history(batch, seq);
	CREATE TABLE reveals(
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		type_id TEXT NOT NULL,
		record TEXT NOT NULL,
		field_id TEXT NOT NULL,
		app TEXT NOT NULL,
		actor TEXT NOT NULL,
		at TEXT NOT NULL
	);
	CREATE TABLE sequences(
		field_id TEXT PRIMARY KEY,
		next INTEGER NOT NULL
	);
	CREATE TABLE outbox(
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		event TEXT NOT NULL,
		type_id TEXT NOT NULL DEFAULT '',
		record TEXT NOT NULL DEFAULT '',
		batch TEXT NOT NULL DEFAULT '',
		app TEXT NOT NULL DEFAULT '',
		by_kind TEXT NOT NULL,
		cause TEXT NOT NULL DEFAULT '',
		payload_json TEXT NOT NULL,
		at TEXT NOT NULL
	);
	CREATE TABLE outbox_receipts(
		seq INTEGER NOT NULL,
		trigger TEXT NOT NULL,
		at TEXT NOT NULL,
		PRIMARY KEY(seq, trigger)
	);
	CREATE TABLE work_items(
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		type_id TEXT NOT NULL DEFAULT '',
		record TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '',
		due TEXT NOT NULL DEFAULT '',
		sla_at TEXT NOT NULL DEFAULT '',
		form_ref TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		outcome_json TEXT NOT NULL DEFAULT 'null',
		app TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX work_items_assignee ON work_items(assignee, status);
	CREATE INDEX work_items_record ON work_items(record);
	CREATE TABLE timers(
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL DEFAULT '',
		record TEXT NOT NULL DEFAULT '',
		fire_at TEXT NOT NULL,
		kind TEXT NOT NULL,
		payload_json TEXT NOT NULL DEFAULT 'null',
		every TEXT NOT NULL DEFAULT '',
		fired_at TEXT,
		cancelled_at TEXT,
		app TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX timers_pending ON timers(fire_at) WHERE fired_at IS NULL AND cancelled_at IS NULL;
	CREATE INDEX timers_record ON timers(record);
	CREATE TABLE signoffs(
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL,
		record TEXT NOT NULL,
		version_hash TEXT NOT NULL,
		statement TEXT NOT NULL,
		signer TEXT NOT NULL,
		app TEXT NOT NULL DEFAULT '',
		at TEXT NOT NULL
	);
	CREATE INDEX signoffs_record ON signoffs(record, at);
	CREATE TABLE comments(
		id TEXT PRIMARY KEY,
		type_id TEXT NOT NULL DEFAULT '',
		record TEXT NOT NULL DEFAULT '',
		node TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL,
		author TEXT NOT NULL,
		mentions_json TEXT NOT NULL DEFAULT '[]',
		app TEXT NOT NULL DEFAULT '',
		at TEXT NOT NULL
	);
	CREATE INDEX comments_record ON comments(record, at);
	CREATE INDEX comments_node ON comments(app, node, at);
	`,
	dataPlaneMigration,
	writeRulesMigration,
	ruleSourceGrantMigration,
	identityMigration,
	agentGrantMigration,
	intakeMigration,
	aiReviewMigration,
	outputMigration,
	runWriteMigration,
	residencyMigration,
}

// querier is what reads take: the reader pool or the writer's transaction.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Store is one person's data file (or a draft's sample file).
type Store struct {
	path  string
	draft string
	now   func() time.Time

	requireAgentPrincipal bool

	wdb *sql.DB
	rdb *sql.DB

	schema atomic.Pointer[schema]

	writes chan writeOp
	notify chan *Change
	quit   chan struct{}
	done   sync.WaitGroup
	closed atomic.Bool
	// closeMu orders Close against a write being queued, so nothing is sent
	// on a closed queue.
	closeMu sync.RWMutex

	// ids is the writer goroutine's own.
	ids idGen

	subsMu sync.Mutex
	subs   map[subscriptionKey]*subscription
	// visibility orders policy publication against live projections.
	visibility sync.RWMutex

	hooksMu  sync.RWMutex
	hooks    map[int]func(Change)
	nextHook int

	// matches are compiled conditions, by schema, type and text.
	matches sync.Map
}

// Options are an [Open]'s choices; the zero value is the real clock.
type Options struct {
	Now func() time.Time
	// Draft names the draft a sample store belongs to; it is carried on
	// every [Change] the store reports.
	Draft string
	// NoRefresh leaves the hourly recompute of clocked fields and the timer
	// clock off, for a test that drives them with [Store.Refresh] and
	// [Store.FireDue].
	NoRefresh bool
	// Person is the person's id to use when the file has none yet: a draft's
	// sample file takes the person of the real one, so "me" is one person in
	// both.
	Person string
	// RequireAgentPrincipal denies legacy AI writes without a bound identity
	// and action in a live app store.
	RequireAgentPrincipal bool
}

// Open opens (making it if need be) the data file at path, runs the
// migrations it lacks, and starts its writer.
func Open(path string, opts Options) (*Store, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("could not make the data folder: %w", err)
	}
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyWait.Milliseconds()))
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	wdb, err := sql.Open("sqlite", sqlitefile.URI(abs, q.Encode()))
	if err != nil {
		return nil, err
	}
	// The writer is one connection: the one writer goroutine is the only
	// user of it.
	wdb.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) {
		_ = wdb.Close()
		return nil, fmt.Errorf("could not open %s: %w", abs, err)
	}
	var mode string
	if err := wdb.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		return fail(err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fail(fmt.Errorf("SQLite chose journal mode %q", mode))
	}
	if err := migrate(wdb); err != nil {
		return fail(err)
	}
	if err := ensurePerson(wdb, opts); err != nil {
		return fail(err)
	}
	rq := url.Values{}
	rq.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyWait.Milliseconds()))
	// A reader connection refuses to write, so nothing can reach the file
	// except through the writer.
	rq.Add("_pragma", "query_only(1)")
	rdb, err := sql.Open("sqlite", sqlitefile.URI(abs, rq.Encode()))
	if err != nil {
		return fail(err)
	}
	rdb.SetMaxOpenConns(readers)
	rdb.SetMaxIdleConns(readers)

	s := &Store{
		path: abs, draft: opts.Draft, now: opts.Now, wdb: wdb, rdb: rdb,
		requireAgentPrincipal: opts.RequireAgentPrincipal,
		writes:                make(chan writeOp, writeQueue), notify: make(chan *Change, dispatchQueue),
		quit: make(chan struct{}), subs: map[subscriptionKey]*subscription{}, hooks: map[int]func(Change){},
	}
	sch, _, err := loadSchema(wdb, s.now)
	if err != nil {
		_ = rdb.Close()
		return fail(err)
	}
	sch.policyAudit = s.AuditRecordPolicy
	s.schema.Store(sch)
	s.done.Add(2)
	go s.writer()
	go s.dispatcher()
	if !opts.NoRefresh {
		s.done.Add(2)
		go s.refresher()
		go s.clock()
	}
	return s, nil
}

// migrate runs every migration the file has not had, each in its own
// transaction with the version bump, so a crash leaves a whole version.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("the data file is version %d, newer than this codeaf (%d); update codeaf", version, len(migrations))
	}
	for v := version; v < len(migrations); v++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ensurePerson gives the file its person, once: the id every "me", every
// person field the person holds and every history row they wrote carries.
func ensurePerson(db *sql.DB, opts Options) error {
	person := opts.Person
	if !validID(person) {
		var g idGen
		person = g.next(opts.Now())
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO space(key, value) VALUES('person', ?)`, person)
	return err
}

// Person is the id of the person this file belongs to.
func (s *Store) Person() string { return s.schema.Load().person }

// Path is the file the store keeps.
func (s *Store) Path() string { return s.path }

// Close stops the writer after the writes already queued, tells subscribers
// and hooks what those committed, and closes the file.
func (s *Store) Close() error {
	s.closeMu.Lock()
	if s.closed.Swap(true) {
		s.closeMu.Unlock()
		return nil
	}
	close(s.quit)
	s.closeMu.Unlock()
	s.done.Wait()
	return errors.Join(s.rdb.Close(), s.wdb.Close())
}

// writeOp is one write for the writer: fn runs inside a transaction and
// answers the change it made (nil for none) and, for a registration, the new
// schema.
type writeOp struct {
	fn    func(w *writeTx) error
	reply chan error
}

// writeTx is what a write's closure works with: the transaction, the schema
// as it is at this write, the write's clock, and the change it is making.
type writeTx struct {
	s      *Store
	tx     *sql.Tx
	schema *schema
	now    time.Time
	change *Change
	// next is the schema a registration made, swapped in after the commit.
	next *schema
	// cursor is the last outbox row this write made.
	cursor      int64
	afterCommit func()
}

// write hands fn to the writer and waits for it to commit or fail.
func (s *Store) write(fn func(w *writeTx) error) error {
	op := writeOp{fn: fn, reply: make(chan error, 1)}
	s.closeMu.RLock()
	if s.closed.Load() {
		s.closeMu.RUnlock()
		return errorf(CodeClosed, "the data store is closed")
	}
	select {
	case s.writes <- op:
	case <-s.quit:
		s.closeMu.RUnlock()
		return errorf(CodeClosed, "the data store is closed")
	}
	s.closeMu.RUnlock()
	return <-op.reply
}

// writer is the one goroutine that writes. It runs each write in its own
// transaction; a panic in a write rolls that write back and fails it, and the
// writer carries on.
func (s *Store) writer() {
	defer s.done.Done()
	defer close(s.notify)
	for {
		select {
		case op := <-s.writes:
			op.reply <- s.run(op.fn)
		case <-s.quit:
			// Finish what was queued before Close, then stop.
			for {
				select {
				case op := <-s.writes:
					op.reply <- s.run(op.fn)
				default:
					return
				}
			}
		}
	}
}

// run is one write in one transaction.
func (s *Store) run(fn func(w *writeTx) error) (err error) {
	tx, err := s.wdb.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	w := &writeTx{s: s, tx: tx, schema: s.schema.Load(), now: s.now()}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			err = fmt.Errorf("the write failed: %v", r)
		}
	}()
	if err := fn(w); err != nil {
		_ = tx.Rollback()
		return err
	}
	s.visibility.Lock()
	if err := tx.Commit(); err != nil {
		s.visibility.Unlock()
		return err
	}
	if w.next != nil {
		w.next.policyAudit = s.AuditRecordPolicy
		s.schema.Store(w.next)
	}
	if w.afterCommit != nil {
		w.afterCommit()
	}
	s.visibility.Unlock()
	if w.change != nil && (len(w.change.Records) > 0 || len(w.change.Items) > 0 || w.change.invalidation != nil || w.change.resume != nil) {
		w.change.Draft = s.draft
		s.notify <- w.change
	}
	return nil
}

// RegisterApp merges one app's record types, fields and capabilities into
// a draft or legacy catalog. Published definitions use [Store.PublishApp].
// Re-registration hides fields dropped by the app and preserves their values.
func (s *Store) RegisterApp(appJSON []byte) error {
	def, err := ParseApp(appJSON)
	if err != nil {
		return err
	}
	return s.write(func(w *writeTx) error {
		if _, err := readPublication(w.tx, def.ID); !errors.Is(err, sql.ErrNoRows) {
			// A published app changes its declarations only with its version.
			return err
		}
		next, err := registerApp(w, def, nil)
		if err != nil {
			return err
		}
		w.next = next
		return w.invalidate(EventPolicyChanged, "", "")
	})
}

// ForgetApp removes an app's capabilities and its claim on its types; every
// record and value stays (apps.delete and apps.archive keep data).
func (s *Store) ForgetApp(app string) error {
	return s.write(func(w *writeTx) error {
		if _, err := readPublication(w.tx, app); !errors.Is(err, sql.ErrNoRows) {
			if err != nil {
				return err
			}
			return errorf(CodeInvalid, "remove a published app through publication")
		}
		next, err := forgetApp(w, app)
		if err != nil {
			return err
		}
		w.next = next
		return w.invalidate(EventPolicyChanged, "", "")
	})
}

// HasApp reports whether app is registered on this store.
func (s *Store) HasApp(app string) bool {
	_, ok := s.schema.Load().caps[app]
	return ok
}

// Types lists every type with its owner, users, fields and live count. An
// app sees only the types its capabilities name, and no hidden field.
func (s *Store) Types(sc Scope) ([]TypeInfo, error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	sch := s.schema.Load()
	counts := map[string]int{}
	stmt := "SELECT type_id, count(*) FROM records WHERE deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM type_residency WHERE type_id = records.type_id AND mode = 'live')"
	var args []any
	if sch.hasRowPolicies {
		var protected []string
		for _, t := range sch.byID {
			if t.HasRowPolicy() {
				protected = append(protected, t.ID)
			}
		}
		stmt += " AND type_id NOT IN (SELECT value FROM json_each(?))"
		args = append(args, canonicalJSON(protected))
	}
	rows, err := s.rdb.Query(stmt+" GROUP BY type_id", args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[typ] = n
	}
	rows.Close()
	var out []TypeInfo
	for _, t := range sch.sortedTypes() {
		if sc.App != "" {
			if allowed, err := sch.allow(sc, t.Name, false); err != nil || allowed.ID != t.ID {
				continue
			}
		}
		if t.HasRowPolicy() {
			query := compiled{t: t, sch: sch, alias: "r"}
			query.applyPolicy(sc)
			where, args := query.filter()
			var count int
			if err := s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&count); err != nil {
				return nil, err
			}
			counts[t.ID] = count
		}
		info := TypeInfo{
			ID: t.ID, Name: t.Name, Common: t.Common, Title: t.titleField(), Label: t.Label, CreatedByApp: t.CreatedByApp, DefinedBy: t.DefinedBy,
			UsedBy: append([]string(nil), t.UsedBy...), Hidden: t.Hidden, Count: counts[t.ID], Ledger: t.Ledger,
		}
		if t.packagePrivate {
			for _, alias := range sortedKeys(sch.bindings[t.DefinedBy]) {
				if sch.bindings[t.DefinedBy][alias].TypeID == t.ID {
					if info.Label == "" {
						info.Label = strings.ReplaceAll(alias, "_", " ")
					}
					if sc.App == t.DefinedBy {
						info.Name = alias
					}
					break
				}
			}
		}
		if t.Lifecycle != nil {
			lc := t.Lifecycle.LifecycleDef
			info.Lifecycle = &lc
		}
		for _, name := range t.Order {
			f := t.Fields[name]
			if f.Hidden && sc.App != "" {
				continue
			}
			info.Fields = append(info.Fields, FieldInfo{ID: f.ID, Name: name, FieldDef: f.FieldDef, App: f.App, Hidden: f.Hidden, Visibility: t.FieldPolicy(f)})
		}
		out = append(out, info)
	}
	return out, nil
}

// Type answers the merged definition of one type a scope may read.
func (s *Store) Type(sc Scope, typ string) (*Type, error) {
	return s.schema.Load().allow(sc, typ, false)
}

// OnCommit registers fn to be told of every committed change, in commit
// order, and answers the function that stops it. fn runs on the store's one
// dispatch goroutine, after subscribers are told: it must return quickly and
// hand any real work elsewhere, or every later change waits for it. The rows
// it is given are whole, masked values included; it is for the engine's own
// use (record.changed triggers), never for a client.
//
// fn MUST NOT write to this store or close it before it returns: the writer
// may be waiting for the dispatcher to take the next change, and the
// dispatcher is the goroutine fn runs on. A trigger that writes hands its
// write to a goroutine of its own. A change committed before fn was
// registered may still reach it, since changes are told after the commit.
func (s *Store) OnCommit(fn func(Change)) (stop func()) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	id := s.nextHook
	s.nextHook++
	s.hooks[id] = fn
	return func() {
		s.hooksMu.Lock()
		defer s.hooksMu.Unlock()
		delete(s.hooks, id)
	}
}

// dispatcher tells subscribers and hooks of each committed change, one at a
// time and in commit order.
func (s *Store) dispatcher() {
	defer s.done.Done()
	for c := range s.notify {
		s.deliver(c)
		if c.resume != nil {
			continue
		}
		s.hooksMu.RLock()
		hooks := make([]func(Change), 0, len(s.hooks))
		for _, id := range sortedHookIDs(s.hooks) {
			hooks = append(hooks, s.hooks[id])
		}
		s.hooksMu.RUnlock()
		for _, fn := range hooks {
			func() {
				defer func() { _ = recover() }()
				fn(*c)
			}()
		}
	}
}

func sortedHookIDs(m map[int]func(Change)) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// timerEvery is how often the store looks for timers come due. A timer is a
// follow-up or an SLA, kept to the minute, not the millisecond.
const timerEvery = 15 * time.Second

// clock fires timers as they come due, and at the start every timer that came
// due while the store was closed (the catch-up).
func (s *Store) clock() {
	defer s.done.Done()
	_, _ = s.FireDue()
	tick := time.NewTicker(timerEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			_, _ = s.FireDue()
		case <-s.quit:
			return
		}
	}
}

// refresher recomputes clocked fields every [refreshEvery], and once at the
// start, since the store may have been closed for days.
func (s *Store) refresher() {
	defer s.done.Done()
	_ = s.Refresh()
	tick := time.NewTicker(refreshEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			_ = s.Refresh()
		case <-s.quit:
			return
		}
	}
}
