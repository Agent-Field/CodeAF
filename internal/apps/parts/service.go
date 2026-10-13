package parts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	_ "modernc.org/sqlite"
)

// Service serializes file changes with their version transactions.
type Service struct {
	cfg svc.Config
	all *svc.Services
	mu  sync.Mutex
}

var _ svc.Parts = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{cfg: cfg, all: all} }
func invalid(message string) error                   { return bridge.Errorf(bridge.CodeInvalid, message) }
func missing(message string) error                   { return bridge.Errorf(bridge.CodeNotFound, message) }
func conflict(message string) error                  { return bridge.Errorf(bridge.CodeConflict, message) }
func mode(scope svc.Scope) string {
	if scope.Data == svc.DataReal || scope.Data == svc.DataDraft {
		return "real"
	}
	return "sample"
}
func content(scope svc.Scope) string {
	if scope.Data == svc.DataReal || scope.Data == svc.DataDraft {
		return "content"
	}
	return "sample-content"
}

type session struct {
	s            *Service
	scope        svc.Scope
	db           *sql.DB
	tx           *sql.Tx
	root         string
	realRoot     string
	versionTable string
	sidecarTable string
	changes      []fileChange
	events       []pendingEvent
	committed    bool
}
type fileChange struct {
	path    string
	old     []byte
	existed bool
}
type pendingEvent struct {
	doc   string
	event bridge.DocEvent
}

func (s *Service) open(ctx context.Context, scope svc.Scope) (*session, error) {
	if s.cfg.Root == "" {
		return nil, invalid("parts root is required")
	}
	if !validRelative(scope.App) || filepath.Base(scope.App) != scope.App {
		return nil, invalid("invalid app id")
	}
	s.mu.Lock()
	c := &session{s: s, scope: scope, versionTable: "part_versions", sidecarTable: "part_sidecar"}
	appDir, err := safePath(s.cfg.Root, scope.App)
	var dir string
	if err == nil {
		dir, err = c.storageDir(appDir)
	}
	if err == nil {
		c.root, err = safePath(dir, content(scope))
	}
	if err == nil {
		c.db, err = openDatabase(ctx, dir)
	}
	if err == nil {
		err = c.attachApplied(ctx, appDir)
	}
	if err == nil {
		c.tx, err = c.db.BeginTx(ctx, nil)
	}
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}
func openDatabase(ctx context.Context, dir string) (*sql.DB, error) {
	for _, name := range []string{"parts.db", "parts.db-wal", "parts.db-shm"} {
		if _, err := safePath(dir, name); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.Join(dir, "parts.db")}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err == nil {
		_, err = db.ExecContext(ctx, schema)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

const schema = `CREATE TABLE IF NOT EXISTS part_versions (
 data TEXT NOT NULL, doc TEXT NOT NULL, part TEXT NOT NULL, version INTEGER NOT NULL,
 kind TEXT NOT NULL, label TEXT NOT NULL, by TEXT NOT NULL, by_agent INTEGER NOT NULL,
 at TEXT NOT NULL, body TEXT NOT NULL, extra TEXT NOT NULL, PRIMARY KEY(data,doc,part,version));
 CREATE TABLE IF NOT EXISTS part_sidecar (
 data TEXT NOT NULL, doc TEXT NOT NULL, part TEXT NOT NULL, status TEXT NOT NULL, owner TEXT NOT NULL,
 PRIMARY KEY(data,doc,part));
 CREATE TABLE IF NOT EXISTS share_links (
 data TEXT NOT NULL, doc TEXT NOT NULL, slug TEXT NOT NULL, "on" INTEGER NOT NULL,
 by TEXT NOT NULL, at TEXT NOT NULL, PRIMARY KEY(data,doc), UNIQUE(data,slug));`

func (c *session) close() {
	if c.tx != nil {
		_ = c.tx.Rollback()
	}
	if !c.committed {
		_ = c.undo()
	}
	if c.db != nil {
		_ = c.db.Close()
	}
	c.s.mu.Unlock()
}
func (c *session) commit() error {
	if err := c.tx.Commit(); err != nil {
		return err
	}
	c.committed = true
	if c.scope.Data != svc.DataDraft && c.s.all != nil && c.s.all.Hub != nil {
		for _, e := range c.events {
			c.s.all.Hub.Publish(bridge.TopicDoc(c.scope.App, e.doc), e.event)
		}
	}
	return nil
}
func (c *session) event(doc string, e bridge.DocEvent) {
	e.By = c.scope.Caller.Member
	c.events = append(c.events, pendingEvent{doc, e})
}
func timestamp() string    { return time.Now().UTC().Format(time.RFC3339Nano) }
func marshal(v any) string { b, _ := json.Marshal(v); return string(b) }
func ioError(err error) error {
	if os.IsNotExist(err) {
		return missing("document or part not found")
	}
	return err
}
func (c *session) docPath(doc, part string) (string, error) {
	dir, err := safePath(c.root, doc)
	if err != nil {
		return "", err
	}
	return safePath(dir, part)
}
func (c *session) version(ctx context.Context, doc, part string) (int, error) {
	var v int
	err := c.tx.QueryRowContext(ctx, c.selectVersions("COALESCE(MAX(version),0)"), mode(c.scope), doc, part).Scan(&v)
	return v, err
}
func (c *session) requirePart(ctx context.Context, doc, part string) (descriptor, error) {
	d, err := c.readDoc(doc)
	if err != nil {
		return d, err
	}
	if err = validPart(part); err != nil {
		return d, err
	}
	for _, id := range d.Parts {
		if id == part {
			return d, nil
		}
	}
	return d, missing(fmt.Sprintf("part %q not found", part))
}
