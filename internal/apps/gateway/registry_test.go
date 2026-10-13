package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestRegisterDuplicatePanics(t *testing.T) {
	for _, name := range []string{bridge.MethodAppsList, bridge.MethodShapeOpen} {
		t.Run(name, func(t *testing.T) {
			before, _ := lookupMethod(name)
			defer func() {
				if got := recover(); got != "gateway: method "+name+" registered twice" {
					t.Fatalf("panic=%v", got)
				}
				after, _ := lookupMethod(name)
				if reflect.ValueOf(before.Handle).Pointer() != reflect.ValueOf(after.Handle).Pointer() {
					t.Fatal("duplicate replaced handler")
				}
			}()
			Register(name, Method{Allow: func(context.Context, svc.Caller, svc.Scope, json.RawMessage) error { return nil }})
		})
	}
}

func TestUnknownBridgeMethodRemainsInvalid(t *testing.T) {
	all := testServices(t)
	ctx, url := testServer(t, all)
	client := dialSocket(t, ctx, url)
	for _, method := range []string{"shape.unknown", "unknown.method", "ai.sort"} {
		t.Run(method, func(t *testing.T) {
			client.send(bridge.TypeCall, "crm", method, "", bridge.Empty{})
			if reply := client.read(); reply.Error == nil || reply.Error.Code != bridge.CodeInvalid {
				t.Fatal(reply)
			}
		})
	}
}

func resetDisconnectHooks(t *testing.T) {
	t.Helper()
	registry.Lock()
	hooks := registry.disconnect
	registry.disconnect = nil
	registry.Unlock()
	t.Cleanup(func() {
		registry.Lock()
		registry.disconnect = hooks
		registry.Unlock()
	})
}

func TestDisconnectHooksRunInOrderOnceWithSocketCaller(t *testing.T) {
	for _, tt := range []struct {
		name              string
		panicFirst, fatal bool
	}{
		{"normal", false, false}, {"panic recovery", true, false}, {"fatal disconnect", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetDisconnectHooks(t)
			resetCallerResolver(t)
			want := svc.Caller{Member: "raj", Role: svc.RoleUser}
			SetCallerResolver(func(context.Context, *http.Request, string) svc.Caller { return want })
			type receipt struct {
				order  int
				caller svc.Caller
			}
			seen := make(chan receipt, 3)
			OnDisconnect(func(caller svc.Caller) {
				seen <- receipt{1, caller}
				if tt.panicFirst {
					panic("hook test")
				}
			})
			OnDisconnect(func(caller svc.Caller) { seen <- receipt{2, caller} })
			all := testServices(t)
			ctx, url := testServer(t, all)
			client := dialSocket(t, ctx, url)
			// A receipt ensures the server accepted the socket before closing it.
			client.send(bridge.TypeSub, "", "", "app/crm/state", nil)
			if reply := client.read(); reply.Type != bridge.TypeResult {
				t.Fatal(reply)
			}
			if tt.fatal {
				all.Hub.Publish("app/crm/state", bridge.Errorf(bridge.CodeConflict, "disconnect test"))
				if reply := client.read(); reply.Type != bridge.TypeError {
					t.Fatal(reply)
				}
				if _, _, err := client.conn.Read(ctx); err == nil {
					t.Fatal("socket stayed open")
				}
			} else {
				_ = client.conn.CloseNow()
			}
			for _, order := range []int{1, 2} {
				select {
				case got := <-seen:
					if got.order != order || got.caller != want {
						t.Fatalf("hook=%+v", got)
					}
				case <-ctx.Done():
					t.Fatal("disconnect hooks did not run")
				}
			}
			// Close the test server and join the session cleanup before inspecting repeats.
			// Its cancellation cleanup runs before the hook registry is restored.
			t.Cleanup(func() {
				select {
				case got := <-seen:
					t.Errorf("hook repeated: %+v", got)
				default:
				}
			})
		})
	}
}
