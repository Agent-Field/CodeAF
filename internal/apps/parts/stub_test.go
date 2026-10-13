package parts

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
		{"ListDocs", func() error { _, err := service.ListDocs(ctx, scope, bridge.ListDocsParams{}); return err }},
		{"GetDoc", func() error { _, err := service.GetDoc(ctx, scope, bridge.GetDocParams{}); return err }},
		{"CreateDoc", func() error { _, err := service.CreateDoc(ctx, scope, bridge.CreateDocParams{}); return err }},
		{"Reorder", func() error { _, err := service.Reorder(ctx, scope, bridge.ReorderDocParams{}); return err }},
		{"Get", func() error { _, err := service.Get(ctx, scope, bridge.GetPartParams{}); return err }},
		{"Write", func() error { _, err := service.Write(ctx, scope, bridge.WritePartParams{}); return err }},
		{"Add", func() error { _, err := service.Add(ctx, scope, bridge.AddPartParams{}); return err }},
		{"Remove", func() error { _, err := service.Remove(ctx, scope, bridge.RemovePartParams{}); return err }},
		{"Versions", func() error { _, err := service.Versions(ctx, scope, bridge.ListPartVersionsParams{}); return err }},
		{"Diff", func() error { _, err := service.Diff(ctx, scope, bridge.DiffPartParams{}); return err }},
		{"Restore", func() error { _, err := service.Restore(ctx, scope, bridge.RestorePartParams{}); return err }},
		{"SetShare", func() error { _, err := service.SetShare(ctx, scope, bridge.SetShareParams{}); return err }},
		{"OpenShare", func() error { _, err := service.OpenShare(ctx, scope, bridge.OpenShareParams{}); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wire *bridge.Error
			if err := tt.call(); !errors.As(err, &wire) || wire.Code != bridge.CodeNotBuilt {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
