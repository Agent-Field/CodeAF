package gateway

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/shape"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func resetCallerResolver(t *testing.T) {
	t.Helper()
	callers.Lock()
	resolve, set := callers.resolve, callers.set
	callers.resolve, callers.set = PersonalCaller, false
	callers.Unlock()
	t.Cleanup(func() {
		callers.Lock()
		callers.resolve, callers.set = resolve, set
		callers.Unlock()
	})
}

func TestDefaultCallerIsPersonalOwnerEditor(t *testing.T) {
	resetCallerResolver(t)
	s := newSession(context.Background(), nil, nil, nil)
	defer s.cancel()
	want := svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
	if s.caller != want {
		t.Fatalf("socket caller=%+v", s.caller)
	}
	for _, app := range []string{"", "crm", "crm@draft"} {
		t.Run(app, func(t *testing.T) {
			if got := s.appScope(app).Caller; got != want {
				t.Fatalf("call caller=%+v", got)
			}
		})
	}
}

func TestCallerResolverRunsBeforeCapsWithUpgradeRequest(t *testing.T) {
	resetCallerResolver(t)
	var mu sync.Mutex
	var apps []string
	var request *http.Request
	SetCallerResolver(func(_ context.Context, r *http.Request, app string) svc.Caller {
		mu.Lock()
		defer mu.Unlock()
		if r == nil || r.URL.Path != bridge.Path || r.Header.Get("Sec-WebSocket-Key") == "" {
			t.Error("resolver lost upgrade request")
		}
		if request == nil {
			request = r
		} else if request != r {
			t.Error("resolver received another request")
		}
		apps = append(apps, app)
		if app == "" {
			return svc.Caller{Member: "raj"}
		}
		return svc.Caller{Member: "raj", Role: svc.RoleViewer}
	})
	all := testServices(t)
	all.Shape = shape.New(svc.Config{}, all)
	seen := make(chan svc.Scope, 3)
	all.Caps = draftScopeCaps{Caps: all.Caps, seen: seen}
	ctx, url := testServer(t, all)
	client := dialSocket(t, ctx, url)
	for _, tt := range []struct {
		method string
		code   bridge.ErrorCode
	}{
		{bridge.MethodShapeOpen, bridge.CodeDenied}, {bridge.MethodVersionsList, ""},
	} {
		t.Run(tt.method, func(t *testing.T) {
			client.send(bridge.TypeCall, "crm@draft", tt.method, "", bridge.Empty{})
			reply := client.read()
			if tt.code == "" && reply.Type != bridge.TypeResult || tt.code != "" && (reply.Error == nil || reply.Error.Code != tt.code) {
				t.Fatal(reply)
			}
			scope := <-seen
			if scope.App != "crm" || scope.Caller.Member != "raj" || scope.Caller.Role != svc.RoleViewer || scope.Data != svc.DataDraft {
				t.Fatalf("caps scope=%+v", scope)
			}
		})
	}
	client.send(bridge.TypeCall, "", bridge.MethodAppsList, "", bridge.Empty{})
	// The socket membership is reused for global calls, without another resolver lookup.
	client.read()
	<-seen
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(apps, []string{"", "crm", "crm"}) {
		t.Fatalf("resolver calls=%v", apps)
	}
}

func TestCallerResolverDoubleSetPanics(t *testing.T) {
	resetCallerResolver(t)
	SetCallerResolver(PersonalCaller)
	defer func() {
		if got := recover(); got != "gateway: caller resolver set twice" {
			t.Fatalf("panic=%v", got)
		}
	}()
	SetCallerResolver(PersonalCaller)
}
