package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	_ "modernc.org/sqlite"
)

type Service struct {
	cfg  svc.Config
	all  *svc.Services
	once sync.Once
	db   *sql.DB
	err  error
	now  func() time.Time
}

var _ svc.Audit = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	return &Service{cfg: cfg, all: all, now: time.Now}
}

// Open lazily so wiring retains its constructor shape and storage failures reach callers.
func (s *Service) open() error {
	s.once.Do(func() {
		s.err = s.initialize()
		if s.err != nil && s.db != nil {
			_ = s.db.Close()
		}
		if s.err == nil && s.cfg.Ctx != nil {
			context.AfterFunc(s.cfg.Ctx, func() { _ = s.db.Close() })
		}
	})
	return s.err
}

func (s *Service) initialize() error {
	if err := os.MkdirAll(s.cfg.Root, 0700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Root, "audit.db"))
	if err != nil {
		return err
	}
	s.db = db
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
PRAGMA busy_timeout = 5000;
CREATE TABLE IF NOT EXISTS audit (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 at TEXT NOT NULL,
 space TEXT NOT NULL,
 app TEXT NOT NULL,
 member TEXT NOT NULL,
 action TEXT NOT NULL,
 activity INTEGER NOT NULL,
 entry BLOB NOT NULL
);
CREATE TRIGGER IF NOT EXISTS audit_no_update BEFORE UPDATE ON audit
 BEGIN SELECT RAISE(ABORT, 'audit is append-only'); END;
CREATE TRIGGER IF NOT EXISTS audit_no_delete BEFORE DELETE ON audit
 BEGIN SELECT RAISE(ABORT, 'audit is append-only'); END;
`)
	return err
}

func (s *Service) Close() error {
	if err := s.open(); err != nil {
		return err
	}
	return s.db.Close()
}

func (s *Service) Append(ctx context.Context, e bridge.AuditEntry) (bridge.AuditEntry, error) {
	if err := s.open(); err != nil {
		return bridge.AuditEntry{}, err
	}
	e.Seq, e.At = 0, s.now().UTC()
	if e.Space == "" {
		e.Space = svc.PersonalSpace
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return bridge.AuditEntry{}, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO audit (at, space, app, member, action, activity, entry) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.At.Format(time.RFC3339Nano), e.Space, e.App, e.Member, e.Action, e.Activity != nil, raw)
	if err != nil {
		return bridge.AuditEntry{}, err
	}
	e.Seq, err = result.LastInsertId()
	return e, err
}

func (s *Service) List(ctx context.Context, scope svc.Scope, p bridge.AuditListParams) (bridge.AuditListResult, error) {
	entries, next, err := s.entries(ctx, scope, p, false)
	return bridge.AuditListResult{Entries: entries, Next: next}, err
}

func (s *Service) Activity(ctx context.Context, scope svc.Scope, p bridge.ActivityListParams) (bridge.ActivityListResult, error) {
	entries, next, err := s.entries(ctx, scope, bridge.AuditListParams{App: p.App, Before: p.Before, Limit: p.Limit}, true)
	lines := make([]bridge.ActivityEntry, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, bridge.ActivityEntry{Seq: e.Seq, At: e.At, App: e.App, Member: e.Member,
			Text: e.Activity.Text, Undo: e.Activity.Undo, View: e.Activity.View})
	}
	return bridge.ActivityListResult{Lines: lines, Next: next}, err
}

func (s *Service) entries(ctx context.Context, scope svc.Scope, p bridge.AuditListParams, activity bool) ([]bridge.AuditEntry, int64, error) {
	if p.Before < 0 || p.Limit < 0 {
		return nil, 0, bridge.Errorf(bridge.CodeInvalid, "before and limit must be nonnegative")
	}
	if err := s.open(); err != nil {
		return nil, 0, err
	}
	limit := p.Limit
	if limit == 0 {
		limit = 100
	}
	query, args := entryQuery(scope, p, activity)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries := make([]bridge.AuditEntry, 0)
	for rows.Next() {
		e, err := readEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		if activity {
			visible, err := s.visible(ctx, scope, e.App)
			if err != nil {
				return nil, 0, err
			}
			if !visible {
				continue
			}
		}
		if len(entries) == limit {
			return entries, entries[len(entries)-1].Seq, nil
		}
		entries = append(entries, e)
	}
	return entries, 0, rows.Err()
}

func entryQuery(scope svc.Scope, p bridge.AuditListParams, activity bool) (string, []any) {
	space := scope.Caller.Space
	if space == "" {
		space = svc.PersonalSpace
	}
	clauses, args := []string{"space = ?"}, []any{space}
	for _, filter := range []struct{ column, value string }{{"app", p.App}, {"member", p.Member}, {"action", p.Action}} {
		if filter.value != "" {
			clauses, args = append(clauses, filter.column+" = ?"), append(args, filter.value)
		}
	}
	if p.Before != 0 {
		clauses, args = append(clauses, "seq < ?"), append(args, p.Before)
	}
	if activity {
		clauses = append(clauses, "activity = 1")
	}
	return "SELECT seq, at, entry FROM audit WHERE " + strings.Join(clauses, " AND ") + " ORDER BY seq DESC", args
}

func readEntry(rows *sql.Rows) (bridge.AuditEntry, error) {
	var seq int64
	var at string
	var raw []byte
	var e bridge.AuditEntry
	if err := rows.Scan(&seq, &at, &raw); err != nil {
		return e, err
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, err
	}
	e.Seq = seq
	var err error
	e.At, err = time.Parse(time.RFC3339Nano, at)
	return e, err
}

func (s *Service) visible(ctx context.Context, scope svc.Scope, app string) (bool, error) {
	if app == "" {
		return true, nil
	}
	role, err := s.all.AppRoles.Role(ctx, app, scope.Caller.Member)
	return role.AtLeast(svc.RoleViewer), err
}
