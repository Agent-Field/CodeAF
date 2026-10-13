package runs

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// dbConn is one app's journal. Max one connection: modernc's own lock is the
// concurrency story, and the engine already serializes writers.
type dbConn struct {
	db *sql.DB
}

const journalSchema = `
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    seq INTEGER NOT NULL,
    status TEXT NOT NULL,
    started_at TEXT NOT NULL,
    body TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY,
    state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    body TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS fires (
    key TEXT PRIMARY KEY,
    at TEXT NOT NULL
);
`

func (e *engine) db(app string) (*sql.DB, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dbLocked(app)
}

func (e *engine) dbLocked(app string) (*sql.DB, error) {
	if conn, ok := e.dbs[app]; ok {
		return conn.db, nil
	}
	path := filepath.Join(e.root, app, "runs", "journal.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(journalSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	e.dbs[app] = &dbConn{db: db}
	return db, nil
}

func (e *engine) saveJob(job *job) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	db, err := e.dbLocked(job.State.App)
	if err != nil {
		return err
	}
	if job.Seq == 0 {
		if err := db.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM jobs`).Scan(&job.Seq); err != nil {
			return err
		}
	}
	_, err = db.Exec(`INSERT INTO jobs (id, seq, status, started_at, body) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status = excluded.status, body = excluded.body`,
		job.State.ID, job.Seq, string(job.State.Status), job.State.StartedAt, string(raw))
	return err
}

func (e *engine) loadJob(app, id string) (*job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	db, err := e.dbLocked(app)
	if err != nil {
		return nil, err
	}
	var body string
	err = db.QueryRow(`SELECT body FROM jobs WHERE id = ?`, id).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeJob(body)
}

func (e *engine) loadJobs(app string) ([]*job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := os.Stat(filepath.Join(e.root, app, "runs", "journal.db")); err != nil {
		return nil, nil
	}
	db, err := e.dbLocked(app)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT body FROM jobs ORDER BY seq DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*job
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		job, err := decodeJob(body)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func decodeJob(body string) (*job, error) {
	var job job
	if err := json.Unmarshal([]byte(body), &job); err != nil {
		return nil, err
	}
	if job.Env == nil {
		job.Env = map[string]any{}
	}
	if job.Done == nil {
		job.Done = map[string]stepRec{}
	}
	if job.Loops == nil {
		job.Loops = map[string]loopRec{}
	}
	if job.Attempts == nil {
		job.Attempts = map[string]int{}
	}
	return &job, nil
}

func (e *engine) saveApproval(a bridge.Approval) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	db, err := e.dbLocked(a.App)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO approvals (id, state, created_at, body) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, body = excluded.body`,
		a.ID, string(a.State), a.CreatedAt, string(raw))
	return err
}

func (e *engine) loadApproval(app, id string) (bridge.Approval, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	db, err := e.dbLocked(app)
	if err != nil {
		return bridge.Approval{}, false, err
	}
	var body string
	err = db.QueryRow(`SELECT body FROM approvals WHERE id = ?`, id).Scan(&body)
	if err == sql.ErrNoRows {
		return bridge.Approval{}, false, nil
	}
	if err != nil {
		return bridge.Approval{}, false, err
	}
	var a bridge.Approval
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return bridge.Approval{}, false, err
	}
	return a, true, nil
}

func (e *engine) loadApprovals(app string) ([]bridge.Approval, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := os.Stat(filepath.Join(e.root, app, "runs", "journal.db")); err != nil {
		return nil, nil
	}
	db, err := e.dbLocked(app)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT body FROM approvals ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bridge.Approval
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var a bridge.Approval
		if err := json.Unmarshal([]byte(body), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (e *engine) rememberFire(key string, at time.Time) {
	e.mu.Lock()
	e.lastFire[key] = at
	app, _, _ := splitFireKey(key)
	db, err := e.dbLocked(app)
	e.mu.Unlock()
	if err != nil || db == nil {
		return
	}
	_, _ = db.Exec(`INSERT INTO fires (key, at) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET at = excluded.at`, key, at.UTC().Format(time.RFC3339Nano))
}

func splitFireKey(key string) (app, trigger string, ok bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}
