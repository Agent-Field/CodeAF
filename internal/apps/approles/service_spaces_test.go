package approles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type auditRecorder struct {
	svc.Audit
	entries []bridge.AuditEntry
	err     error
}

func (a *auditRecorder) Append(_ context.Context, e bridge.AuditEntry) (bridge.AuditEntry, error) {
	if a.err != nil {
		return bridge.AuditEntry{}, a.err
	}
	a.entries = append(a.entries, e)
	return e, nil
}

type published struct {
	topic string
	data  any
}
type hubRecorder struct {
	svc.Hub
	events []published
}

func (h *hubRecorder) Publish(topic string, data any) {
	h.events = append(h.events, published{topic, data})
}

type rosterStore struct {
	svc.Members
	members []bridge.Member
}

func (m rosterStore) List(context.Context, svc.Caller) (bridge.MembersListResult, error) {
	return bridge.MembersListResult{Members: m.members}, nil
}

type appStore struct {
	svc.Store
	space string
	name  string
}

func (s appStore) Get(context.Context, string) (bridge.AppDetail, error) {
	return bridge.AppDetail{AppSummary: bridge.AppSummary{Name: s.name, Space: s.space}}, nil
}

func rolePtr(role svc.Role) *string { value := string(role); return &value }
func workspaceScope(role svc.Role) svc.Scope {
	return svc.Scope{App: "crm", Caller: svc.Caller{Member: "mona", Space: "acme", WS: svc.WorkspaceMember, Role: role}}
}
func newTestService(t *testing.T) (*Service, *auditRecorder, *hubRecorder) {
	t.Helper()
	audit, hub := &auditRecorder{}, &hubRecorder{}
	all := &svc.Services{Store: appStore{space: "acme", name: "CRM"}, Hub: hub}
	all.Audit = audit
	all.Members = rosterStore{members: []bridge.Member{{ID: "mona"}, {ID: "jm"}, {ID: "raj"}}}
	service := New(svc.Config{Root: t.TempDir()}, all)
	service.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	all.AppRoles = service
	t.Cleanup(func() { _ = service.Close() })
	return service, audit, hub
}
func requireCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestRolePrecedenceAndPersonal(t *testing.T) {
	for _, tt := range []struct {
		name   string
		seed   bool
		own    svc.Role
		member string
		space  string
		want   svc.Role
	}{
		{"workspace without rows", false, "", "jm", "acme", ""},
		{"personal context", false, "", "me", svc.PersonalSpace, svc.RoleEditor},
		{"own Editor", true, "", "mona", "acme", svc.RoleEditor},
		{"Everyone", true, "", "jm", "acme", svc.RoleUser},
		{"person overrides Everyone", true, svc.RoleViewer, "jm", "acme", svc.RoleViewer},
		{"Personal remains Editor", true, svc.RoleViewer, "jm", svc.PersonalSpace, svc.RoleEditor},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newTestService(t)
			ctx := svc.WithCaller(context.Background(), svc.Caller{Space: tt.space})
			if tt.seed {
				requireCode(t, s.Seed(ctx, "crm", "mona"), "")
			}
			if tt.own != "" {
				_, err := s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: tt.member, Role: rolePtr(tt.own)})
				requireCode(t, err, "")
			}
			got, err := s.Role(ctx, "crm", tt.member)
			requireCode(t, err, "")
			if got != tt.want {
				t.Fatalf("role=%s, want %s", got, tt.want)
			}
		})
	}
	t.Run("Everyone absent denies nonmaker", func(t *testing.T) {
		s, _, _ := newTestService(t)
		requireCode(t, s.Seed(context.Background(), "crm", "mona"), "")
		_, err := s.db.Exec(`DELETE FROM app_roles WHERE member = '*'`)
		requireCode(t, err, "")
		got, err := s.Role(context.Background(), "crm", "jm")
		requireCode(t, err, "")
		if got != "" {
			t.Fatal(got)
		}
	})
	t.Run("resolver context missing uses Store", func(t *testing.T) {
		s, _, _ := newTestService(t)
		s.all.Store = appStore{space: svc.PersonalSpace}
		got, err := s.Role(context.Background(), "crm", "me")
		requireCode(t, err, "")
		if got != svc.RoleEditor {
			t.Fatal(got)
		}
	})
}

func TestSetFallbackListAuditAndPublication(t *testing.T) {
	ctx := context.Background()
	s, audit, hub := newTestService(t)
	requireCode(t, s.Seed(ctx, "crm", "mona"), "")
	scope := workspaceScope(svc.RoleEditor)
	members, err := s.Set(ctx, scope, bridge.AppRoleSetParams{Member: "jm", Role: rolePtr(svc.RoleViewer)})
	requireCode(t, err, "")
	want := bridge.AppMembers{Everyone: "user", People: []bridge.AppMember{
		{Member: "jm", Role: "viewer", From: "set", SetBy: "mona", At: timePtr(s.now())},
		{Member: "mona", Role: "editor", From: "set", SetBy: "mona", At: timePtr(s.now()), Maker: true},
		{Member: "raj", Role: "user", From: "everyone"},
	}}
	if !reflect.DeepEqual(members, want) {
		t.Fatalf("members=%+v want=%+v", members, want)
	}
	if len(hub.events) != 1 || hub.events[0].topic != "app/crm/roles" || !reflect.DeepEqual(hub.events[0].data, members) {
		t.Fatalf("events=%+v", hub.events)
	}
	entry := audit.entries[0]
	if entry.Activity.Text != "@mona made @jm Viewer of CRM" || entry.Action != bridge.MethodAppRolesSet || entry.Space != "acme" {
		t.Fatalf("audit=%+v", entry)
	}
	got, err := s.Role(ctx, "crm", "jm")
	requireCode(t, err, "")
	if got != svc.RoleViewer {
		t.Fatal(got)
	}
	members, err = s.Set(ctx, scope, bridge.AppRoleSetParams{Member: "jm"})
	requireCode(t, err, "")
	if members.People[0].Role != "user" || members.People[0].From != "everyone" || members.People[0].SetBy != "" {
		t.Fatal(members)
	}
	got, err = s.Role(ctx, "crm", "jm")
	requireCode(t, err, "")
	if got != svc.RoleUser {
		t.Fatal(got)
	}
	// Seeding twice keeps the original maker and every membership decision.
	_, err = s.Set(ctx, scope, bridge.AppRoleSetParams{Member: "raj", Role: rolePtr(svc.RoleViewer)})
	requireCode(t, err, "")
	requireCode(t, s.Seed(ctx, "crm", "raj"), "")
	got, err = s.Role(ctx, "crm", "raj")
	requireCode(t, err, "")
	if got != svc.RoleViewer {
		t.Fatal(got)
	}
}
func timePtr(at time.Time) *time.Time { return &at }

func TestLastEditorAndSetPermissions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		caller svc.Caller
		member string
		role   *string
		code   bridge.ErrorCode
	}{
		{"demote last", workspaceScope(svc.RoleEditor).Caller, "mona", rolePtr(svc.RoleUser), bridge.CodeConflict},
		{"remove last", workspaceScope(svc.RoleEditor).Caller, "mona", nil, bridge.CodeConflict},
		{"add Editor", workspaceScope(svc.RoleEditor).Caller, "jm", rolePtr(svc.RoleEditor), ""},
		{"viewer denied", workspaceScope(svc.RoleViewer).Caller, "jm", rolePtr(svc.RoleUser), bridge.CodeDenied},
		{"user denied", workspaceScope(svc.RoleUser).Caller, "jm", rolePtr(svc.RoleViewer), bridge.CodeDenied},
		{"owner repair", svc.Caller{Member: "owner", Space: "acme", WS: svc.WorkspaceOwner}, "jm", rolePtr(svc.RoleEditor), ""},
		{"admin repair", svc.Caller{Member: "admin", Space: "acme", WS: svc.WorkspaceAdmin}, "jm", rolePtr(svc.RoleEditor), ""},
		{"agent denied", svc.Caller{Member: "mona", Space: "acme", Role: svc.RoleEditor, WS: svc.WorkspaceOwner, Agent: true}, "jm", rolePtr(svc.RoleUser), bridge.CodeDenied},
		{"Everyone cannot be removed", workspaceScope(svc.RoleEditor).Caller, "*", nil, bridge.CodeInvalid},
		{"invalid role", workspaceScope(svc.RoleEditor).Caller, "jm", rolePtr("owner"), bridge.CodeInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, audit, hub := newTestService(t)
			ctx := context.Background()
			requireCode(t, s.Seed(ctx, "crm", "mona"), "")
			_, err := s.Set(ctx, svc.Scope{App: "crm", Caller: tt.caller}, bridge.AppRoleSetParams{Member: tt.member, Role: tt.role})
			requireCode(t, err, tt.code)
			if tt.code == bridge.CodeConflict && err.(*bridge.Error).Message != "CRM needs at least one Editor" {
				t.Fatal(err)
			}
			if tt.code != "" {
				if len(audit.entries) != 0 || len(hub.events) != 0 {
					t.Fatal("failed edit was published")
				}
				role, err := s.Role(ctx, "crm", "mona")
				requireCode(t, err, "")
				if role != svc.RoleEditor {
					t.Fatal(role)
				}
			}
		})
	}
	t.Run("Everyone Editor counts and maker survives fallback", func(t *testing.T) {
		s, _, _ := newTestService(t)
		ctx := context.Background()
		requireCode(t, s.Seed(ctx, "crm", "mona"), "")
		_, err := s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: "*", Role: rolePtr(svc.RoleEditor)})
		requireCode(t, err, "")
		members, err := s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: "mona"})
		requireCode(t, err, "")
		if members.People[1].Member != "mona" || !members.People[1].Maker || members.People[1].From != "everyone" {
			t.Fatal(members)
		}
		_, err = s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: "*", Role: rolePtr(svc.RoleUser)})
		requireCode(t, err, bridge.CodeConflict)
	})
}

func TestDBSelectionAndPersistence(t *testing.T) {
	for _, mode := range []string{"fallback", "existing space", "option"} {
		t.Run(mode, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "apps")
			want := filepath.Join(root, "approles.db")
			options := Options{}
			if mode == "existing space" {
				want = filepath.Join(root, "..", "space.db")
				db, err := sql.Open("sqlite", want)
				requireCode(t, err, "")
				_, err = db.Exec("CREATE TABLE members (id TEXT)")
				requireCode(t, err, "")
				requireCode(t, db.Close(), "")
			}
			if mode == "option" {
				want = filepath.Join(root, "..", "workspace.db")
				options.DBPath = want
			}
			s := NewWithOptions(svc.Config{Root: root}, nil, options)
			requireCode(t, s.Seed(context.Background(), "crm", "mona"), "")
			if filepath.Clean(s.path) != filepath.Clean(want) {
				t.Fatalf("path=%s want=%s", s.path, want)
			}
			requireCode(t, s.Close(), "")
			s = NewWithOptions(svc.Config{Root: root}, nil, options)
			t.Cleanup(func() { _ = s.Close() })
			role, err := s.Role(context.Background(), "crm", "mona")
			requireCode(t, err, "")
			if role != svc.RoleEditor {
				t.Fatal(role)
			}
		})
	}
}

func TestConcurrentLastEditorAcrossConnections(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	requireCode(t, s.Seed(ctx, "crm", "mona"), "")
	_, err := s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: "jm", Role: rolePtr(svc.RoleEditor)})
	requireCode(t, err, "")
	// Use independent peers as well as independent connections to avoid sharing test recorders.
	other, audit, hub := newTestService(t)
	other.path = s.path
	other.all.Audit, other.all.Hub = audit, hub
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pair := range []struct {
		s      *Service
		member string
	}{{s, "mona"}, {other, "jm"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := pair.s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: pair.member, Role: rolePtr(svc.RoleUser)})
			errorsOut <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsOut)
	conflicts := 0
	for err := range errorsOut {
		if err != nil {
			requireCode(t, err, bridge.CodeConflict)
			conflicts++
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts=%d, want 1", conflicts)
	}
}

func TestAskEditorAuditInboxAndWaiting(t *testing.T) {
	for _, role := range []svc.Role{svc.RoleUser, svc.RoleViewer, svc.RoleEditor, ""} {
		t.Run(string(role), func(t *testing.T) {
			s, audit, hub := newTestService(t)
			ctx := context.Background()
			requireCode(t, s.Seed(ctx, "crm", "mona"), "")
			scope := workspaceScope(role)
			scope.Caller.Member = "raj"
			_, err := s.AskEditor(ctx, scope, bridge.AskEditorParams{Kind: "shape", Text: "add a column for last reply"})
			if role != svc.RoleUser && role != svc.RoleViewer {
				requireCode(t, err, bridge.CodeDenied)
				return
			}
			requireCode(t, err, "")
			if len(audit.entries) != 1 || audit.entries[0].Action != bridge.MethodAppRolesAsk {
				t.Fatal(audit.entries)
			}
			var request EditorRequest
			requireCode(t, json.Unmarshal(audit.entries[0].Detail, &request), "")
			if request.Member != "mona" || request.From != "raj" || request.Kind != "shape" {
				t.Fatal(request)
			}
			if audit.entries[0].Activity.Text != `@raj asks @mona to shape CRM "add a column for last reply"` {
				t.Fatal(audit.entries[0].Activity)
			}
			if !reflect.DeepEqual(hub.events, []published{{"space/waiting", Waiting{Member: "mona", Count: 1}}}) {
				t.Fatal(hub.events)
			}
		})
	}
	t.Run("last shaper is an Editor", func(t *testing.T) {
		members := bridge.AppMembers{People: []bridge.AppMember{{Member: "jm", Role: "editor"}, {Member: "mona", Role: "editor"}, {Member: "raj", Role: "user"}}}
		if chooseEditor(members, "mona") != "mona" || chooseEditor(members, "raj") != "jm" {
			t.Fatal("wrong Editor")
		}
	})
	t.Run("audit failure does not notify", func(t *testing.T) {
		s, audit, hub := newTestService(t)
		requireCode(t, s.Seed(context.Background(), "crm", "mona"), "")
		audit.err = errors.New("audit unavailable")
		_, err := s.AskEditor(context.Background(), workspaceScope(svc.RoleUser), bridge.AskEditorParams{Kind: "role", Text: "make me an Editor"})
		if !errors.Is(err, audit.err) || len(hub.events) != 0 {
			t.Fatal(err, hub.events)
		}
	})
}

func TestAuditFailureStillPublishesCommittedRole(t *testing.T) {
	s, audit, hub := newTestService(t)
	ctx := context.Background()
	requireCode(t, s.Seed(ctx, "crm", "mona"), "")
	audit.err = errors.New("audit unavailable")
	_, err := s.Set(ctx, workspaceScope(svc.RoleEditor), bridge.AppRoleSetParams{Member: "jm", Role: rolePtr(svc.RoleViewer)})
	if !errors.Is(err, audit.err) || len(hub.events) != 1 {
		t.Fatal(err, hub.events)
	}
	role, err := s.Role(ctx, "crm", "jm")
	requireCode(t, err, "")
	if role != svc.RoleViewer {
		t.Fatal(role)
	}
}
