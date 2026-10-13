package grants

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var testTime = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
var mona = svc.Caller{Member: "mona", Role: svc.RoleEditor}
var testCtx = context.Background()

type fakeApps struct {
	svc.Store
	apps map[string]*spec.App
}

func (f fakeApps) Definition(_ context.Context, id string) (*spec.Bundle, error) {
	app := f.apps[id]
	if app == nil {
		return nil, bridge.Errorf(bridge.CodeNotFound, "No app")
	}
	return &spec.Bundle{App: app}, nil
}
func (f fakeApps) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	out := []bridge.AppSummary{}
	for id, app := range f.apps {
		out = append(out, bridge.AppSummary{ID: id, Name: app.Name})
	}
	return out, nil
}

type fakeSpaces struct{}

func (fakeSpaces) RoleOf(c svc.Caller) string {
	if c.Member == "mona" {
		return "owner"
	}
	if c.Member == "raj" {
		return "admin"
	}
	return "member"
}
func (fakeSpaces) NameOf(c svc.Caller) string {
	if c.Member == "mona" {
		return "Mona"
	}
	return c.Member
}
func (fakeSpaces) Members(context.Context, string) ([]string, error) {
	return []string{"mona", "raj", "jm", "santosh"}, nil
}

type fakeAccounts struct {
	personal  map[string]bridge.GrantCredential
	workspace map[string]bridge.GrantCredential
	signedOut []string
	reads     int
	signIns   int
}

func (f *fakeAccounts) Personal(_ context.Context, member string) ([]bridge.Connection, error) {
	f.reads++
	out := []bridge.Connection{}
	for service, c := range f.personal {
		status := "connected"
		if expired(c, testTime) {
			status = "expired"
		}
		out = append(out, bridge.Connection{ID: member + ":" + service, Service: service, Account: member + "@acme.com", Level: "personal", Status: status, Since: "2026-09-02T00:00:00Z"})
	}
	return out, nil
}
func (f *fakeAccounts) PersonalCredential(_ context.Context, member, service string) (bridge.GrantCredential, error) {
	f.reads++
	c, ok := f.personal[service]
	if !ok {
		return c, bridge.Errorf(bridge.CodeNotFound, "Connect first")
	}
	return c, nil
}
func (f *fakeAccounts) SignIn(_ context.Context, id, service string) (bridge.Connection, error) {
	f.signIns++
	f.workspace[id] = bridge.GrantCredential{Kind: "grant", Token: "workspace-secret"}
	return bridge.Connection{ID: id, Service: service, Account: "acme live"}, nil
}
func (f *fakeAccounts) WorkspaceCredential(_ context.Context, id, service string) (bridge.GrantCredential, error) {
	c, ok := f.workspace[id]
	if !ok {
		return c, bridge.Errorf(bridge.CodeNotFound, "No workspace credential")
	}
	return c, nil
}
func (f *fakeAccounts) SignOut(_ context.Context, id, service string) error {
	f.signedOut = append(f.signedOut, id)
	delete(f.workspace, id)
	return nil
}

type fakeAudit struct{ lines []AuditLine }

func (a *fakeAudit) Append(_ context.Context, line AuditLine) error {
	a.lines = append(a.lines, line)
	return nil
}

type fakeHub struct {
	svc.Hub
	events []svc.Event
}

func (h *fakeHub) Publish(topic string, data any) {
	h.events = append(h.events, svc.Event{Topic: topic, Data: data})
}

type fakeRuns struct {
	svc.Runs
	rows []bridge.RunState
}

func (r fakeRuns) List(context.Context, svc.Scope, bridge.ListRunsParams) (bridge.ListRunsResult, error) {
	return bridge.ListRunsResult{Runs: r.rows}, nil
}

func fixture(t *testing.T, space string) (*Service, *fakeAccounts, *fakeAudit, *fakeHub) {
	t.Helper()
	gmail := spec.Connector{As: "member", Can: []string{"send", "read replies"}, Approve: []string{"send"}}
	stripe := spec.Connector{As: "workspace", Can: []string{"read invoices for won deals"}}
	apps := fakeApps{apps: map[string]*spec.App{
		"crm":     {ID: "crm", Name: "CRM", Space: space, Capabilities: spec.Capabilities{Connectors: map[string]spec.Connector{"gmail": gmail, "stripe": stripe, "calendar": {As: "member", Can: []string{"book calls on deals"}}}}},
		"support": {ID: "support", Name: "Support", Space: space, Capabilities: spec.Capabilities{Connectors: map[string]spec.Connector{"stripe": {As: "workspace", Can: []string{"refunds"}}, "gmail": gmail}}},
		"slides":  {ID: "slides", Name: "Slides", Space: space, Capabilities: spec.Capabilities{Connectors: map[string]spec.Connector{"calendar": {As: "member", Can: []string{"read free/busy"}}}}},
	}}
	f := &fakeAccounts{personal: map[string]bridge.GrantCredential{"gmail": {Kind: "refresh", Token: "personal-secret"}}, workspace: map[string]bridge.GrantCredential{}}
	audit := &fakeAudit{}
	hub := &fakeHub{}
	all := &svc.Services{Store: apps, Hub: hub}
	s := NewWithOptions(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, all, Options{Space: space, Spaces: fakeSpaces{}, Connections: f, Audit: audit})
	s.now = func() time.Time { return testTime }
	t.Cleanup(func() { _ = s.Close() })
	if s.initErr != nil {
		t.Fatal(s.initErr)
	}
	return s, f, audit, hub
}
func appScope(app string) svc.Scope { return svc.Scope{App: app, Caller: mona, Data: svc.DataReal} }
func assertReason(t *testing.T, err error, code bridge.ErrorCode, why string) {
	t.Helper()
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != code || (why != "" && typed.Details["reason"] != why) {
		t.Fatalf("error=%v, want %s/%s", err, code, why)
	}
}
func allow(t *testing.T, s *Service, member, app, service, token string) bridge.Grant {
	t.Helper()
	scope := appScope(app)
	scope.Caller.Member = member
	g, err := s.Allow(testCtx, scope, bridge.AllowParams{Service: service, Answer: "allow", Credential: &bridge.GrantCredential{Kind: "grant", Token: token}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func tokenCount(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM grant_credentials`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestResolve(t *testing.T) {
	for _, tt := range []struct {
		name, space, setup, level, why string
		code                           bridge.ErrorCode
	}{
		{name: "Personal needs no grant", space: "personal", level: "personal"},
		{name: "workspace asks", space: "acme", code: bridge.CodeConflict, why: bridge.ReasonNeedsAllow},
		{name: "workspace grant", space: "acme", setup: "grant", level: "personal"},
		{name: "personal expiry", space: "personal", setup: "expired personal", code: bridge.CodeConflict, why: bridge.ReasonExpired},
		{name: "grant expiry", space: "acme", setup: "expired grant", code: bridge.CodeConflict, why: bridge.ReasonExpired},
		{name: "workspace preferred", space: "acme", setup: "workspace", level: "workspace"},
		{name: "chosen denies", space: "acme", setup: "chosen", code: bridge.CodeDenied, why: bridge.ReasonNotAllowedForApp},
		{name: "workspace expiry", space: "acme", setup: "expired workspace", code: bridge.CodeConflict, why: bridge.ReasonExpired},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, f, _, hub := fixture(t, tt.space)
			switch tt.setup {
			case "grant", "expired grant":
				allow(t, s, "mona", "crm", "gmail", "grant-secret")
				if tt.setup == "expired grant" {
					s.now = func() time.Time { return testTime.Add(24 * time.Hour) }
					c := bridge.GrantCredential{Kind: "grant", Token: "expired", ExpiresAt: testTime.Add(time.Hour).Format(time.RFC3339)}
					raw, err := s.seal(c, grantBinding("mona", "crm", "acme", "gmail"))
					if err != nil {
						t.Fatal(err)
					}
					_, err = s.db.Exec(`UPDATE grant_credentials SET credential=?`, raw)
					if err != nil {
						t.Fatal(err)
					}
				}
			case "expired personal":
				f.personal["gmail"] = bridge.GrantCredential{Kind: "grant", Token: "expired", ExpiresAt: testTime.Add(-time.Hour).Format(time.RFC3339)}
			case "workspace", "chosen", "expired workspace":
				p := bridge.AddWorkspaceConnectionParams{Service: "gmail", Who: "all"}
				if tt.setup == "chosen" {
					p.Who = "chosen"
					p.Apps = []string{"support"}
				}
				c, err := s.AddWorkspace(testCtx, mona, p)
				if err != nil {
					t.Fatal(err)
				}
				if tt.setup == "expired workspace" {
					f.workspace[c.ID] = bridge.GrantCredential{Kind: "grant", Token: "expired", ExpiresAt: testTime.Add(-time.Hour).Format(time.RFC3339)}
				}
			}
			cred, err := s.Resolve(testCtx, appScope("crm"), "gmail")
			if tt.code != "" {
				assertReason(t, err, tt.code, tt.why)
			} else if err != nil || cred.Level != tt.level || cred.Token == "" {
				t.Fatalf("credential level=%s error=%v", cred.Level, err)
			}
			if tt.why == bridge.ReasonNeedsAllow {
				if len(hub.events) != 1 || hub.events[0].Topic != bridge.TopicConnectAllow("mona") {
					t.Fatalf("events=%v", hub.events)
				}
				if _, ok := hub.events[0].Data.(bridge.AllowSheet); !ok {
					t.Fatal("event is not AllowSheet")
				}
			}
			if tt.space != "personal" && f.reads != 0 {
				t.Fatal("workspace server read personal store")
			}
		})
	}
}

func TestAllowAnswersAndRefresh(t *testing.T) {
	for _, tt := range []struct {
		name, answer, space string
		credential          *bridge.GrantCredential
		grant               bool
		code                bridge.ErrorCode
	}{
		{name: "allow", answer: "allow", space: "acme", credential: &bridge.GrantCredential{Kind: "refresh", Token: "first"}, grant: true},
		{name: "not now", answer: "not_now", space: "acme"},
		{name: "missing credential", answer: "allow", space: "acme", code: bridge.CodeInvalid},
		{name: "refresh missing credential", answer: "refresh", space: "acme", code: bridge.CodeInvalid},
		{name: "bad kind", answer: "allow", space: "acme", credential: &bridge.GrantCredential{Kind: "other", Token: "first"}, code: bridge.CodeInvalid},
		{name: "bad expiry", answer: "allow", space: "acme", credential: &bridge.GrantCredential{Kind: "grant", Token: "first", ExpiresAt: "bad"}, code: bridge.CodeInvalid},
		{name: "expired", answer: "allow", space: "acme", credential: &bridge.GrantCredential{Kind: "grant", Token: "first", ExpiresAt: testTime.Add(-time.Hour).Format(time.RFC3339)}, code: bridge.CodeConflict},
		{name: "unknown answer", answer: "yes", space: "acme", code: bridge.CodeInvalid},
		{name: "Personal directly", answer: "allow", space: "personal"},
		{name: "Personal refuses transferred secret", answer: "allow", space: "personal", credential: &bridge.GrantCredential{Kind: "grant", Token: "first"}, code: bridge.CodeInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, f, audit, _ := fixture(t, tt.space)
			_, err := s.Allow(testCtx, appScope("crm"), bridge.AllowParams{Service: "gmail", Answer: tt.answer, Credential: tt.credential})
			if tt.code != "" {
				assertReason(t, err, tt.code, "")
			} else if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tt.grant {
				want = 1
			}
			if n := tokenCount(t, s); n != want {
				t.Fatalf("tokens=%d want=%d", n, want)
			}
			if f.signIns != 0 {
				t.Fatal("Allow attempted sign-in")
			}
			if tt.grant && (len(audit.lines) != 1 || audit.lines[0].Action != "connector.allow") {
				t.Fatal("Allow audit missing")
			}
		})
	}
	s, _, audit, _ := fixture(t, "acme")
	g := allow(t, s, "mona", "crm", "gmail", "first")
	s.now = func() time.Time { return testTime.Add(time.Hour) }
	fresh := bridge.GrantCredential{Kind: "grant", Token: "replacement", ExpiresAt: testTime.Add(24 * time.Hour).Format(time.RFC3339)}
	next, err := s.Allow(testCtx, appScope("crm"), bridge.AllowParams{Service: "gmail", Answer: "refresh", Credential: &fresh})
	if err != nil || next.At != g.At || tokenCount(t, s) != 1 {
		t.Fatalf("refresh err=%v grant=%v", err, next)
	}
	cred, err := s.Resolve(testCtx, appScope("crm"), "gmail")
	if err != nil || cred.Token != fresh.Token {
		t.Fatal("refresh did not replace credential")
	}
	if len(audit.lines) != 2 {
		t.Fatal("refresh audit missing")
	}
	_, err = s.Allow(testCtx, appScope("support"), bridge.AllowParams{Service: "gmail", Answer: "refresh", Credential: &fresh})
	assertReason(t, err, bridge.CodeConflict, bridge.ReasonNeedsAllow)
}

func TestRevokeAndForgetDeleteSecrets(t *testing.T) {
	for _, method := range []string{"revoke", "forget"} {
		t.Run(method, func(t *testing.T) {
			s, _, audit, _ := fixture(t, "acme")
			allow(t, s, "mona", "crm", "gmail", "secret-one")
			allow(t, s, "mona", "support", "gmail", "secret-two")
			allow(t, s, "raj", "crm", "gmail", "other-member")
			var err error
			if method == "revoke" {
				err = s.Revoke(testCtx, mona, bridge.RevokeParams{App: "crm", Space: "acme", Service: "gmail"})
			} else {
				err = s.ForgetMember(testCtx, "mona")
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if method == "forget" {
				want = 1
			}
			if n := tokenCount(t, s); n != want {
				t.Fatalf("tokens=%d want=%d", n, want)
			}
			_, err = s.Resolve(testCtx, appScope("crm"), "gmail")
			assertReason(t, err, bridge.CodeConflict, bridge.ReasonNeedsAllow)
			if method == "revoke" && audit.lines[len(audit.lines)-1].Action != "connector.revoke" {
				t.Fatal("no revoke audit")
			}
			allow(t, s, "mona", "crm", "gmail", "allowed-again")
			if _, err = s.Resolve(testCtx, appScope("crm"), "gmail"); err != nil {
				t.Fatal(err)
			}
			if err = s.ForgetMember(testCtx, "absent"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAllowSheet(t *testing.T) {
	for _, tt := range []struct {
		name, space string
		missing     bool
		remove      bool
	}{
		{name: "Personal connected", space: "personal"},
		{name: "Personal missing", space: "personal", missing: true, remove: true},
		{name: "workspace awaiting desktop metadata", space: "acme"},
		{name: "workspace allowed", space: "acme"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, f, _, _ := fixture(t, tt.space)
			if tt.remove {
				delete(f.personal, "gmail")
			}
			if tt.name == "workspace allowed" {
				allow(t, s, "mona", "crm", "gmail", "secret")
			}
			sheet, err := s.AllowSheet(testCtx, appScope("crm"), bridge.AllowSheetParams{Service: "gmail"})
			if err != nil {
				t.Fatal(err)
			}
			if sheet.Title != "CRM may send email as Mona" || sheet.Missing != tt.missing || len(sheet.Can) != 2 || !strings.Contains(sheet.Can[0], "approval needed every time") || !strings.Contains(sheet.Cant[1], "without your approval") {
				t.Fatalf("sheet=%v", sheet)
			}
			raw, _ := json.Marshal(sheet)
			if bytes.Contains(raw, []byte("secret")) {
				t.Fatal("sheet leaked credential")
			}
		})
	}
}

func TestAccounts(t *testing.T) {
	for _, space := range []string{"personal", "acme"} {
		t.Run(space, func(t *testing.T) {
			s, f, _, _ := fixture(t, space)
			if space != "personal" {
				allow(t, s, "mona", "crm", "gmail", "one")
				allow(t, s, "raj", "support", "gmail", "two")
			}
			f.personal["slack"] = bridge.GrantCredential{Kind: "grant", Token: "expired", ExpiresAt: testTime.Add(-time.Hour).Format(time.RFC3339)}
			out, err := s.Accounts(testCtx, mona)
			if err != nil {
				t.Fatal(err)
			}
			if space == "personal" {
				if len(out.Connections) != 2 {
					t.Fatalf("connections=%v", out.Connections)
				}
				for _, c := range out.Connections {
					if c.Since == "" || (c.Service == "slack" && c.Status != "expired") {
						t.Fatal("status/since missing")
					}
				}
			} else {
				if len(out.Connections) != 0 || len(out.Grants) != 1 || out.Grants[0].AppName != "CRM" || out.Grants[0].Space != "acme" || !reflect.DeepEqual(out.Grants[0].Can, []string{"send", "read replies"}) {
					t.Fatalf("accounts=%v", out)
				}
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "expired\"") && strings.Contains(string(raw), "token") {
				t.Fatal("accounts leaked token")
			}
		})
	}
}

func TestExportRuleAndHandler(t *testing.T) {
	for _, tt := range []struct {
		name, space, app, member string
		agent                    bool
		denied                   bool
	}{
		{name: "own desktop", space: "personal", member: svc.Owner},
		{name: "app", space: "personal", member: svc.Owner, app: "crm", denied: true},
		{name: "workspace server", space: "acme", member: svc.Owner, denied: true},
		{name: "other member", space: "personal", member: "mona", denied: true},
		{name: "agent", space: "personal", member: svc.Owner, agent: true, denied: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, f, _, _ := fixture(t, tt.space)
			scope := svc.Scope{App: tt.app, Caller: svc.Caller{Member: tt.member, Agent: tt.agent}}
			handled, ruleErr := ExportRule(scope, bridge.MethodConnectCredentialExport, nil)
			if !handled || (ruleErr != nil) != (tt.denied && tt.space != "acme") {
				t.Fatalf("rule handled=%v err=%v", handled, ruleErr)
			}
			out, err := s.ExportCredential(testCtx, scope, bridge.CredentialExportParams{Service: "gmail", App: "crm", Space: "acme"})
			if tt.denied {
				assertReason(t, err, bridge.CodeDenied, "")
				if f.reads != 0 {
					t.Fatal("denied export touched credentials")
				}
			} else if err != nil || out.Credential.Token != "personal-secret" {
				t.Fatal("export failed")
			}
		})
	}
	if handled, err := ExportRule(svc.Scope{}, "connect.accounts", nil); handled || err != nil {
		t.Fatal("rule intercepted unrelated method")
	}
}

func TestEncryptionAndReopen(t *testing.T) {
	s, _, _, _ := fixture(t, "acme")
	secret := "recognizable-token-no-plaintext"
	allow(t, s, "mona", "crm", "gmail", secret)
	var raw []byte
	if err := s.db.QueryRow(`SELECT credential FROM grant_credentials`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("database token is plaintext")
	}
	if _, err := s.unseal(raw, grantBinding("raj", "crm", "acme", "gmail")); err == nil {
		t.Fatal("ciphertext transplanted into another grant")
	}
	var journal string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal=%s err=%v", journal, err)
	}
	home := s.home
	all := s.all
	accounts := s.connections
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := NewWithOptions(svc.Config{Root: filepath.Join(home, "apps")}, all, Options{Space: "acme", Connections: accounts, Spaces: fakeSpaces{}})
	defer reopened.Close()
	reopened.now = func() time.Time { return testTime }
	cred, err := reopened.Resolve(testCtx, appScope("crm"), "gmail")
	if err != nil || cred.Token != secret {
		t.Fatal("persisted credential did not survive reopen")
	}
	for _, name := range []string{"connect-apps.db", ".connect-apps-key"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private %s mode/error %v", name, err)
		}
	}
	if err := reopened.ForgetMember(testCtx, "mona"); err != nil {
		t.Fatal(err)
	}
	if tokenCount(t, reopened) != 0 {
		t.Fatal("reopen lost foreign keys")
	}
	rawWire, _ := json.Marshal(cred)
	if bytes.Contains(rawWire, []byte(secret)) {
		t.Fatal("resolved credential serialized secret")
	}
}

func TestAllowSheetDesktopConnectionMetadata(t *testing.T) {
	for _, connected := range []bool{true, false} {
		s, f, _, _ := fixture(t, "acme")
		sheet, err := s.AllowSheet(testCtx, appScope("crm"), bridge.AllowSheetParams{Service: "gmail", Connected: &connected, Account: "mona@acme.com"})
		if err != nil || sheet.Missing == connected || sheet.Account != "mona@acme.com" || f.reads != 0 {
			t.Fatal("desktop metadata not respected")
		}
		_, err = s.Resolve(testCtx, appScope("crm"), "gmail")
		assertReason(t, err, bridge.CodeConflict, bridge.ReasonNeedsAllow)
	}
}

func TestAllowSheetNamesApprovedAction(t *testing.T) {
	s, _, _, _ := fixture(t, "personal")
	apps := s.all.Store.(fakeApps)
	apps.apps["crm"].Capabilities.Connectors["gmail"] = spec.Connector{As: "member", Can: []string{"read", "draft", "send"}, Approve: []string{"send"}}
	sheet, err := s.AllowSheet(testCtx, appScope("crm"), bridge.AllowSheetParams{Service: "gmail"})
	if err != nil || sheet.Title != "CRM may send email as Mona" {
		t.Fatalf("title=%s err=%v", sheet.Title, err)
	}
}
