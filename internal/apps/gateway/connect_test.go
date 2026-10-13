package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/connect"
	"github.com/Agent-Field/codeaf/internal/apps/grants"
	"github.com/Agent-Field/codeaf/internal/apps/imports"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var connectMethodCases = []struct {
	name   string
	global bool
	params string
}{
	{bridge.MethodConnectCredentialExport, true, `{"service":"gmail","space":"acme","app":"crm"}`},
	{bridge.MethodConnectAccounts, true, `{}`},
	{bridge.MethodConnectAllow, false, `{"service":"gmail","answer":"allow"}`},
	{bridge.MethodConnectAllowSheet, false, `{"service":"gmail"}`},
	{bridge.MethodConnectRevoke, true, `{"app":"crm","space":"acme","service":"gmail"}`},
	{bridge.MethodConnectWorkspaceList, true, `{}`},
	{bridge.MethodConnectWorkspaceAdd, true, `{"service":"stripe","who":"chosen","apps":["crm"]}`},
	{bridge.MethodConnectWorkspaceWho, true, `{"id":"stripe-acme","who":"all"}`},
	{bridge.MethodConnectWorkspaceImpact, true, `{"id":"stripe-acme"}`},
	{bridge.MethodConnectWorkspaceRemove, true, `{"id":"stripe-acme"}`},
	{bridge.MethodConnectNeeds, false, `{}`},
	{bridge.MethodConnectWaits, true, `{}`},
	{bridge.MethodConnectFakes, false, `{"run_id":"run-1"}`},
	{bridge.MethodImportsSources, false, `{"type":"deal"}`},
	{bridge.MethodImportsPreview, false, `{"type":"deal","source":{"kind":"csv","file":"bmFtZQ=="},"mapping":[{"column":"name","field":"name"}]}`},
	{bridge.MethodImportsRun, false, `{"import_id":"import-1","mapping":[{"column":"name","field":"name"}]}`},
	{bridge.MethodImportsUndo, false, `{"import_id":"import-1"}`},
}

func stubConnectServices() svc.ConnectServices {
	return svc.ConnectServices{
		Grants:  grants.New(svc.Config{}, nil),
		Imports: imports.New(svc.Config{}, nil),
	}
}

func TestConnectMethodsRouteToServices(t *testing.T) {
	all := &svc.Services{Connect: stubConnectServices()}
	for _, tt := range connectMethodCases {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := lookupMethod(tt.name)
			if !ok || entry.Global != tt.global {
				t.Fatalf("registered=%v global=%v, want global=%v", ok, entry.Global, tt.global)
			}
			_, err := entry.Handle(context.Background(), all, connectScope(connectTestApp(tt.global)), json.RawMessage(tt.params))
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != connectTestCode(tt.name) {
				t.Fatalf("%s returned %v, want its service error", tt.name, err)
			}
		})
	}
}

func TestConnectMethodsDecodeBeforeDispatch(t *testing.T) {
	for _, tt := range connectMethodCases {
		t.Run(tt.name, func(t *testing.T) {
			// Missing services make an accidental dispatch fail immediately.
			_, err := connectEntry(t, tt.name).Handle(context.Background(), &svc.Services{}, connectScope("crm"), json.RawMessage(`[]`))
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != bridge.CodeInvalid {
				t.Fatalf("invalid params returned %v", err)
			}
		})
	}
}

type connectCheck struct {
	mu         sync.Mutex
	seenMethod string
	seenScope  svc.Scope
	seenParams json.RawMessage
	err        error
	delegate   svc.Caps
}

func (c *connectCheck) Allow(ctx context.Context, scope svc.Scope, method string, raw json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seenMethod, c.seenScope, c.seenParams = method, scope, append(json.RawMessage(nil), raw...)
	if c.err != nil {
		return c.err
	}
	if c.delegate != nil {
		return c.delegate.Allow(ctx, scope, method, raw)
	}
	return nil
}

func (c *connectCheck) checkedMethod() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seenMethod
}

func (c *connectCheck) checkedParams() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(json.RawMessage(nil), c.seenParams...)
}

func TestConnectCallsUseGatewayDispatch(t *testing.T) {
	all := testServices(t)
	all.Connect = stubConnectServices()
	checker := &connectCheck{delegate: all.Caps}
	all.Caps = checker
	ctx, url := testServer(t, all)
	client := dialSocket(t, ctx, url)
	for _, tt := range connectMethodCases {
		t.Run(tt.name, func(t *testing.T) {
			app := connectTestApp(tt.global)
			id := client.send(bridge.TypeCall, app, tt.name, "", json.RawMessage(tt.params))
			reply := client.read()
			if reply.ID != id || reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != connectTestCode(tt.name) {
				t.Fatalf("reply=%+v", reply)
			}
			checker.mu.Lock()
			defer checker.mu.Unlock()
			if checker.seenMethod != tt.name || checker.seenScope.App != app || string(checker.seenParams) != tt.params {
				t.Fatalf("capability check=%+v", checker)
			}
		})
	}
}

func TestConnectCallsRespectCapabilityDenial(t *testing.T) {
	for _, tt := range connectMethodCases {
		t.Run(tt.name, func(t *testing.T) {
			checker := &connectCheck{err: bridge.Errorf(bridge.CodeDenied, "test denial")}
			s := newSession(context.Background(), nil, &svc.Services{Caps: checker}, nil)
			defer s.cancel()
			s.call(bridge.Envelope{ID: "denied", Type: bridge.TypeCall, Method: tt.name, App: "crm", Params: json.RawMessage(tt.params)})
			reply := <-s.out
			if reply.Error == nil || reply.Error.Code != bridge.CodeDenied || checker.seenMethod != tt.name {
				t.Fatalf("check=%+v reply=%+v", checker, reply)
			}
		})
	}
}

func TestConnectOptionalQueriesStayNotBuilt(t *testing.T) {
	all := &svc.Services{Connect: stubConnectServices()}
	// Embedding only the contract interface hides the stub's optional queries.
	all.Connect.Grants = struct{ svc.Grants }{all.Connect.Grants}
	for _, name := range []string{bridge.MethodConnectAllowSheet, bridge.MethodConnectWaits, bridge.MethodConnectFakes} {
		t.Run(name, func(t *testing.T) {
			_, err := connectEntry(t, name).Handle(context.Background(), all, connectScope("crm"), json.RawMessage(`{}`))
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != bridge.CodeNotBuilt {
				t.Fatalf("missing optional query returned %v", err)
			}
		})
	}
}

func TestConnectSubscriptionsUseGatewaySubPath(t *testing.T) {
	for _, topic := range []string{
		bridge.TopicConnectAllow(svc.Owner), bridge.TopicConnectWaits(svc.Owner),
		bridge.TopicConnectWorkspace(), bridge.TopicFakes("crm"),
	} {
		t.Run(topic, func(t *testing.T) {
			all := testServices(t)
			checker := &connectCheck{delegate: all.Caps}
			all.Caps = checker
			ctx, url := testServer(t, all)
			c := dialSocket(t, ctx, url)
			id := c.send(bridge.TypeSub, "", "", topic, nil)
			if reply := c.read(); reply.ID != id || reply.Type != bridge.TypeResult {
				t.Fatalf("sub reply=%+v", reply)
			}
			if checked := checker.checkedMethod(); checked != "sub" {
				t.Fatalf("sub did not reach capability checker: %s", checked)
			}
			var params map[string]string
			raw := checker.checkedParams()
			if err := json.Unmarshal(raw, &params); err != nil || params["topic"] != topic {
				t.Fatalf("checked params=%s err=%v", raw, err)
			}
			all.Hub.Publish(topic, map[string]string{"service": "gmail"})
			if event := c.read(); event.Type != bridge.TypeEvent || event.Topic != topic || event.Seq < 1 {
				t.Fatalf("event=%+v", event)
			}
			id = c.send(bridge.TypeUnsub, "", "", topic, nil)
			if reply := c.read(); reply.ID != id || reply.Type != bridge.TypeResult || checker.checkedMethod() != "unsub" {
				t.Fatalf("unsub check=%s reply=%+v", checker.checkedMethod(), reply)
			}
		})
	}
}

func TestConnectTopicAuthorization(t *testing.T) {
	for _, tt := range []struct {
		name, topic, app string
		deny             bool
		code             bridge.ErrorCode
	}{
		{"allow", bridge.TopicConnectAllow(svc.Owner), "", false, ""},
		{"waits", bridge.TopicConnectWaits(svc.Owner), "", false, ""},
		{"workspace", bridge.TopicConnectWorkspace(), "", false, ""},
		{"fakes", bridge.TopicFakes("crm"), "crm", false, ""},
		{"other member", bridge.TopicConnectAllow("mona"), "", false, bridge.CodeDenied},
		{"other waits", bridge.TopicConnectWaits("mona"), "", false, bridge.CodeDenied},
		{"member wildcard", "connect/*/allow", "", false, bridge.CodeDenied},
		{"global mismatch", bridge.TopicConnectWorkspace(), "crm", false, bridge.CodeInvalid},
		{"app mismatch", bridge.TopicFakes("crm"), "other", false, bridge.CodeInvalid},
		{"denied allow", bridge.TopicConnectAllow(svc.Owner), "", true, bridge.CodeDenied},
		{"denied waits", bridge.TopicConnectWaits(svc.Owner), "", true, bridge.CodeDenied},
		{"denied workspace", bridge.TopicConnectWorkspace(), "", true, bridge.CodeDenied},
		{"denied fakes", bridge.TopicFakes("crm"), "", true, bridge.CodeDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			all := testServices(t)
			checker := &connectCheck{delegate: all.Caps}
			if tt.deny {
				checker.err = bridge.Errorf(bridge.CodeDenied, "test denial")
			}
			all.Caps = checker
			s := newSession(context.Background(), nil, all, nil)
			defer s.cancel()
			scope, err := s.topicScope(bridge.Envelope{Topic: tt.topic, App: tt.app}, "sub")
			if tt.code == "" {
				if err != nil || scope.Caller.Member != svc.Owner {
					t.Fatalf("scope=%+v err=%v", scope, err)
				}
				return
			}
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != tt.code {
				t.Fatalf("topic error=%v, want %s", err, tt.code)
			}
		})
	}
}

type connectParamsProbe struct {
	svc.Grants
	scope  svc.Scope
	caller svc.Caller
	params any
}

func (p *connectParamsProbe) Allow(_ context.Context, scope svc.Scope, params bridge.AllowParams) (bridge.Grant, error) {
	p.scope, p.params = scope, params
	return bridge.Grant{}, nil
}

func (p *connectParamsProbe) SetWho(_ context.Context, caller svc.Caller, params bridge.SetWhoParams) (bridge.WorkspaceConnection, error) {
	p.caller, p.params = caller, params
	return bridge.WorkspaceConnection{}, nil
}

func TestConnectDispatchPreservesScopeAndParams(t *testing.T) {
	scope := svc.Scope{App: "crm", Data: svc.DataReal, Caller: svc.Caller{Member: "mona", Role: svc.RoleUser}}
	for _, tt := range []struct {
		name   string
		params any
		global bool
	}{
		{bridge.MethodConnectAllow, bridge.AllowParams{Service: "gmail", Answer: "not_now"}, false},
		{bridge.MethodConnectWorkspaceWho, bridge.SetWhoParams{ID: "stripe-acme", Who: "chosen", Apps: []string{"crm"}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			probe := &connectParamsProbe{}
			all := &svc.Services{Connect: svc.ConnectServices{Grants: probe}}
			raw, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := connectEntry(t, tt.name).Handle(context.Background(), all, scope, raw); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(probe.params, tt.params) || (tt.global && probe.caller != scope.Caller) || (!tt.global && probe.scope != scope) {
				t.Fatalf("dispatch=%+v want scope=%+v params=%+v", probe, scope, tt.params)
			}
		})
	}
}

// An unconfigured real grants service refuses a missing home; other lanes still
// expose their explicit pending queries. Dispatch must retain those errors.
func connectTestCode(method string) bridge.ErrorCode {
	if method == bridge.MethodConnectWaits || method == bridge.MethodConnectFakes || strings.HasPrefix(method, "imports.") {
		return bridge.CodeNotBuilt
	}
	return bridge.CodeInvalid
}
func connectTestApp(global bool) string {
	if global {
		return ""
	}
	return "crm"
}

func TestCredentialExportDeniedThroughGateway(t *testing.T) {
	for _, tt := range []struct {
		name, app, space string
		caller           svc.Caller
	}{
		{"app", "crm", "personal", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}},
		{"draft app", "crm@draft", "personal", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}},
		{"app viewer", "crm", "personal", svc.Caller{Member: svc.Owner, Role: svc.RoleViewer}},
		{"agent", "", "personal", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor, Agent: true}},
		{"other member", "", "personal", svc.Caller{Member: "mona", Role: svc.RoleEditor}},
		{"workspace", "", "acme", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCallerResolver(t)
			SetCallerResolver(func(context.Context, *http.Request, string) svc.Caller { return tt.caller })
			all := testServices(t)
			// App and agent cases have no exporter, so only the caps rule can refuse them.
			if tt.space != "personal" {
				service := grants.NewWithOptions(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, all, grants.Options{Space: tt.space})
				t.Cleanup(func() { _ = service.Close() })
				all.Connect.Grants = service
			}
			ctx, url := testServer(t, all)
			client := dialSocket(t, ctx, url)
			id := client.send(bridge.TypeCall, tt.app, bridge.MethodConnectCredentialExport, "", bridge.CredentialExportParams{Service: "gmail", Space: "acme", App: "crm"})
			reply := client.read()
			if reply.ID != id || reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != bridge.CodeDenied {
				t.Fatalf("export reply=%+v", reply)
			}
			if tt.space == "personal" && reply.Error.Message != "Only your desktop session can export a connection" {
				t.Fatalf("export did not reach the caps rule: %+v", reply)
			}
		})
	}
}

func connectEntry(t *testing.T, name string) Method {
	t.Helper()
	entry, ok := lookupMethod(name)
	if !ok {
		t.Fatalf("method %s is not registered", name)
	}
	return entry
}
func connectScope(app string) svc.Scope {
	return svc.Scope{App: app, Caller: PersonalCaller(context.Background(), nil, app), Data: svc.DataSample}
}

func TestConnectorActivityMethodsUseDoor(t *testing.T) {
	ctx := context.Background()
	all := &svc.Services{Connect: stubConnectServices()}
	door := connect.New(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, all)
	defer door.Close()
	all.Connectors = door
	scope := connectScope("crm")
	scope.Data = svc.DataSample
	if _, err := door.Call(connect.WithInvocation(ctx, "run-1", "read"), scope, "gmail.read", map[string]any{"id": "message"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ method, params string }{{bridge.MethodConnectFakes, `{"run_id":"run-1"}`}, {bridge.MethodConnectWaits, `{}`}} {
		t.Run(tt.method, func(t *testing.T) {
			result, err := connectEntry(t, tt.method).Handle(ctx, all, scope, json.RawMessage(tt.params))
			if err != nil {
				t.Fatal(err)
			}
			if tt.method == bridge.MethodConnectFakes {
				calls := result.(bridge.ConnectFakesResult).Calls
				if len(calls) != 1 || calls[0].RunID != "run-1" {
					t.Fatalf("result=%+v", result)
				}
			} else if len(result.(bridge.ConnectWaitsResult).Waits) != 0 {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestConnectSubscriptionsUseResolvedMember(t *testing.T) {
	for _, tt := range []struct {
		name, topic string
		caller      svc.Caller
		code        bridge.ErrorCode
	}{
		{"own allow", "connect/mona/allow", svc.Caller{Member: "mona"}, ""},
		{"own waits", "connect/mona/waits", svc.Caller{Member: "mona"}, ""},
		{"other allow", "connect/me/allow", svc.Caller{Member: "mona"}, bridge.CodeDenied},
		{"other waits", "connect/me/waits", svc.Caller{Member: "mona"}, bridge.CodeDenied},
		{"workspace member", "connect/workspace", svc.Caller{Member: "mona"}, ""},
		{"workspace guest", "connect/workspace", svc.Caller{}, bridge.CodeDenied},
		{"fakes viewer", "app/crm/fakes", svc.Caller{Member: "mona", Role: svc.RoleViewer}, ""},
		{"fakes without role", "app/crm/fakes", svc.Caller{Member: "mona"}, bridge.CodeDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCallerResolver(t)
			SetCallerResolver(func(context.Context, *http.Request, string) svc.Caller { return tt.caller })
			all := testServices(t)
			ctx, url := testServer(t, all)
			client := dialSocket(t, ctx, url)
			for _, kind := range []bridge.MessageType{bridge.TypeSub, bridge.TypeUnsub} {
				id := client.send(kind, "", "", tt.topic, nil)
				reply := client.read()
				if tt.code == "" {
					if reply.ID != id || reply.Type != bridge.TypeResult {
						t.Fatalf("%s reply=%+v", kind, reply)
					}
				} else if reply.ID != id || reply.Error == nil || reply.Error.Code != tt.code {
					t.Fatalf("%s reply=%+v, want %s", kind, reply, tt.code)
				}
			}
		})
	}
}
