package custom_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/coder/websocket"
)

type wireStore struct {
	svc.Store
}

func (wireStore) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	if app != "crm" {
		return bridge.AppDetail{}, bridge.Errorf(bridge.CodeNotFound, "no app")
	}
	return bridge.AppDetail{
		AppSummary: bridge.AppSummary{ID: app, Version: 1, State: bridge.StateKept},
		Files: map[string]string{
			"custom/chart.tsx":  `import React from "react"; export default () => <div>Deals</div>;`,
			"custom/unsafe.tsx": `fetch('/records');`,
		},
	}, nil
}

func TestCustomBundleThroughGuardedWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	all := &svc.Services{Store: wireStore{}}
	cfg := svc.Config{Root: t.TempDir()}
	all.Caps = gateway.CrewsCaps(caps.New(cfg, all), all)
	all.Crews.Custom = custom.New(cfg, all)
	const token = "custom-test-launch-token-0123456789abcdef"
	api := appapi.New(token, appapi.Options{Context: ctx})
	t.Cleanup(api.Close)
	api.Handle(bridge.Path, gateway.Handler(ctx, all))
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+bridge.Path,
		&websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + token}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socket.CloseNow() })
	for _, tc := range []struct {
		file string
		code bridge.ErrorCode
	}{
		{"custom/chart.tsx", ""},
		{"custom/missing.tsx", bridge.CodeNotFound},
		{"custom/unsafe.tsx", bridge.CodeCheckFailed},
		{"../chart.tsx", bridge.CodeInvalid},
	} {
		t.Run(tc.file, func(t *testing.T) {
			params, _ := json.Marshal(bridge.CustomBundleParams{File: tc.file})
			request, _ := json.Marshal(bridge.Envelope{ID: tc.file, Type: bridge.TypeCall, App: "crm", Method: bridge.MethodCustomBundle, Params: params})
			if err := socket.Write(ctx, websocket.MessageText, request); err != nil {
				t.Fatal(err)
			}
			_, raw, err := socket.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var reply bridge.Envelope
			if err := json.Unmarshal(raw, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID != tc.file {
				t.Fatalf("wrong receipt: %+v", reply)
			}
			if tc.code != "" {
				if reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != tc.code {
					t.Fatalf("want %s, got %+v", tc.code, reply)
				}
				return
			}
			var result bridge.CustomBundleResult
			if err := json.Unmarshal(reply.Result, &result); err != nil || reply.Type != bridge.TypeResult || reply.Error != nil {
				t.Fatalf("bundle receipt=%+v, err=%v", reply, err)
			}
			if len(result.Hash) != 64 || !strings.Contains(result.JS, "createElement") || !strings.Contains(result.JS, `from "react"`) {
				t.Fatalf("bundle=%+v", result)
			}
		})
	}
}
