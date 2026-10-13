package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/shape"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestDraftEnvelopeScopesBeforeCaps(t *testing.T) {
	for _, state := range []bridge.AppState{bridge.StateKept, bridge.StatePreview} {
		t.Run(string(state), func(t *testing.T) {
			all := testServices(t)
			all.Store = stateStore{all.Store, state}
			seen := make(chan svc.Scope, 1)
			all.Caps = draftScopeCaps{Caps: all.Caps, seen: seen}
			scopes := make(chan svc.Scope, 1)
			all.Records = queryRecorder{scope: scopes}
			ctx, url := testServer(t, all)
			client := dialSocket(t, ctx, url)
			client.send(bridge.TypeCall, "crm@draft", bridge.MethodRecordsQuery, "", bridge.QueryParams{Type: "deal"})
			if reply := client.read(); reply.Type != bridge.TypeResult {
				t.Fatal(reply)
			}
			for _, scope := range []svc.Scope{<-seen, <-scopes} {
				if scope.App != "crm" || scope.Data != svc.DataDraft || scope.Caller.Member != svc.Owner {
					t.Fatalf("scope = %+v", scope)
				}
			}
		})
	}
}

type draftScopeCaps struct {
	svc.Caps
	seen chan svc.Scope
}

func (c draftScopeCaps) Allow(ctx context.Context, scope svc.Scope, method string, raw json.RawMessage) error {
	c.seen <- scope
	return c.Caps.Allow(ctx, scope, method, raw)
}

func TestDraftRecordCreateOnlyWritesOverlay(t *testing.T) {
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	all.Store = store.New(cfg, all)
	all.Hub = realtime.New(cfg, all)
	all.Caps = caps.New(cfg, all)
	all.Records = records.New(cfg, all)
	real := ownerScope("crm")
	real.Data = svc.DataReal
	before, err := all.Records.Query(context.Background(), real, bridge.QueryParams{Type: "company"})
	if err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(cfg.Root, "crm", "records.db")
	original, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(cfg.Root, "crm", "drafts", svc.Owner, "overlay.db")
	if err := os.MkdirAll(filepath.Dir(overlay), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, url := testServer(t, all)
	client := dialSocket(t, ctx, url)
	id := client.send(bridge.TypeCall, "crm@draft", bridge.MethodRecordsCreate, "", bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "Draft company"}})
	reply := client.read()
	if reply.ID != id || reply.Type != bridge.TypeResult {
		t.Fatal(reply)
	}
	var created bridge.Record
	if err := json.Unmarshal(reply.Result, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Fields["name"] != "Draft company" {
		t.Fatalf("created = %+v", created)
	}
	after, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("draft write changed records.db")
	}
	for _, tt := range []struct {
		app   string
		total int
	}{{"crm@draft", before.Total + 1}, {"crm", before.Total}} {
		t.Run(tt.app, func(t *testing.T) {
			client.send(bridge.TypeCall, tt.app, bridge.MethodRecordsQuery, "", bridge.QueryParams{Type: "company"})
			reply := client.read()
			if reply.Type != bridge.TypeResult {
				t.Fatal(reply)
			}
			var page bridge.QueryResult
			if err := json.Unmarshal(reply.Result, &page); err != nil {
				t.Fatal(err)
			}
			if page.Total != tt.total {
				t.Fatalf("total = %d, want %d", page.Total, tt.total)
			}
			found := false
			for _, row := range page.Rows {
				if row.ID == created.ID {
					found = true
				}
			}
			if found != (tt.app == "crm@draft") {
				t.Fatalf("draft row visible in %s: %v", tt.app, found)
			}
		})
	}
}

func TestShapeMethodDispatch(t *testing.T) {
	all := testServices(t)
	all.Shape = shape.New(svc.Config{}, all)
	// The wave-1 socket authenticates the owner. The non-editor Ask door is
	// exercised through its typed dispatch entry until spaces supplies callers.
	tests := []struct{ method, params string }{
		{"shape.open", `{}`}, {"shape.get", `{}`},
		{"shape.request", `{"text":"add column"}`},
		{"shape.plan.edit", `{"section":"data","id":"deal","text":"rename"}`},
		{"shape.change.fix", `{"change":"c1","hint":"retry"}`},
		{"shape.change.cancel", `{"change":"c1"}`},
		{"shape.answer", `{"change":"c1","option":"drop"}`},
		{"shape.diff", `{}`}, {"shape.preview", `{}`},
		{"shape.apply", `{"base":1}`}, {"shape.discard", `{}`},
		{"shape.replay.dismiss", `{}`}, {"shape.ask", `{"text":"add column"}`},
		{"versions.list", `{}`}, {"versions.diff", `{"version":1}`},
		{"versions.restore", `{"version":1,"confirm":true}`}, {"apps.health", `{}`},
	}
	if len(tests) != len(shapeMethods) {
		t.Fatalf("shape dispatch coverage=%d methods=%d", len(tests), len(shapeMethods))
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			entry, ok := lookupShapeMethod(tt.method)
			if !ok || entry.global {
				t.Fatal("missing app-scoped dispatch entry")
			}
			scope := ownerScope("crm")
			if tt.method == bridge.MethodShapeAsk {
				scope.Caller.Role = svc.RoleUser
			}
			raw := json.RawMessage(tt.params)
			if err := all.Caps.Allow(context.Background(), scope, tt.method, raw); err != nil {
				t.Fatal(err)
			}
			result, err := entry.run(context.Background(), all, scope, raw)
			if tt.method == bridge.MethodVersionsList {
				if err != nil {
					t.Fatal(err)
				}
				rows := result.(bridge.VersionsListResult).Versions
				if len(rows) != 1 || rows[0].N != 1 || !rows[0].Current || !rows[0].Restorable || rows[0].At == "" || rows[0].Requests == nil {
					t.Fatalf("versions = %+v", rows)
				}
				return
			}
			typed, ok := err.(*bridge.Error)
			if !ok || typed.Code != bridge.CodeNotBuilt || typed.Message != "shape is not built yet" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestShapeSocketDispatchAndTopics(t *testing.T) {
	all := testServices(t)
	all.Shape = shape.New(svc.Config{}, all)
	ctx, url := testServer(t, all)
	client := dialSocket(t, ctx, url)
	for _, tt := range []struct {
		method string
		code   bridge.ErrorCode
	}{
		{bridge.MethodShapeOpen, bridge.CodeNotBuilt},
		{bridge.MethodVersionsList, ""},
		{bridge.MethodAgentSend, bridge.CodeDenied},
	} {
		client.send(bridge.TypeCall, "crm@draft", tt.method, "", bridge.Empty{})
		reply := client.read()
		if tt.code == "" {
			if reply.Type != bridge.TypeResult {
				t.Fatal(reply)
			}
			var result bridge.VersionsListResult
			if err := json.Unmarshal(reply.Result, &result); err != nil || len(result.Versions) != 1 || result.Versions[0].N != 1 {
				t.Fatalf("versions = %+v, %v", result, err)
			}
		} else if reply.Error == nil || reply.Error.Code != tt.code {
			t.Fatal(reply)
		}
	}
	for _, tt := range []struct {
		topic string
		code  bridge.ErrorCode
	}{
		{bridge.TopicShape("crm", svc.Owner), ""},
		{bridge.TopicShape("crm", "raj"), bridge.CodeDenied},
		{bridge.TopicVersion("crm"), ""}, {bridge.TopicHealth("crm"), ""},
		{"app/crm/*", bridge.CodeDenied},
	} {
		client.send(bridge.TypeSub, "", "", tt.topic, nil)
		reply := client.read()
		if tt.code == "" {
			if reply.Type != bridge.TypeResult {
				t.Fatal(reply)
			}
		} else if reply.Error == nil || reply.Error.Code != tt.code {
			t.Fatal(reply)
		}
	}
	event := bridge.ShapeEvent{Kind: "draft", At: "now"}
	all.Hub.Publish(bridge.TopicShape("crm", svc.Owner), event)
	reply := client.read()
	if reply.Type != bridge.TypeEvent || reply.Topic != bridge.TopicShape("crm", svc.Owner) {
		t.Fatal(reply)
	}
}
