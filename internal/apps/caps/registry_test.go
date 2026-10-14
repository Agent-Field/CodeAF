package caps

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func resetPermissions(t *testing.T) {
	t.Helper()
	permissions.Lock()
	methods, topics, denies := permissions.methods, permissions.topics, permissions.denies
	permissions.methods, permissions.topics, permissions.denies = map[string]AllowFunc{}, nil, nil
	permissions.Unlock()
	t.Cleanup(func() {
		permissions.Lock()
		permissions.methods, permissions.topics, permissions.denies = methods, topics, denies
		permissions.Unlock()
	})
}

func TestWaveOneVerdictsWithoutExtensions(t *testing.T) {
	resetPermissions(t)
	checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{Store: definitionStore{bundle: testBundle(t)}})
	for _, method := range []string{
		bridge.MethodAppsList, bridge.MethodAppsCreate, bridge.MethodAppsGet, bridge.MethodAppsKeep, bridge.MethodAppsDiscard,
		bridge.MethodBuilderPlanEdit, bridge.MethodBuilderWrite, bridge.MethodBuilderCancel,
		bridge.MethodRecordsQuery, bridge.MethodRecordsGet, bridge.MethodRecordsCreate, bridge.MethodRecordsUpdate, bridge.MethodRecordsDelete,
		bridge.MethodRunsStart, bridge.MethodRunsGet, bridge.MethodRunsList, bridge.MethodApprovalsList, bridge.MethodApprovalsDecide, bridge.MethodAgentSend,
		"sub", "unsub",
	} {
		for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer, ""} {
			for _, agent := range []bool{false, true} {
				t.Run(method+"/"+string(role)+"/agent="+map[bool]string{true: "true", false: "false"}[agent], func(t *testing.T) {
					scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: role, Agent: agent}}
					raw := json.RawMessage(`{"type":"deal","run":"follow-up","topic":"app/crm/records/deal"}`)
					allowed := role == svc.RoleEditor || role == svc.RoleUser && !definitionMethod(method)
					if role == svc.RoleViewer {
						switch method {
						case bridge.MethodAppsList, bridge.MethodAppsGet, bridge.MethodRecordsQuery, bridge.MethodRecordsGet,
							bridge.MethodRunsGet, bridge.MethodRunsList, bridge.MethodApprovalsList, "sub", "unsub":
							allowed = true
						}
					}
					if agent && (definitionMethod(method) || method == bridge.MethodAppsList || method == bridge.MethodAppsGet) {
						allowed = false
					}
					err := checker.Allow(context.Background(), scope, method, raw)
					if allowed && err != nil {
						t.Fatal(err)
					}
					if !allowed && errorCode(err) != bridge.CodeDenied {
						t.Fatalf("got %v, want denied", err)
					}
				})
			}
		}
	}
}

func TestDenyRulesCannotGrantAndRunInOrder(t *testing.T) {
	for _, tt := range []struct {
		name   string
		role   svc.Role
		denyAt int
		want   []int
		code   bridge.ErrorCode
	}{
		{"deny wave one", svc.RoleEditor, 2, []int{1, 2}, bridge.CodeDenied},
		{"nil cannot grant", svc.RoleViewer, 0, []int{1, 2, 3}, bridge.CodeDenied},
		{"all nil retain allow", svc.RoleEditor, 0, []int{1, 2, 3}, "allowed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetPermissions(t)
			var seen []int
			for n := 1; n <= 3; n++ {
				AddDenyRule(func(_ context.Context, caller svc.Caller, scope svc.Scope, method string, raw json.RawMessage) error {
					if caller != scope.Caller || method != bridge.MethodRecordsUpdate || string(raw) != `{"type":"deal"}` {
						t.Fatal("rule received wrong call")
					}
					seen = append(seen, n)
					if n == tt.denyAt {
						return denied("cross-cutting limit")
					}
					return nil
				})
			}
			checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{Store: definitionStore{bundle: testBundle(t)}})
			scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: tt.role}}
			err := checker.Allow(context.Background(), scope, bridge.MethodRecordsUpdate, json.RawMessage(`{"type":"deal"}`))
			if errorCode(err) != tt.code || !reflect.DeepEqual(seen, tt.want) {
				t.Fatalf("got %v, order %v", err, seen)
			}
		})
	}
}

func TestRegisteredAllowOwnsOnlyItsMethod(t *testing.T) {
	resetPermissions(t)
	called := 0
	RegisterAllow(bridge.MethodShapeOpen, func(_ context.Context, caller svc.Caller, scope svc.Scope, _ json.RawMessage) error {
		if caller != scope.Caller {
			t.Fatal("caller mismatch")
		}
		called++
		return nil
	})
	checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{Store: definitionStore{bundle: testBundle(t)}})
	scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: svc.RoleViewer}}
	for _, tt := range []struct {
		method string
		code   bridge.ErrorCode
		calls  int
	}{
		{bridge.MethodShapeOpen, "allowed", 1}, {bridge.MethodRecordsUpdate, bridge.CodeDenied, 1}, {"shape.unknown", bridge.CodeDenied, 1},
	} {
		t.Run(tt.method, func(t *testing.T) {
			err := checker.Allow(context.Background(), scope, tt.method, json.RawMessage(`{"type":"deal"}`))
			if errorCode(err) != tt.code || called != tt.calls {
				t.Fatalf("error=%v allow calls=%d", err, called)
			}
		})
	}
	AddDenyRule(func(context.Context, svc.Caller, svc.Scope, string, json.RawMessage) error { return denied("limit") })
	if err := checker.Allow(context.Background(), scope, bridge.MethodShapeOpen, nil); errorCode(err) != bridge.CodeDenied || called != 1 {
		t.Fatalf("deny did not precede Allow: %v, %d", err, called)
	}
}

func TestGenericChecksPrecedeExtensionsAndAuditOnce(t *testing.T) {
	for _, tt := range []struct {
		name, raw           string
		noMember, cancelled bool
		code                bridge.ErrorCode
	}{
		{"member", `{}`, true, false, bridge.CodeDenied}, {"params", `[]`, false, false, bridge.CodeInvalid}, {"context", `{}`, false, true, bridge.CodeInternal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetPermissions(t)
			fail := func() { t.Fatal("extension ran before generic checks") }
			RegisterAllow("track.method", func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { fail(); return nil })
			AddDenyRule(func(context.Context, svc.Caller, svc.Scope, string, json.RawMessage) error { fail(); return nil })
			root := t.TempDir()
			checker := New(svc.Config{Root: root}, nil)
			t.Cleanup(func() { _ = checker.audit.fallback.Close() })
			scope := svc.Scope{Caller: svc.Caller{Member: "raj", Role: svc.RoleEditor}}
			if tt.noMember {
				scope.Caller.Member = ""
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			err := checker.Allow(ctx, scope, "track.method", json.RawMessage(tt.raw))
			if errorCode(err) != tt.code {
				t.Fatal(err)
			}
			page, err := checker.audit.fallback.List(context.Background(), svc.Scope{Caller: svc.PersonalCaller()}, bridge.AuditListParams{})
			if err != nil || len(page.Entries) != 1 {
				t.Fatalf("audit=%+v error=%v", page, err)
			}
			entry := page.Entries[0]
			if entry.Action != "track.method" || entry.Decision != "deny" || entry.Member != scope.Caller.Member {
				t.Fatalf("audit entry=%+v", entry)
			}
		})
	}
}

func TestRegisteredAllowCanDeny(t *testing.T) {
	resetPermissions(t)
	RegisterAllow("track.method", func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { return denied("track limit") })
	checker := New(svc.Config{Root: t.TempDir()}, nil)
	err := checker.Allow(context.Background(), svc.Scope{Caller: svc.Caller{Member: "raj", Role: svc.RoleEditor}}, "track.method", nil)
	if errorCode(err) != bridge.CodeDenied {
		t.Fatal(err)
	}
}

func TestRegisterAllowCannotReplaceReservedMethod(t *testing.T) {
	resetPermissions(t)
	RegisterAllow(bridge.MethodRecordsUpdate, nil)
	defer func() {
		if got := recover(); got != "caps: method records.update registered twice" {
			t.Fatalf("panic=%v", got)
		}
	}()
	RegisterAllow(bridge.MethodRecordsUpdate, func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { return nil })
}

func TestTopicRegistrationIsScoped(t *testing.T) {
	resetPermissions(t)
	called := 0
	RegisterTopic("app/*/track/", func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { called++; return nil })
	checker := New(svc.Config{Root: t.TempDir()}, &svc.Services{Store: definitionStore{bundle: testBundle(t)}})
	scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Role: svc.RoleViewer}}
	for _, method := range []string{"sub", "unsub"} {
		for _, tt := range []struct {
			topic string
			code  bridge.ErrorCode
		}{
			{"app/crm/track/private", "allowed"}, {"app/crm/records/secret", bridge.CodeDenied}, {"app/crm/unknown", bridge.CodeInvalid},
		} {
			t.Run(method+"/"+tt.topic, func(t *testing.T) {
				raw, _ := json.Marshal(map[string]string{"topic": tt.topic})
				if err := checker.Allow(context.Background(), scope, method, raw); errorCode(err) != tt.code {
					t.Fatal(err)
				}
			})
		}
	}
	if called != 2 {
		t.Fatalf("topic Allow ran %d times", called)
	}
}
