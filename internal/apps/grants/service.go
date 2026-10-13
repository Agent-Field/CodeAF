// Package grants owns per-person Allow and the workspace's shared connections.
package grants

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	_ "modernc.org/sqlite"
)

type Service struct {
	all         *svc.Services
	db          *sql.DB
	initErr     error
	home        string
	serverSpace string
	spaces      Spaces
	auditWriter auditor
	connections Connections
	now         func() time.Time
}

var _ svc.Grants = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return NewWithOptions(cfg, all, Options{}) }

func NewWithOptions(cfg svc.Config, all *svc.Services, opts Options) *Service {
	s := &Service{all: all, home: filepath.Dir(cfg.Root), serverSpace: opts.Space, spaces: opts.Spaces, auditWriter: opts.Audit, connections: opts.Connections, now: time.Now}
	if s.serverSpace == "" {
		s.serverSpace = "personal"
	}
	if s.spaces == nil {
		s.spaces = personalSpace{}
	}
	if s.auditWriter == nil {
		s.auditWriter = noAudit{}
	}
	if s.connections == nil {
		s.connections = &localConnections{home: s.home}
	}
	// An empty config must never silently open a database in the working directory.
	if cfg.Root == "" {
		s.initErr = bridge.Errorf(bridge.CodeInvalid, "connection home is required")
		return s
	}
	s.initErr = s.open()
	return s
}

func (s *Service) open() error {
	if err := os.MkdirAll(s.home, 0700); err != nil {
		return err
	}
	path := filepath.Join(s.home, "connect-apps.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	s.db, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	s.db.SetMaxOpenConns(1)
	_, err = s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA secure_delete=ON;
 CREATE TABLE IF NOT EXISTS grants(member TEXT NOT NULL, app TEXT NOT NULL, space TEXT NOT NULL, service TEXT NOT NULL, at TEXT NOT NULL, PRIMARY KEY(member,app,space,service));
 CREATE TABLE IF NOT EXISTS grant_credentials(member TEXT NOT NULL, app TEXT NOT NULL, space TEXT NOT NULL, service TEXT NOT NULL, credential BLOB NOT NULL, PRIMARY KEY(member,app,space,service), FOREIGN KEY(member,app,space,service) REFERENCES grants(member,app,space,service) ON DELETE CASCADE);
 CREATE TABLE IF NOT EXISTS workspace_connections(id TEXT PRIMARY KEY, service TEXT NOT NULL, account TEXT NOT NULL, added_by TEXT NOT NULL, who TEXT NOT NULL CHECK(who IN ('all','chosen')), at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS workspace_connection_apps(id TEXT NOT NULL REFERENCES workspace_connections(id) ON DELETE CASCADE, app TEXT NOT NULL, PRIMARY KEY(id,app));`)
	if err != nil {
		_ = s.db.Close()
	}
	return err
}

func (s *Service) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
func (s *Service) ready() error { return s.initErr }
func (s *Service) definition(ctx context.Context, app string) (*spec.App, error) {
	if s.all == nil || s.all.Store == nil {
		return nil, bridge.Errorf(bridge.CodeNotFound, "App definition is unavailable")
	}
	bundle, err := s.all.Store.Definition(ctx, app)
	if err != nil {
		return nil, err
	}
	if bundle == nil || bundle.App == nil {
		return nil, bridge.Errorf(bridge.CodeNotFound, "App definition is unavailable")
	}
	return bundle.App, nil
}
func (s *Service) connector(ctx context.Context, scope svc.Scope, service string) (*spec.App, spec.Connector, error) {
	app, err := s.definition(ctx, scope.App)
	if err != nil {
		return nil, spec.Connector{}, err
	}
	need, ok := app.Capabilities.Connectors[service]
	if !ok {
		return nil, spec.Connector{}, bridge.Errorf(bridge.CodeDenied, "This app does not use that connection")
	}
	return app, need, nil
}
func (s *Service) grant(ctx context.Context, member, app, space, service string) (bridge.Grant, error) {
	g := bridge.Grant{Member: member, App: app, Space: space, Service: service}
	if err := s.db.QueryRowContext(ctx, `SELECT at FROM grants WHERE member=? AND app=? AND space=? AND service=?`, member, app, space, service).Scan(&g.At); err != nil {
		return g, err
	}
	def, err := s.definition(ctx, app)
	if err != nil {
		return g, err
	}
	g.AppName = def.Name
	g.Can = append([]string{}, def.Capabilities.Connectors[service].Can...)
	return g, nil
}
func (s *Service) publish(topic string, value any) {
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(topic, value)
	}
}

func (s *Service) ForgetMember(ctx context.Context, member string) error {
	if err := s.ready(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM grants WHERE member=?`, member)
	return err
}

func credentialValue(c bridge.GrantCredential, service, id, account, member, level string) svc.Credential {
	return svc.Credential{ID: id, Service: service, Account: account, Member: member, Level: level, Token: c.Token, Kind: c.Kind, ExpiresAt: c.ExpiresAt}
}
func expired(c bridge.GrantCredential, now time.Time) bool {
	if c.ExpiresAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, c.ExpiresAt)
	return err != nil || !at.After(now)
}
func validCredential(c *bridge.GrantCredential) bool {
	if c == nil || strings.TrimSpace(c.Token) == "" || (c.Kind != "refresh" && c.Kind != "grant") {
		return false
	}
	if c.ExpiresAt != "" {
		_, err := time.Parse(time.RFC3339, c.ExpiresAt)
		return err == nil
	}
	return true
}

func (s *Service) ExportCredential(ctx context.Context, scope svc.Scope, p bridge.CredentialExportParams) (bridge.CredentialExportResult, error) {
	out := bridge.CredentialExportResult{}
	if _, err := ExportRule(scope, bridge.MethodConnectCredentialExport, nil); err != nil {
		return out, err
	}
	if s.space() != "personal" {
		return out, bridge.Errorf(bridge.CodeDenied, "Connections can only be exported from Personal")
	}
	if err := s.ready(); err != nil {
		return out, err
	}
	if p.Service == "" || p.Space == "" || p.Space == "personal" || p.App == "" {
		return out, bridge.Errorf(bridge.CodeInvalid, "Service, workspace and app are required")
	}
	c, err := s.connections.PersonalCredential(ctx, scope.Caller.Member, p.Service)
	if err != nil {
		return out, err
	}
	if expired(c, s.now()) {
		return out, reason(bridge.CodeConflict, bridge.ReasonExpired, "Reconnect this account")
	}
	if !validCredential(&c) {
		return out, bridge.Errorf(bridge.CodeInvalid, "Connect this account first")
	}
	out.Credential = c
	return out, nil
}

// Secret JSON is encoded only for encryption, never for an ordinary bridge result.
func encodeCredential(c bridge.GrantCredential) []byte { raw, _ := json.Marshal(c); return raw }
