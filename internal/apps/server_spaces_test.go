package apps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/coder/websocket"
)

type resolverRoles struct {
	svc.AppRoles
	mu      sync.Mutex
	roles   map[string]svc.Role
	err     error
	lookups []string
	scopes  chan svc.Scope
}

func (r *resolverRoles) Role(_ context.Context, app, member string) (svc.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, app+"/"+member)
	return r.roles[app], r.err
}

func (r *resolverRoles) List(_ context.Context, scope svc.Scope) (bridge.AppMembers, error) {
	r.scopes <- scope
	return bridge.AppMembers{People: []bridge.AppMember{{Member: scope.Caller.Member, Role: string(scope.Caller.Role)}}}, nil
}

func TestBridgeSpacesCallerRoles(t *testing.T) {
	previous := spacesServices.Load()
	t.Cleanup(func() { spacesServices.Store(previous) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	all := Wire(ctx, t.TempDir())
	roles := &resolverRoles{roles: map[string]svc.Role{}, scopes: make(chan svc.Scope, 1)}
	all.AppRoles = roles
	caller := svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember}
	bridgeHandler := gateway.NewHandler(ctx, all)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeHandler.ServeHTTP(w, r.WithContext(svc.WithCaller(r.Context(), caller)))
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+bridge.Path, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	c := &client{t: t, ctx: ctx, conn: conn}
	for _, tt := range []struct {
		app  string
		role svc.Role
		data svc.DataMode
	}{
		{"crm", svc.RoleViewer, svc.DataReal},
		{"crm@draft", svc.RoleUser, svc.DataDraft},
		{"crm", svc.RoleEditor, svc.DataReal},
	} {
		roles.mu.Lock()
		roles.roles["crm"] = tt.role
		roles.mu.Unlock()
		var members bridge.AppMembers
		if err := c.call(tt.app, bridge.MethodAppRolesList, bridge.Empty{}, &members); err != nil {
			t.Fatal(err)
		}
		scope := <-roles.scopes
		want := caller
		want.Role = tt.role
		if scope.Caller != want || scope.App != "crm" || scope.Data != tt.data {
			t.Fatalf("service scope = %+v, want caller %+v and data %s", scope, want, tt.data)
		}
		if tt.role == svc.RoleViewer {
			if err := c.call("crm", bridge.MethodRecordsCreate, bridge.CreateRecordParams{Type: "deal", Set: map[string]any{"name": "blocked"}}, nil); err == nil || err.Code != bridge.CodeDenied {
				t.Fatalf("Viewer write = %v, want denied", err)
			}
		}
	}
	roles.mu.Lock()
	defer roles.mu.Unlock()
	if !reflect.DeepEqual(roles.lookups, []string{"crm/raj", "crm/raj", "crm/raj", "crm/raj"}) {
		t.Fatalf("role lookups = %v", roles.lookups)
	}
}

func TestSpacesCallerResolver(t *testing.T) {
	previous := spacesServices.Load()
	t.Cleanup(func() { spacesServices.Store(previous) })
	workspace := svc.Caller{Member: "raj", Space: "acme", WS: svc.WorkspaceMember, Role: svc.RoleEditor}
	for _, tt := range []struct {
		name     string
		caller   *svc.Caller
		app      string
		role     svc.Role
		err      error
		services bool
		wantRole svc.Role
	}{
		{"personal socket", nil, "", "", nil, true, svc.RoleEditor},
		{"workspace socket", &workspace, "", "", nil, true, svc.RoleEditor},
		{"viewer in crm", &workspace, "crm", svc.RoleViewer, nil, true, svc.RoleViewer},
		{"user in support", &workspace, "support", svc.RoleUser, nil, true, svc.RoleUser},
		{"editor in slides", &workspace, "slides", svc.RoleEditor, nil, true, svc.RoleEditor},
		{"workspace lookup failure", &workspace, "crm", svc.RoleEditor, errors.New("roles unavailable"), true, ""},
		{"workspace services missing", &workspace, "crm", "", nil, false, ""},
		{"personal editor", nil, "crm", svc.RoleEditor, nil, true, svc.RoleEditor},
		{"personal lookup fallback", nil, "crm", "", errors.New("roles unavailable"), true, svc.RoleEditor},
		{"personal services fallback", nil, "crm", "", nil, false, svc.RoleEditor},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, bridge.Path, nil)
			want := svc.PersonalCaller()
			if tt.caller != nil {
				request = request.WithContext(svc.WithCaller(request.Context(), *tt.caller))
				want = *tt.caller
			}
			roles := &resolverRoles{roles: map[string]svc.Role{tt.app: tt.role}, err: tt.err}
			spacesServices.Store(nil)
			if tt.services {
				spacesServices.Store(&svc.Services{SpacesServices: svc.SpacesServices{AppRoles: roles}})
			}
			want.Role = tt.wantRole
			if got := resolveSpacesCaller(context.Background(), request, tt.app); got != want {
				t.Fatalf("caller = %+v, want %+v", got, want)
			}
			if tt.app != "" && tt.services {
				if !reflect.DeepEqual(roles.lookups, []string{tt.app + "/" + want.Member}) {
					t.Fatalf("role lookups = %v", roles.lookups)
				}
				roles.err = nil
				roles.roles[tt.app] = svc.RoleUser
				if got := resolveSpacesCaller(context.Background(), request, tt.app); got.Role != svc.RoleUser || len(roles.lookups) != 2 {
					t.Fatalf("role cached between calls: %+v, lookups=%v", got, roles.lookups)
				}
			} else if len(roles.lookups) != 0 {
				t.Fatalf("unexpected role lookup = %v", roles.lookups)
			}
		})
	}
}

func TestBridgePersonalSpaces(t *testing.T) {
	c, all := dialPersonalSpaces(t)
	entry, err := all.Audit.Append(c.ctx, bridge.AuditEntry{Member: svc.Owner, Action: "records.update", App: "crm", Decision: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	var info bridge.SpaceInfo
	if err := c.call("", bridge.MethodSpaceInfo, bridge.Empty{}, &info); err != nil {
		t.Fatal(err)
	}
	if info.ID != svc.PersonalSpace || info.Name != "Personal" || info.Kind != "personal" || info.Me.ID != svc.Owner || info.Me.Role != string(svc.WorkspaceOwner) || info.Apps != 1 {
		t.Fatalf("space.info = %+v", info)
	}
	var audit bridge.AuditListResult
	if err := c.call("", bridge.MethodAuditList, bridge.AuditListParams{App: "crm", Limit: 1}, &audit); err != nil {
		t.Fatal(err)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Seq != entry.Seq || !audit.Entries[0].At.Equal(entry.At) || audit.Entries[0].Action != entry.Action || audit.Next != 0 {
		t.Fatalf("audit.list = %+v", audit)
	}
	for _, topic := range []string{bridge.TopicSpaceMembers, bridge.TopicSpaceActivity, bridge.TopicSpaceWaiting, bridge.TopicRoles("crm"), bridge.TopicSignoff("crm")} {
		id := c.send(bridge.Envelope{Type: bridge.TypeSub, Topic: topic})
		if reply := c.read(); reply.ID != id || reply.Type != bridge.TypeResult {
			t.Fatalf("subscribe %s = %+v", topic, reply)
		}
	}
}

func dialPersonalSpaces(t *testing.T) (*client, *svc.Services) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	api := appapi.New(testToken, appapi.Options{Context: ctx})
	t.Cleanup(api.Close)
	all := Wire(ctx, t.TempDir())
	Mount(ctx, api, all)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http") + bridge.Path
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + testToken}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return &client{t: t, ctx: ctx, conn: conn}, all
}
