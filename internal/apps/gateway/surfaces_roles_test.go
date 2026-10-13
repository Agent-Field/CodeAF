package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestSurfaceRoles(t *testing.T) {
	tests := []struct {
		method string
		viewer bool
	}{
		{bridge.MethodDocsList, true}, {bridge.MethodDocsGet, true}, {bridge.MethodDocsCreate, false}, {bridge.MethodDocsReorder, false},
		{bridge.MethodPartsGet, true}, {bridge.MethodPartsLock, false}, {bridge.MethodPartsTakeover, false}, {bridge.MethodPartsAnswer, false},
		{bridge.MethodPartsRelease, false}, {bridge.MethodPartsWrite, false}, {bridge.MethodPartsAdd, false}, {bridge.MethodPartsRemove, false},
		{bridge.MethodPartsVersions, true}, {bridge.MethodPartsDiff, true}, {bridge.MethodPartsRestore, false}, {bridge.MethodPresenceSet, true},
		{bridge.MethodCommentsList, true}, {bridge.MethodCommentsAdd, false}, {bridge.MethodCommentsResolve, false}, {bridge.MethodCommentsSend, false},
		{bridge.MethodShareSet, false}, {bridge.MethodShareOpen, true},
	}
	if len(tests) != len(surfaceMethods) {
		t.Fatalf("role coverage=%d rules=%d", len(tests), len(surfaceMethods))
	}
	service := caps.New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, tt := range tests {
		for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer, "unknown"} {
			t.Run(tt.method+"/"+string(role), func(t *testing.T) {
				scope := svc.Scope{App: "slides", Caller: svc.Caller{Member: "mona", Role: role}, Data: svc.DataSample}
				err := service.Allow(context.Background(), scope, tt.method, json.RawMessage(`{}`))
				allowed := role == svc.RoleEditor || role == svc.RoleUser || (role == svc.RoleViewer && tt.viewer)
				if allowed && err != nil {
					t.Fatal(err)
				}
				if !allowed && errorCode(err) != bridge.CodeDenied {
					t.Fatalf("error=%v", err)
				}
			})
		}
	}
}

func TestSurfaceCallGuards(t *testing.T) {
	scope := svc.Scope{App: "slides", Caller: svc.Caller{Member: "mona", Role: svc.RoleEditor}}
	tests := []struct {
		name  string
		scope svc.Scope
		raw   string
		code  bridge.ErrorCode
	}{
		{"no member", svc.Scope{App: "slides", Caller: svc.Caller{Role: svc.RoleEditor}}, `{}`, bridge.CodeDenied},
		{"no app", svc.Scope{Caller: scope.Caller}, `{}`, bridge.CodeInvalid},
		{"array", scope, `[]`, bridge.CodeInvalid},
		{"malformed", scope, `{`, bridge.CodeInvalid},
		{"scalar", scope, `true`, bridge.CodeInvalid},
	}
	service := caps.New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.Allow(context.Background(), tt.scope, bridge.MethodDocsGet, json.RawMessage(tt.raw))
			if errorCode(err) != tt.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Allow(ctx, scope, bridge.MethodDocsGet, nil); err != context.Canceled {
		t.Fatalf("canceled context=%v", err)
	}
}

func TestSurfaceDocumentTopics(t *testing.T) {
	tests := []struct {
		topic string
		code  bridge.ErrorCode
	}{
		{bridge.TopicDoc("slides", "decks/q4"), "allowed"},
		{bridge.TopicDoc("other", "decks/q4"), bridge.CodeInvalid},
		{bridge.TopicDoc("slides", ""), bridge.CodeInvalid},
		{bridge.TopicDoc("slides", "decks/../q4"), bridge.CodeInvalid},
		{bridge.TopicDoc("slides", "../q4"), bridge.CodeInvalid},
		{bridge.TopicDoc("slides", "*"), bridge.CodeInvalid},
		{bridge.TopicDoc("slides", "decks\\q4"), bridge.CodeInvalid},
	}
	service := caps.New(svc.Config{Root: t.TempDir()}, &svc.Services{})
	for _, tt := range tests {
		for _, role := range []svc.Role{svc.RoleEditor, svc.RoleUser, svc.RoleViewer} {
			for _, method := range []string{"sub", "unsub"} {
				t.Run(tt.topic+"/"+string(role)+"/"+method, func(t *testing.T) {
					raw, _ := json.Marshal(map[string]string{"topic": tt.topic})
					scope := svc.Scope{App: "slides", Caller: svc.Caller{Member: "mona", Role: role}}
					if err := service.Allow(context.Background(), scope, method, raw); errorCode(err) != tt.code {
						t.Fatalf("error=%v", err)
					}
				})
			}
		}
	}
}

func errorCode(err error) bridge.ErrorCode {
	if err == nil {
		return "allowed"
	}
	var wire *bridge.Error
	if errors.As(err, &wire) {
		return wire.Code
	}
	return bridge.CodeInternal
}
