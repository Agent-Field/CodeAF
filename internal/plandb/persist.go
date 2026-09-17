package plandb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
)

const stateVersion = 2

// state is the whole plan as the store keeps it in memory: the project and its
// root, every task in admission order, the notes and context entries, and the
// next id the store has not handed out. It is read from and written to the
// SQLite database WHOLE, inside one transaction, and never half of it: the
// store holds hundreds of tasks, not millions, so reloading all of them is the
// cheapest thing that is also the correct one, and it is what makes a handle's
// stale memory unable to erase another writer's task.
type state struct {
	Version  int
	Project  string
	RootID   string
	Tasks    map[string]*Task
	Order    []string
	Notes    []Note
	Contexts []ContextEntry
	NextID   uint64
}

// sqliteBusyCode is SQLITE_BUSY, the refusal SQLite gives a writer while
// another holds the write lock.
const sqliteBusyCode = 5

// isBusy reports whether err is SQLite's "the database is locked" refusal,
// which a writer is free to retry.
func isBusy(err error) bool {
	var refusal *sqlite.Error
	return errors.As(err, &refusal) && refusal.Code() == sqliteBusyCode
}

// errNoStore says the database has no meta row yet — a file that exists (or
// was just created) but has never held a plan.
var errNoStore = errors.New("plan store is not initialized")

// errNoChange lets a transaction's change function say it decided to write
// nothing after all — a claim that found nothing ready, a root already
// finished — so the transaction rolls back and the store only adopts the
// fresh read.
var errNoChange = errors.New("no change")

// The schema: one row of meta, one row per task, one row per dependency, and
// one row per note and context entry, in insertion order. Columns carry the
// fields the store reasons with and reads back whole; the rarely-read list
// fields — capabilities, resources, context inputs, deliverables, evidence
// requirements, artifacts, evidence — ride as JSON in a single column each.
// The spend table is the ledger's, created empty and written by nothing here.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS meta (
		id      INTEGER PRIMARY KEY CHECK (id = 1),
		project TEXT    NOT NULL,
		root_id TEXT    NOT NULL,
		next_id INTEGER NOT NULL,
		version INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id                    TEXT    PRIMARY KEY,
		ord                   INTEGER NOT NULL,
		title                 TEXT    NOT NULL,
		description           TEXT    NOT NULL,
		kind                  TEXT    NOT NULL,
		parent_id             TEXT    NOT NULL,
		priority              INTEGER NOT NULL,
		effect                TEXT    NOT NULL,
		parallel              TEXT    NOT NULL,
		isolation             TEXT    NOT NULL,
		role                  TEXT    NOT NULL,
		agent                 TEXT    NOT NULL,
		acceptance            TEXT    NOT NULL,
		capabilities          TEXT    NOT NULL,
		resources             TEXT    NOT NULL,
		context_inputs        TEXT    NOT NULL,
		deliverables          TEXT    NOT NULL,
		evidence_requirements TEXT    NOT NULL,
		status                TEXT    NOT NULL,
		composite             INTEGER NOT NULL,
		claimed_by            TEXT    NOT NULL,
		result                TEXT    NOT NULL,
		err                   TEXT    NOT NULL,
		artifacts             TEXT    NOT NULL,
		evidence              TEXT    NOT NULL,
		created_at            TEXT    NOT NULL,
		updated_at            TEXT    NOT NULL,
		completed_at          TEXT    NOT NULL,
		project               TEXT    NOT NULL DEFAULT '',
		chat                  TEXT    NOT NULL DEFAULT '',
		paused                INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS deps (
		downstream TEXT    NOT NULL,
		upstream   TEXT    NOT NULL,
		kind       TEXT    NOT NULL,
		ord        INTEGER NOT NULL,
		PRIMARY KEY (downstream, upstream)
	)`,
	// The archive keeps whole task rows, in the same columns as tasks, plus
	// the moment each was archived. It is a separate table so every read of
	// the live plan — Tasks, ReadySet, Search — stops seeing an archived
	// subtree, while Archived still reads it back whole.
	`CREATE TABLE IF NOT EXISTS archived_tasks (
		id                    TEXT    PRIMARY KEY,
		ord                   INTEGER NOT NULL,
		title                 TEXT    NOT NULL,
		description           TEXT    NOT NULL,
		kind                  TEXT    NOT NULL,
		parent_id             TEXT    NOT NULL,
		priority              INTEGER NOT NULL,
		effect                TEXT    NOT NULL,
		parallel              TEXT    NOT NULL,
		isolation             TEXT    NOT NULL,
		role                  TEXT    NOT NULL,
		agent                 TEXT    NOT NULL,
		acceptance            TEXT    NOT NULL,
		capabilities          TEXT    NOT NULL,
		resources             TEXT    NOT NULL,
		context_inputs        TEXT    NOT NULL,
		deliverables          TEXT    NOT NULL,
		evidence_requirements TEXT    NOT NULL,
		status                TEXT    NOT NULL,
		composite             INTEGER NOT NULL,
		claimed_by            TEXT    NOT NULL,
		result                TEXT    NOT NULL,
		err                   TEXT    NOT NULL,
		artifacts             TEXT    NOT NULL,
		evidence              TEXT    NOT NULL,
		created_at            TEXT    NOT NULL,
		updated_at            TEXT    NOT NULL,
		completed_at          TEXT    NOT NULL,
		project               TEXT    NOT NULL DEFAULT '',
		chat                  TEXT    NOT NULL DEFAULT '',
		paused                INTEGER NOT NULL DEFAULT 0,
		archived_at           TEXT    NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS notes (
		seq     INTEGER PRIMARY KEY,
		id      TEXT NOT NULL,
		task_id TEXT NOT NULL,
		agent   TEXT NOT NULL,
		body    TEXT NOT NULL,
		at      TEXT NOT NULL,
		project TEXT NOT NULL DEFAULT '',
		chat    TEXT NOT NULL DEFAULT '',
		"from"  TEXT NOT NULL DEFAULT 'worker'
	)`,
	`CREATE TABLE IF NOT EXISTS contexts (
		seq        INTEGER PRIMARY KEY,
		id         TEXT NOT NULL,
		task_id    TEXT NOT NULL,
		kind       TEXT NOT NULL,
		content    TEXT NOT NULL,
		created_at TEXT NOT NULL,
		project    TEXT NOT NULL DEFAULT '',
		chat       TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS spend (
		task_id    TEXT    NOT NULL,
		model      TEXT    NOT NULL,
		role       TEXT    NOT NULL,
		usd        REAL    NOT NULL,
		in_tokens  INTEGER NOT NULL,
		out_tokens INTEGER NOT NULL,
		at         TEXT    NOT NULL
	)`,
}

// openDatabase opens (creating when absent) the SQLite database at path, making
// its directory first. THE TWO SETTINGS THAT CARRY THE CONCURRENCY are here:
// WAL journal mode lets a reader run while a writer holds the write lock, and
// the busy timeout makes a second writer wait a few seconds for the first
// rather than failing at once. With BEGIN IMMEDIATE — the transaction mode
// _txlock asks for — those two stand in for the advisory lock the sidecar file
// used to carry, and the sidecar goes away.
func openDatabase(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create plan store directory: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_txlock=immediate&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// ONE CONNECTION, so the pragmas above land on the connection every
	// transaction uses and a transaction never races its own store for the
	// write lock.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open plan store database: %w", err)
	}
	if err := ensureSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// ensureSchema makes sure the database holds the store's tables, entering the
// file into WAL mode as it creates them. THE CHECK IS ONE READ on a store that
// is already built — the common case, every open of an existing plan — so a
// process that opens the store for one verb pays for one query and not for the
// schema, and a store that exists but is empty (a file an interrupted build
// left behind) is built the same way a fresh one is.
func ensureSchema(db *sql.DB) error {
	var present int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'meta'`).Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		if err := enterWAL(db); err != nil {
			return err
		}
		for _, statement := range schemaStatements {
			if _, err := db.Exec(statement); err != nil {
				return fmt.Errorf("create plan store schema: %w", err)
			}
		}
	}
	// THE TAGS ARRIVED AFTER THE FIRST STORES, and the paused and from
	// columns arrived after them. A store built before a column existed still
	// opens: the column is added, and its old rows read back with an honest
	// "made before this change" value — the empty tag, an unpaused task, a
	// worker's note.
	return migrateColumns(db)
}

// migrateColumns adds every column that arrived after the store's first
// schema to the tables that carry it. It runs on a fresh store too, where the
// columns are already there and every step is a no-op.
func migrateColumns(db *sql.DB) error {
	for _, table := range []string{"tasks", "notes", "contexts"} {
		for _, column := range []string{"project", "chat"} {
			if err := ensureColumn(db, table, column, "TEXT", "''"); err != nil {
				return err
			}
		}
	}
	if err := ensureColumn(db, "tasks", "paused", "INTEGER", "0"); err != nil {
		return err
	}
	return ensureColumn(db, "notes", "from", "TEXT", "'worker'")
}

// ensureColumn adds one column when its table predates it and does nothing
// when it is already there. SQLite has no ADD COLUMN IF NOT EXISTS, so the
// check is a read of the table's own description. The column name is quoted,
// because one of them is a SQL keyword.
func ensureColumn(db *sql.DB, table, column, columnType, dflt string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var (
			cid, notNull, primary int
			name, ctype           string
			fallback              sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &fallback, &primary); err != nil {
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN "` + column + `" ` + columnType + ` NOT NULL DEFAULT ` + dflt)
	return err
}

// enterWAL puts a new store in WAL journal mode. The mode is a property of the
// FILE and not of a connection, so this runs once, as the schema is made, and
// later opens inherit it. SQLite does NOT run the busy handler for a
// journal-mode change, so the switch is retried briefly: two processes can
// create the store at once and only one of them changes the mode.
func enterWAL(db *sql.DB) error {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("read plan store journal mode: %w", err)
	}
	for attempt := 0; !strings.EqualFold(mode, "wal"); attempt++ {
		if attempt >= 40 {
			return fmt.Errorf("plan store will not enter WAL journal mode (mode %q)", mode)
		}
		if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			mode = ""
			time.Sleep(25 * time.Millisecond)
		}
	}
	return nil
}

// loadState reads the whole plan out of the database inside tx. Every caller
// reads inside its own transaction, so what it sees is a committed, consistent
// plan and never a half-written one. The store's own invariants are asked of
// the result — the same validateLoadedState the file store asked of its bytes
// — so a database that has been tampered with is refused rather than trusted.
func loadState(tx *sql.Tx) (state, error) {
	var value state
	row := tx.QueryRow(`SELECT project, root_id, next_id, version FROM meta WHERE id = 1`)
	if err := row.Scan(&value.Project, &value.RootID, &value.NextID, &value.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state{}, errNoStore
		}
		return state{}, err
	}
	if value.Version != stateVersion {
		return state{}, fmt.Errorf("unsupported plan store version %d", value.Version)
	}
	if value.RootID == "" || value.Project == "" {
		return state{}, errors.New("invalid plan store")
	}
	tasks, order, err := loadTasks(tx)
	if err != nil {
		return state{}, err
	}
	value.Tasks, value.Order = tasks, order
	if err := loadDeps(tx, value.Tasks); err != nil {
		return state{}, err
	}
	if value.Notes, err = loadNotes(tx); err != nil {
		return state{}, err
	}
	if value.Contexts, err = loadContexts(tx); err != nil {
		return state{}, err
	}
	if err := validateLoadedState(value); err != nil {
		return state{}, fmt.Errorf("validate plan store: %w", err)
	}
	return value, nil
}

// taskColumns is every column a task row carries, in the order scanTask
// reads them. The archive keeps the same columns and adds archived_at, so the
// list lives here and both the loader and the archive reader share it.
const taskColumns = `id, title, description, kind, parent_id, priority, effect,
	parallel, isolation, role, agent, acceptance, capabilities, resources, context_inputs,
	deliverables, evidence_requirements, status, composite, claimed_by, result, err,
	artifacts, evidence, created_at, updated_at, completed_at, project, chat, paused`

// rowQuerier is the read half both the database handle and a transaction
// carry, so the archive reader can share the task scan with the loader
// without opening a transaction of its own.
type rowQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// scanTask reads one row of taskColumns into a Task. Any extra columns the
// caller's SELECT carried — the archive's archived_at — are scanned into the
// extra destinations after the task's own.
func scanTask(row *sql.Rows, extra ...any) (*Task, error) {
	task := &Task{}
	var (
		capabilities, resources, contextInputs, deliverables string
		evidenceRequirements, artifacts, evidence            string
		composite, paused                                    int
		createdAt, updatedAt, completedAt                    string
		effect, parallel, isolation                          string
	)
	dest := []any{
		&task.ID, &task.Title, &task.Description, &task.Kind, &task.ParentID,
		&task.Priority, &effect, &parallel, &isolation, &task.Role, &task.Agent, &task.Acceptance,
		&capabilities, &resources, &contextInputs, &deliverables, &evidenceRequirements,
		&task.Status, &composite, &task.ClaimedBy, &task.Result, &task.Error,
		&artifacts, &evidence, &createdAt, &updatedAt, &completedAt, &task.Project, &task.Chat, &paused,
	}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	task.Effect = Effect(effect)
	task.Parallel, task.Isolation = parallel, isolation
	task.Composite = composite != 0
	task.Paused = paused != 0
	if err := decodeJSON(capabilities, &task.Capabilities); err != nil {
		return nil, err
	}
	if err := decodeJSON(resources, &task.Resources); err != nil {
		return nil, err
	}
	if err := decodeJSON(contextInputs, &task.ContextInputs); err != nil {
		return nil, err
	}
	if err := decodeJSON(deliverables, &task.Deliverables); err != nil {
		return nil, err
	}
	if err := decodeJSON(evidenceRequirements, &task.EvidenceRequirements); err != nil {
		return nil, err
	}
	if err := decodeJSON(artifacts, &task.Artifacts); err != nil {
		return nil, err
	}
	if err := decodeJSON(evidence, &task.Evidence); err != nil {
		return nil, err
	}
	var err error
	if task.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if task.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if task.CompletedAt, err = parseTime(completedAt); err != nil {
		return nil, err
	}
	return task, nil
}

func loadTasks(tx *sql.Tx) (map[string]*Task, []string, error) {
	rows, err := tx.Query(`SELECT ` + taskColumns + ` FROM tasks ORDER BY ord`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tasks := map[string]*Task{}
	var order []string
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, nil, err
		}
		tasks[task.ID] = task
		order = append(order, task.ID)
	}
	return tasks, order, rows.Err()
}

// loadArchived reads the archive back as whole tasks, in admission order,
// each carrying the moment it was archived.
func loadArchived(q rowQuerier) ([]*Task, error) {
	rows, err := q.Query(`SELECT ` + taskColumns + `, archived_at FROM archived_tasks ORDER BY ord`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var archived []*Task
	for rows.Next() {
		var at string
		task, err := scanTask(rows, &at)
		if err != nil {
			return nil, err
		}
		if task.ArchivedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		archived = append(archived, task)
	}
	return archived, rows.Err()
}

// insertArchived writes a task selection into the archive, stamped with the
// moment it was archived. It runs inside the archiving transaction, so the
// move out of the live tables and the copy into the archive commit together.
func insertArchived(tx *sql.Tx, tasks []*Task, now time.Time) error {
	statement, err := tx.Prepare(`INSERT INTO archived_tasks (
		id, ord, title, description, kind, parent_id, priority, effect, parallel, isolation,
		role, agent, acceptance, capabilities, resources, context_inputs, deliverables,
		evidence_requirements, status, composite, claimed_by, result, err, artifacts, evidence,
		created_at, updated_at, completed_at, project, chat, paused, archived_at) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for ord, task := range tasks {
		capabilities, err := encodeJSON(task.Capabilities)
		if err != nil {
			return err
		}
		resources, err := encodeJSON(task.Resources)
		if err != nil {
			return err
		}
		contextInputs, err := encodeJSON(task.ContextInputs)
		if err != nil {
			return err
		}
		deliverables, err := encodeJSON(task.Deliverables)
		if err != nil {
			return err
		}
		evidenceRequirements, err := encodeJSON(task.EvidenceRequirements)
		if err != nil {
			return err
		}
		artifacts, err := encodeJSON(task.Artifacts)
		if err != nil {
			return err
		}
		evidence, err := encodeJSON(task.Evidence)
		if err != nil {
			return err
		}
		if _, err := statement.Exec(task.ID, ord, task.Title, task.Description, task.Kind,
			task.ParentID, task.Priority, string(task.Effect), task.Parallel, task.Isolation,
			task.Role, task.Agent, task.Acceptance, capabilities, resources, contextInputs, deliverables,
			evidenceRequirements, string(task.Status), boolInt(task.Composite), task.ClaimedBy,
			task.Result, task.Error, artifacts, evidence, formatTime(task.CreatedAt),
			formatTime(task.UpdatedAt), formatTime(task.CompletedAt), task.Project, task.Chat,
			boolInt(task.Paused), formatTime(now)); err != nil {
			return err
		}
	}
	return nil
}

func loadDeps(tx *sql.Tx, tasks map[string]*Task) error {
	rows, err := tx.Query(`SELECT downstream, upstream, kind FROM deps ORDER BY downstream, ord`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var downstream string
		var dep Dependency
		var kind string
		if err := rows.Scan(&downstream, &dep.TaskID, &kind); err != nil {
			return err
		}
		dep.Kind = DepKind(kind)
		if task := tasks[downstream]; task != nil {
			task.Dependencies = append(task.Dependencies, dep)
		}
	}
	return rows.Err()
}

func loadNotes(tx *sql.Tx) ([]Note, error) {
	rows, err := tx.Query(`SELECT id, task_id, agent, body, at, project, chat, "from" FROM notes ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []Note
	for rows.Next() {
		var note Note
		var at, from string
		if err := rows.Scan(&note.ID, &note.TaskID, &note.Agent, &note.Body, &at, &note.Project, &note.Chat, &from); err != nil {
			return nil, err
		}
		if note.At, err = parseTime(at); err != nil {
			return nil, err
		}
		if from == "" {
			from = NoteFromWorker
		}
		note.From = from
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

func loadContexts(tx *sql.Tx) ([]ContextEntry, error) {
	rows, err := tx.Query(`SELECT id, task_id, kind, content, created_at, project, chat FROM contexts ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ContextEntry
	for rows.Next() {
		var entry ContextEntry
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.Kind, &entry.Content, &createdAt, &entry.Project, &entry.Chat); err != nil {
			return nil, err
		}
		if entry.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// saveState writes the whole plan into the database inside tx. The changed
// tables are rewritten rather than diffed row by row: at this size that is one
// prepared insert per row and nothing to get wrong, and the transaction is what
// makes the rewrite atomic — a reader sees the old plan or the new one, never a
// half-written one, which is the property the file's temp-and-rename used to
// give.
func saveState(tx *sql.Tx, value state) error {
	for _, table := range []string{"meta", "tasks", "deps", "notes", "contexts"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta (id, project, root_id, next_id, version) VALUES (1, ?, ?, ?, ?)`,
		value.Project, value.RootID, value.NextID, value.Version); err != nil {
		return err
	}
	if err := saveTasks(tx, value); err != nil {
		return err
	}
	if err := saveDeps(tx, value); err != nil {
		return err
	}
	if err := saveNotes(tx, value.Notes); err != nil {
		return err
	}
	return saveContexts(tx, value.Contexts)
}

func saveTasks(tx *sql.Tx, value state) error {
	statement, err := tx.Prepare(`INSERT INTO tasks (
		id, ord, title, description, kind, parent_id, priority, effect, parallel, isolation,
		role, agent, acceptance, capabilities, resources, context_inputs, deliverables,
		evidence_requirements, status, composite, claimed_by, result, err, artifacts, evidence,
		created_at, updated_at, completed_at, project, chat, paused) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for ord, id := range value.Order {
		task := value.Tasks[id]
		columns, err := encodeJSON(task.Capabilities)
		if err != nil {
			return err
		}
		resources, err := encodeJSON(task.Resources)
		if err != nil {
			return err
		}
		contextInputs, err := encodeJSON(task.ContextInputs)
		if err != nil {
			return err
		}
		deliverables, err := encodeJSON(task.Deliverables)
		if err != nil {
			return err
		}
		evidenceRequirements, err := encodeJSON(task.EvidenceRequirements)
		if err != nil {
			return err
		}
		artifacts, err := encodeJSON(task.Artifacts)
		if err != nil {
			return err
		}
		evidence, err := encodeJSON(task.Evidence)
		if err != nil {
			return err
		}
		if _, err := statement.Exec(task.ID, ord, task.Title, task.Description, task.Kind,
			task.ParentID, task.Priority, string(task.Effect), task.Parallel, task.Isolation,
			task.Role, task.Agent, task.Acceptance, columns, resources, contextInputs, deliverables,
			evidenceRequirements, string(task.Status), boolInt(task.Composite), task.ClaimedBy,
			task.Result, task.Error, artifacts, evidence, formatTime(task.CreatedAt),
			formatTime(task.UpdatedAt), formatTime(task.CompletedAt), task.Project, task.Chat, boolInt(task.Paused)); err != nil {
			return err
		}
	}
	return nil
}

func saveDeps(tx *sql.Tx, value state) error {
	statement, err := tx.Prepare(`INSERT INTO deps (downstream, upstream, kind, ord) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, id := range value.Order {
		for ord, dep := range value.Tasks[id].Dependencies {
			if _, err := statement.Exec(id, dep.TaskID, string(dep.Kind), ord); err != nil {
				return err
			}
		}
	}
	return nil
}

func saveNotes(tx *sql.Tx, notes []Note) error {
	statement, err := tx.Prepare(`INSERT INTO notes (seq, id, task_id, agent, body, at, project, chat, "from") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for seq, note := range notes {
		from := note.From
		if from == "" {
			from = NoteFromWorker
		}
		if _, err := statement.Exec(seq, note.ID, note.TaskID, note.Agent, note.Body, formatTime(note.At), note.Project, note.Chat, from); err != nil {
			return err
		}
	}
	return nil
}

func saveContexts(tx *sql.Tx, entries []ContextEntry) error {
	statement, err := tx.Prepare(`INSERT INTO contexts (seq, id, task_id, kind, content, created_at, project, chat) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for seq, entry := range entries {
		if _, err := statement.Exec(seq, entry.ID, entry.TaskID, entry.Kind, entry.Content, formatTime(entry.CreatedAt), entry.Project, entry.Chat); err != nil {
			return err
		}
	}
	return nil
}

// encodeJSON packs a rarely-read list field into its column. A nil field packs
// to "null", which decodes back to nil.
func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// decodeJSON unpacks a column encodeJSON wrote. An empty column is a nil field.
func decodeJSON(text string, out any) error {
	if text == "" {
		return nil
	}
	return json.Unmarshal([]byte(text), out)
}

// formatTime writes a timestamp as RFC3339Nano in UTC; the zero time writes as
// an empty column so it reads back as the zero time and not as year one.
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(text string) (time.Time, error) {
	if text == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid stored timestamp %q: %w", text, err)
	}
	return value, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func cloneState(source state) state {
	copyState := source
	copyState.Order = append([]string(nil), source.Order...)
	copyState.Notes = append([]Note(nil), source.Notes...)
	copyState.Contexts = append([]ContextEntry(nil), source.Contexts...)
	copyState.Tasks = make(map[string]*Task, len(source.Tasks))
	for id, task := range source.Tasks {
		copyTask := *task
		copyTask.Dependencies = append([]Dependency(nil), task.Dependencies...)
		copyTask.Capabilities = append([]string(nil), task.Capabilities...)
		copyTask.Resources = append([]ResourceClaim(nil), task.Resources...)
		copyTask.ContextInputs = append([]string(nil), task.ContextInputs...)
		copyTask.Deliverables = append([]string(nil), task.Deliverables...)
		copyTask.EvidenceRequirements = append([]string(nil), task.EvidenceRequirements...)
		copyTask.Artifacts = append([]string(nil), task.Artifacts...)
		copyTask.Evidence = append([]string(nil), task.Evidence...)
		copyState.Tasks[id] = &copyTask
	}
	return copyState
}

func validateLoadedState(value state) error {
	root := value.Tasks[value.RootID]
	if root == nil || root.ParentID != "" {
		return errors.New("root task is missing or has a parent")
	}
	seen := make(map[string]bool, len(value.Order))
	for _, id := range value.Order {
		if seen[id] || value.Tasks[id] == nil {
			return fmt.Errorf("invalid task order entry %q", id)
		}
		seen[id] = true
	}
	if len(seen) != len(value.Tasks) {
		return errors.New("task order does not cover every task")
	}
	for id, task := range value.Tasks {
		if id != task.ID {
			return fmt.Errorf("task map key %q does not match id %q", id, task.ID)
		}
		if !validStatus(task.Status) {
			return fmt.Errorf("task %q has invalid status %q", id, task.Status)
		}
		if id != value.RootID {
			if err := validateSpec(task.TaskSpec); err != nil {
				return fmt.Errorf("task %q: %w", id, err)
			}
			if value.Tasks[task.ParentID] == nil {
				return fmt.Errorf("task %q has unknown parent %q", id, task.ParentID)
			}
		}
		for _, dep := range task.Dependencies {
			if value.Tasks[dep.TaskID] == nil {
				return fmt.Errorf("task %q has unknown dependency %q", id, dep.TaskID)
			}
		}
	}
	if err := validateGraphs(value); err != nil {
		return err
	}
	for id, task := range value.Tasks {
		hasChild := false
		for _, candidate := range value.Tasks {
			if candidate.ParentID == id {
				hasChild = true
				break
			}
		}
		if task.Composite != hasChild {
			return fmt.Errorf("task %q has inconsistent composite flag", id)
		}
	}
	return nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusPending, StatusReady, StatusClaimed, StatusRunning, StatusDone, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}
