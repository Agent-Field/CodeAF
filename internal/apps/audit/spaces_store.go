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
	mu   sync.Mutex
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
PRAGMA journal_mode = WAL;
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
CREATE INDEX IF NOT EXISTS audit_space_seq ON audit (space, seq DESC);
CREATE INDEX IF NOT EXISTS audit_space_app_seq ON audit (space, app, seq DESC);
CREATE TRIGGER IF NOT EXISTS audit_no_replace BEFORE INSERT ON audit
 WHEN EXISTS (SELECT 1 FROM audit WHERE seq = NEW.seq)
 BEGIN SELECT RAISE(ABORT, 'audit is append-only'); END;
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
	// Keep the commit and its publication together so concurrent writers publish in sequence order.
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if err == nil {
		s.publishActivity(e)
	}
	return e, err
}

func (s *Service) List(ctx context.Context, scope svc.Scope, p bridge.AuditListParams) (bridge.AuditListResult, error) {
	if !scope.Caller.IsAdmin() {
		return bridge.AuditListResult{}, bridge.Errorf(bridge.CodeDenied, "audit needs workspace Owner or Admin")
	}
	entries, next, err := s.entries(ctx, scope, p, false)
	return bridge.AuditListResult{Entries: entries, Next: next}, err
}

func (s *Service) Activity(ctx context.Context, scope svc.Scope, p bridge.ActivityListParams) (bridge.ActivityListResult, error) {
	if scope.Caller.Member == "" {
		return bridge.ActivityListResult{}, bridge.Errorf(bridge.CodeDenied, "a member is required")
	}
	entries, next, err := s.entries(ctx, scope, bridge.AuditListParams{App: p.App, Before: p.Before, Limit: p.Limit}, true)
	lines := make([]bridge.ActivityEntry, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, activityEntry(e))
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
	limit := pageLimit(p.Limit)
	entries := make([]bridge.AuditEntry, 0, limit+1)
	visibility := map[string]bool{"": true}
	for {
		batch, err := s.batch(ctx, scope, p, activity, limit+1)
		if err != nil {
			return nil, 0, err
		}
		for _, e := range batch {
			visible := true
			if activity {
				visible, err = s.cachedVisibility(ctx, scope, e.App, visibility)
				if err != nil {
					return nil, 0, err
				}
			}
			if visible {
				entries = append(entries, e)
			}
			if len(entries) > limit {
				return entries[:limit], entries[limit-1].Seq, nil
			}
		}
		if len(batch) < limit+1 {
			return entries, 0, nil
		}
		p.Before = batch[len(batch)-1].Seq
	}
}

func pageLimit(limit int) int {
	if limit == 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

// Finish reading before consulting peer services, which may use the same database.
func (s *Service) batch(ctx context.Context, scope svc.Scope, p bridge.AuditListParams, activity bool, limit int) ([]bridge.AuditEntry, error) {
	query, args := entryQuery(scope, p, activity)
	rows, err := s.db.QueryContext(ctx, query+" LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]bridge.AuditEntry, 0, limit)
	for rows.Next() {
		e, err := readEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Service) cachedVisibility(ctx context.Context, scope svc.Scope, app string, cache map[string]bool) (bool, error) {
	if visible, ok := cache[app]; ok {
		return visible, nil
	}
	visible, err := s.visible(ctx, scope, app)
	if err == nil {
		cache[app] = visible
	}
	return visible, err
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
	if s.all == nil || s.all.AppRoles == nil {
		return false, nil
	}
	role, err := s.all.AppRoles.Role(ctx, app, scope.Caller.Member)
	return role.AtLeast(svc.RoleViewer), err
}

func activityEntry(e bridge.AuditEntry) bridge.ActivityEntry {
	return bridge.ActivityEntry{Seq: e.Seq, At: e.At, App: e.App, Member: e.Member,
		Text: e.Activity.Text, Undo: e.Activity.Undo, View: e.Activity.View}
}

func (s *Service) publishActivity(e bridge.AuditEntry) {
	if e.Activity == nil || s.all == nil || s.all.Hub == nil {
		return
	}
	line := activityEntry(e)
	s.all.Hub.Publish(bridge.TopicSpaceActivity, line)
	if e.App != "" {
		s.all.Hub.Publish("app/"+e.App+"/activity", line)
	}
}
