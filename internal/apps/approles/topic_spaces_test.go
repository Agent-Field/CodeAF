package approles_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps"
	"github.com/Agent-Field/codeaf/internal/apps/approles"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/coder/websocket"
)

type workspaceStore struct{ svc.Store }
type workspaceMembers struct{ svc.Members }

func (workspaceMembers) List(context.Context, svc.Caller) (bridge.MembersListResult, error) {
	return bridge.MembersListResult{Members: []bridge.Member{{ID: "mona"}, {ID: "raj"}}}, nil
}

func (workspaceStore) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	return bridge.AppDetail{AppSummary: bridge.AppSummary{ID: app, Space: "acme", Name: "CRM", State: bridge.StateKept}}, nil
}

func (workspaceStore) Definition(context.Context, string) (*spec.Bundle, error) {
	files, _ := spec.GoldenCRM()
	return spec.Decode(files)
}

type recordWriter struct {
	svc.Records
	scopes chan svc.Scope
}

func (r recordWriter) Create(_ context.Context, scope svc.Scope, _ bridge.CreateRecordParams) (bridge.Record, error) {
	r.scopes <- scope
	return bridge.Record{}, nil
}

type lookupRoles struct {
	svc.AppRoles
	mu   sync.Mutex
	apps []string
}

func (r *lookupRoles) Role(ctx context.Context, app, member string) (svc.Role, error) {
	r.mu.Lock()
	r.apps = append(r.apps, app)
	r.mu.Unlock()
	return r.AppRoles.Role(ctx, app, member)
}

// Exercise real gateway sub/unsub frames, the production resolver, and the registered deny rule.
func TestSocketTopicsResolveAndDenyWorkspaceNonmembers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	all := apps.Wire(ctx, root)
	all.Store = workspaceStore{Store: all.Store}
	all.Members = workspaceMembers{}
	writer := recordWriter{scopes: make(chan svc.Scope, 1)}
	all.Records = writer
	cfg := svc.Config{Root: root, Ctx: ctx}
	all.Caps = caps.New(cfg, all)
	all.Hub = realtime.New(cfg, all)
	roles := approles.New(svc.Config{Root: root}, all)
	defer roles.Close()
	if err := roles.Seed(ctx, "crm", "mona"); err != nil {
		t.Fatal(err)
	}
	viewer := "viewer"
	_, err := roles.Set(ctx, svc.Scope{App: "crm", Caller: svc.Caller{Member: "mona", Space: "acme", WS: svc.WorkspaceOwner, Role: svc.RoleEditor}}, bridge.AppRoleSetParams{Member: "raj", Role: &viewer})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &lookupRoles{AppRoles: roles}
	all.AppRoles = resolver
	caller := svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember}
	handler := gateway.NewHandler(ctx, all)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(svc.WithCaller(r.Context(), caller)))
	}))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+bridge.Path, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, message := range []bridge.MessageType{bridge.TypeSub, bridge.TypeUnsub} {
		for _, app := range []string{"x", "crm"} {
			for _, family := range []string{"records/deal", "runs", "agent/t1", "roles", "build", "shape/raj", "*"} {
				if app == "crm" && (family == "records/deal" || family == "runs" || family == "agent/t1") {
					continue
				}
				topic := "app/" + app + "/" + family
				t.Run(string(message)+"/"+topic, func(t *testing.T) {
					request := bridge.Envelope{ID: "topic", Type: message, Topic: topic}
					raw, _ := json.Marshal(request)
					if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
						t.Fatal(err)
					}
					_, raw, err := conn.Read(ctx)
					if err != nil {
						t.Fatal(err)
					}
					var reply bridge.Envelope
					if err := json.Unmarshal(raw, &reply); err != nil {
						t.Fatal(err)
					}
					wantDenied := app == "x" || family != "roles"
					if wantDenied && (reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != bridge.CodeDenied) {
						t.Fatalf("reply=%s want denied", raw)
					}
					if !wantDenied && reply.Type != bridge.TypeResult {
						t.Fatalf("reply=%s want result", raw)
					}
					resolver.mu.Lock()
					defer resolver.mu.Unlock()
					if len(resolver.apps) == 0 || resolver.apps[len(resolver.apps)-1] != app {
						t.Fatalf("resolver calls=%v, want bare %s", resolver.apps, app)
					}
				})
			}
		}
	}
	// A membership update changes the next call's resolver result on the same socket.
	user := "user"
	_, err = roles.Set(ctx, svc.Scope{App: "crm", Caller: svc.Caller{Member: "mona", Space: "acme", WS: svc.WorkspaceOwner, Role: svc.RoleEditor}}, bridge.AppRoleSetParams{Member: "raj", Role: &user})
	if err != nil {
		t.Fatal(err)
	}
	role, err := roles.Role(ctx, "crm", "raj")
	if err != nil || role != svc.RoleUser {
		t.Fatal(role, err)
	}
	raw, _ := json.Marshal(bridge.Envelope{ID: "changed", Type: bridge.TypeCall, App: "crm", Method: bridge.MethodRecordsCreate, Params: json.RawMessage(`{"type":"deal","set":{"name":"now allowed"}}`)})
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var reply bridge.Envelope
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Type == bridge.TypeEvent {
			continue
		}
		if reply.ID != "changed" || reply.Type != bridge.TypeResult {
			t.Fatalf("reply=%s", raw)
		}
		break
	}
	select {
	case scope := <-writer.scopes:
		if scope.Caller.Role != svc.RoleUser {
			t.Fatal(scope)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.apps[len(resolver.apps)-1] != "crm" {
		t.Fatal(resolver.apps)
	}
}

func TestWorkspaceApprovalsNeedUserAndBelongToCaller(t *testing.T) {
	for _, tt := range []struct {
		name, member string
		role         svc.Role
		denied       bool
	}{
		{"User own", "raj", svc.RoleUser, false},
		{"User someone else's", "jm", svc.RoleUser, true},
		{"Viewer own", "raj", svc.RoleViewer, true},
		{"Editor own", "raj", svc.RoleEditor, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			all := apps.Wire(ctx, t.TempDir())
			item, err := all.Approvals.Open(ctx, svc.Scope{App: "crm", Caller: svc.Caller{Member: "raj", Space: "acme", Role: svc.RoleUser}}, bridge.Approval{ID: "own", Title: "Approval needed: start", Kind: "start", Who: "raj"})
			if err != nil {
				t.Fatal(err)
			}
			scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: tt.member, Space: "acme", WS: svc.WorkspaceMember, Role: tt.role}}
			params := bridge.DecideApprovalParams{ApprovalID: item.ID, Answer: "yes"}
			raw, _ := json.Marshal(params)
			err = all.Caps.Allow(ctx, scope, bridge.MethodApprovalsDecide, raw)
			if err == nil {
				_, err = all.Approvals.Decide(ctx, scope, params)
			}
			if tt.denied {
				if typed, ok := err.(*bridge.Error); !ok || typed.Code != bridge.CodeDenied {
					t.Fatalf("error=%v, want denied", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
