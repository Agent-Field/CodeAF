package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
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
			entry, ok := methods[tt.name]
			if !ok || entry.global != tt.global {
				t.Fatalf("registered=%v global=%v, want global=%v", ok, entry.global, tt.global)
			}
			_, err := entry.run(context.Background(), all, ownerScope(connectTestApp(tt.global)), json.RawMessage(tt.params))
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
			_, err := methods[tt.name].run(context.Background(), &svc.Services{}, ownerScope("crm"), json.RawMessage(`[]`))
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
}

func (c *connectCheck) Allow(_ context.Context, scope svc.Scope, method string, raw json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seenMethod, c.seenScope, c.seenParams = method, scope, append(json.RawMessage(nil), raw...)
	return c.err
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
	checker := &connectCheck{}
	all.Caps = checker
	for _, tt := range connectMethodCases {
		t.Run(tt.name, func(t *testing.T) {
			app := "crm"
			if tt.global {
				app = ""
			}
			s := newSession(context.Background(), nil, all)
			defer s.cancel()
			s.call(bridge.Envelope{ID: "connect-test", Type: bridge.TypeCall, Method: tt.name, App: app, Params: json.RawMessage(tt.params)})
			reply := <-s.out
			if reply.ID != "connect-test" || reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != connectTestCode(tt.name) {
				t.Fatalf("reply=%+v", reply)
			}
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
			s := newSession(context.Background(), nil, &svc.Services{Caps: checker})
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
			_, err := methods[name].run(context.Background(), all, ownerScope("crm"), json.RawMessage(`{}`))
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
			checker := &connectCheck{}
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
			checker := &connectCheck{}
			if tt.deny {
				checker.err = bridge.Errorf(bridge.CodeDenied, "test denial")
			}
			all.Caps = checker
			s := newSession(context.Background(), nil, all)
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
			if _, err := methods[tt.name].run(context.Background(), all, scope, raw); err != nil {
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

func TestCredentialExportRejectsAppThroughGateway(t *testing.T) {
	all := &svc.Services{Connect: stubConnectServices(), Caps: &connectCheck{}}
	session := newSession(context.Background(), nil, all)
	defer session.cancel()
	session.call(bridge.Envelope{ID: "export", Type: bridge.TypeCall, Method: bridge.MethodConnectCredentialExport, App: "crm", Params: json.RawMessage(`{"service":"gmail","space":"acme","app":"crm"}`)})
	reply := <-session.out
	if reply.Error == nil || reply.Error.Code != bridge.CodeDenied {
		t.Fatalf("app export reply=%v", reply)
	}
}
