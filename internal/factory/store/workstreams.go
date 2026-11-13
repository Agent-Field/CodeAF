package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/sqlitefile"

	_ "modernc.org/sqlite"
)

// ── AN ORGANIZATION'S WORKSTREAMS, KEPT ────────────────────────────────────
//
// Every organization workstream ([factory.OrgWorkstream]) lives in ONE SQLite
// database beside the go-ahead policies, WorkstreamsDB. It used to be one JSON
// document rewritten whole on every save; a save now writes only the rows it
// changed, inside a transaction, under the same cross-process lock.
//
//   - workstreams: one row per workstream, the workstream as JSON.
//   - page_versions: one row per saved version of one page.
//   - parts: a workstream's plan, wakes and suggestions, each a desk-shaped
//     JSON object the server keeps with its own rev, author and time. The
//     server does not interpret them: their shape is the desk's.
//   - spend: one row per spend someone posted against a workstream.
//
// THE OLD DOCUMENT IS MIGRATED ONCE. A server that still has WorkstreamsFile
// and no database copies it in on first use and renames the file aside, so a
// later start never reads it twice.

// WorkstreamsDB is the database. Neither it nor the files SQLite keeps beside
// it are numbers, so [Store.List] never reads them as jobs.
const WorkstreamsDB = "factory-workstreams.db"

// WorkstreamsFile is the JSON document workstreams were kept in before the
// database, read once by the migration.
const WorkstreamsFile = "factory-workstreams.json"

// workstreamsLock serializes every change to the workstreams across
// processes, as the policies' lock does for theirs.
const workstreamsLock = "factory-workstreams.lock"

// IsWorkstreamsFile is whether name is one of the files the workstreams keep
// under the root: the database, its journal, the lock or the migrated JSON.
// None is read by a look at the floor.
func IsWorkstreamsFile(name string) bool {
	return strings.HasPrefix(name, "factory-workstreams.")
}

// The parts kept beside a workstream.
const (
	PartPlan        = "plan"
	PartWakes       = "wakes"
	PartSuggestions = "suggestions"
)

// ErrPartStale is a part write whose rev is not the one kept.
var ErrPartStale = errors.New("this changed since you read it; read it again, then make the change")

// ErrPartGone is a part that is not kept.
var ErrPartGone = errors.New("not found")

const workstreamsSchema = `
CREATE TABLE IF NOT EXISTS workstreams (
	id  TEXT PRIMARY KEY,
	rev INTEGER NOT NULL,
	doc TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS page_versions (
	ws            TEXT NOT NULL,
	page          TEXT NOT NULL,
	version       INTEGER NOT NULL,
	at            TEXT NOT NULL,
	by            TEXT NOT NULL,
	title         TEXT NOT NULL,
	reason        TEXT NOT NULL,
	restored_from INTEGER NOT NULL DEFAULT 0,
	body_md       TEXT NOT NULL,
	state         BLOB,
	PRIMARY KEY (ws, page, version)
);
CREATE TABLE IF NOT EXISTS parts (
	ws         TEXT NOT NULL,
	part       TEXT NOT NULL,
	id         TEXT NOT NULL,
	rev        INTEGER NOT NULL,
	at         TEXT NOT NULL,
	by         TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	doc        TEXT NOT NULL,
	PRIMARY KEY (ws, part, id)
);
CREATE TABLE IF NOT EXISTS spend (
	seq  INTEGER PRIMARY KEY AUTOINCREMENT,
	ws   TEXT NOT NULL,
	at   TEXT NOT NULL,
	by   TEXT NOT NULL,
	usd  REAL NOT NULL,
	job  INTEGER NOT NULL DEFAULT 0,
	chat TEXT NOT NULL DEFAULT '',
	key  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS spend_ws ON spend (ws);
CREATE UNIQUE INDEX IF NOT EXISTS spend_key ON spend (ws, key) WHERE key <> '';
`

// workstreamDBs holds one open database per root for the life of the process:
// a store is opened again and again over one root, and each open must not
// leave another connection behind.
var workstreamDBs = struct {
	sync.Mutex
	open map[string]*sql.DB
}{open: map[string]*sql.DB{}}

// wsDB is the root's database, opened (and migrated) on first use. create is
// false for a read, which answers nil, with no error, when the server never
// kept a workstream: a read must not leave an empty database behind.
func (st *Store) wsDB(create bool) (*sql.DB, error) {
	path := filepath.Join(st.root, WorkstreamsDB)
	workstreamDBs.Lock()
	defer workstreamDBs.Unlock()
	if db := workstreamDBs.open[path]; db != nil {
		if _, err := os.Stat(path); err == nil {
			return db, nil
		}
		// The folder was removed under a long-lived process (a test's
		// temporary root reused by name); open it fresh.
		_ = db.Close()
		delete(workstreamDBs.open, path)
	}
	_, statErr := os.Stat(path)
	_, jsonErr := os.Stat(filepath.Join(st.root, WorkstreamsFile))
	if errors.Is(statErr, os.ErrNotExist) && errors.Is(jsonErr, os.ErrNotExist) && !create {
		return nil, nil
	}
	if errors.Is(statErr, os.ErrNotExist) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
	}
	q := make(url.Values)
	q.Set("mode", "rw")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", sqlitefile.URI(path, q.Encode()))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(workstreamsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("the organization's workstreams cannot be opened: %w", err)
	}
	if err := st.migrateWorkstreams(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	workstreamDBs.open[path] = db
	return db, nil
}

// migrateWorkstreams copies WorkstreamsFile into an empty database once, then
// renames the file aside. A file that cannot be read stops the open rather
// than starting the organization with no workstreams.
func (st *Store) migrateWorkstreams(db *sql.DB) error {
	old := filepath.Join(st.root, WorkstreamsFile)
	return st.underLock(filepath.Join(st.root, workstreamsLock), func() error {
		raw, err := os.ReadFile(old)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		var all factory.OrgWorkstreams
		if err := json.Unmarshal(raw, &all); err != nil {
			return errors.New("the organization's workstreams cannot be read")
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM workstreams`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			for id, w := range all {
				w.ID = id
				if err := putWorkstream(tx, normalizeWorkstream(w)); err != nil {
					return err
				}
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return os.Rename(old, old+".migrated")
	})
}

// normalizeWorkstream fills what an older save left out, so every reader sees
// lists rather than null and every page a rev, and folds the teams-only
// shares kept before roles into role shares ([factory.OrgWorkstream.FoldShares]).
func normalizeWorkstream(w factory.OrgWorkstream) factory.OrgWorkstream {
	w.FoldShares()
	for i := range w.Pages {
		if w.Pages[i].Rev < 1 {
			w.Pages[i].Rev = 1
		}
	}
	if w.Jobs == nil {
		w.Jobs = []int{}
	}
	return w
}

func putWorkstream(tx *sql.Tx, w factory.OrgWorkstream) error {
	doc, err := json.Marshal(w)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO workstreams (id, rev, doc) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET rev = excluded.rev, doc = excluded.doc`, w.ID, w.Rev, string(doc))
	return err
}

func readWorkstreams(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (factory.OrgWorkstreams, error) {
	rows, err := q.Query(`SELECT id, doc FROM workstreams`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := factory.OrgWorkstreams{}
	for rows.Next() {
		var id, doc string
		if err := rows.Scan(&id, &doc); err != nil {
			return nil, err
		}
		var w factory.OrgWorkstream
		if err := json.Unmarshal([]byte(doc), &w); err != nil {
			return nil, errors.New("the organization's workstreams cannot be read")
		}
		w.ID = id
		out[id] = normalizeWorkstream(w)
	}
	return out, rows.Err()
}

// Workstreams are every organization workstream this server keeps, archived
// ones included. A server that was never given one answers none.
//
// A DATABASE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET: answering none
// would let the next save act on an organization with no workstreams.
func (st *Store) Workstreams() (factory.OrgWorkstreams, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return factory.OrgWorkstreams{}, err
	}
	return readWorkstreams(db)
}

// Workstream is one workstream, and false when the server keeps none by id.
func (st *Store) Workstream(id string) (factory.OrgWorkstream, bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return factory.OrgWorkstream{}, false, err
	}
	var doc string
	err = db.QueryRow(`SELECT doc FROM workstreams WHERE id = ?`, id).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return factory.OrgWorkstream{}, false, nil
	}
	if err != nil {
		return factory.OrgWorkstream{}, false, err
	}
	var w factory.OrgWorkstream
	if err := json.Unmarshal([]byte(doc), &w); err != nil {
		return factory.OrgWorkstream{}, false, errors.New("the organization's workstreams cannot be read")
	}
	w.ID = id
	return normalizeWorkstream(w), true, nil
}

// wsTx runs write in one transaction under the cross-process lock.
func (st *Store) wsTx(write func(tx *sql.Tx) error) error {
	db, err := st.wsDB(true)
	if err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, workstreamsLock), func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err := write(tx); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// EditWorkstreams changes any number of workstreams in one save, under a
// cross-process lock, and answers every workstream as saved. edit sees them
// all, may add, change or delete entries, and names the ids it changed: each
// of those still present has its rev counted up by one, so a single save never
// moves a workstream it did not touch. An error from edit writes nothing.
//
// ONE SAVE FOR SEVERAL WORKSTREAMS is what lets a share that includes the
// linked ones land whole or not at all. A workstream edit deletes is removed
// with every page version, part and spend kept for it.
func (st *Store) EditWorkstreams(edit func(all factory.OrgWorkstreams) (changed []string, err error)) (factory.OrgWorkstreams, error) {
	return st.editWorkstreams(true, edit)
}

// MirrorWorkstreams is EditWorkstreams WITHOUT counting any rev up. It is for
// a change no writer guards with a rev: the plain-text mirror of a page body
// whose truth is the live document, refreshed when a version is saved. Counting
// the rev for it would refuse every other writer every thirty seconds while
// anyone types.
func (st *Store) MirrorWorkstreams(edit func(all factory.OrgWorkstreams) (changed []string, err error)) (factory.OrgWorkstreams, error) {
	return st.editWorkstreams(false, edit)
}

func (st *Store) editWorkstreams(bump bool, edit func(all factory.OrgWorkstreams) ([]string, error)) (factory.OrgWorkstreams, error) {
	var out factory.OrgWorkstreams
	err := st.wsTx(func(tx *sql.Tx) error {
		all, err := readWorkstreams(tx)
		if err != nil {
			return err
		}
		before := make(map[string]int, len(all))
		for id, w := range all {
			before[id] = w.Rev
		}
		changed, err := edit(all)
		if err != nil {
			return err
		}
		out = all
		for _, id := range changed {
			w, ok := all[id]
			if !ok {
				for _, q := range []string{
					`DELETE FROM workstreams WHERE id = ?`, `DELETE FROM page_versions WHERE ws = ?`,
					`DELETE FROM parts WHERE ws = ?`, `DELETE FROM spend WHERE ws = ?`,
				} {
					if _, err := tx.Exec(q, id); err != nil {
						return err
					}
				}
				continue
			}
			w.ID = id
			if bump || before[id] == 0 {
				w.Rev = before[id] + 1
			} else {
				w.Rev = before[id]
			}
			all[id] = normalizeWorkstream(w)
			if err := putWorkstream(tx, all[id]); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// TeamOwnsWorkstream is whether team owns any workstream, archived ones
// included: a team that owns one cannot be deleted, because a workstream with
// no owner has nobody to carry its budget or approve its go-aheads.
func (st *Store) TeamOwnsWorkstream(team string) (bool, error) {
	all, err := st.Workstreams()
	if err != nil {
		return false, err
	}
	for _, w := range all {
		if w.Owner.Kind == "team" && w.Owner.ID == team {
			return true, nil
		}
	}
	return false, nil
}

// WorkstreamOfJob is the workstream job belongs to by membership, and false
// when it belongs to none.
func (st *Store) WorkstreamOfJob(job int) (string, bool, error) {
	all, err := st.Workstreams()
	if err != nil {
		return "", false, err
	}
	for id, w := range all {
		for _, j := range w.Jobs {
			if j == job {
				return id, true, nil
			}
		}
	}
	return "", false, nil
}

// SortWorkstreams orders ws newest change first, the id breaking a tie so the
// order never wobbles between two reads.
func SortWorkstreams(ws []factory.OrgWorkstream) {
	sort.Slice(ws, func(i, j int) bool {
		if !ws[i].UpdatedAt.Equal(ws[j].UpdatedAt) {
			return ws[i].UpdatedAt.After(ws[j].UpdatedAt)
		}
		return ws[i].ID > ws[j].ID
	})
}

// ── page versions ──

// PageVersion is one saved version of one page. Body and State are left out
// of a list; State is the live document's encoded state, kept as the client
// sent it and never read here.
type PageVersion struct {
	Version      int    `json:"version"`
	At           string `json:"at"`
	By           string `json:"by"`
	Title        string `json:"title"`
	Reason       string `json:"reason"`
	RestoredFrom int    `json:"restored_from,omitempty"`
	Bytes        int    `json:"bytes"`
	BodyMD       string `json:"body_md,omitempty"`
	State        []byte `json:"state,omitempty"`
}

// pageVersionsKept is how many versions one page keeps; the oldest beyond it
// are let go when a new one is saved.
const pageVersionsKept = 200

// AddPageVersion saves v as the page's next version, stamping its number. A
// version whose words and state are the same as the latest is not saved again:
// the latest is answered and added is false.
func (st *Store) AddPageVersion(ws, page string, v PageVersion) (saved PageVersion, added bool, err error) {
	err = st.wsTx(func(tx *sql.Tx) error {
		var last PageVersion
		var state []byte
		row := tx.QueryRow(`SELECT version, at, by, title, reason, restored_from, body_md, state FROM page_versions
			WHERE ws = ? AND page = ? ORDER BY version DESC LIMIT 1`, ws, page)
		switch err := row.Scan(&last.Version, &last.At, &last.By, &last.Title, &last.Reason, &last.RestoredFrom, &last.BodyMD, &state); {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		default:
			last.State, last.Bytes = state, len(last.BodyMD)
			if v.RestoredFrom == 0 && last.BodyMD == v.BodyMD && last.Title == v.Title && bytes.Equal(state, v.State) {
				saved = last
				return nil
			}
		}
		v.Version, v.Bytes = last.Version+1, len(v.BodyMD)
		if _, err := tx.Exec(`INSERT INTO page_versions (ws, page, version, at, by, title, reason, restored_from, body_md, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, ws, page, v.Version, v.At, v.By, v.Title, v.Reason, v.RestoredFrom, v.BodyMD, v.State); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM page_versions WHERE ws = ? AND page = ? AND version <= ?`, ws, page, v.Version-pageVersionsKept); err != nil {
			return err
		}
		saved, added = v, true
		return nil
	})
	return saved, added, err
}

// PageVersions are the page's versions, newest first, without words or state.
func (st *Store) PageVersions(ws, page string) ([]PageVersion, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []PageVersion{}, err
	}
	rows, err := db.Query(`SELECT version, at, by, title, reason, restored_from, length(CAST(body_md AS BLOB)) FROM page_versions
		WHERE ws = ? AND page = ? ORDER BY version DESC`, ws, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PageVersion{}
	for rows.Next() {
		var v PageVersion
		if err := rows.Scan(&v.Version, &v.At, &v.By, &v.Title, &v.Reason, &v.RestoredFrom, &v.Bytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PageVersionAt is one version whole, and false when it is not kept.
func (st *Store) PageVersionAt(ws, page string, version int) (PageVersion, bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return PageVersion{}, false, err
	}
	var v PageVersion
	err = db.QueryRow(`SELECT version, at, by, title, reason, restored_from, body_md, state FROM page_versions
		WHERE ws = ? AND page = ? AND version = ?`, ws, page, version).
		Scan(&v.Version, &v.At, &v.By, &v.Title, &v.Reason, &v.RestoredFrom, &v.BodyMD, &v.State)
	if errors.Is(err, sql.ErrNoRows) {
		return v, false, nil
	}
	v.Bytes = len(v.BodyMD)
	return v, err == nil, err
}

// DropPageVersions lets go of every version of one page, when the page is
// deleted.
func (st *Store) DropPageVersions(ws, page string) error {
	return st.wsTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM page_versions WHERE ws = ? AND page = ?`, ws, page)
		return err
	})
}

// ── parts: plan, wakes, suggestions ──

// Part is one plan, wake or suggestion: the desk's object, with the server's
// id, rev, author and times.
type Part struct {
	ID        string          `json:"id"`
	Rev       int             `json:"rev"`
	At        string          `json:"at"`
	By        string          `json:"by"`
	UpdatedAt string          `json:"updated_at"`
	UpdatedBy string          `json:"updated_by"`
	Doc       json.RawMessage `json:"-"`
}

// Parts are every part of one kind kept for ws, oldest first.
func (st *Store) Parts(ws, part string) ([]Part, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []Part{}, err
	}
	rows, err := db.Query(`SELECT id, rev, at, by, updated_at, updated_by, doc FROM parts
		WHERE ws = ? AND part = ? ORDER BY at, id`, ws, part)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Part{}
	for rows.Next() {
		var p Part
		var doc string
		if err := rows.Scan(&p.ID, &p.Rev, &p.At, &p.By, &p.UpdatedAt, &p.UpdatedBy, &doc); err != nil {
			return nil, err
		}
		p.Doc = json.RawMessage(doc)
		out = append(out, p)
	}
	return out, rows.Err()
}

// PartAt is one part, and false when it is not kept.
func (st *Store) PartAt(ws, part, id string) (Part, bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return Part{}, false, err
	}
	return partAt(db, ws, part, id)
}

func partAt(q interface {
	QueryRow(string, ...any) *sql.Row
}, ws, part, id string) (Part, bool, error) {
	var p Part
	var doc string
	err := q.QueryRow(`SELECT id, rev, at, by, updated_at, updated_by, doc FROM parts WHERE ws = ? AND part = ? AND id = ?`, ws, part, id).
		Scan(&p.ID, &p.Rev, &p.At, &p.By, &p.UpdatedAt, &p.UpdatedBy, &doc)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	p.Doc = json.RawMessage(doc)
	return p, err == nil, err
}

// PutPart keeps doc as the part id. rev is the rev the writer read: 0 makes a
// new part (refused when one is kept), anything else must be the rev kept.
// The rev counts up by one; who and now are the server's stamp.
func (st *Store) PutPart(ws, part, id string, rev int, who string, now time.Time, doc json.RawMessage) (Part, error) {
	var out Part
	err := st.wsTx(func(tx *sql.Tx) error {
		cur, ok, err := partAt(tx, ws, part, id)
		if err != nil {
			return err
		}
		stamp := now.UTC().Format(time.RFC3339Nano)
		switch {
		case !ok && rev != 0:
			return ErrPartGone
		case ok && cur.Rev != rev:
			return ErrPartStale
		case !ok:
			cur = Part{ID: id, At: stamp, By: who}
		}
		cur.Rev, cur.UpdatedAt, cur.UpdatedBy, cur.Doc = cur.Rev+1, stamp, who, doc
		_, err = tx.Exec(`INSERT INTO parts (ws, part, id, rev, at, by, updated_at, updated_by, doc) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (ws, part, id) DO UPDATE SET rev = excluded.rev, updated_at = excluded.updated_at,
			updated_by = excluded.updated_by, doc = excluded.doc`,
			ws, part, id, cur.Rev, cur.At, cur.By, cur.UpdatedAt, cur.UpdatedBy, string(doc))
		out = cur
		return err
	})
	return out, err
}

// ── spend ──

// Spend is one spend someone posted against a workstream.
type Spend struct {
	At   string
	By   string
	USD  float64
	Job  int
	Chat string
	// Key is the poster's idempotency key: the same key posted twice is kept
	// once.
	Key string
}

// AddSpend keeps s against ws, once per key, and answers the workstream's
// total spend.
func (st *Store) AddSpend(ws string, s Spend) (float64, error) {
	var total float64
	err := st.wsTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO spend (ws, at, by, usd, job, chat, key) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, ws, s.At, s.By, s.USD, s.Job, s.Chat, s.Key); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT coalesce(sum(usd), 0) FROM spend WHERE ws = ?`, ws).Scan(&total)
	})
	return total, err
}

// WorkstreamSpend is what every workstream has spent, by id; one with none
// posted is absent.
func (st *Store) WorkstreamSpend() (map[string]float64, error) {
	out := map[string]float64{}
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return out, err
	}
	rows, err := db.Query(`SELECT ws, sum(usd) FROM spend GROUP BY ws`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var usd float64
		if err := rows.Scan(&id, &usd); err != nil {
			return nil, err
		}
		out[id] = usd
	}
	return out, rows.Err()
}
