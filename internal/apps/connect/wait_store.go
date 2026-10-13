package connect

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	_ "modernc.org/sqlite"
)

type waitStore struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
}

func newWaitStore(root string) *waitStore {
	return &waitStore{path: filepath.Join(filepath.Dir(root), "connect-waits.db")}
}
func (s *waitStore) open() (*sql.DB, error) {
	if s.db != nil {
		return s.db, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: s.path}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS waits (app TEXT NOT NULL,run_id TEXT NOT NULL,step TEXT NOT NULL,service TEXT NOT NULL,member TEXT NOT NULL,body TEXT NOT NULL,PRIMARY KEY(app,run_id,step,service,member))`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s.db = db
	return db, nil
}
func (s *waitStore) put(ctx context.Context, w bridge.ConnectorWait) (bridge.ConnectorWait, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.open()
	if err != nil {
		return w, false, err
	}
	raw, err := json.Marshal(w)
	if err != nil {
		return w, false, err
	}
	result, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO waits VALUES (?,?,?,?,?,?)`, w.App, w.RunID, w.Step, w.Service, w.Member, string(raw))
	if err != nil {
		return w, false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n > 0 {
		return w, n > 0, err
	}
	var body string
	err = db.QueryRowContext(ctx, `SELECT body FROM waits WHERE app=? AND run_id=? AND step=? AND service=? AND member=?`, w.App, w.RunID, w.Step, w.Service, w.Member).Scan(&body)
	if err == nil {
		err = json.Unmarshal([]byte(body), &w)
	}
	return w, false, err
}
func (s *waitStore) list(ctx context.Context) ([]bridge.ConnectorWait, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT body FROM waits ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []bridge.ConnectorWait{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var w bridge.ConnectorWait
		if err := json.Unmarshal([]byte(body), &w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (s *waitStore) remove(ctx context.Context, w bridge.ConnectorWait) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.open()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM waits WHERE app=? AND run_id=? AND step=? AND service=? AND member=?`, w.App, w.RunID, w.Step, w.Service, w.Member)
	return err
}
func (s *waitStore) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}
