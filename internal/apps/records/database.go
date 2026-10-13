package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	_ "modernc.org/sqlite"
)

// Service opens a separate SQLite file for each app and data mode. Per-file
// locks serialize writes while WAL lets readers use independent connections.
type Service struct {
	cfg     svc.Config
	all     *svc.Services
	mu      sync.Mutex
	writers map[string]*sync.Mutex
	scopes  map[string]svc.Scope
}

var _ svc.Records = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	s := &Service{cfg: cfg, all: all, writers: map[string]*sync.Mutex{}, scopes: map[string]svc.Scope{}}
	if cfg.Ctx != nil {
		go s.tick(cfg.Ctx)
	}
	return s
}
func (s *Service) writer(scope svc.Scope) *sync.Mutex {
	key := scope.App + "/" + string(scope.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writers[key] == nil {
		s.writers[key] = &sync.Mutex{}
	}
	s.scopes[key] = scope
	return s.writers[key]
}

type connection struct {
	db     *sql.DB
	def    *spec.Bundle
	unlock func()
}

func (c *connection) close() { _ = c.db.Close(); c.unlock() }
func (s *Service) open(ctx context.Context, scope svc.Scope, writing, seeding bool) (*connection, error) {
	if !query.ValidName(scope.App) {
		return nil, invalid("invalid app id")
	}
	filename, err := s.shapeDatabaseFilename(scope)
	if err != nil {
		return nil, err
	}
	if s.cfg.Root == "" {
		return nil, invalid("records root is required")
	}
	if s.all == nil || s.all.Store == nil {
		return nil, fmt.Errorf("records requires the app store")
	}
	def, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return nil, err
	}
	if def == nil || def.App == nil {
		return nil, invalid("app has no record schema")
	}
	lock := s.writer(scope)
	lock.Lock()
	c := &connection{def: def, unlock: lock.Unlock}
	dir := filepath.Join(s.cfg.Root, scope.App)
	if err := os.MkdirAll(dir, 0700); err != nil {
		lock.Unlock()
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.Join(dir, filename)}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	s.prepareShapeConnection(scope, c, &writing, q)
	u.RawQuery = q.Encode()
	c.db, err = sql.Open("sqlite", u.String())
	if err != nil {
		c.unlock()
		return nil, err
	}
	c.db.SetMaxOpenConns(1)
	if _, err = c.db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err == nil {
		if !seeding {
			err = ensureSchema(ctx, c.db, def, scope, dir)
		}
	}
	if err != nil {
		c.close()
		return nil, err
	}
	if !writing {
		lock.Unlock()
		c.unlock = func() {}
	}
	return c, nil
}
func ensureSchema(ctx context.Context, db *sql.DB, def *spec.Bundle, scope svc.Scope, dir string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var initialized bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='_schema')`).Scan(&initialized); err != nil {
		return err
	}
	if err := schema(ctx, tx, def.App); err != nil {
		return err
	}
	if !initialized {
		rows, err := fixtureRows(def, scope, dir)
		if err != nil {
			return err
		}
		if rows != nil {
			if _, err := seedRows(ctx, tx, def, rows); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func schema(ctx context.Context, tx *sql.Tx, app *spec.App) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _schema (id INTEGER PRIMARY KEY CHECK(id=1), definition TEXT NOT NULL)`); err != nil {
		return err
	}
	data, err := json.Marshal(app.Records)
	if err != nil {
		return invalid("invalid record schema")
	}
	var old string
	err = tx.QueryRowContext(ctx, `SELECT definition FROM _schema WHERE id=1`).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		if old != string(data) {
			return bridge.Errorf(bridge.CodeConflict, "record schema changed; reseed sample data")
		}
		return nil
	}
	for _, name := range sorted(app.Records) {
		if err := createTable(ctx, tx, name, app.Records[name]); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _history (seq INTEGER PRIMARY KEY, record_type TEXT NOT NULL, id TEXT NOT NULL, op TEXT NOT NULL, by TEXT NOT NULL, at TEXT NOT NULL, before TEXT, after TEXT)`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO _schema (id, definition) VALUES (1, ?)`, string(data))
	return err
}
func createTable(ctx context.Context, tx *sql.Tx, name string, typ *spec.RecordType) error {
	table, err := query.Table(name)
	if err != nil || typ == nil {
		return invalid("invalid record type " + name)
	}
	columns := []string{`id TEXT PRIMARY KEY`, `record TEXT NOT NULL CHECK(json_valid(record))`}
	for _, field := range sorted(typ.Fields) {
		f := typ.Fields[field]
		if f == nil || !query.ValidName(field) {
			return invalid("invalid field " + field)
		}
		if (f.Type == spec.FieldRef || f.Type == spec.FieldRefs) && !query.ValidName(f.To) {
			return invalid("invalid relation " + field)
		}
		if query.Indexed(f) {
			columns = append(columns, query.Column(field)+` GENERATED ALWAYS AS (json_extract(record, '$.fields."`+field+`"')) VIRTUAL`)
		}
	}
	if _, err := tx.ExecContext(ctx, "CREATE TABLE "+table+" ("+join(columns)+")"); err != nil {
		return err
	}
	for _, field := range sorted(typ.Fields) {
		f := typ.Fields[field]
		if !query.Indexed(f) {
			continue
		}
		unique := ""
		if f.Unique {
			unique = "UNIQUE "
		}
		index := `"index_` + name + `_` + field + `"`
		if _, err := tx.ExecContext(ctx, "CREATE "+unique+"INDEX "+index+" ON "+table+" ("+query.Column(field)+")"); err != nil {
			return err
		}
	}
	return nil
}
func sorted[T any](m map[string]T) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func join(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += ", "
		}
		result += value
	}
	return result
}
func invalid(message string) error { return bridge.Errorf(bridge.CodeInvalid, message) }
func recordType(def *spec.Bundle, name string) (*spec.RecordType, error) {
	typ := def.App.Records[name]
	if typ == nil || !query.ValidName(name) {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no record type "+name)
	}
	return typ, nil
}
func (s *Service) publish(scope svc.Scope, diff bridge.RecordDiff) {
	if s.all.Hub != nil {
		if diff.Record != nil {
			row := *diff.Record
			row.Fields, _ = copyFields(row.Fields)
			row.AI = slices.Clone(row.AI)
			diff.Record = &row
		}
		s.all.Hub.Publish(bridge.TopicRecords(scope.App, diff.Type), diff)
	}
}
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
