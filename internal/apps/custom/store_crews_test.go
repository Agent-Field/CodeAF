package custom_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const persistedSource = `import React from "react"; export default () => <div>Deals</div>;`

func TestBuilderCustomFilesSurviveKeepAndReopen(t *testing.T) {
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	s := store.NewWithOptions(cfg, all, store.Options{WithoutDemo: true})
	all.Store = s
	app, err := s.Create(ctx, svc.Caller{Member: "me", Role: svc.RoleEditor}, "CRM", "Custom chart")
	if err != nil {
		t.Fatal(err)
	}
	files, _ := spec.GoldenCRM()
	const file = "custom/chart.tsx"
	files[file] = []byte(persistedSource)
	if err := s.WriteFiles(ctx, app.ID, files); err != nil {
		t.Fatal(err)
	}
	assertSource := func(t *testing.T) {
		t.Helper()
		detail, err := all.Store.Get(ctx, app.ID)
		if err != nil || detail.Files[file] != persistedSource {
			t.Fatalf("custom source=%q err=%v", detail.Files[file], err)
		}
		result, err := custom.New(cfg, all).Bundle(ctx, svc.Scope{App: app.ID}, bridge.CustomBundleParams{File: file})
		if err != nil || len(result.Hash) != 64 || !strings.Contains(result.JS, "createElement") {
			t.Fatalf("bundle=%+v err=%v", result, err)
		}
	}
	t.Run("draft", assertSource)
	for _, state := range []bridge.AppState{bridge.StateBuilding, bridge.StatePreview} {
		if err := s.SetState(ctx, app.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Keep(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("kept", assertSource)
	all.Store = store.NewWithOptions(cfg, all, store.Options{WithoutDemo: true})
	t.Run("reopened", assertSource)
}

func TestBuilderCustomPathAllowance(t *testing.T) {
	for _, tc := range []struct {
		file string
		good bool
	}{
		{"custom/chart.tsx", true},
		{"custom/deal-card.tsx", true},
		{"custom/chart.jsx", false},
		{"custom/chart.ts", false},
		{"custom/.tsx", false},
		{"custom/nested/chart.tsx", false},
		{"custom/../chart.tsx", false},
		{"custom/../custom/chart.tsx", false},
		{"custom/chart\\other.tsx", false},
		{"custom/chart\x00.tsx", false},
		{"/custom/chart.tsx", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			ctx := context.Background()
			s := store.NewWithOptions(svc.Config{Root: t.TempDir()}, &svc.Services{}, store.Options{WithoutDemo: true})
			app, err := s.Create(ctx, svc.Caller{Member: "me", Role: svc.RoleEditor}, "CRM", "Custom chart")
			if err != nil {
				t.Fatal(err)
			}
			err = s.WriteFiles(ctx, app.ID, spec.Files{tc.file: []byte(persistedSource)})
			if tc.good {
				if err != nil {
					t.Fatal(err)
				}
				detail, err := s.Get(ctx, app.ID)
				if err != nil || detail.Files[tc.file] != persistedSource {
					t.Fatalf("files=%v err=%v", detail.Files, err)
				}
				return
			}
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != bridge.CodeInvalid {
				t.Fatalf("invalid path error=%v", err)
			}
		})
	}
}
