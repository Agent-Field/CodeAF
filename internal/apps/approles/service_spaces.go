package approles

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	_ "modernc.org/sqlite"
)

// Options lets a workspace share its database even before space.db exists.
type Options struct{ DBPath string }

type Service struct {
	cfg  svc.Config
	all  *svc.Services
	path string
	once sync.Once
	mu   sync.Mutex
	db   *sql.DB
	err  error
	now  func() time.Time
}

func New(cfg svc.Config, all *svc.Services) *Service {
	return NewWithOptions(cfg, all, Options{})
}

func NewWithOptions(cfg svc.Config, all *svc.Services, options Options) *Service {
	return &Service{cfg: cfg, all: all, path: options.DBPath, now: time.Now}
}

var _ svc.AppRoles = (*Service)(nil)

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
	if s.path == "" {
		s.path = filepath.Join(s.cfg.Root, "approles.db")
		shared := filepath.Join(s.cfg.Root, "..", "space.db")
		if _, err := os.Stat(shared); err == nil {
			s.path = shared
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	s.db = db
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
CREATE TABLE IF NOT EXISTS app_roles (
 app TEXT NOT NULL,
 member TEXT NOT NULL,
 role TEXT NOT NULL CHECK (role IN ('editor', 'user', 'viewer')),
 set_by TEXT NOT NULL,
 at TEXT NOT NULL,
 PRIMARY KEY (app, member)
);
CREATE TABLE IF NOT EXISTS app_role_makers (
 app TEXT PRIMARY KEY,
 member TEXT NOT NULL
);`)
	return err
}

func (s *Service) Close() error {
	if err := s.open(); err != nil {
		return err
	}
	return s.db.Close()
}

func (s *Service) Role(ctx context.Context, app, member string) (svc.Role, error) {
	if caller, ok := svc.CallerFrom(ctx); ok && caller.Space == svc.PersonalSpace {
		return svc.RoleEditor, nil
	}
	if _, known := svc.CallerFrom(ctx); !known && s.all != nil && s.all.Store != nil {
		detail, err := s.all.Store.Get(ctx, app)
		if err != nil {
			return "", err
		}
		if detail.Space == "" || detail.Space == svc.PersonalSpace {
			return svc.RoleEditor, nil
		}
	}
	if err := s.open(); err != nil {
		return "", err
	}
	var role svc.Role
	err := s.db.QueryRowContext(ctx, `SELECT role FROM app_roles WHERE app = ? AND member IN (?, '*') ORDER BY member = '*' LIMIT 1`, app, member).Scan(&role)
	if err == nil {
		return role, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM app_roles WHERE app = ?`, app).Scan(&count); err != nil {
		return "", err
	}
	if count != 0 {
		return "", nil
	}
	if caller, ok := svc.CallerFrom(ctx); ok {
		if caller.Space == "" {
			return svc.RoleEditor, nil
		}
		return "", nil
	}

	return "", nil
}

func (s *Service) Seed(ctx context.Context, app, maker string) error {
	if app == "" || maker == "" || maker == "*" {
		return bridge.Errorf(bridge.CodeInvalid, "app and maker are required")
	}
	if err := s.open(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO app_role_makers (app, member) VALUES (?, ?) ON CONFLICT DO NOTHING`, app, maker)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	// Repeated Keep or Apply must never overwrite membership edits or the original maker.
	if inserted == 0 {
		return tx.Commit()
	}
	at := s.now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO app_roles (app, member, role, set_by, at) VALUES (?, ?, 'editor', ?, ?), (?, '*', 'user', ?, ?) ON CONFLICT DO NOTHING`, app, maker, maker, at, app, maker, at)
	if err != nil {
		return err
	}
	return tx.Commit()
}
