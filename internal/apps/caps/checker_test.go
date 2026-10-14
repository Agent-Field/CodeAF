package caps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/audit"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type definitionStore struct {
	svc.Store
	bundle *spec.Bundle
	err    error
}

func (s definitionStore) Definition(context.Context, string) (*spec.Bundle, error) {
	return s.bundle, s.err
}

func testBundle(t *testing.T) *spec.Bundle {
	t.Helper()
	files, _ := spec.GoldenCRM()
	b, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAllow(t *testing.T) {
	tests := []struct {
		name, method, raw string
		role              svc.Role
		agent             bool
		change            func(*spec.Bundle)
		code              bridge.ErrorCode
	}{
		{name: "editor write", method: bridge.MethodRecordsUpdate, raw: `{"type":"deal"}`, role: svc.RoleEditor},
		{name: "user write", method: bridge.MethodRecordsCreate, raw: `{"type":"deal"}`, role: svc.RoleUser},
		{name: "viewer read", method: bridge.MethodRecordsGet, raw: `{"type":"deal"}`, role: svc.RoleViewer},
		{name: "viewer write", method: bridge.MethodRecordsDelete, raw: `{"type":"deal"}`, role: svc.RoleViewer, code: bridge.CodeDenied},
		{name: "invalid role", method: bridge.MethodAppsList, role: "", code: bridge.CodeDenied},
		{name: "user definition", method: bridge.MethodBuilderWrite, role: svc.RoleUser, code: bridge.CodeDenied},
		{name: "agent definition", method: bridge.MethodAppsKeep, role: svc.RoleEditor, agent: true, code: bridge.CodeDenied},
		{name: "agent content", method: bridge.MethodRecordsQuery, raw: `{"type":"deal"}`, role: svc.RoleUser, agent: true},
		{name: "missing capability", method: bridge.MethodRecordsQuery, raw: `{"type":"deal"}`, role: svc.RoleEditor, change: func(b *spec.Bundle) { delete(b.App.Capabilities.Records, "deal") }, code: bridge.CodeDenied},
		{name: "unknown record type", method: bridge.MethodRecordsQuery, raw: `{"type":"secret"}`, role: svc.RoleEditor, code: bridge.CodeDenied},
		{name: "read only write", method: bridge.MethodRecordsUpdate, raw: `{"type":"deal"}`, role: svc.RoleEditor, change: func(b *spec.Bundle) { b.App.Capabilities.Records["deal"] = spec.AccessRead }, code: bridge.CodeDenied},
		{name: "read only query", method: bridge.MethodRecordsQuery, raw: `{"type":"deal"}`, role: svc.RoleEditor, change: func(b *spec.Bundle) { b.App.Capabilities.Records["deal"] = spec.AccessRead }},
		{name: "invalid record type", method: bridge.MethodRecordsQuery, raw: `{"type":2}`, role: svc.RoleEditor, code: bridge.CodeInvalid},
		{name: "missing record type", method: bridge.MethodRecordsQuery, raw: `{}`, role: svc.RoleEditor, code: bridge.CodeInvalid},
		{name: "non object", method: bridge.MethodRecordsQuery, raw: `[]`, role: svc.RoleEditor, code: bridge.CodeInvalid},
		{name: "connector declared", method: "gmail.draft", role: svc.RoleUser},
		{name: "connector missing", method: "slack.post", role: svc.RoleUser, code: bridge.CodeDenied},
		{name: "connector verb missing", method: "gmail.delete", role: svc.RoleUser, code: bridge.CodeDenied},
		{name: "ai declared", method: "ai.sort", role: svc.RoleUser},
		{name: "ai missing", method: "ai.extract", role: svc.RoleUser, code: bridge.CodeDenied},
		{name: "run declared", method: bridge.MethodRunsStart, raw: `{"run":"follow-up"}`, role: svc.RoleUser},
		{name: "run missing", method: bridge.MethodRunsStart, raw: `{"run":"secret"}`, role: svc.RoleUser, code: bridge.CodeNotFound},
		{name: "subscription read", method: "sub", raw: `{"topic":"app/crm/records/deal"}`, role: svc.RoleViewer},
		{name: "subscription missing capability", method: "sub", raw: `{"topic":"app/crm/records/deal"}`, role: svc.RoleViewer, change: func(b *spec.Bundle) { delete(b.App.Capabilities.Records, "deal") }, code: bridge.CodeDenied},
		{name: "record wildcard", method: "sub", raw: `{"topic":"app/crm/records/*"}`, role: svc.RoleViewer},
		{name: "wildcard leak", method: "sub", raw: `{"topic":"app/crm/*"}`, role: svc.RoleViewer, change: func(b *spec.Bundle) { delete(b.App.Capabilities.Records, "contact") }, code: bridge.CodeDenied},
		{name: "cross app topic", method: "sub", raw: `{"topic":"app/other/build"}`, role: svc.RoleEditor, code: bridge.CodeInvalid},
		{name: "unknown topic", method: "sub", raw: `{"topic":"app/crm/secret"}`, role: svc.RoleEditor, code: bridge.CodeInvalid},
		{name: "embedded wildcard", method: "sub", raw: `{"topic":"app/crm/records/d*"}`, role: svc.RoleEditor, code: bridge.CodeInvalid},
		{name: "unsubscribe checked", method: "unsub", raw: `{"topic":"app/crm/records/secret"}`, role: svc.RoleEditor, code: bridge.CodeDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBundle(t)
			if tt.change != nil {
				tt.change(b)
			}
			all := &svc.Services{Store: definitionStore{bundle: b}}
			checker := New(svc.Config{Root: t.TempDir()}, all)
			err := checker.Allow(context.Background(), svc.Scope{App: "crm", Caller: svc.Caller{Member: svc.Owner, Role: tt.role, Agent: tt.agent}}, tt.method, json.RawMessage(tt.raw))
			if tt.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			typed, ok := err.(*bridge.Error)
			if !ok || typed.Code != tt.code {
				t.Fatalf("error = %v, want %s", err, tt.code)
			}
		})
	}
}

func TestPlanningDoesNotRequireAnAppDefinition(t *testing.T) {
	all := &svc.Services{Store: definitionStore{err: bridge.Errorf(bridge.CodeNotFound, "app.json not written")}}
	checker := New(svc.Config{Root: t.TempDir()}, all)
	for _, method := range []string{bridge.MethodAppsCreate, bridge.MethodAppsGet, bridge.MethodBuilderPlanEdit, bridge.MethodBuilderWrite, bridge.MethodBuilderCancel} {
		if err := checker.Allow(context.Background(), svc.Scope{App: "draft", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}}, method, nil); err != nil {
			t.Fatal(method, err)
		}
	}
}

func TestDenialAppendsAuditWithoutContent(t *testing.T) {
	root := t.TempDir()
	all := &svc.Services{Store: store.New(svc.Config{}, nil)}
	checker := New(svc.Config{Root: root}, all)
	t.Cleanup(func() { _ = checker.audit.fallback.Close() })
	scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleViewer}}
	for range 2 {
		if err := checker.Allow(context.Background(), scope, bridge.MethodRecordsCreate, json.RawMessage(`{"type":"deal","set":{"name":"private prompt"}}`)); err == nil {
			t.Fatal("viewer write allowed")
		}
	}
	// Read through a separate service to prove the standalone checker persisted both denials.
	log := audit.New(svc.Config{Root: root}, all)
	t.Cleanup(func() { _ = log.Close() })
	page, err := log.List(context.Background(), svc.Scope{Caller: svc.PersonalCaller()}, bridge.AuditListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("audit = %+v", page)
	}
	for i, entry := range page.Entries {
		if entry.App != "crm" || entry.Member != svc.Owner || entry.Decision != "deny" || entry.Action != bridge.MethodRecordsCreate || entry.Seq != int64(2-i) || entry.At.IsZero() {
			t.Fatalf("entry = %+v", entry)
		}
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private prompt") || len(entry.Detail) != 0 {
			t.Fatalf("content leaked: %s", data)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "audit.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("unexpected JSONL audit: %v", err)
	}
}

func TestNeedsIsComputedAndStable(t *testing.T) {
	app := testBundle(t).App
	first := Needs(app)
	second := Needs(app)
	if !reflect.DeepEqual(first, second) || len(first) != 8 {
		t.Fatalf("needs = %+v", first)
	}
	if first[0].Line != "Read and write company records" || first[4].Kind != "approval" {
		t.Fatal(first)
	}
	if len(Needs(nil)) != 0 {
		t.Fatal("nil definition must have no needs")
	}
}
