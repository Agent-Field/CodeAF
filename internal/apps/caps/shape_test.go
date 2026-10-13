package caps_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	_ "github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestShapeRoles(t *testing.T) {
	methods := []string{
		bridge.MethodShapeOpen, bridge.MethodShapeGet, bridge.MethodShapeRequest, bridge.MethodShapePlanEdit,
		bridge.MethodShapeChangeFix, bridge.MethodShapeChangeCancel, bridge.MethodShapeAnswer, bridge.MethodShapeDiff,
		bridge.MethodShapePreview, bridge.MethodShapeApply, bridge.MethodShapeDiscard, bridge.MethodShapeReplayDismiss,
		bridge.MethodShapeAsk, bridge.MethodVersionsList, bridge.MethodVersionsDiff, bridge.MethodVersionsRestore, bridge.MethodAppsHealth,
	}
	checker := caps.New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer, ""} {
		for _, method := range methods {
			t.Run(string(role)+"/"+method, func(t *testing.T) {
				scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: role}}
				err := checker.Allow(context.Background(), scope, method, json.RawMessage(`{}`))
				read := method == bridge.MethodVersionsList || method == bridge.MethodVersionsDiff || method == bridge.MethodAppsHealth
				allowed := role != "" && (read || (method == bridge.MethodShapeAsk && role != svc.RoleEditor) || (method != bridge.MethodShapeAsk && role == svc.RoleEditor))
				if allowed && err != nil {
					t.Fatal(err)
				}
				if !allowed {
					requireShapeCode(t, err, bridge.CodeDenied)
				}
			})
		}
	}
}

func TestShapeSubscriptionPrivacy(t *testing.T) {
	checker := caps.New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, tt := range []struct {
		topic string
		code  bridge.ErrorCode
	}{
		{"app/crm/shape/raj", ""}, {"app/crm/shape/me", bridge.CodeDenied},
		{"app/crm/shape/*", bridge.CodeInvalid}, {"app/crm/*", bridge.CodeDenied},
		{"app/crm/shape/raj/extra", bridge.CodeInvalid}, {"app/other/shape/raj", bridge.CodeInvalid},
		{"app/crm/version", ""}, {"app/crm/health", ""}, {"app/crm/version/*", bridge.CodeInvalid},
	} {
		for _, method := range []string{"sub", "unsub"} {
			t.Run(method+"/"+tt.topic, func(t *testing.T) {
				scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: svc.RoleViewer}}
				raw, _ := json.Marshal(map[string]string{"topic": tt.topic})
				err := checker.Allow(context.Background(), scope, method, raw)
				if tt.code == "" {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				requireShapeCode(t, err, tt.code)
			})
		}
	}
}

func TestDraftAgentAndShapeValidation(t *testing.T) {
	checker := caps.New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "me", Role: svc.RoleEditor}, Data: svc.DataDraft}
	requireShapeCode(t, checker.Allow(context.Background(), scope, bridge.MethodAgentSend, nil), bridge.CodeDenied)
	for _, raw := range []string{`[]`, `"text"`, `{`, `42`} {
		requireShapeCode(t, checker.Allow(context.Background(), scope, bridge.MethodShapeOpen, json.RawMessage(raw)), bridge.CodeInvalid)
	}
	scope.Caller.Agent = true
	requireShapeCode(t, checker.Allow(context.Background(), scope, bridge.MethodShapeOpen, nil), bridge.CodeDenied)
	scope.Caller.Agent = false
	scope.Caller.Member = ""
	requireShapeCode(t, checker.Allow(context.Background(), scope, bridge.MethodVersionsList, nil), bridge.CodeDenied)
	scope.Caller.Member = "me"
	scope.App = ""
	requireShapeCode(t, checker.Allow(context.Background(), scope, bridge.MethodVersionsList, nil), bridge.CodeInvalid)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checker.Allow(ctx, scope, bridge.MethodVersionsList, nil); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func requireShapeCode(t *testing.T, err error, code bridge.ErrorCode) {
	t.Helper()
	typed, ok := err.(*bridge.Error)
	if !ok || typed.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}
