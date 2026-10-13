package collab

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestStubReturnsNotBuilt(t *testing.T) {
	service := New(svc.Config{}, &svc.Services{})
	ctx := context.Background()
	scope := svc.Scope{App: "slides", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}}
	tests := []struct {
		name string
		call func() error
	}{
		{"Lock", func() error { _, err := service.Lock(ctx, scope, bridge.LockPartParams{}); return err }},
		{"Takeover", func() error { _, err := service.Takeover(ctx, scope, bridge.TakeoverPartParams{}); return err }},
		{"Answer", func() error { _, err := service.Answer(ctx, scope, bridge.AnswerPartParams{}); return err }},
		{"Release", func() error { _, err := service.Release(ctx, scope, bridge.ReleasePartParams{}); return err }},
		{"SetPresence", func() error { _, err := service.SetPresence(ctx, scope, bridge.SetPresenceParams{}); return err }},
		{"ListComments", func() error { _, err := service.ListComments(ctx, scope, bridge.ListCommentsParams{}); return err }},
		{"AddComment", func() error { _, err := service.AddComment(ctx, scope, bridge.AddCommentParams{}); return err }},
		{"ResolveComment", func() error { _, err := service.ResolveComment(ctx, scope, bridge.ResolveCommentParams{}); return err }},
		{"Comment", func() error { _, err := service.Comment(ctx, scope, bridge.SendCommentParams{}); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wire *bridge.Error
			if err := tt.call(); !errors.As(err, &wire) || wire.Code != bridge.CodeNotBuilt {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if service.Holds(scope, "decks/q4", "slides/1.json") {
		t.Fatal("stub claims a lock")
	}
	service.Dropped(svc.Owner)
}
