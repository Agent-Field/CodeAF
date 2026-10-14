package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestSocketSetTintReturnsSummaryAndStateEvent(t *testing.T) {
	all := testServices(t)
	ctx, url := testServer(t, all)
	c := dialSocket(t, ctx, url)
	topic := bridge.TopicState("crm")
	c.send(bridge.TypeSub, "", "", topic, nil)
	if reply := c.read(); reply.Type != bridge.TypeResult {
		t.Fatal(reply)
	}
	id := c.send(bridge.TypeCall, "", bridge.MethodAppSetTint, "", bridge.SetTintParams{App: "crm", Tint: "tide"})
	var result, event bridge.AppSummary
	for range 2 {
		reply := c.read()
		switch reply.Type {
		case bridge.TypeResult:
			if reply.ID != id {
				t.Fatal(reply)
			}
			if err := json.Unmarshal(reply.Result, &result); err != nil {
				t.Fatal(err)
			}
		case bridge.TypeEvent:
			if reply.Topic != topic || reply.Seq != 1 {
				t.Fatal(reply)
			}
			if err := json.Unmarshal(reply.Event, &event); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal(reply)
		}
	}
	if result.ID != "crm" || result.Tint != "tide" || result.State != bridge.StateKept || result != event {
		t.Fatalf("result=%+v event=%+v", result, event)
	}
	c.send(bridge.TypeCall, "crm", bridge.MethodAppsGet, "", nil)
	reply := c.read()
	var detail bridge.AppDetail
	if err := json.Unmarshal(reply.Result, &detail); err != nil || detail.Tint != "tide" {
		t.Fatalf("detail=%+v error=%v reply=%+v", detail, err, reply)
	}
	var app spec.App
	if err := json.Unmarshal([]byte(detail.Files[spec.AppFile]), &app); err != nil || app.Tint != "tide" {
		t.Fatalf("app.json tint=%q error=%v", app.Tint, err)
	}
}

func TestSocketSetTintErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		caller svc.Caller
		params bridge.SetTintParams
		code   bridge.ErrorCode
	}{
		{"invalid", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, bridge.SetTintParams{App: "crm", Tint: "blue"}, bridge.CodeInvalid},
		{"non-owner", svc.Caller{Member: "raj", Role: svc.RoleEditor}, bridge.SetTintParams{App: "crm", Tint: "rose"}, bridge.CodeDenied},
		{"agent", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor, Agent: true}, bridge.SetTintParams{App: "crm", Tint: "rose"}, bridge.CodeDenied},
		{"unknown", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, bridge.SetTintParams{App: "missing", Tint: "rose"}, bridge.CodeNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCallerResolver(t)
			SetCallerResolver(func(context.Context, *http.Request, string) svc.Caller { return tt.caller })
			all := testServices(t)
			ctx, url := testServer(t, all)
			c := dialSocket(t, ctx, url)
			id := c.send(bridge.TypeCall, "", bridge.MethodAppSetTint, "", tt.params)
			reply := c.read()
			if reply.ID != id || reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != tt.code {
				t.Fatal(reply)
			}
			if tt.code == bridge.CodeInvalid && reply.Error.Message != "tint must be one of iris, sage, sand, tide, rose, graphite" {
				t.Fatal(reply.Error)
			}
		})
	}
}
