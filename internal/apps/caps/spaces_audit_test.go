package caps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/audit"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestSpacesAuditDecisions(t *testing.T) {
	for _, tt := range []struct {
		name, method, params, decision, object string
		role                                   svc.Role
		ws                                     svc.WorkspaceRole
	}{
		{"deny write", bridge.MethodRecordsCreate, `{"type":"deal","set":{"name":"private prompt"}}`, "deny", "", svc.RoleViewer, svc.WorkspaceMember},
		{"allow write", bridge.MethodRecordsUpdate, `{"type":"deal","id":"d1","set":{"name":"private prompt"}}`, "allow", "deal/d1", svc.RoleUser, svc.WorkspaceMember},
		{"denied read", bridge.MethodRecordsQuery, `{"type":"deal"}`, "deny", "", "", svc.WorkspaceMember},
		{"deny audit", bridge.MethodAuditList, `{}`, "deny", "", svc.RoleUser, svc.WorkspaceMember},
		{"member role", bridge.MethodMembersRole, `{"member":"raj","role":"member"}`, "allow", "member/raj", svc.RoleEditor, svc.WorkspaceOwner},
		{"approval", bridge.MethodApprovalsDecide, `{"approval_id":"a1","answer":"no"}`, "allow", "approval/a1", svc.RoleUser, svc.WorkspaceMember},
		{"reveal", "secret.reveal", `{"id":"s1"}`, "allow", "s1", svc.RoleUser, svc.WorkspaceMember},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			bundle := testBundle(t)
			bundle.App.Capabilities.Tools = append(bundle.App.Capabilities.Tools, "secret.reveal")
			all := &svc.Services{Store: definitionStore{bundle: bundle}}
			log := audit.New(svc.Config{Root: root}, all)
			t.Cleanup(func() { _ = log.Close() })
			all.Audit = log
			checker := New(svc.Config{Root: root}, all)
			caller := svc.Caller{Member: "mona", Space: "acme", WS: tt.ws, Role: tt.role, Agent: tt.method == bridge.MethodRecordsUpdate}
			err := checker.Allow(ctx, svc.Scope{App: "crm", Caller: caller}, tt.method, json.RawMessage(tt.params))
			if (err != nil) != (tt.decision == "deny") {
				t.Fatalf("Allow error=%v", err)
			}
			page, err := log.List(ctx, svc.Scope{Caller: svc.Caller{Member: "owner", Space: "acme", WS: svc.WorkspaceOwner}}, bridge.AuditListParams{})
			if err != nil || len(page.Entries) != 1 {
				t.Fatalf("page=%+v err=%v", page, err)
			}
			e := page.Entries[0]
			if e.Action != tt.method || e.Decision != tt.decision || e.Object != tt.object || e.Member != "mona" || e.App != "crm" || e.Agent != caller.Agent || e.Activity != nil {
				t.Fatalf("entry=%+v", e)
			}
			raw, _ := json.Marshal(e)
			if strings.Contains(string(raw), "private prompt") || len(e.Detail) != 0 {
				t.Fatalf("content leaked: %s", raw)
			}
			if _, err := os.Stat(filepath.Join(root, "audit.jsonl")); !os.IsNotExist(err) {
				t.Fatalf("unexpected JSONL audit: %v", err)
			}
		})
	}
}

func TestSpacesAllowedReadsDoNotAppend(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	all := &svc.Services{Store: definitionStore{bundle: testBundle(t)}}
	log := audit.New(svc.Config{Root: root}, all)
	defer log.Close()
	all.Audit = log
	checker := New(svc.Config{Root: root}, all)
	scope := svc.Scope{App: "crm", Caller: svc.PersonalCaller()}
	for _, tt := range []struct{ method, params string }{
		{bridge.MethodRecordsGet, `{"type":"deal","id":"d1"}`},
		{bridge.MethodRecordsQuery, `{"type":"deal"}`},
		{bridge.MethodRunsGet, `{"run_id":"r1"}`},
		{bridge.MethodRunsList, `{}`}, {bridge.MethodApprovalsList, `{}`},
		{bridge.MethodAppsGet, `{}`}, {bridge.MethodAppsList, `{}`},
		{bridge.MethodAppRolesList, `{}`}, {bridge.MethodSignoffState, `{}`},
		{"sub", `{"topic":"app/crm/records/deal"}`},
		{"unsub", `{"topic":"app/crm/records/deal"}`},
	} {
		t.Run(tt.method, func(t *testing.T) {
			if err := checker.Allow(ctx, scope, tt.method, json.RawMessage(tt.params)); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "audit.db")); !os.IsNotExist(err) {
		t.Fatalf("allowed reads opened storage: %v", err)
	}
	page, err := log.List(ctx, scope, bridge.AuditListParams{})
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("read audit=%+v err=%v", page, err)
	}
}

func TestSpacesAuditCanceledDenial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	all := &svc.Services{}
	log := audit.New(svc.Config{Root: t.TempDir()}, all)
	defer log.Close()
	all.Audit = log
	checker := New(svc.Config{}, all)
	scope := svc.Scope{App: "crm", Caller: svc.PersonalCaller()}
	if err := checker.Allow(ctx, scope, bridge.MethodRecordsGet, nil); err == nil {
		t.Fatal("canceled call allowed")
	}
	page, err := log.List(context.Background(), scope, bridge.AuditListParams{})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Decision != "deny" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}

func TestSpacesAuditObjects(t *testing.T) {
	for _, tt := range []struct{ method, params, want string }{
		{"records.delete", `{"type":"deal","id":"d1"}`, "deal/d1"},
		{"records.delete", `{"type":"deal","ids":["d1","d2"]}`, ""},
		{"member.remove", `{"member":"raj"}`, "member/raj"},
		{"app.restore", `{"version":"v12"}`, "v12"},
		{"runs.start", `{"run":"follow-up"}`, "run/follow-up"},
		{"trigger.pause", `{"trigger":"quiet"}`, "trigger/quiet"},
		{"records.update", `{"type":1,"id":2}`, ""},
		{"agent.send", `{"text":"private prompt"}`, ""},
		{"records.create", `bad json`, ""},
	} {
		t.Run(tt.method+tt.params, func(t *testing.T) {
			if got := auditObject(tt.method, json.RawMessage(tt.params)); got != tt.want {
				t.Fatalf("object=%q want=%q", got, tt.want)
			}
		})
	}
}
